package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	trafficservice "github.com/hi2shark/santaizi-dashboard/service/traffic"
	"gorm.io/gorm"
)

func registerTypedAdminRoutes(admin *gin.RouterGroup) {
	admin.GET("/monitors", v2ListMonitors)
	admin.POST("/monitors", v2CreateMonitor)
	admin.GET("/monitors/:id", v2GetMonitor)
	admin.PATCH("/monitors/:id", v2UpdateMonitor)
	admin.DELETE("/monitors/:id", v2DeleteMonitor)

	admin.GET("/notifications", v2ListNotifications)
	admin.POST("/notifications", v2CreateNotification)
	admin.GET("/notifications/:id", v2GetNotification)
	admin.PATCH("/notifications/:id", v2UpdateNotification)
	admin.DELETE("/notifications/:id", v2DeleteNotification)

	admin.GET("/alert-rules", v2ListAlertRules)
	admin.POST("/alert-rules", v2CreateAlertRule)
	admin.GET("/alert-rules/:id", v2GetAlertRule)
	admin.PATCH("/alert-rules/:id", v2UpdateAlertRule)
	admin.DELETE("/alert-rules/:id", v2DeleteAlertRule)

	admin.GET("/ddns", v2ListDDNSProfiles)
	admin.POST("/ddns", v2CreateDDNSProfile)
	admin.GET("/ddns/:id", v2GetDDNSProfile)
	admin.PATCH("/ddns/:id", v2UpdateDDNSProfile)
	admin.DELETE("/ddns/:id", v2DeleteDDNSProfile)

	admin.GET("/nat", v2ListNATTunnels)
	admin.POST("/nat", v2CreateNATTunnel)
	admin.GET("/nat/:id", v2GetNATTunnel)
	admin.PATCH("/nat/:id", v2UpdateNATTunnel)
	admin.DELETE("/nat/:id", v2DeleteNATTunnel)

	admin.GET("/probe-capabilities", v2ProbeCapabilities)
	admin.GET("/script-commands", v2ScriptCommands)
	admin.GET("/servers/:id/credential", v2ServerCredential)
	admin.POST("/servers/:id/install-preview", v2InstallPreview)
	admin.POST("/servers/:id/upgrade-preview", v2UpgradePreview)
	admin.GET("/servers/:id/traffic-policies", v2ListTrafficPolicies)
	admin.POST("/servers/:id/traffic-policies", v2CreateTrafficPolicy)
	admin.GET("/servers/:id/traffic-policies/:policyId", v2GetTrafficPolicy)
	admin.PATCH("/servers/:id/traffic-policies/:policyId", v2UpdateTrafficPolicy)
	admin.DELETE("/servers/:id/traffic-policies/:policyId", v2DeleteTrafficPolicy)
	admin.GET("/servers/:id/traffic-policies/:policyId/usage", v2TrafficPolicyUsage)
	admin.GET("/servers/:id/traffic-history", v2ServerTrafficHistory)
}

type monitorScopeDTO struct {
	Mode      string   `json:"mode" binding:"required"`
	ServerIDs []uint64 `json:"server_ids"`
}

type monitorWriteDTO struct {
	Name            string          `json:"name" binding:"required"`
	Type            string          `json:"type" binding:"required"`
	Target          string          `json:"target" binding:"required"`
	IntervalSeconds uint64          `json:"interval_seconds"`
	Scope           monitorScopeDTO `json:"scope" binding:"required"`
	Notify          bool            `json:"notify"`
	NotificationTag string          `json:"notification_tag"`
	ShowInService   bool            `json:"show_in_service"`
	LatencyNotify   bool            `json:"latency_notify"`
	MinLatencyMS    float32         `json:"min_latency_ms"`
	MaxLatencyMS    float32         `json:"max_latency_ms"`
}

var monitorTypes = map[string]uint8{
	"http": model.MonitorTypeHTTPGet,
	"icmp": model.MonitorTypeICMPPing,
	"tcp":  model.MonitorTypeTCPPing,
}

func monitorTypeName(value uint8) string {
	for name, current := range monitorTypes {
		if current == value {
			return name
		}
	}
	return ""
}

func monitorDTO(row model.Monitor) gin.H {
	mode := "exclude"
	if row.Cover == model.MonitorCoverIgnoreAll {
		mode = "include"
	} else if len(row.SkipServers) == 0 {
		mode = "all"
	}
	serverIDs := make([]uint64, 0, len(row.SkipServers))
	for id := range row.SkipServers {
		serverIDs = append(serverIDs, id)
	}
	sort.Slice(serverIDs, func(i, j int) bool { return serverIDs[i] < serverIDs[j] })
	return gin.H{"id": row.ID, "name": row.Name, "type": monitorTypeName(row.Type), "target": row.Target,
		"interval_seconds": row.Duration, "scope": gin.H{"mode": mode, "server_ids": serverIDs},
		"notify": row.Notify, "notification_tag": row.NotificationTag, "show_in_service": row.EnableShowInService,
		"latency_notify": row.LatencyNotify, "min_latency_ms": row.MinLatency, "max_latency_ms": row.MaxLatency,
		"created_at": row.CreatedAt.Format(time.RFC3339), "updated_at": row.UpdatedAt.Format(time.RFC3339)}
}

func applyMonitorWrite(row *model.Monitor, request monitorWriteDTO) error {
	row.Name, row.Target = strings.TrimSpace(request.Name), strings.TrimSpace(request.Target)
	if row.Name == "" || row.Target == "" {
		return errors.New("name and target are required")
	}
	typeID, ok := monitorTypes[strings.ToLower(request.Type)]
	if !ok {
		return errors.New("monitor type must be http, icmp, or tcp")
	}
	row.Type = typeID
	if request.IntervalSeconds == 0 {
		request.IntervalSeconds = 30
	}
	if request.IntervalSeconds < 5 || request.IntervalSeconds > 86400 {
		return errors.New("interval_seconds must be between 5 and 86400")
	}
	row.Duration = request.IntervalSeconds
	serverIDs := request.Scope.ServerIDs
	switch request.Scope.Mode {
	case "all":
		row.Cover, serverIDs = model.MonitorCoverAll, nil
	case "exclude":
		row.Cover = model.MonitorCoverAll
	case "include":
		row.Cover = model.MonitorCoverIgnoreAll
		if len(serverIDs) == 0 {
			return errors.New("include scope requires at least one server")
		}
	default:
		return errors.New("scope mode must be all, include, or exclude")
	}
	raw, err := utils.Json.Marshal(serverIDs)
	if err != nil {
		return err
	}
	row.SkipServersRaw = string(raw)
	_ = row.InitSkipServers()
	row.Notify, row.NotificationTag = request.Notify, strings.TrimSpace(request.NotificationTag)
	row.EnableShowInService = request.ShowInService
	row.LatencyNotify, row.MinLatency, row.MaxLatency = request.LatencyNotify, request.MinLatencyMS, request.MaxLatencyMS
	if row.LatencyNotify && row.MaxLatency > 0 && row.MinLatency > row.MaxLatency {
		return errors.New("min_latency_ms must not exceed max_latency_ms")
	}
	return nil
}

