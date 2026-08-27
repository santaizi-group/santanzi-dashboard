package controller

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/copier"
	"golang.org/x/net/idna"
	"gorm.io/gorm"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/mygin"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

type memberAPI struct {
	r gin.IRouter
}

func (ma *memberAPI) serve() {
	mr := ma.r.Group("")
	mr.Use(mygin.Authorize(mygin.AuthorizeOption{
		MemberOnly: true,
		IsPage:     false,
		Msg:        "访问此接口需要登录",
		Btn:        "点此登录",
		Redirect:   "/login",
	}))

	mr.GET("/search-server", ma.searchServer)
	mr.GET("/search-ddns", ma.searchDDNS)
	mr.POST("/server", ma.addOrEditServer)
	mr.POST("/server/:id/reset-secret", ma.resetServerSecret)
	mr.POST("/server/:id/reset-availability", ma.resetServerAvailability)
	mr.POST("/monitor", ma.addOrEditMonitor)
	mr.POST("/batch-update-server-group", ma.batchUpdateServerGroup)
	mr.POST("/batch-delete-server", ma.batchDeleteServer)
	mr.POST("/notification", ma.addOrEditNotification)
	mr.POST("/ddns", ma.addOrEditDDNS)
	mr.POST("/nat", ma.addOrEditNAT)
	mr.POST("/alert-rule", ma.addOrEditAlertRule)
	mr.POST("/setting", ma.updateSetting)
	mr.DELETE("/:model/:id", ma.delete)
	mr.POST("/logout", ma.logout)
	// 服务器离线历史
	mr.GET("/offline-history", ma.offlineHistory)
	mr.GET("/offline-history/summary", ma.offlineSummary)
	mr.POST("/offline-history/cleanup", ma.cleanupOfflineHistory)
	mr.DELETE("/offline-history/:id", ma.deleteOfflineHistory)

	// API v1 只读接口供现有 Santaizi API 客户端使用。
	v1 := ma.r.Group("v1")
	{
		apiv1 := &apiV1{v1}
		apiv1.serve()
	}
	registerAPIV2(ma.r)
}

func (ma *memberAPI) delete(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id < 1 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "错误的 Server ID",
		})
		return
	}

	var err error
	switch c.Param("model") {
	case "server":
		err := singleton.DB.Transaction(func(tx *gorm.DB) error {
			err = singleton.DB.Unscoped().Delete(&model.Server{}, "id = ?", id).Error
			if err != nil {
				return err
			}
			err = singleton.DB.Unscoped().Delete(&model.MonitorHistory{}, "server_id = ?", id).Error
			if err != nil {
				return err
			}
			return nil
		})
		if err == nil {
			// 删除服务器
			singleton.ServerLock.Lock()
			onServerDelete(id)
			singleton.ServerLock.Unlock()
			singleton.ReSortServer()
		}
	case "notification":
		err = singleton.DB.Unscoped().Delete(&model.Notification{}, "id = ?", id).Error
		if err == nil {
			singleton.OnDeleteNotification(id)
		}
	case "ddns":
		err = singleton.DB.Unscoped().Delete(&model.DDNSProfile{}, "id = ?", id).Error
		if err == nil {
			singleton.OnDDNSUpdate()
		}
	case "nat":
		err = singleton.DB.Unscoped().Delete(&model.NAT{}, "id = ?", id).Error
		if err == nil {
			singleton.OnNATUpdate()
		}
	case "monitor":
		err = singleton.DB.Unscoped().Delete(&model.Monitor{}, "id = ?", id).Error
		if err == nil {
			singleton.ServiceSentinelShared.OnMonitorDelete(id)
			err = singleton.DB.Unscoped().Delete(&model.MonitorHistory{}, "monitor_id = ?", id).Error
		}
	case "alert-rule":
		err = singleton.DB.Unscoped().Delete(&model.AlertRule{}, "id = ?", id).Error
		if err == nil {
			singleton.OnDeleteAlert(id)
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("数据库错误：%s", err),
		})
		return
	}
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type searchResult struct {
	Name  string `json:"name,omitempty"`
	Value uint64 `json:"value,omitempty"`
	Text  string `json:"text,omitempty"`
}

