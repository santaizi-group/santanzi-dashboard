package controller

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	"golang.org/x/sync/singleflight"
)

type commonPage struct {
	r            *gin.Engine
	requestGroup singleflight.Group
}

var upgrader = websocket.Upgrader{ReadBufferSize: 32768, WriteBufferSize: 32768}

// getServerStat 公开 WS 载荷必须与 publicServerSnapshot（HTTP）同形，禁止直接 Marshal model.Server。
func (cp *commonPage) getServerStat(c *gin.Context, withPublicNote bool) ([]byte, error) {
	_, member := c.Get(model.CtxKeyAuthorizedUser)
	_, verified := c.Get(model.CtxKeyViewPasswordVerified)
	authorized := member || verified
	isAPI := isAPITokenRequest(c)
	value, err, _ := cp.requestGroup.Do(fmt.Sprintf("serverStats::%t::%t::%t", authorized, withPublicNote, isAPI), func() (any, error) {
		servers := publicServerSnapshot(c)
		if !withPublicNote {
			for _, row := range servers {
				delete(row, "public_note")
			}
		}
		return utils.Json.Marshal(gin.H{"now": time.Now().UnixMilli(), "servers": servers})
	})
	if err != nil {
		return nil, err
	}
	return value.([]byte), nil
}

func (cp *commonPage) ws(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	const pongWait = 60 * time.Second
	const writeWait = 10 * time.Second
	conn.SetReadLimit(512)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	go func() {
		defer conn.Close()
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	count := 0
	for {
		stat, err := cp.getServerStat(c, true)
		if err != nil {
			<-ticker.C
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.TextMessage, stat); err != nil {
			return
		}
		count++
		if count%4 == 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
		<-ticker.C
	}
}
