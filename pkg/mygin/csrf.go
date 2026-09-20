package mygin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

const (
	ctxKeyCSRFToken = "csrf_token"
	csrfTokenAge    = 60 * 60 * 24
)

var csrfSecret = mustRandomBytes(32)

func CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := csrfSessionID(c)
		token := currentCSRFToken(c, sessionID)
		c.Set(ctxKeyCSRFToken, token)
		setCSRFCookie(c, token)

		if !requiresCSRF(c) || c.GetHeader("Authorization") != "" {
			return
		}

		submittedToken := c.GetHeader("X-CSRF-Token")
		if submittedToken == "" {
			submittedToken = c.GetHeader("X-XSRF-Token")
		}
		if submittedToken == "" {
			submittedToken = c.PostForm("_csrf")
		}
		if !validCSRFPair(submittedToken, token, sessionID) {
			if strings.HasPrefix(c.Request.URL.Path, "/api/v2/") {
				trace := make([]byte, 8)
				_, _ = rand.Read(trace)
				c.Header("Content-Type", "application/problem+json")
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"type": "https://santaizi.dev/problems/csrf_invalid", "title": "Forbidden", "status": http.StatusForbidden, "code": "csrf_invalid", "detail": "CSRF token invalid", "trace_id": base64.RawURLEncoding.EncodeToString(trace)})
				return
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    http.StatusForbidden,
				"message": "CSRF token invalid",
			})
			return
		}
	}
}

func CSRFToken(c *gin.Context) string {
	if token, ok := c.Get(ctxKeyCSRFToken); ok {
		if tokenString, ok := token.(string); ok {
			return tokenString
		}
	}
	token := currentCSRFToken(c, csrfSessionID(c))
	c.Set(ctxKeyCSRFToken, token)
	setCSRFCookie(c, token)
	return token
}

func currentCSRFToken(c *gin.Context, sessionID string) string {
	token, err := c.Cookie(csrfCookieName())
	if err == nil && validCSRFToken(token, sessionID) {
		return token
	}
	return newCSRFToken(sessionID)
}

func newCSRFToken(sessionID string) string {
	nonce := mustRandomBytes(32)
	encodedNonce := base64.RawURLEncoding.EncodeToString(nonce)
	return encodedNonce + "." + csrfSignature(encodedNonce, sessionID)
}

func validCSRFPair(submittedToken, cookieToken, sessionID string) bool {
	if submittedToken == "" || cookieToken == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(submittedToken), []byte(cookieToken)) != 1 {
		return false
	}
	return validCSRFToken(cookieToken, sessionID)
}

func validCSRFToken(token, sessionID string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	if _, err := base64.RawURLEncoding.DecodeString(parts[0]); err != nil {
		return false
	}
	expected := csrfSignature(parts[0], sessionID)
	return subtle.ConstantTimeCompare([]byte(parts[1]), []byte(expected)) == 1
}

func csrfSignature(nonce, sessionID string) string {
	mac := hmac.New(sha256.New, csrfSecret)
	mac.Write([]byte(sessionID))
	mac.Write([]byte{0})
	mac.Write([]byte(nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func csrfSessionID(c *gin.Context) string {
	authToken, _ := c.Cookie(singleton.Conf.Site.CookieName)
	viewPasswordToken, _ := c.Cookie(singleton.Conf.Site.CookieName + "-vp")
	return authToken + "|" + viewPasswordToken
}

func csrfCookieName() string {
	return singleton.Conf.Site.CookieName + "-csrf"
}

func setCSRFCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     csrfCookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   csrfTokenAge,
		Expires:  time.Now().Add(time.Second * csrfTokenAge),
		Secure:   requestIsHTTPS(c),
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

func requestIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	if strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Ssl"), "on")
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func requiresCSRF(c *gin.Context) bool {
	if isSafeMethod(c.Request.Method) {
		return false
	}
	if c.Request.URL.Path == "/api/v2/bot/telegram/webhook" {
		return false
	}
	return true
}

func mustRandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