func v2ListMonitors(c *gin.Context) {
	page, size := parsePage(c)
	query := singleton.DB.Model(&model.Monitor{})
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		query = query.Where("name LIKE ? OR target LIKE ?", "%"+q+"%", "%"+q+"%")
	}
	var total int64
	query.Count(&total)
	var rows []model.Monitor
	if err := query.Order(orderClause(c, map[string]string{"id": "id", "name": "name", "updated_at": "updated_at"}, "id")).Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, monitorDTO(row))
	}
	writeV2List(c, items, v2Meta{Page: page, PageSize: size, Total: total})
}

func v2GetMonitor(c *gin.Context) { v2ReadMonitor(c) }
func v2ReadMonitor(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.Monitor
	if err := singleton.DB.First(&row, id).Error; err != nil {
		writeV2Problem(c, 404, "monitor_not_found", "服务监控不存在")
		return
	}
	writeV2Data(c, 200, monitorDTO(row))
}
func v2CreateMonitor(c *gin.Context) { v2SaveMonitor(c, 0) }
func v2UpdateMonitor(c *gin.Context) {
	id, ok := v2ID(c)
	if ok {
		v2SaveMonitor(c, id)
	}
}
func v2SaveMonitor(c *gin.Context, id uint64) {
	var request monitorWriteDTO
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_monitor", err.Error())
		return
	}
	row := model.Monitor{Common: model.Common{ID: id}}
	if id > 0 && singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "monitor_not_found", "服务监控不存在")
		return
	}
	if err := applyMonitorWrite(&row, request); err != nil {
		writeV2Problem(c, 400, "invalid_monitor", err.Error())
		return
	}
	if err := singleton.DB.Save(&row).Error; err != nil {
		writeV2Problem(c, 400, "monitor_save_failed", err.Error())
		return
	}
	if singleton.ServiceSentinelShared != nil {
		if err := singleton.ServiceSentinelShared.OnMonitorUpdate(row); err != nil {
			writeV2Problem(c, 400, "monitor_activate_failed", err.Error())
			return
		}
	}
	status := http.StatusOK
	if id == 0 {
		status = http.StatusCreated
	}
	writeV2Data(c, status, monitorDTO(row))
}
func v2DeleteMonitor(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	result := singleton.DB.Unscoped().Delete(&model.Monitor{}, "id = ?", id)
	if result.Error != nil {
		writeV2Problem(c, 500, "monitor_delete_failed", result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		writeV2Problem(c, 404, "monitor_not_found", "服务监控不存在")
		return
	}
	if singleton.ServiceSentinelShared != nil {
		singleton.ServiceSentinelShared.OnMonitorDelete(id)
	}
	_ = singleton.DB.Unscoped().Delete(&model.MonitorHistory{}, "monitor_id = ?", id).Error
	c.Status(http.StatusNoContent)
}

type keyValueDTO struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type notificationWriteDTO struct {
	Name        string        `json:"name" binding:"required"`
	Tag         string        `json:"tag" binding:"required"`
	URL         string        `json:"url" binding:"required"`
	Method      string        `json:"method" binding:"required"`
	RequestType string        `json:"request_type" binding:"required"`
	Headers     []keyValueDTO `json:"headers"`
	Body        string        `json:"body"`
	VerifyTLS   bool          `json:"verify_tls"`
}

func keyValuesFromJSON(raw string) []keyValueDTO {
	values := map[string]string{}
	_ = utils.Json.Unmarshal([]byte(raw), &values)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]keyValueDTO, 0, len(keys))
	for _, key := range keys {
		result = append(result, keyValueDTO{Key: key, Value: values[key]})
	}
	return result
}
func keyValuesJSON(values []keyValueDTO) (string, error) {
	result := map[string]string{}
	for _, item := range values {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			return "", errors.New("header key is required")
		}
		result[key] = item.Value
	}
	raw, err := utils.Json.Marshal(result)
	return string(raw), err
}
func notificationDTO(row model.Notification) gin.H {
	method := "get"
	if row.RequestMethod == model.NotificationRequestMethodPOST {
		method = "post"
	}
	requestType := "json"
	if row.RequestType == model.NotificationRequestTypeForm {
		requestType = "form"
	}
	verify := row.VerifySSL != nil && *row.VerifySSL
	return gin.H{"id": row.ID, "name": row.Name, "tag": row.Tag, "url": row.URL, "method": method, "request_type": requestType, "headers": keyValuesFromJSON(row.RequestHeader), "body": row.RequestBody, "verify_tls": verify, "created_at": row.CreatedAt.Format(time.RFC3339), "updated_at": row.UpdatedAt.Format(time.RFC3339)}
}
func applyNotificationWrite(row *model.Notification, request notificationWriteDTO) error {
	row.Name, row.Tag, row.URL = strings.TrimSpace(request.Name), strings.TrimSpace(request.Tag), strings.TrimSpace(request.URL)
	if row.Name == "" || row.Tag == "" {
		return errors.New("name and tag are required")
	}
	parsed, err := url.ParseRequestURI(row.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("url must be an absolute HTTP or HTTPS URL")
	}
	switch strings.ToLower(request.Method) {
	case "get":
		row.RequestMethod = model.NotificationRequestMethodGET
	case "post":
		row.RequestMethod = model.NotificationRequestMethodPOST
	default:
		return errors.New("method must be get or post")
	}
	switch strings.ToLower(request.RequestType) {
	case "json":
		row.RequestType = model.NotificationRequestTypeJSON
	case "form":
		row.RequestType = model.NotificationRequestTypeForm
	default:
		return errors.New("request_type must be json or form")
	}
	headers, err := keyValuesJSON(request.Headers)
	if err != nil {
		return err
	}
	row.RequestHeader, row.RequestBody = headers, request.Body
	row.VerifySSL = &request.VerifyTLS
	return nil
}
func v2ListNotifications(c *gin.Context) {
	v2ListNamed(c, &model.Notification{}, func(rows []model.Notification) []gin.H {
		out := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			out = append(out, notificationDTO(row))
		}
		return out
	})
}
func v2GetNotification(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.Notification
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "notification_not_found", "通知渠道不存在")
		return
	}
	writeV2Data(c, 200, notificationDTO(row))
}
func v2CreateNotification(c *gin.Context) { v2SaveNotification(c, 0) }
func v2UpdateNotification(c *gin.Context) {
	id, ok := v2ID(c)
	if ok {
		v2SaveNotification(c, id)
	}
}
func v2SaveNotification(c *gin.Context, id uint64) {
	var request notificationWriteDTO
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_notification", err.Error())
		return
	}
	row := model.Notification{Common: model.Common{ID: id}}
	if id > 0 && singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "notification_not_found", "通知渠道不存在")
		return
	}
	if err := applyNotificationWrite(&row, request); err != nil {
		writeV2Problem(c, 400, "invalid_notification", err.Error())
		return
	}
	if err := singleton.DB.Save(&row).Error; err != nil {
		writeV2Problem(c, 400, "notification_save_failed", err.Error())
		return
	}
	singleton.OnRefreshOrAddNotification(&row)
	status := 200
	if id == 0 {
		status = 201
	}
	writeV2Data(c, status, notificationDTO(row))
}
func v2DeleteNotification(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	result := singleton.DB.Unscoped().Delete(&model.Notification{}, "id = ?", id)
	if result.Error != nil {
		writeV2Problem(c, 500, "notification_delete_failed", result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		writeV2Problem(c, 404, "notification_not_found", "通知渠道不存在")
		return
	}
	singleton.OnDeleteNotification(id)
	c.Status(204)
}

type alertConditionDTO struct {
	Type            string          `json:"type" binding:"required"`
	Min             *float64        `json:"min"`
	Max             *float64        `json:"max"`
	DurationSeconds uint64          `json:"duration_seconds"`
	Scope           monitorScopeDTO `json:"scope" binding:"required"`
}
type alertRuleWriteDTO struct {
	Name            string              `json:"name" binding:"required"`
	Enabled         bool                `json:"enabled"`
	TriggerMode     string              `json:"trigger_mode" binding:"required"`
	NotificationTag string              `json:"notification_tag" binding:"required"`
	Conditions      []alertConditionDTO `json:"conditions" binding:"required"`
}

var alertTypes = map[string]bool{"cpu": true, "gpu": true, "temperature_max": true, "memory": true, "swap": true, "disk": true, "net_in_speed": true, "net_out_speed": true, "net_all_speed": true, "offline": true, "load1": true, "load5": true, "load15": true, "tcp_conn_count": true, "udp_conn_count": true, "process_count": true}

func scopeFromRule(row model.Rule) monitorScopeDTO {
	mode := "exclude"
	if row.Cover == model.RuleCoverIgnoreAll {
		mode = "include"
	} else if len(row.Ignore) == 0 {
		mode = "all"
	}
	ids := make([]uint64, 0, len(row.Ignore))
	for id := range row.Ignore {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return monitorScopeDTO{Mode: mode, ServerIDs: ids}
}
func alertRuleDTO(row model.AlertRule) gin.H {
	conditions := make([]gin.H, 0, len(row.Rules))
	for _, rule := range row.Rules {
		var min, max any
		if rule.Min != 0 {
			min = rule.Min
		}
		if rule.Max != 0 {
			max = rule.Max
		}
		conditions = append(conditions, gin.H{"type": rule.Type, "min": min, "max": max, "duration_seconds": rule.Duration, "scope": scopeFromRule(rule)})
	}
	mode := "always"
	if row.TriggerMode == model.ModeOnetimeTrigger {
		mode = "once"
	}
	return gin.H{"id": row.ID, "name": row.Name, "enabled": row.Enabled(), "trigger_mode": mode, "notification_tag": row.NotificationTag, "conditions": conditions, "created_at": row.CreatedAt.Format(time.RFC3339), "updated_at": row.UpdatedAt.Format(time.RFC3339)}
}
func applyAlertRuleWrite(row *model.AlertRule, request alertRuleWriteDTO) error {
	row.Name, row.NotificationTag = strings.TrimSpace(request.Name), strings.TrimSpace(request.NotificationTag)
	if row.Name == "" || row.NotificationTag == "" {
		return errors.New("name and notification_tag are required")
	}
	switch request.TriggerMode {
	case "always":
		row.TriggerMode = model.ModeAlwaysTrigger
	case "once":
		row.TriggerMode = model.ModeOnetimeTrigger
	default:
		return errors.New("trigger_mode must be always or once")
	}
	row.Enable = &request.Enabled
	if len(request.Conditions) == 0 {
		return errors.New("at least one condition is required")
	}
	row.Rules = make([]model.Rule, 0, len(request.Conditions))
	for _, condition := range request.Conditions {
		condition.Type = strings.ToLower(condition.Type)
		if !alertTypes[condition.Type] {
			return fmt.Errorf("unsupported alert condition %q", condition.Type)
		}
		rule := model.Rule{Type: condition.Type, Duration: condition.DurationSeconds, Ignore: map[uint64]bool{}}
		if condition.Min != nil {
			rule.Min = *condition.Min
		}
		if condition.Max != nil {
			rule.Max = *condition.Max
		}
		if condition.Type != "offline" && condition.Min == nil && condition.Max == nil {
			return fmt.Errorf("condition %s requires min or max", condition.Type)
		}
		if condition.Min != nil && condition.Max != nil && *condition.Min > *condition.Max {
			return fmt.Errorf("condition %s min must not exceed max", condition.Type)
		}
		switch condition.Scope.Mode {
		case "all":
			rule.Cover = model.RuleCoverAll
		case "exclude":
			rule.Cover = model.RuleCoverAll
			for _, id := range condition.Scope.ServerIDs {
				rule.Ignore[id] = true
			}
		case "include":
			if len(condition.Scope.ServerIDs) == 0 {
				return errors.New("include scope requires at least one server")
			}
			rule.Cover = model.RuleCoverIgnoreAll
			for _, id := range condition.Scope.ServerIDs {
				rule.Ignore[id] = true
			}
		default:
			return errors.New("scope mode must be all, include, or exclude")
		}
		row.Rules = append(row.Rules, rule)
	}
	return nil
}
func v2ListAlertRules(c *gin.Context) {
	v2ListNamed(c, &model.AlertRule{}, func(rows []model.AlertRule) []gin.H {
		out := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			out = append(out, alertRuleDTO(row))
		}
		return out
	})
}
func v2GetAlertRule(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.AlertRule
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "alert_rule_not_found", "告警规则不存在")
		return
	}
	writeV2Data(c, 200, alertRuleDTO(row))
}
func v2CreateAlertRule(c *gin.Context) { v2SaveAlertRule(c, 0) }
func v2UpdateAlertRule(c *gin.Context) {
	id, ok := v2ID(c)
	if ok {
		v2SaveAlertRule(c, id)
	}
}
func v2SaveAlertRule(c *gin.Context, id uint64) {
	var request alertRuleWriteDTO
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_alert_rule", err.Error())
		return
	}
	row := model.AlertRule{Common: model.Common{ID: id}}
	if id > 0 && singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "alert_rule_not_found", "告警规则不存在")
		return
	}
	if err := applyAlertRuleWrite(&row, request); err != nil {
		writeV2Problem(c, 400, "invalid_alert_rule", err.Error())
		return
	}
	if err := singleton.DB.Save(&row).Error; err != nil {
		writeV2Problem(c, 400, "alert_rule_save_failed", err.Error())
		return
	}
	singleton.OnRefreshOrAddAlert(row)
	status := 200
	if id == 0 {
		status = 201
	}
	writeV2Data(c, status, alertRuleDTO(row))
}
func v2DeleteAlertRule(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	result := singleton.DB.Unscoped().Delete(&model.AlertRule{}, "id = ?", id)
	if result.Error != nil {
		writeV2Problem(c, 500, "alert_rule_delete_failed", result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		writeV2Problem(c, 404, "alert_rule_not_found", "告警规则不存在")
		return
	}
	singleton.OnDeleteAlert(id)
	c.Status(204)
}

type ddnsWriteDTO struct {
	Name               string        `json:"name" binding:"required"`
	Provider           string        `json:"provider" binding:"required"`
	EnableIPv4         bool          `json:"enable_ipv4"`
	EnableIPv6         bool          `json:"enable_ipv6"`
	MaxRetries         uint64        `json:"max_retries"`
	Domains            []string      `json:"domains" binding:"required"`
	AccessID           string        `json:"access_id"`
	AccessSecret       string        `json:"access_secret"`
	WebhookURL         string        `json:"webhook_url"`
	WebhookMethod      string        `json:"webhook_method"`
	WebhookRequestType string        `json:"webhook_request_type"`
	WebhookBody        string        `json:"webhook_body"`
	WebhookHeaders     []keyValueDTO `json:"webhook_headers"`
}

func ddnsProviderID(name string) (uint8, bool) {
	for id, current := range model.ProviderMap {
		if current == name {
			return id, true
		}
	}
	return 0, false
}
func ddnsDTO(row model.DDNSProfile) gin.H {
	return gin.H{"id": row.ID, "name": row.Name, "provider": model.ProviderMap[row.Provider], "enable_ipv4": row.EnableIPv4 != nil && *row.EnableIPv4, "enable_ipv6": row.EnableIPv6 != nil && *row.EnableIPv6, "max_retries": row.MaxRetries, "domains": row.Domains, "access_id": row.AccessID, "access_secret": row.AccessSecret, "webhook_url": row.WebhookURL, "webhook_method": map[bool]string{true: "post", false: "get"}[row.WebhookMethod == 1], "webhook_request_type": map[bool]string{true: "form", false: "json"}[row.WebhookRequestType == 1], "webhook_body": row.WebhookRequestBody, "webhook_headers": keyValuesFromJSON(row.WebhookHeaders), "created_at": row.CreatedAt.Format(time.RFC3339), "updated_at": row.UpdatedAt.Format(time.RFC3339)}
}
func applyDDNSWrite(row *model.DDNSProfile, request ddnsWriteDTO) error {
	row.Name = strings.TrimSpace(request.Name)
	provider, ok := ddnsProviderID(strings.ToLower(request.Provider))
	if !ok {
		return errors.New("unsupported DDNS provider")
	}
	if row.Name == "" || len(request.Domains) == 0 {
		return errors.New("name and domains are required")
	}
	for index, domain := range request.Domains {
		request.Domains[index] = strings.TrimSpace(domain)
		if request.Domains[index] == "" {
			return errors.New("domain must not be empty")
		}
	}
	row.Provider = provider
	row.EnableIPv4 = &request.EnableIPv4
	row.EnableIPv6 = &request.EnableIPv6
	if !request.EnableIPv4 && !request.EnableIPv6 {
		return errors.New("at least one IP protocol must be enabled")
	}
	row.MaxRetries = request.MaxRetries
	if row.MaxRetries == 0 {
		row.MaxRetries = 3
	}
	row.Domains = request.Domains
	row.DomainsRaw = strings.Join(request.Domains, ",")
	row.AccessID, row.AccessSecret = request.AccessID, request.AccessSecret
	row.WebhookURL, row.WebhookRequestBody = request.WebhookURL, request.WebhookBody
	if strings.ToLower(request.WebhookMethod) == "post" {
		row.WebhookMethod = 1
	} else {
		row.WebhookMethod = 0
	}
	if strings.ToLower(request.WebhookRequestType) == "form" {
		row.WebhookRequestType = 1
	} else {
		row.WebhookRequestType = 0
	}
	headers, err := keyValuesJSON(request.WebhookHeaders)
	if err != nil {
		return err
	}
	row.WebhookHeaders = headers
	return nil
}
func v2ListDDNSProfiles(c *gin.Context) {
	v2ListNamed(c, &model.DDNSProfile{}, func(rows []model.DDNSProfile) []gin.H {
		out := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			out = append(out, ddnsDTO(row))
		}
		return out
	})
}
func v2GetDDNSProfile(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.DDNSProfile
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "ddns_not_found", "DDNS 配置不存在")
		return
	}
	writeV2Data(c, 200, ddnsDTO(row))
}
func v2CreateDDNSProfile(c *gin.Context) { v2SaveDDNSProfile(c, 0) }
func v2UpdateDDNSProfile(c *gin.Context) {
	id, ok := v2ID(c)
	if ok {
		v2SaveDDNSProfile(c, id)
	}
}
func v2SaveDDNSProfile(c *gin.Context, id uint64) {
	var request ddnsWriteDTO
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_ddns", err.Error())
		return
	}
	row := model.DDNSProfile{Common: model.Common{ID: id}}
	if id > 0 && singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "ddns_not_found", "DDNS 配置不存在")
		return
	}
	if err := applyDDNSWrite(&row, request); err != nil {
		writeV2Problem(c, 400, "invalid_ddns", err.Error())
		return
	}
	if err := singleton.DB.Save(&row).Error; err != nil {
		writeV2Problem(c, 400, "ddns_save_failed", err.Error())
		return
	}
	singleton.OnDDNSUpdate()
	status := 200
	if id == 0 {
		status = 201
	}
	writeV2Data(c, status, ddnsDTO(row))
}
func v2DeleteDDNSProfile(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	result := singleton.DB.Unscoped().Delete(&model.DDNSProfile{}, "id = ?", id)
	if result.Error != nil {
		writeV2Problem(c, 500, "ddns_delete_failed", result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		writeV2Problem(c, 404, "ddns_not_found", "DDNS 配置不存在")
		return
	}
	singleton.OnDDNSUpdate()
	c.Status(204)
}

type natWriteDTO struct {
	Name     string `json:"name" binding:"required"`
	ServerID uint64 `json:"server_id" binding:"required"`
	Target   string `json:"target" binding:"required"`
	Domain   string `json:"domain" binding:"required"`
}

func natDTO(row model.NAT) gin.H {
	return gin.H{"id": row.ID, "name": row.Name, "server_id": row.ServerID, "target": row.Host, "domain": row.Domain, "created_at": row.CreatedAt.Format(time.RFC3339), "updated_at": row.UpdatedAt.Format(time.RFC3339)}
}
func applyNATWrite(row *model.NAT, request natWriteDTO) error {
	row.Name, row.Host, row.Domain = strings.TrimSpace(request.Name), strings.TrimSpace(request.Target), strings.ToLower(strings.TrimSpace(request.Domain))
	row.ServerID = request.ServerID
	if row.Name == "" || row.Host == "" || row.Domain == "" || row.ServerID == 0 {
		return errors.New("name, server_id, target, and domain are required")
	}
	var count int64
	if err := singleton.DB.Model(&model.Server{}).Where("id = ?", row.ServerID).Count(&count).Error; err != nil || count == 0 {
		return errors.New("selected server does not exist")
	}
	return nil
}
func v2ListNATTunnels(c *gin.Context) {
	v2ListNamed(c, &model.NAT{}, func(rows []model.NAT) []gin.H {
		out := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			out = append(out, natDTO(row))
		}
		return out
	})
}
func v2GetNATTunnel(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.NAT
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "nat_not_found", "内网穿透不存在")
		return
	}
	writeV2Data(c, 200, natDTO(row))
}
func v2CreateNATTunnel(c *gin.Context) { v2SaveNATTunnel(c, 0) }
func v2UpdateNATTunnel(c *gin.Context) {
	id, ok := v2ID(c)
	if ok {
		v2SaveNATTunnel(c, id)
	}
}
func v2SaveNATTunnel(c *gin.Context, id uint64) {
	var request natWriteDTO
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_nat", err.Error())
		return
	}
	row := model.NAT{Common: model.Common{ID: id}}
	if id > 0 && singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "nat_not_found", "内网穿透不存在")
		return
	}
	if err := applyNATWrite(&row, request); err != nil {
		writeV2Problem(c, 400, "invalid_nat", err.Error())
		return
	}
	if err := singleton.DB.Save(&row).Error; err != nil {
		writeV2Problem(c, 400, "nat_save_failed", err.Error())
		return
	}
	singleton.OnNATUpdate()
	status := 200
	if id == 0 {
		status = 201
	}
	writeV2Data(c, status, natDTO(row))
}
func v2DeleteNATTunnel(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	result := singleton.DB.Unscoped().Delete(&model.NAT{}, "id = ?", id)
	if result.Error != nil {
		writeV2Problem(c, 500, "nat_delete_failed", result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		writeV2Problem(c, 404, "nat_not_found", "内网穿透不存在")
		return
	}
	singleton.OnNATUpdate()
	c.Status(204)
}

func v2ListNamed[T any](c *gin.Context, modelValue any, convert func([]T) []gin.H) {
	page, size := parsePage(c)
	query := singleton.DB.Model(modelValue)
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		query = query.Where("name LIKE ?", "%"+q+"%")
	}
	var total int64
	query.Count(&total)
	var rows []T
	if err := query.Order(orderClause(c, map[string]string{"id": "id", "name": "name", "created_at": "created_at", "updated_at": "updated_at"}, "id")).Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	writeV2List(c, convert(rows), v2Meta{Page: page, PageSize: size, Total: total})
}