func (ma *memberAPI) searchServer(c *gin.Context) {
	var servers []model.Server
	likeWord := "%" + c.Query("word") + "%"
	singleton.DB.Select("id,name").Where("id = ? OR name LIKE ? OR tag LIKE ? OR note LIKE ?",
		c.Query("word"), likeWord, likeWord, likeWord).Find(&servers)

	var resp []searchResult
	for i := 0; i < len(servers); i++ {
		resp = append(resp, searchResult{
			Value: servers[i].ID,
			Name:  servers[i].Name,
			Text:  servers[i].Name,
		})
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"results": resp,
	})
}

func (ma *memberAPI) searchDDNS(c *gin.Context) {
	var ddns []model.DDNSProfile
	likeWord := "%" + c.Query("word") + "%"
	singleton.DB.Select("id,name").Where("id = ? OR name LIKE ?",
		c.Query("word"), likeWord).Find(&ddns)

	var resp []searchResult
	for i := 0; i < len(ddns); i++ {
		resp = append(resp, searchResult{
			Value: ddns[i].ID,
			Name:  ddns[i].Name,
			Text:  ddns[i].Name,
		})
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"results": resp,
	})
}

type serverForm struct {
	ID              uint64
	Name            string `binding:"required"`
	DisplayIndex    int
	Secret          string
	Tag             string
	Note            string
	PublicNote      string
	HideForGuest    string
	EnableDDNS      string
	DDNSProfilesRaw string
}

func (ma *memberAPI) addOrEditServer(c *gin.Context) {
	var sf serverForm
	var s model.Server
	var isEdit bool
	err := c.ShouldBindJSON(&sf)
	if err == nil {
		s.Name = sf.Name
		s.Secret = sf.Secret
		s.DisplayIndex = sf.DisplayIndex
		s.ID = sf.ID
		s.Tag = sf.Tag
		s.Note = sf.Note
		s.PublicNote = sf.PublicNote
		s.HideForGuest = sf.HideForGuest == "on"
		s.EnableDDNS = sf.EnableDDNS == "on"
		s.DDNSProfilesRaw = sf.DDNSProfilesRaw
		err = utils.Json.Unmarshal([]byte(sf.DDNSProfilesRaw), &s.DDNSProfiles)
		if err == nil {
			if s.ID == 0 {
				s.Secret, err = utils.GenerateRandomString(18)
				if err == nil {
					err = singleton.DB.Create(&s).Error
				}
			} else {
				isEdit = true
				err = singleton.DB.Save(&s).Error
			}
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	if isEdit {
		if err := singleton.RefreshObserverAssignmentsForServer(s.ID, time.Now()); err != nil {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusInternalServerError, Message: err.Error()})
			return
		}
		singleton.ServerLock.Lock()
		s.CopyFromRunningServer(singleton.ServerList[s.ID])
		// 如果修改了 Secret
		if s.Secret != singleton.ServerList[s.ID].Secret {
			// 删除旧 Secret-ID 绑定关系
			singleton.SecretToID[s.Secret] = s.ID
			// 设置新的 Secret-ID 绑定关系
			delete(singleton.SecretToID, singleton.ServerList[s.ID].Secret)
		}
		// 如果修改了Tag
		oldTag := singleton.ServerList[s.ID].Tag
		newTag := s.Tag
		if newTag != oldTag {
			index := -1
			for i := 0; i < len(singleton.ServerTagToIDList[oldTag]); i++ {
				if singleton.ServerTagToIDList[oldTag][i] == s.ID {
					index = i
					break
				}
			}
			if index > -1 {
				// 删除旧 Tag-ID 绑定关系
				singleton.ServerTagToIDList[oldTag] = append(singleton.ServerTagToIDList[oldTag][:index], singleton.ServerTagToIDList[oldTag][index+1:]...)
				if len(singleton.ServerTagToIDList[oldTag]) == 0 {
					delete(singleton.ServerTagToIDList, oldTag)
				}
			}
			// 设置新的 Tag-ID 绑定关系
			singleton.ServerTagToIDList[newTag] = append(singleton.ServerTagToIDList[newTag], s.ID)
		}
		singleton.ServerList[s.ID] = &s
		singleton.ServerLock.Unlock()
	} else {
		s.Host = &model.Host{}
		s.State = &model.HostState{}
		singleton.ServerLock.Lock()
		singleton.SecretToID[s.Secret] = s.ID
		singleton.ServerList[s.ID] = &s
		singleton.ServerTagToIDList[s.Tag] = append(singleton.ServerTagToIDList[s.Tag], s.ID)
		singleton.ServerLock.Unlock()
	}
	singleton.ReSortServer()
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

// resetServerSecret 重置服务器密钥
func (ma *memberAPI) resetServerSecret(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}

	singleton.ServerLock.RLock()
	server, ok := singleton.ServerList[id]
	singleton.ServerLock.RUnlock()
	if !ok {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "服务器不存在",
		})
		return
	}

	newSecret, err := utils.GenerateRandomString(18)
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}

	if err = singleton.DB.Model(&model.Server{}).Where("id = ?", id).Update("secret", newSecret).Error; err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}

	singleton.ServerLock.Lock()
	oldSecret := server.Secret
	server.Secret = newSecret
	singleton.SecretToID[newSecret] = id
	delete(singleton.SecretToID, oldSecret)
	singleton.ServerLock.Unlock()

	c.JSON(http.StatusOK, model.Response{
		Code:    http.StatusOK,
		Message: newSecret,
	})
}

