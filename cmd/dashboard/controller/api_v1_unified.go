package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

// registerUnified 在 /api/v1 下注册统一前端模型接口。
// pr 为公开路由组，tr 为需要登录或 API Token 的路由组。
func (v *apiV1) registerUnified(pr, tr gin.IRouter) {
	pr.GET("/server", v.unifiedServerList)
	pr.GET("/server/:id", v.unifiedServerByID)
	pr.GET("/:model", v.unifiedModelList)
}

// unifiedModelList 统一模型列表入口
// 支持 notification / ddns / nat / alert-rule / user / transfer
func (v *apiV1) unifiedModelList(c *gin.Context) {
	withToken := isTokenAuthorized(c)
	name := strings.ToLower(c.Param("model"))

	// 部分模型必须携带 Token 才能访问
	tokenRequiredModels := map[string]bool{
		"api-token":      true,
		"server-runtime": true,
	}
	if tokenRequiredModels[name] && !withToken {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusForbidden,
			Message: "访问此接口需要认证",
		})
		return
	}

	var data any
	var err error
	switch name {
	case "notification":
		data, err = unifiedListNotification(withToken)
	case "ddns":
		data, err = unifiedListDDNS(withToken)
	case "nat":
		data, err = unifiedListNAT(withToken)
	case "alert-rule":
		data, err = unifiedListAlertRule(withToken)
	case "user":
		data, err = unifiedListUser(withToken)
	case "transfer":
		data, err = unifiedListTransfer(withToken)
	case "setting":
		data, err = unifiedListSetting(withToken)
	case "api-token":
		u, _ := c.Get(model.CtxKeyAuthorizedUser)
		data, err = unifiedListApiToken(u)
	case "server-runtime":
		data, err = unifiedListServerRuntime()
	default:
		c.JSON(http.StatusNotFound, model.Response{
			Code:    http.StatusNotFound,
			Message: "未定义的模型",
		})
		return
	}

	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("查询失败：%s", err),
		})
		return
	}
	c.JSON(http.StatusOK, model.Response{
		Code:   http.StatusOK,
		Result: data,
	})
}

// ---------------------- Server ----------------------

type unifiedServerListItem struct {
	ID           uint64 `json:"id"`
	Name         string `json:"name"`
	Tag          string `json:"tag"`
	PublicNote   string `json:"public_note,omitempty"`
	DisplayIndex int    `json:"display_index"`
	HideForGuest bool   `json:"hide_for_guest"`
	LastActive   int64  `json:"last_active"`
	Online       bool   `json:"online"`
}

type unifiedServerDetail struct {
	unifiedServerListItem
	Host  *model.Host      `json:"host,omitempty"`
	State *model.HostState `json:"state,omitempty"`

	// 仅携带 Token 时返回
	Secret          string `json:"secret,omitempty"`
	Note            string `json:"note,omitempty"`
	IP              string `json:"ip,omitempty"`
	DDNSProfilesRaw string `json:"ddns_profiles_raw,omitempty"`
}

func (v *apiV1) unifiedServerList(c *gin.Context) {
	visible := isVisible(c)
	withToken := isTokenAuthorized(c)

	singleton.SortedServerLock.RLock()
	var list []*model.Server
	if visible {
		list = singleton.SortedServerList
	} else {
		list = singleton.SortedServerListForGuest
	}
	singleton.SortedServerLock.RUnlock()

	items := make([]*unifiedServerListItem, 0, len(list))
	for _, s := range list {
		items = append(items, toUnifiedServerListItem(s, withToken))
	}
	c.JSON(http.StatusOK, model.Response{
		Code:   http.StatusOK,
		Result: items,
	})
}

func (v *apiV1) unifiedServerByID(c *gin.Context) {
	visible := isVisible(c)
	withToken := isTokenAuthorized(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "id 参数错误",
		})
		return
	}

	singleton.ServerLock.RLock()
	s := singleton.ServerList[id]
	singleton.ServerLock.RUnlock()

	if s == nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusNotFound,
			Message: "服务器不存在",
		})
		return
	}
	if !visible && s.HideForGuest {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusForbidden,
			Message: "需要认证",
		})
		return
	}

	c.JSON(http.StatusOK, model.Response{
		Code:   http.StatusOK,
		Result: toUnifiedServerDetail(s, withToken),
	})
}