type trafficPolicyWriteDTO struct {
	Name            string     `json:"name" binding:"required"`
	Direction       string     `json:"direction" binding:"required"`
	Mode            string     `json:"mode" binding:"required"`
	CycleStart      *time.Time `json:"cycle_start"`
	CycleInterval   uint64     `json:"cycle_interval"`
	CycleUnit       string     `json:"cycle_unit"`
	QuotaBytes      uint64     `json:"quota_bytes" binding:"required"`
	WarningPercent  float64    `json:"warning_percent"`
	NotificationTag string     `json:"notification_tag"`
	Enabled         bool       `json:"enabled"`
}

func applyTrafficPolicyWrite(row *model.TrafficPolicy, request trafficPolicyWriteDTO) error {
	row.Name = request.Name
	row.Direction = request.Direction
	row.Mode = request.Mode
	row.CycleStart = request.CycleStart
	row.CycleInterval = request.CycleInterval
	row.CycleUnit = request.CycleUnit
	row.QuotaBytes = request.QuotaBytes
	row.WarningPercent = request.WarningPercent
	if row.WarningPercent == 0 {
		row.WarningPercent = 80
	}
	row.NotificationTag = strings.TrimSpace(request.NotificationTag)
	row.Enabled = request.Enabled
	return trafficservice.Validate(row)
}
func trafficPolicyDTO(row model.TrafficPolicy) gin.H {
	return gin.H{"id": row.ID, "server_id": row.ServerID, "name": row.Name, "direction": row.Direction, "mode": row.Mode, "cycle_start": row.CycleStart, "cycle_interval": row.CycleInterval, "cycle_unit": row.CycleUnit, "quota_bytes": row.QuotaBytes, "warning_percent": row.WarningPercent, "notification_tag": row.NotificationTag, "enabled": row.Enabled, "created_at": row.CreatedAt.Format(time.RFC3339), "updated_at": row.UpdatedAt.Format(time.RFC3339)}
}
func trafficPolicyIDs(c *gin.Context) (uint64, uint64, bool) {
	serverID, ok := v2ID(c)
	if !ok {
		return 0, 0, false
	}
	policyID, err := strconv.ParseUint(c.Param("policyId"), 10, 64)
	if err != nil || policyID == 0 {
		writeV2Problem(c, 400, "invalid_policy_id", "流量策略 ID 无效")
		return 0, 0, false
	}
	return serverID, policyID, true
}
func v2ListTrafficPolicies(c *gin.Context) {
	serverID, ok := v2ID(c)
	if !ok {
		return
	}
	var rows []model.TrafficPolicy
	if err := singleton.DB.Where("server_id = ?", serverID).Order("id ASC").Find(&rows).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		item := trafficPolicyDTO(row)
		if usage, err := trafficservice.Calculate(singleton.DB, row, time.Now()); err == nil {
			item["usage"] = trafficUsageDTO(usage)
		}
		items = append(items, item)
	}
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}
func v2GetTrafficPolicy(c *gin.Context) {
	serverID, policyID, ok := trafficPolicyIDs(c)
	if !ok {
		return
	}
	var row model.TrafficPolicy
	if singleton.DB.First(&row, "id = ? AND server_id = ?", policyID, serverID).Error != nil {
		writeV2Problem(c, 404, "traffic_policy_not_found", "流量策略不存在")
		return
	}
	writeV2Data(c, 200, trafficPolicyDTO(row))
}
func v2CreateTrafficPolicy(c *gin.Context) {
	serverID, ok := v2ID(c)
	if !ok {
		return
	}
	v2SaveTrafficPolicy(c, serverID, 0)
}
func v2UpdateTrafficPolicy(c *gin.Context) {
	serverID, policyID, ok := trafficPolicyIDs(c)
	if ok {
		v2SaveTrafficPolicy(c, serverID, policyID)
	}
}
func v2SaveTrafficPolicy(c *gin.Context, serverID, policyID uint64) {
	var request trafficPolicyWriteDTO
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_traffic_policy", err.Error())
		return
	}
	var serverCount int64
	singleton.DB.Model(&model.Server{}).Where("id = ?", serverID).Count(&serverCount)
	if serverCount == 0 {
		writeV2Problem(c, 404, "server_not_found", "服务器不存在")
		return
	}
	row := model.TrafficPolicy{Common: model.Common{ID: policyID}, ServerID: serverID}
	if policyID > 0 && singleton.DB.First(&row, "id = ? AND server_id = ?", policyID, serverID).Error != nil {
		writeV2Problem(c, 404, "traffic_policy_not_found", "流量策略不存在")
		return
	}
	if err := applyTrafficPolicyWrite(&row, request); err != nil {
		writeV2Problem(c, 400, "invalid_traffic_policy", err.Error())
		return
	}
	if err := singleton.DB.Save(&row).Error; err != nil {
		writeV2Problem(c, 400, "traffic_policy_save_failed", err.Error())
		return
	}
	status := 200
	if policyID == 0 {
		status = 201
	}
	writeV2Data(c, status, trafficPolicyDTO(row))
}
func v2DeleteTrafficPolicy(c *gin.Context) {
	serverID, policyID, ok := trafficPolicyIDs(c)
	if !ok {
		return
	}
	err := singleton.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Unscoped().Delete(&model.TrafficPolicy{}, "id = ? AND server_id = ?", policyID, serverID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Unscoped().Delete(&model.TrafficPolicyState{}, "policy_id = ?", policyID).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeV2Problem(c, 404, "traffic_policy_not_found", "流量策略不存在")
		return
	}
	if err != nil {
		writeV2Problem(c, 500, "traffic_policy_delete_failed", err.Error())
		return
	}
	c.Status(204)
}
func v2TrafficPolicyUsage(c *gin.Context) {
	serverID, policyID, ok := trafficPolicyIDs(c)
	if !ok {
		return
	}
	var row model.TrafficPolicy
	if singleton.DB.First(&row, "id = ? AND server_id = ?", policyID, serverID).Error != nil {
		writeV2Problem(c, 404, "traffic_policy_not_found", "流量策略不存在")
		return
	}
	usage, err := trafficservice.Calculate(singleton.DB, row, time.Now())
	if err != nil {
		writeV2Problem(c, 500, "traffic_usage_failed", err.Error())
		return
	}
	writeV2Data(c, 200, trafficUsageDTO(usage))
}
func trafficUsageDTO(usage trafficservice.Usage) gin.H {
	return gin.H{"policy_id": usage.PolicyID, "server_id": usage.ServerID, "direction": usage.Direction, "mode": usage.Mode, "window_start": usage.WindowStart.Format(time.RFC3339), "window_end": usage.WindowEnd, "used_bytes": usage.UsedBytes, "quota_bytes": usage.QuotaBytes, "warning_percent": usage.WarningPercent, "usage_percent": usage.UsagePercent, "status": usage.Status, "updated_at": usage.UpdatedAt.Format(time.RFC3339)}
}