// resetServerAvailability 重置单台服务器的可用性数据：
// 清空该服务器全部离线历史并复位运行态，用于修复异常数据
// （如遗留未关闭记录导致的“无限离线”）或人工重新统计。
func (ma *memberAPI) resetServerAvailability(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "错误的服务器 ID",
		})
		return
	}

	singleton.ServerLock.RLock()
	_, ok := singleton.ServerList[id]
	singleton.ServerLock.RUnlock()
	if !ok {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "服务器不存在",
		})
		return
	}

	deleted, err := singleton.ResetServerAvailability(id)
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("数据库错误：%s", err),
		})
		return
	}

	c.JSON(http.StatusOK, model.Response{
		Code:   http.StatusOK,
		Result: map[string]int64{"deleted": deleted},
	})
}

type monitorForm struct {
	ID                  uint64
	Name                string
	Target              string
	Type                uint8
	Cover               uint8
	Notify              string
	NotificationTag     string
	SkipServersRaw      string
	Duration            uint64
	MinLatency          float32
	MaxLatency          float32
	LatencyNotify       string
	EnableShowInService string
}

func (ma *memberAPI) addOrEditMonitor(c *gin.Context) {
	var mf monitorForm
	var m model.Monitor
	err := c.ShouldBindJSON(&mf)
	if err == nil {
		m.Name = mf.Name
		m.Target = strings.TrimSpace(mf.Target)
		m.Type = mf.Type
		m.ID = mf.ID
		m.SkipServersRaw = mf.SkipServersRaw
		m.Cover = mf.Cover
		m.Notify = mf.Notify == "on"
		m.NotificationTag = mf.NotificationTag
		m.Duration = mf.Duration
		m.LatencyNotify = mf.LatencyNotify == "on"
		m.MinLatency = mf.MinLatency
		m.MaxLatency = mf.MaxLatency
		m.EnableShowInService = mf.EnableShowInService == "on"
		err = m.InitSkipServers()
	}
	if err == nil {
		// 保证NotificationTag不为空
		if m.NotificationTag == "" {
			m.NotificationTag = "default"
		}
		if m.ID == 0 {
			err = singleton.DB.Create(&m).Error
		} else {
			err = singleton.DB.Save(&m).Error
		}
	}
	if err == nil {
		if m.Cover == 0 {
			err = singleton.DB.Unscoped().Delete(&model.MonitorHistory{}, "monitor_id = ? and server_id in (?)", m.ID, strings.Split(m.SkipServersRaw[1:len(m.SkipServersRaw)-1], ",")).Error
		} else {
			err = singleton.DB.Unscoped().Delete(&model.MonitorHistory{}, "monitor_id = ? and server_id not in (?)", m.ID, strings.Split(m.SkipServersRaw[1:len(m.SkipServersRaw)-1], ",")).Error
		}
	}
	if err == nil {
		err = singleton.ServiceSentinelShared.OnMonitorUpdate(m)
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type BatchUpdateServerGroupRequest struct {
	Servers []uint64
	Group   string
}

func (ma *memberAPI) batchUpdateServerGroup(c *gin.Context) {
	var req BatchUpdateServerGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if err := singleton.DB.Model(&model.Server{}).Where("id in (?)", req.Servers).Update("tag", req.Group).Error; err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	for _, serverID := range req.Servers {
		if err := singleton.RefreshObserverAssignmentsForServer(serverID, time.Now()); err != nil {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusInternalServerError, Message: err.Error()})
			return
		}
	}

	singleton.ServerLock.Lock()

	for i := 0; i < len(req.Servers); i++ {
		serverId := req.Servers[i]
		var s model.Server
		_ = copier.Copy(&s, singleton.ServerList[serverId])
		s.Tag = req.Group
		// 如果修改了Ta
		oldTag := singleton.ServerList[serverId].Tag
		newTag := s.Tag
		if newTag != oldTag {
			index := -1
			for i := 0; i < len(singleton.ServerTagToIDList[oldTag]); i++ {
				if singleton.ServerTagToIDList[oldTag][i] == s.ID {
					index = i
					break
				}
			}
			if index > -1 {
				// 删除旧 Tag-ID 绑定关系
				singleton.ServerTagToIDList[oldTag] = append(singleton.ServerTagToIDList[oldTag][:index], singleton.ServerTagToIDList[oldTag][index+1:]...)
				if len(singleton.ServerTagToIDList[oldTag]) == 0 {
					delete(singleton.ServerTagToIDList, oldTag)
				}
			}
			// 设置新的 Tag-ID 绑定关系
			singleton.ServerTagToIDList[newTag] = append(singleton.ServerTagToIDList[newTag], s.ID)
		}
		singleton.ServerList[s.ID] = &s
	}

	singleton.ServerLock.Unlock()

	singleton.ReSortServer()

	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type notificationForm struct {
	ID            uint64
	Name          string
	Tag           string // 分组名
	URL           string
	RequestMethod int
	RequestType   int
	RequestHeader string
	RequestBody   string
	VerifySSL     string
	SkipCheck     string
}

func (ma *memberAPI) addOrEditNotification(c *gin.Context) {
	var nf notificationForm
	var n model.Notification
	err := c.ShouldBindJSON(&nf)
	if err == nil {
		n.Name = nf.Name
		n.Tag = nf.Tag
		n.RequestMethod = nf.RequestMethod
		n.RequestType = nf.RequestType
		n.RequestHeader = nf.RequestHeader
		n.RequestBody = nf.RequestBody
		n.URL = nf.URL
		verifySSL := nf.VerifySSL == "on"
		n.VerifySSL = &verifySSL
		n.ID = nf.ID
		ns := model.NotificationServerBundle{
			Notification: &n,
			Server:       nil,
			Loc:          singleton.Loc,
		}
		// 勾选了跳过检查
		if nf.SkipCheck != "on" {
			err = ns.Send("这是测试消息")
		}
	}
	if err == nil {
		// 保证Tag不为空
		if n.Tag == "" {
			n.Tag = "default"
		}
		if n.ID == 0 {
			err = singleton.DB.Create(&n).Error
		} else {
			err = singleton.DB.Save(&n).Error
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	singleton.OnRefreshOrAddNotification(&n)
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type ddnsForm struct {
	ID                 uint64
	MaxRetries         uint64
	EnableIPv4         string
	EnableIPv6         string
	Name               string
	Provider           uint8
	DomainsRaw         string
	AccessID           string
	AccessSecret       string
	WebhookURL         string
	WebhookMethod      uint8
	WebhookRequestType uint8
	WebhookRequestBody string
	WebhookHeaders     string
}

func (ma *memberAPI) addOrEditDDNS(c *gin.Context) {
	var df ddnsForm
	var p model.DDNSProfile
	err := c.ShouldBindJSON(&df)
	if err == nil {
		if df.MaxRetries < 1 || df.MaxRetries > 10 {
			err = errors.New("重试次数必须为大于 1 且不超过 10 的整数")
		}
	}
	if err == nil {
		p.Name = df.Name
		p.ID = df.ID
		enableIPv4 := df.EnableIPv4 == "on"
		enableIPv6 := df.EnableIPv6 == "on"
		p.EnableIPv4 = &enableIPv4
		p.EnableIPv6 = &enableIPv6
		p.MaxRetries = df.MaxRetries
		p.Provider = df.Provider
		p.DomainsRaw = df.DomainsRaw
		p.Domains = strings.Split(p.DomainsRaw, ",")
		p.AccessID = df.AccessID
		p.AccessSecret = df.AccessSecret
		p.WebhookURL = df.WebhookURL
		p.WebhookMethod = df.WebhookMethod
		p.WebhookRequestType = df.WebhookRequestType
		p.WebhookRequestBody = df.WebhookRequestBody
		p.WebhookHeaders = df.WebhookHeaders

		for n, domain := range p.Domains {
			// IDN to ASCII
			domainValid, domainErr := idna.Lookup.ToASCII(domain)
			if domainErr != nil {
				err = fmt.Errorf("域名 %s 解析错误: %v", domain, domainErr)
				break
			}
			p.Domains[n] = domainValid
		}
	}
	if err == nil {
		if p.ID == 0 {
			err = singleton.DB.Create(&p).Error
		} else {
			err = singleton.DB.Save(&p).Error
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	singleton.OnDDNSUpdate()
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type natForm struct {
	ID       uint64
	Name     string
	ServerID uint64
	Host     string
	Domain   string
}

func (ma *memberAPI) addOrEditNAT(c *gin.Context) {
	var nf natForm
	var n model.NAT
	err := c.ShouldBindJSON(&nf)
	if err == nil {
		n.Name = nf.Name
		n.ID = nf.ID
		n.Domain = nf.Domain
		n.Host = nf.Host
		n.ServerID = nf.ServerID
	}
	if err == nil {
		if n.ID == 0 {
			err = singleton.DB.Create(&n).Error
		} else {
			err = singleton.DB.Save(&n).Error
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	singleton.OnNATUpdate()
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type alertRuleForm struct {
	ID              uint64
	Name            string
	RulesRaw        string
	NotificationTag string
	TriggerMode     int
	Enable          string
}

func (ma *memberAPI) addOrEditAlertRule(c *gin.Context) {
	var arf alertRuleForm
	var r model.AlertRule
	err := c.ShouldBindJSON(&arf)
	if err == nil {
		err = utils.Json.Unmarshal([]byte(arf.RulesRaw), &r.Rules)
	}
	if err == nil {
		if len(r.Rules) == 0 {
			err = errors.New("至少定义一条规则")
		} else {
			for i := 0; i < len(r.Rules); i++ {
				if r.Rules[i].Duration < 3 {
					err = errors.New("错误：Duration 至少为 3")
					break
				}
			}
		}
	}
	if err == nil {
		r.Name = arf.Name
		r.RulesRaw = arf.RulesRaw
		r.NotificationTag = arf.NotificationTag
		enable := arf.Enable == "on"
		r.TriggerMode = arf.TriggerMode
		r.Enable = &enable
		r.ID = arf.ID
	}
	//保证NotificationTag不为空
	if err == nil {
		if r.NotificationTag == "" {
			r.NotificationTag = "default"
		}
		if r.ID == 0 {
			err = singleton.DB.Create(&r).Error
		} else {
			err = singleton.DB.Save(&r).Error
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	singleton.OnRefreshOrAddAlert(r)
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type logoutForm struct {
	ID uint64
}

func (ma *memberAPI) logout(c *gin.Context) {
	admin := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	var lf logoutForm
	if err := c.ShouldBindJSON(&lf); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	if lf.ID != admin.ID {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", "用户ID不匹配"),
		})
		return
	}
	singleton.DB.Model(admin).UpdateColumns(model.User{
		Token:        "",
		TokenExpired: time.Now(),
	})
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})

	if oidcLogoutUrl := singleton.Conf.Oauth2.OidcLogoutURL; oidcLogoutUrl != "" {
		// 重定向到 OIDC 退出登录地址。不知道为什么，这里的重定向不生效
		c.Redirect(http.StatusOK, oidcLogoutUrl)
	}
}

type settingForm struct {
	Title                   string
	Admin                   string
	Language                string
	Theme                   string
	DashboardTheme          string
	CustomCode              string
	CustomCodeDashboard     string
	CustomNameservers       string
	ViewPassword            string
	IgnoredIPNotification   string
	IPChangeNotificationTag string // IP变更提醒的通知组
	GRPCHost                string
	Cover                   uint8

	EnableIPChangeNotification      string
	EnablePlainIPInNotification     string
	DisableSwitchTemplateInFrontend string

	// 离线历史配置
	EnableOfflineHistory        string
	OfflineThresholdSeconds     uint64
	OfflineCheckIntervalSeconds uint64
	OfflineMergeGapSeconds      uint64
	OfflineHistoryRetentionDays uint64
	EnableOfflineNotification   string
	EnableRecoveryNotification  string
	ShowAvailabilityToGuest     string
}

func (ma *memberAPI) updateSetting(c *gin.Context) {
	var sf settingForm
	if err := c.ShouldBind(&sf); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}

	sf.Theme = model.NormalizePublicTheme(sf.Theme)
	sf.DashboardTheme = "spa"

	singleton.Conf.Language = sf.Language
	singleton.Conf.EnableIPChangeNotification = sf.EnableIPChangeNotification == "on"

	// 记录离线历史开关的旧值，用于决定保存后是结构性重启检测器还是仅热重载配置
	offlineHistoryWasEnabled := singleton.Conf.EnableOfflineHistory

	// 离线历史配置校验与保存
	if sf.OfflineThresholdSeconds < 10 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "离线判定阈值至少为 10 秒",
		})
		return
	}
	if sf.OfflineCheckIntervalSeconds < 5 || sf.OfflineCheckIntervalSeconds > sf.OfflineThresholdSeconds {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "离线检测间隔需大于等于 5 秒且小于等于离线阈值",
		})
		return
	}
	if sf.OfflineHistoryRetentionDays < 1 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "历史保留天数至少为 1 天",
		})
		return
	}
	if sf.OfflineMergeGapSeconds > 3600 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "离线合并间隔需小于等于 3600 秒",
		})
		return
	}
	singleton.Conf.EnableOfflineHistory = sf.EnableOfflineHistory == "on"
	singleton.Conf.OfflineThresholdSeconds = sf.OfflineThresholdSeconds
	singleton.Conf.OfflineCheckIntervalSeconds = sf.OfflineCheckIntervalSeconds
	// 离线合并间隔未提交时保留现有值。
	if sf.OfflineMergeGapSeconds >= 1 {
		singleton.Conf.OfflineMergeGapSeconds = sf.OfflineMergeGapSeconds
	}
	singleton.Conf.OfflineHistoryRetentionDays = sf.OfflineHistoryRetentionDays
	singleton.Conf.EnableOfflineNotification = sf.EnableOfflineNotification == "on"
	singleton.Conf.EnableRecoveryNotification = sf.EnableRecoveryNotification == "on"
	singleton.Conf.ShowAvailabilityToGuest = sf.ShowAvailabilityToGuest == "on"
	singleton.Conf.EnablePlainIPInNotification = sf.EnablePlainIPInNotification == "on"
	singleton.Conf.DisableSwitchTemplateInFrontend = sf.DisableSwitchTemplateInFrontend == "on"
	singleton.Conf.Cover = sf.Cover
	singleton.Conf.GRPCHost = sf.GRPCHost
	singleton.Conf.IgnoredIPNotification = sf.IgnoredIPNotification
	singleton.Conf.IPChangeNotificationTag = sf.IPChangeNotificationTag
	singleton.Conf.Site.Brand = sf.Title
	singleton.Conf.Site.Theme = sf.Theme
	singleton.Conf.Site.DashboardTheme = sf.DashboardTheme
	singleton.Conf.Site.CustomCode = sf.CustomCode
	singleton.Conf.Site.CustomCodeDashboard = sf.CustomCodeDashboard
	singleton.Conf.DNSServers = sf.CustomNameservers
	singleton.Conf.Site.ViewPassword = sf.ViewPassword
	singleton.Conf.Oauth2.Admin = sf.Admin
	// 保证NotificationTag不为空
	if singleton.Conf.IPChangeNotificationTag == "" {
		singleton.Conf.IPChangeNotificationTag = "default"
	}
	if err := singleton.Conf.Save(); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	// 更新系统语言
	singleton.InitLocalizer()
	// 更新DNS服务器
	singleton.OnNameserverUpdate()
	// 离线检测器：仅当离线历史开关翻转时才结构性重启；否则热重载配置（如检测间隔）
	if offlineHistoryWasEnabled != singleton.Conf.EnableOfflineHistory {
		singleton.StartOfflineDetector()
	} else {
		singleton.ReloadOfflineDetectorConfig()
	}
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