func toUnifiedServerListItem(s *model.Server, withToken bool) *unifiedServerListItem {
	online := s.LastActive.After(time.Now().Add(-time.Second * 30))
	var binding model.ServerNodeBinding
	if singleton.DB.First(&binding, "server_id = ? AND current = ?", s.ID, true).Error == nil {
		var runtime model.ServerRuntime
		recovering := singleton.DB.First(&runtime, "server_id = ?", s.ID).Error == nil && runtime.Status == model.ServerRuntimeStatusRecovering
		bucket, found, err := model.LatestClosedAvailabilityBucket(singleton.DB, binding.NodeUUID, time.Now())
		switch {
		case err == nil && found:
			online = bucket.ConnectivityState == model.ConnectivityFull || bucket.ConnectivityState == model.ConnectivityPartial
		case recovering:
			online = false
		}
	}
	return &unifiedServerListItem{
		ID:           s.ID,
		Name:         s.Name,
		Tag:          s.Tag,
		PublicNote:   s.PublicNote,
		DisplayIndex: s.DisplayIndex,
		HideForGuest: s.HideForGuest,
		LastActive:   s.LastActive.Unix(),
		Online:       online,
	}
}

func toUnifiedServerDetail(s *model.Server, withToken bool) *unifiedServerDetail {
	d := &unifiedServerDetail{
		unifiedServerListItem: *toUnifiedServerListItem(s, withToken),
		Host:                  s.Host,
		State:                 s.State,
	}
	if withToken {
		d.Secret = s.Secret
		d.Note = s.Note
		d.DDNSProfilesRaw = s.DDNSProfilesRaw
		if s.Host != nil {
			d.IP = s.Host.IP
		}
	}
	return d
}

// Notification
type unifiedPublicNotification struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	Tag  string `json:"tag"`
}

type unifiedPrivateNotification struct {
	unifiedPublicNotification
	URL           string `json:"url"`
	RequestMethod int    `json:"request_method"`
	RequestType   int    `json:"request_type"`
	RequestHeader string `json:"request_header"`
	RequestBody   string `json:"request_body"`
	VerifySSL     *bool  `json:"verify_ssl,omitempty"`
}

func unifiedListNotification(withToken bool) (any, error) {
	var items []model.Notification
	if err := singleton.DB.Find(&items).Error; err != nil {
		return nil, err
	}
	if withToken {
		res := make([]unifiedPrivateNotification, len(items))
		for i, n := range items {
			res[i] = unifiedPrivateNotification{
				unifiedPublicNotification: unifiedPublicNotification{ID: n.ID, Name: n.Name, Tag: n.Tag},
				URL:                       n.URL,
				RequestMethod:             n.RequestMethod,
				RequestType:               n.RequestType,
				RequestHeader:             n.RequestHeader,
				RequestBody:               n.RequestBody,
				VerifySSL:                 n.VerifySSL,
			}
		}
		return res, nil
	}
	res := make([]unifiedPublicNotification, len(items))
	for i, n := range items {
		res[i] = unifiedPublicNotification{ID: n.ID, Name: n.Name, Tag: n.Tag}
	}
	return res, nil
}

// DDNS
type unifiedPublicDDNS struct {
	ID         uint64   `json:"id"`
	Name       string   `json:"name"`
	Provider   uint8    `json:"provider"`
	EnableIPv4 *bool    `json:"enable_ipv4,omitempty"`
	EnableIPv6 *bool    `json:"enable_ipv6,omitempty"`
	MaxRetries uint64   `json:"max_retries"`
	Domains    []string `json:"domains"`
}

type unifiedPrivateDDNS struct {
	unifiedPublicDDNS
	AccessID           string `json:"access_id"`
	AccessSecret       string `json:"access_secret"`
	WebhookURL         string `json:"webhook_url"`
	WebhookMethod      uint8  `json:"webhook_method"`
	WebhookRequestType uint8  `json:"webhook_request_type"`
	WebhookRequestBody string `json:"webhook_request_body"`
	WebhookHeaders     string `json:"webhook_headers"`
}