func trafficSummaryDTO(row trafficservice.Summary) gin.H {
	return gin.H{"policy_id": row.PolicyID, "name": row.Name, "used_bytes": row.UsedBytes, "quota_bytes": row.QuotaBytes, "usage_percent": row.UsagePercent, "status": row.Status}
}

func trafficHistoryPointDTO(point trafficservice.Point) gin.H {
	return gin.H{"window_start": point.Start.Format(time.RFC3339), "window_end": point.End.Format(time.RFC3339), "bytes": point.Bytes}
}

func trafficPointsDTO(points []trafficservice.Point) []gin.H {
	items := make([]gin.H, 0, len(points))
	for _, point := range points {
		items = append(items, trafficHistoryPointDTO(point))
	}
	return items
}

func trafficPolicyHistoryDTO(item trafficservice.PolicyHistory) gin.H {
	return gin.H{"policy_id": item.Policy.ID, "server_id": item.Policy.ServerID, "name": item.Policy.Name, "enabled": item.Policy.Enabled, "direction": item.Policy.Direction, "usage": trafficUsageDTO(item.Usage), "hourly": trafficPointsDTO(item.Hourly), "daily": trafficPointsDTO(item.Daily)}
}

func v2ServerTrafficHistory(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var server model.Server
	if singleton.DB.Select("id").First(&server, id).Error != nil {
		writeV2Problem(c, 404, "server_not_found", "服务器不存在")
		return
	}
	items, err := trafficservice.Histories(singleton.DB, id, time.Now(), trafficservice.LocationOrUTC(c.Query("tz")))
	if err != nil {
		writeV2Problem(c, 500, "traffic_history_failed", err.Error())
		return
	}
	payload := make([]gin.H, 0, len(items))
	for _, item := range items {
		payload = append(payload, trafficPolicyHistoryDTO(item))
	}
	writeV2List(c, payload, v2Meta{Page: 1, PageSize: len(payload), Total: int64(len(payload))})
}