func (ma *memberAPI) batchDeleteServer(c *gin.Context) {
	var servers []uint64
	if err := c.ShouldBindJSON(&servers); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if err := singleton.DB.Unscoped().Delete(&model.Server{}, "id in (?)", servers).Error; err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	singleton.ServerLock.Lock()
	for i := 0; i < len(servers); i++ {
		id := servers[i]
		onServerDelete(id)
	}
	singleton.ServerLock.Unlock()
	singleton.ReSortServer()
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

func onServerDelete(id uint64) {
	if err := singleton.EndServerNodeBinding(id, time.Now()); err != nil {
		log.Printf("SANTAIZI>> end observer assignments for deleted server %d: %v", id, err)
	}
	singleton.OnDeleteServer(id)
	tag := singleton.ServerList[id].Tag
	delete(singleton.SecretToID, singleton.ServerList[id].Secret)
	delete(singleton.ServerList, id)
	index := -1
	for i := 0; i < len(singleton.ServerTagToIDList[tag]); i++ {
		if singleton.ServerTagToIDList[tag][i] == id {
			index = i
			break
		}
	}
	if index > -1 {

		singleton.ServerTagToIDList[tag] = append(singleton.ServerTagToIDList[tag][:index], singleton.ServerTagToIDList[tag][index+1:]...)
		if len(singleton.ServerTagToIDList[tag]) == 0 {
			delete(singleton.ServerTagToIDList, tag)
		}
	}

	singleton.DB.Unscoped().Delete(&model.Transfer{}, "server_id = ?", id)
	singleton.DB.Unscoped().Delete(&model.ServerRuntime{}, "server_id = ?", id)
	singleton.DB.Unscoped().Delete(&model.ServerOfflineHistory{}, "server_id = ?", id)
}

type offlineHistoryItem struct {
	ID                uint64     `json:"id"`
	ServerID          uint64     `json:"server_id"`
	StartedAt         time.Time  `json:"started_at"`
	DetectedAt        time.Time  `json:"detected_at"`
	EndedAt           *time.Time `json:"ended_at"`
	DurationSeconds   uint64     `json:"duration_seconds"`
	Reason            string     `json:"reason"`
	Status            string     `json:"status"`
	ThresholdSeconds  uint64     `json:"threshold_seconds"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
	LastBootTime      uint64     `json:"last_boot_time"`
	RecoveredBootTime uint64     `json:"recovered_boot_time"`
	LastIP            string     `json:"last_ip"`
	RecoveredIP       string     `json:"recovered_ip"`
}

type offlineHistoryResponse struct {
	Items []offlineHistoryItem `json:"items"`
	Total int64                `json:"total"`
}

func (ma *memberAPI) offlineHistory(c *gin.Context) {
	serverID, err := strconv.ParseUint(c.Query("server_id"), 10, 64)
	if err != nil || serverID == 0 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "server_id 参数错误",
		})
		return
	}

	singleton.ServerLock.RLock()
	_, ok := singleton.ServerList[serverID]
	singleton.ServerLock.RUnlock()
	if !ok {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "服务器不存在",
		})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var histories []model.ServerOfflineHistory
	var total int64
	singleton.DB.Model(&model.ServerOfflineHistory{}).Where("server_id = ?", serverID).Count(&total)
	singleton.DB.Where("server_id = ?", serverID).Order("started_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&histories)

	items := make([]offlineHistoryItem, len(histories))
	for i, h := range histories {
		items[i] = offlineHistoryItem{
			ID:                h.ID,
			ServerID:          h.ServerID,
			StartedAt:         h.StartedAt,
			DetectedAt:        h.DetectedAt,
			EndedAt:           h.EndedAt,
			DurationSeconds:   h.DurationSeconds,
			Reason:            h.Reason,
			Status:            h.Status,
			ThresholdSeconds:  h.ThresholdSeconds,
			LastSeenAt:        h.LastSeenAt,
			LastBootTime:      h.LastBootTime,
			RecoveredBootTime: h.RecoveredBootTime,
			LastIP:            h.LastIP,
			RecoveredIP:       h.RecoveredIP,
		}
	}

	c.JSON(http.StatusOK, model.Response{
		Code:   http.StatusOK,
		Result: offlineHistoryResponse{Items: items, Total: total},
	})
}

type offlineSummaryResponse struct {
	ServerID               uint64   `json:"server_id"`
	Days                   int      `json:"days"`
	OfflineCount           int      `json:"offline_count"`
	TotalOfflineSeconds    uint64   `json:"total_offline_seconds"`
	LongestOfflineSeconds  uint64   `json:"longest_offline_seconds"`
	AvailabilityPercent    *float64 `json:"availability_percent"`
	RebootCount            int      `json:"reboot_count"`
	NetworkDisconnectCount int      `json:"network_disconnect_count"`
	UnknownCount           int      `json:"unknown_count"`
}

const maxOfflineSummaryDays = 3660 // 最多允许查询 10 年，防止大查询拖垮服务

func (ma *memberAPI) offlineSummary(c *gin.Context) {
	serverID, err := strconv.ParseUint(c.Query("server_id"), 10, 64)
	if err != nil || serverID == 0 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "server_id 参数错误",
		})
		return
	}

	singleton.ServerLock.RLock()
	_, ok := singleton.ServerList[serverID]
	singleton.ServerLock.RUnlock()
	if !ok {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "服务器不存在",
		})
		return
	}

	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days < 1 {
		days = 30
	}
	if days > maxOfflineSummaryDays {
		days = maxOfflineSummaryDays
	}

	now := time.Now()
	periodStart := now.AddDate(0, 0, -days)
	periodEnd := now
	periodSeconds := uint64(periodEnd.Sub(periodStart).Seconds())

	var histories []model.ServerOfflineHistory
	singleton.DB.Where(
		"server_id = ? AND started_at < ? AND (ended_at IS NULL OR ended_at > ?)",
		serverID, periodEnd, periodStart,
	).Order("started_at DESC").Find(&histories)

	// 离线时长按区间并集计算：重叠的离线记录只计一次，避免可用率被重复扣除
	totalOfflineSeconds, longestOfflineSeconds := singleton.SummarizeOfflineIntervals(histories, periodStart, periodEnd)

	rebootCount := 0
	networkCount := 0
	unknownCount := 0
	for _, h := range histories {
		switch h.Reason {
		case model.OfflineReasonMachineReboot:
			rebootCount++
		case model.OfflineReasonNetworkDisconnect:
			networkCount++
		default:
			unknownCount++
		}
	}

	// 可用率：服务器从未上报过数据（LastSeenAt 为空）时为空值（nil），
	// 与前台可用性口径一致；否则按离线时长折算（已上报且无离线为 100）。
	var availability *float64
	var rt model.ServerRuntime
	if err := singleton.DB.Select("last_seen_at").Where("server_id = ?", serverID).First(&rt).Error; err == nil && rt.LastSeenAt != nil {
		pct := 100.0
		if periodSeconds > 0 {
			if totalOfflineSeconds >= periodSeconds {
				pct = 0.0
			} else {
				pct = singleton.FormatAvailabilityPercent((1.0 - float64(totalOfflineSeconds)/float64(periodSeconds)) * 100)
			}
		}
		availability = &pct
	}

	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
		Result: offlineSummaryResponse{
			ServerID:               serverID,
			Days:                   days,
			OfflineCount:           len(histories),
			TotalOfflineSeconds:    totalOfflineSeconds,
			LongestOfflineSeconds:  longestOfflineSeconds,
			AvailabilityPercent:    availability,
			RebootCount:            rebootCount,
			NetworkDisconnectCount: networkCount,
			UnknownCount:           unknownCount,
		},
	})
}

type cleanupOfflineHistoryRequest struct {
	BeforeDays uint64 `json:"before_days"`
}

func (ma *memberAPI) cleanupOfflineHistory(c *gin.Context) {
	var req cleanupOfflineHistoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	if req.BeforeDays == 0 {
		req.BeforeDays = singleton.Conf.OfflineHistoryRetentionDays
	}
	if req.BeforeDays < 1 {
		req.BeforeDays = 1
	}

	before := time.Now().AddDate(0, 0, -int(req.BeforeDays))
	res := singleton.DB.Unscoped().Delete(&model.ServerOfflineHistory{}, "started_at < ?", before)
	if res.Error != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("数据库错误：%s", res.Error),
		})
		return
	}

	c.JSON(http.StatusOK, model.Response{
		Code:   http.StatusOK,
		Result: map[string]int64{"deleted": res.RowsAffected},
	})
}

func (ma *memberAPI) deleteOfflineHistory(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "错误的记录 ID",
		})
		return
	}

	if err := singleton.DB.Unscoped().Delete(&model.ServerOfflineHistory{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("数据库错误：%s", err),
		})
		return
	}

	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}