func unifiedListDDNS(withToken bool) (any, error) {
	var items []model.DDNSProfile
	if err := singleton.DB.Find(&items).Error; err != nil {
		return nil, err
	}
	if withToken {
		res := make([]unifiedPrivateDDNS, len(items))
		for i, d := range items {
			res[i] = unifiedPrivateDDNS{
				unifiedPublicDDNS: unifiedPublicDDNS{
					ID:         d.ID,
					Name:       d.Name,
					Provider:   d.Provider,
					EnableIPv4: d.EnableIPv4,
					EnableIPv6: d.EnableIPv6,
					MaxRetries: d.MaxRetries,
					Domains:    d.Domains,
				},
				AccessID:           d.AccessID,
				AccessSecret:       d.AccessSecret,
				WebhookURL:         d.WebhookURL,
				WebhookMethod:      d.WebhookMethod,
				WebhookRequestType: d.WebhookRequestType,
				WebhookRequestBody: d.WebhookRequestBody,
				WebhookHeaders:     d.WebhookHeaders,
			}
		}
		return res, nil
	}
	res := make([]unifiedPublicDDNS, len(items))
	for i, d := range items {
		res[i] = unifiedPublicDDNS{
			ID:         d.ID,
			Name:       d.Name,
			Provider:   d.Provider,
			EnableIPv4: d.EnableIPv4,
			EnableIPv6: d.EnableIPv6,
			MaxRetries: d.MaxRetries,
			Domains:    d.Domains,
		}
	}
	return res, nil
}

// NAT
func unifiedListNAT(withToken bool) (any, error) {
	var items []model.NAT
	if err := singleton.DB.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// AlertRule
type unifiedPublicAlertRule struct {
	ID              uint64 `json:"id"`
	Name            string `json:"name"`
	Enable          bool   `json:"enable"`
	TriggerMode     int    `json:"trigger_mode"`
	NotificationTag string `json:"notification_tag"`
	Summary         string `json:"summary"`
}

type unifiedPrivateAlertRule struct {
	unifiedPublicAlertRule
	Rules []model.Rule `json:"rules"`
}

func unifiedListAlertRule(withToken bool) (any, error) {
	var items []model.AlertRule
	if err := singleton.DB.Find(&items).Error; err != nil {
		return nil, err
	}
	if withToken {
		res := make([]unifiedPrivateAlertRule, len(items))
		for i, a := range items {
			res[i] = unifiedPrivateAlertRule{
				unifiedPublicAlertRule: unifiedPublicAlertRule{
					ID:              a.ID,
					Name:            a.Name,
					Enable:          a.Enabled(),
					TriggerMode:     a.TriggerMode,
					NotificationTag: a.NotificationTag,
					Summary:         a.RulesSummary(),
				},
				Rules: a.Rules,
			}
		}
		return res, nil
	}
	res := make([]unifiedPublicAlertRule, len(items))
	for i, a := range items {
		res[i] = unifiedPublicAlertRule{
			ID:              a.ID,
			Name:            a.Name,
			Enable:          a.Enabled(),
			TriggerMode:     a.TriggerMode,
			NotificationTag: a.NotificationTag,
			Summary:         a.RulesSummary(),
		}
	}
	return res, nil
}

// User
type unifiedPrivateUser struct {
	model.User
	Token string `json:"token,omitempty"`
}

func unifiedListUser(withToken bool) (any, error) {
	var items []model.User
	if err := singleton.DB.Find(&items).Error; err != nil {
		return nil, err
	}
	if withToken {
		res := make([]unifiedPrivateUser, len(items))
		for i, u := range items {
			res[i] = unifiedPrivateUser{User: u, Token: u.Token}
		}
		return res, nil
	}
	return items, nil
}

// Transfer
func unifiedListTransfer(withToken bool) (any, error) {
	var items []model.Transfer
	if err := singleton.DB.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// Setting / Config
func unifiedListSetting(withToken bool) (any, error) {
	if withToken {
		return singleton.Conf, nil
	}
	return model.PublicConfig{
		Site: model.PublicSiteConfig{
			Brand:               singleton.Conf.Site.Brand,
			Theme:               singleton.Conf.Site.Theme,
			DashboardTheme:      singleton.Conf.Site.DashboardTheme,
			CustomCode:          singleton.Conf.Site.CustomCode,
			CustomCodeDashboard: singleton.Conf.Site.CustomCodeDashboard,
		},
		Language:                        singleton.Conf.Language,
		MaxTCPPingValue:                 singleton.Conf.MaxTCPPingValue,
		DisableSwitchTemplateInFrontend: singleton.Conf.DisableSwitchTemplateInFrontend,
		ShowAvailabilityToGuest:         singleton.Conf.ShowAvailabilityToGuest,
	}, nil
}

// ApiToken
func unifiedListApiToken(u any) (any, error) {
	user, ok := u.(*model.User)
	if !ok || user == nil {
		return nil, fmt.Errorf("无法获取当前用户")
	}
	var tokens []model.ApiToken
	if err := singleton.DB.Where("user_id = ?", user.ID).Find(&tokens).Error; err != nil {
		return nil, err
	}
	return tokens, nil
}

// ServerRuntime
func unifiedListServerRuntime() (any, error) {
	var items []model.ServerRuntime
	if err := singleton.DB.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