type monitoringOptionsDTO struct {
	CPU         bool `json:"cpu"`
	Memory      bool `json:"memory"`
	Disk        bool `json:"disk"`
	Network     bool `json:"network"`
	Connections bool `json:"connections"`
	Processes   bool `json:"processes"`
	Temperature bool `json:"temperature"`
	GPU         bool `json:"gpu"`
	HostInfo    bool `json:"host_info"`
	IPReport    bool `json:"ip_report"`
	HTTPProbe   bool `json:"http_probe"`
	ICMPProbe   bool `json:"icmp_probe"`
	TCPProbe    bool `json:"tcp_probe"`
	NAT         bool `json:"nat"`
}
type installPreviewDTO struct {
	Platform       string               `json:"platform" binding:"required"`
	CleanInstall   bool                 `json:"clean_install"`
	Options        monitoringOptionsDTO `json:"options"`
	IPReportConfig ipReportConfigDTO    `json:"ip_report_config"`
	Implementation string               `json:"implementation"`
}

type ipReportConfigDTO struct {
	Interface   string `json:"interface"`
	CountryCode string `json:"country_code"`
	PreferIPv6  bool   `json:"prefer_ipv6"`
}

func v2ProbeCapabilities(c *gin.Context) {
	standardBase := monitoringOptionsDTO{CPU: true, Memory: true, Disk: true, Network: true, Connections: true, Processes: true, HostInfo: true, IPReport: true, HTTPProbe: true, ICMPProbe: true, TCPProbe: true, NAT: false}
	cloud := standardBase
	physical := standardBase
	physical.Temperature = true
	physical.GPU = true
	writeV2Data(c, 200, gin.H{
		"required": []string{"heartbeat", "identity"},
		"optional": []gin.H{
			{"id": "cpu", "disable_flag": "--disable-cpu"}, {"id": "memory", "disable_flag": "--disable-memory"}, {"id": "disk", "disable_flag": "--disable-disk"},
			{"id": "network", "disable_flag": "--disable-network"}, {"id": "connections", "disable_flag": "--disable-connections"}, {"id": "processes", "disable_flag": "--disable-processes"},
			{"id": "temperature", "enable_flag": "--temperature"}, {"id": "gpu", "enable_flag": "--gpu"}, {"id": "host_info", "disable_flag": "--disable-host-info"},
			{"id": "ip_report", "disable_flag": "--disable-ip-report"}, {"id": "http_probe", "disable_flag": "--disable-http-probe"}, {"id": "icmp_probe", "disable_flag": "--disable-icmp-probe"},
			{"id": "tcp_probe", "disable_flag": "--disable-tcp-probe"}, {"id": "nat", "disable_flag": "--disable-nat"},
		},
		"presets": gin.H{
			"standard_cloud":    cloud,
			"standard_physical": physical,
			"light":             monitoringOptionsDTO{CPU: true, Memory: true, Disk: true, Network: true, HostInfo: true, IPReport: true, HTTPProbe: true, ICMPProbe: true, TCPProbe: true, NAT: false},
			"alive":             monitoringOptionsDTO{},
		},
	})
}
func v2ServerCredential(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.Server
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "server_not_found", "服务器不存在")
		return
	}
	writeV2Data(c, 200, gin.H{"server_id": row.ID, "secret": row.Secret, "grpc_host": singleton.Conf.GRPCHost, "grpc_port": publicGRPCPort()})
}
func normalizeInstallImplementation(value string) (string, error) {
	impl := strings.ToLower(strings.TrimSpace(value))
	if impl == "" {
		return "go", nil
	}
	if impl == "go" || impl == "rust" {
		return impl, nil
	}
	return "", errors.New("implementation must be go or rust")
}

func v2InstallPreview(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.Server
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "server_not_found", "服务器不存在")
		return
	}
	var request installPreviewDTO
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_install_preview", err.Error())
		return
	}
	implementation, err := normalizeInstallImplementation(request.Implementation)
	if err != nil {
		writeV2Problem(c, 400, "invalid_install_preview", err.Error())
		return
	}
	platform := strings.ToLower(request.Platform)
	host := singleton.Conf.GRPCHost
	if host == "" {
		host = "127.0.0.1"
	}
	script := singleton.Conf.InstallScript.Linux
	if implementation == "rust" {
		if platform != "linux" {
			writeV2Problem(c, 400, "invalid_platform", "rust agent install is linux only")
			return
		}
		script = singleton.Conf.InstallScript.LinuxRust
	} else if platform == "macos" {
		script = singleton.Conf.InstallScript.MacOS
	} else if platform == "windows" {
		script = singleton.Conf.InstallScript.Windows
	}
	command, err := buildInstallCommandWithImpl(platform, script, host, publicGRPCPort(), row.Secret, request.CleanInstall, singleton.Conf.TLS, request.Options, request.IPReportConfig, resolveGRPCHintIPs(host), implementation)
	if err != nil {
		writeV2Problem(c, 400, "invalid_platform", err.Error())
		return
	}
	writeV2Data(c, 200, gin.H{"platform": platform, "command": command, "clean_install": request.CleanInstall, "options": request.Options, "ip_report_config": request.IPReportConfig, "implementation": implementation})
}

type upgradePreviewDTO struct {
	Platform string `json:"platform" binding:"required"`
}

func v2UpgradePreview(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.Server
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "server_not_found", "服务器不存在")
		return
	}
	var request upgradePreviewDTO
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_upgrade_preview", err.Error())
		return
	}
	platform := strings.ToLower(request.Platform)
	script := ""
	if singleton.Conf != nil {
		script = singleton.Conf.InstallScript.UpgradeLinux
		if platform == "macos" {
			script = singleton.Conf.InstallScript.UpgradeMacOS
		} else if platform == "windows" {
			script = singleton.Conf.InstallScript.UpgradeWindows
		}
	}
	command, err := buildUpgradeCommand(platform, script)
	if err != nil {
		writeV2Problem(c, 400, "invalid_platform", err.Error())
		return
	}
	writeV2Data(c, 200, gin.H{"platform": platform, "command": command})
}
func publicGRPCPort() uint {
	if singleton.Conf != nil && singleton.Conf.ProxyGRPCPort != 0 {
		return singleton.Conf.ProxyGRPCPort
	}
	if singleton.Conf != nil && singleton.Conf.GRPCPort != 0 {
		return singleton.Conf.GRPCPort
	}
	return 5555
}

var lookupGRPCHost = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

func resolveGRPCHintIPs(host string) []string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || net.ParseIP(host) != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addresses, err := lookupGRPCHost(ctx, host)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address.IP == nil || address.IP.IsUnspecified() || address.IP.IsMulticast() {
			continue
		}
		value := address.IP.String()
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func appendServerIPFlags(parts []string, hintIPs []string, windows bool) []string {
	for _, ip := range hintIPs {
		if net.ParseIP(ip) == nil {
			continue
		}
		if windows {
			parts = append(parts, "-ServerIP", powershellQuote(ip))
			continue
		}
		parts = append(parts, "--server-ip", shellQuote(ip))
	}
	return parts
}

func buildInstallCommand(platform, script, host string, port uint, secret string, clean, useTLS bool, options monitoringOptionsDTO, ipCfg ipReportConfigDTO, hintIPs []string) (string, error) {
	return buildInstallCommandWithImpl(platform, script, host, port, secret, clean, useTLS, options, ipCfg, hintIPs, "go")
}

func buildInstallCommandWithImpl(platform, script, host string, port uint, secret string, clean, useTLS bool, options monitoringOptionsDTO, ipCfg ipReportConfigDTO, hintIPs []string, implementation string) (string, error) {
	impl, err := normalizeInstallImplementation(implementation)
	if err != nil {
		return "", err
	}
	if impl == "rust" {
		if platform != "linux" {
			return "", errors.New("rust agent install is linux only")
		}
		parts := []string{"curl -fsSL", shellQuote(script), "| bash -s --", shellQuote(host), strconv.FormatUint(uint64(port), 10), shellQuote(secret)}
		if clean {
			parts = append(parts, "--clean-install", "--confirm-clean-install")
		}
		if useTLS {
			parts = append(parts, "--tls")
		}
		return strings.Join(parts, " "), nil
	}
	flags := installFlags(options, platform == "windows", ipCfg)
	switch platform {
	case "linux", "macos":
		parts := []string{"curl -fsSL", shellQuote(script), "| bash -s --", shellQuote(host), strconv.FormatUint(uint64(port), 10), shellQuote(secret)}
		if clean {
			parts = append(parts, "--clean-install", "--confirm-clean-install")
		}
		if useTLS {
			parts = append(parts, "--tls")
		}
		parts = appendServerIPFlags(parts, hintIPs, false)
		parts = append(parts, flags...)
		return strings.Join(parts, " "), nil
	case "windows":
		parts := []string{"& ([scriptblock]::Create((irm", powershellQuote(script), "))) -Server", powershellQuote(host), "-Port", strconv.FormatUint(uint64(port), 10), "-Key", powershellQuote(secret)}
		if clean {
			parts = append(parts, "-CleanInstall", "-ConfirmCleanInstall")
		}
		if useTLS {
			parts = append(parts, "-Tls")
		}
		parts = appendServerIPFlags(parts, hintIPs, true)
		parts = append(parts, flags...)
		return strings.Join(parts, " "), nil
	default:
		return "", errors.New("platform must be linux, macos, or windows")
	}
}

func buildUpgradeCommand(platform, script string) (string, error) {
	if strings.TrimSpace(script) == "" {
		return "", errors.New("upgrade script url is empty")
	}
	switch platform {
	case "linux":
		return strings.Join([]string{"curl -fsSL", shellQuote(script), "| bash"}, " "), nil
	case "macos":
		return strings.Join([]string{"curl -fsSL", shellQuote(script), "| sudo bash"}, " "), nil
	case "windows":
		return strings.Join([]string{"& ([scriptblock]::Create((irm", powershellQuote(script), ")))"}, " "), nil
	default:
		return "", errors.New("platform must be linux, macos, or windows")
	}
}

type scriptCommandDTO struct {
	ID          string `json:"id"`
	Group       string `json:"group"`
	Platform    string `json:"platform"`
	Command     string `json:"command"`
	Destructive bool   `json:"destructive"`
}

func buildDashboardInstallCommand(script string) string {
	return `sh -c "$(curl -fsSL ` + shellQuote(script) + `)"`
}

func appendUpgradeScriptCommand(out []scriptCommandDTO, id, group, platform, script string) []scriptCommandDTO {
	command, err := buildUpgradeCommand(platform, script)
	if err != nil {
		return out
	}
	return append(out, scriptCommandDTO{ID: id, Group: group, Platform: platform, Command: command})
}

func buildScriptCommands(conf *model.Config) []scriptCommandDTO {
	out := make([]scriptCommandDTO, 0, 9)
	if conf == nil {
		return out
	}
	scripts := conf.InstallScript
	if strings.TrimSpace(scripts.Dashboard) != "" {
		out = append(out, scriptCommandDTO{
			ID:       "dashboard_install",
			Group:    "dashboard",
			Platform: "linux",
			Command:  buildDashboardInstallCommand(scripts.Dashboard),
		})
	}
	out = append(out, scriptCommandDTO{
		ID:       "dashboard_upgrade",
		Group:    "dashboard",
		Platform: "linux",
		Command:  "cd /opt/santaizi && docker compose pull && docker compose up -d",
	})
	out = appendUpgradeScriptCommand(out, "collector_upgrade", "collector", "linux", scripts.UpgradeCollector)
	out = append(out, scriptCommandDTO{
		ID:          "collector_remove",
		Group:       "collector",
		Platform:    "linux",
		Command:     "cd /opt/santaizi/collector && docker compose down",
		Destructive: true,
	})
	out = appendUpgradeScriptCommand(out, "agent_upgrade_linux", "agent", "linux", scripts.UpgradeLinux)
	out = appendUpgradeScriptCommand(out, "agent_upgrade_macos", "agent", "macos", scripts.UpgradeMacOS)
	out = appendUpgradeScriptCommand(out, "agent_upgrade_windows", "agent", "windows", scripts.UpgradeWindows)
	out = append(out, scriptCommandDTO{
		ID:          "agent_uninstall_posix",
		Group:       "agent",
		Platform:    "posix",
		Command:     "santaizi-agent-uninstall",
		Destructive: true,
	})
	out = append(out, scriptCommandDTO{
		ID:          "agent_uninstall_windows",
		Group:       "agent",
		Platform:    "windows",
		Command:     `C:\santaizi\santaizi-agent-uninstall.cmd`,
		Destructive: true,
	})
	return out
}

func v2ScriptCommands(c *gin.Context) {
	writeV2Data(c, 200, gin.H{"commands": buildScriptCommands(singleton.Conf)})
}
func installFlags(options monitoringOptionsDTO, windows bool, ipCfg ipReportConfigDTO) []string {
	pairs := []struct {
		enabled   bool
		positive  bool
		unix, win string
	}{{options.CPU, false, "--disable-cpu", "-DisableCPU"}, {options.Memory, false, "--disable-memory", "-DisableMemory"}, {options.Disk, false, "--disable-disk", "-DisableDisk"}, {options.Network, false, "--disable-network", "-DisableNetwork"}, {options.Connections, false, "--disable-connections", "-DisableConnections"}, {options.Processes, false, "--disable-processes", "-DisableProcesses"}, {options.Temperature, true, "--temperature", "-Temperature"}, {options.GPU, true, "--gpu", "-GPU"}, {options.HostInfo, false, "--disable-host-info", "-DisableHostInfo"}, {options.IPReport, false, "--disable-ip-report", "-DisableIPReport"}, {options.HTTPProbe, false, "--disable-http-probe", "-DisableHTTPProbe"}, {options.ICMPProbe, false, "--disable-icmp-probe", "-DisableICMPProbe"}, {options.TCPProbe, false, "--disable-tcp-probe", "-DisableTCPProbe"}, {options.NAT, false, "--disable-nat", "-DisableNAT"}}
	out := []string{}
	for _, pair := range pairs {
		include := pair.enabled
		if !pair.positive {
			include = !pair.enabled
		}
		if include {
			if windows {
				out = append(out, pair.win)
			} else {
				out = append(out, pair.unix)
			}
		}
	}
	if options.IPReport {
		iface := strings.TrimSpace(ipCfg.Interface)
		code := strings.TrimSpace(ipCfg.CountryCode)
		if windows {
			if iface != "" {
				out = append(out, "-IpReportInterface", powershellQuote(iface))
			}
			if code != "" {
				out = append(out, "-CountryCode", powershellQuote(code))
			}
			if ipCfg.PreferIPv6 {
				out = append(out, "-UseIPv6CountryCode")
			}
		} else {
			if iface != "" {
				out = append(out, "--ip-report-interface", shellQuote(iface))
			}
			if code != "" {
				out = append(out, "--country-code", shellQuote(code))
			}
			if ipCfg.PreferIPv6 {
				out = append(out, "--use-ipv6-countrycode")
			}
		}
	}
	return out
}
func shellQuote(value string) string      { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func powershellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

type collectorInstallPreviewDTO struct {
	PrimaryEndpoint    string `json:"primary_endpoint"`
	PrimaryTLS         bool   `json:"primary_tls"`
	PrimaryInsecureTLS bool   `json:"primary_insecure_tls"`
	GRPCPort           int    `json:"grpc_port"`
}

func parseCollectorListenPort(address string) (int, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return 5556, nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		// address may be host without port
		if !strings.Contains(address, ":") {
			return 5556, nil
		}
		return 0, fmt.Errorf("invalid collector address: %w", err)
	}
	_ = host
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return 0, errors.New("collector address port must be between 1 and 65535")
	}
	return value, nil
}

func normalizeCollectorListenPort(listenPort uint, address string) (uint, error) {
	if listenPort > 65535 {
		return 0, errors.New("listen_port must be between 0 and 65535")
	}
	if listenPort != 0 {
		return listenPort, nil
	}
	if strings.TrimSpace(address) == "" {
		return 0, nil
	}
	parsed, err := parseCollectorListenPort(address)
	if err != nil {
		return 0, err
	}
	return uint(parsed), nil
}

func resolveCollectorInstallPort(listenPort uint, address string, requested int) (int, error) {
	if requested != 0 {
		if requested < 1 || requested > 65535 {
			return 0, errors.New("grpc_port must be between 1 and 65535")
		}
		return requested, nil
	}
	if listenPort != 0 {
		if listenPort > 65535 {
			return 0, errors.New("listen_port must be between 1 and 65535")
		}
		return int(listenPort), nil
	}
	return parseCollectorListenPort(address)
}

func buildCollectorInstallCommand(script, endpoint, token string, grpcPort int, primaryTLS, primaryInsecureTLS bool) string {
	parts := []string{
		"curl -fsSL", shellQuote(script), "| bash -s --",
		"--primary-endpoint", shellQuote(endpoint),
		"--token", shellQuote(token),
		"--grpc-port", strconv.Itoa(grpcPort),
		"--primary-tls", strconv.FormatBool(primaryTLS),
		"--primary-insecure-tls", strconv.FormatBool(primaryInsecureTLS),
	}
	return strings.Join(parts, " ")
}

func decodePublicNote(raw string) any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	var result any
	if json.Unmarshal([]byte(raw), &result) != nil {
		return map[string]any{"text": raw}
	}
	return result
}
