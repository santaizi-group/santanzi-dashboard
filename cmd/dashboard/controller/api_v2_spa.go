package controller

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/mygin"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"github.com/hi2shark/santaizi-dashboard/service/telemetry"
	trafficservice "github.com/hi2shark/santaizi-dashboard/service/traffic"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const ddnsRedactedPlaceholder = "••••••" // #nosec G101 -- UI mask sentinel, not a credential

var DatabaseMaintainer *telemetry.DatabaseMaintainer

type v2Problem struct {
	Type    string              `json:"type,omitempty"`
	Title   string              `json:"title"`
	Status  int                 `json:"status"`
	Code    string              `json:"code"`
	Detail  string              `json:"detail,omitempty"`
	TraceID string              `json:"trace_id"`
	Errors  map[string][]string `json:"errors,omitempty"`
}

type v2Meta struct {
	Page       int    `json:"page,omitempty"`
	PageSize   int    `json:"page_size,omitempty"`
	Total      int64  `json:"total,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func registerSPAAPIV2(root gin.IRouter) {
	auth := root.Group("v2/auth")
	auth.Use(mygin.Authorize(mygin.AuthorizeOption{AllowAPI: true}))
	auth.GET("/session", v2Session)
	auth.POST("/logout", v2Logout)

	public := root.Group("v2/public")
	public.Use(mygin.Authorize(mygin.AuthorizeOption{AllowAPI: true}))
	public.Use(mygin.ValidateViewPassword(mygin.ValidateViewPasswordOption{IsPage: false, AbortWhenFail: false}))
	public.GET("/bootstrap", v2PublicBootstrap)
	public.POST("/view-password/session", v2ViewPasswordSession)
	public.GET("/servers", v2PublicServers)
	public.GET("/servers/:id", v2PublicServer)
	public.GET("/servers/:id/availability", v2PublicServerAvailability)
	public.GET("/services", v2PublicServices)
	public.GET("/network/:id", v2PublicNetwork)
	public.GET("/cycle-transfer", v2PublicCycleTransfer)
	public.GET("/metrics/:id", v2PublicMetrics)

	admin := root.Group("v2/admin")
	admin.Use(mygin.Authorize(mygin.AuthorizeOption{AllowAPI: true}))
	admin.Use(v2RequireAdmin)
	admin.Use(mygin.RejectReadOnlyAPITokenWrites())
	admin.GET("/summary", v2AdminSummary)
	admin.GET("/connections/summary", v2ConnectionSummary)
	admin.GET("/connections/paths", v2ConnectionPaths)
	admin.GET("/connections/latency", v2ConnectionLatency)
	admin.GET("/probes/summary", v2ProbeSummary)
	admin.GET("/probes/paths", v2ProbePaths)
	admin.GET("/probes/samples", v2ProbeSamples)
	admin.GET("/probes/trace", v2ProbeTrace)
	admin.GET("/servers", v2AdminServers)
	admin.POST("/servers", v2CreateServer)
	admin.GET("/servers/export", v2ExportServers)
	admin.POST("/servers/import/preview", v2PreviewServerImport)
	admin.POST("/servers/import", v2ImportServers)
	admin.GET("/servers/:id", v2AdminServer)
	admin.GET("/servers/:id/availability", v2AdminServerAvailability)
	admin.PATCH("/servers/:id", v2UpdateServer)
	admin.PATCH("/servers/:id/display-index", v2UpdateServerDisplayIndex)
	admin.DELETE("/servers/:id", v2DeleteServer)
	admin.POST("/servers/:id/reset-secret", v2ResetServerSecret)
	admin.POST("/servers/:id/reset-availability", v2ResetAvailability)
	admin.POST("/servers/batch/group", v2BatchServerGroup)
	admin.POST("/servers/batch/delete", v2BatchServerDelete)
	admin.GET("/server-groups", v2ListServerGroups)
	admin.POST("/server-groups/rename", v2RenameServerGroup)
	registerTypedAdminRoutes(admin)
	admin.GET("/monitors/:id/history", v2MonitorHistory)
	admin.POST("/notifications/:id/test", v2TestNotification)
	admin.GET("/ddns/providers", v2DDNSProviders)
	admin.GET("/settings", v2GetSettings)
	admin.PATCH("/settings", v2UpdateSettings)
	admin.GET("/api-tokens", v2ListAPITokens)
	admin.POST("/api-tokens", v2CreateAPIToken)
	admin.GET("/api-tokens/:id", v2GetAPIToken)
	admin.PATCH("/api-tokens/:id", v2PatchAPIToken)
	admin.DELETE("/api-tokens/:id", v2DeleteAPIToken)
	admin.GET("/offline-history", v2OfflineHistory)
	admin.DELETE("/offline-history/:id", v2DeleteOfflineHistory)
	admin.POST("/offline-history/cleanup", v2CleanupOfflineHistory)
	admin.GET("/database", v2GetDatabase)
	admin.POST("/database/optimize", v2OptimizeDatabase)

	telemetry := admin.Group("/telemetry")
	telemetry.GET("/overview", v2TelemetryOverview)
	telemetry.GET("/collectors", v2Collectors)
	telemetry.POST("/collectors", v2CreateCollector)
	telemetry.GET("/collectors/:id", v2Collector)
	telemetry.PATCH("/collectors/:id", v2UpdateCollector)
	telemetry.DELETE("/collectors/:id", v2DeleteCollector)
	telemetry.POST("/collectors/:id/rotate-token", v2RotateCollector)
	telemetry.GET("/collectors/:id/token", v2CollectorToken)
	telemetry.POST("/collectors/:id/revoke", v2RevokeCollector)
	telemetry.PUT("/collectors/:id/scope", v2UpdateCollectorScope)
	telemetry.POST("/collectors/:id/install-preview", v2CollectorInstallPreview)
	for _, dataset := range []string{"assignments", "agents", "incidents", "incident-revisions", "data-loss", "alerts"} {
		name := dataset
		telemetry.GET("/"+name, func(c *gin.Context) {
			c.Params = append(c.Params, gin.Param{Key: "dataset", Value: name})
			v2TelemetryDataset(c)
		})
	}
	telemetry.GET("/:dataset", v2TelemetryDataset)
}

func v2RequireAdmin(c *gin.Context) {
	if _, ok := c.Get(model.CtxKeyAuthorizedUser); !ok {
		writeV2Problem(c, http.StatusUnauthorized, "authentication_required", "管理员会话已过期或 API Token 无效")
		return
	}
	c.Next()
}

func writeV2Problem(c *gin.Context, status int, code, detail string) {
	trace := make([]byte, 8)
	_, _ = rand.Read(trace)
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(status, v2Problem{Type: "https://santaizi.dev/problems/" + code, Title: http.StatusText(status), Status: status, Code: code, Detail: detail, TraceID: hex.EncodeToString(trace)})
}

func writeV2Data(c *gin.Context, status int, data any) { c.JSON(status, gin.H{"data": data}) }

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func serverOnline(lastActive time.Time) bool {
	if lastActive.IsZero() {
		return false
	}
	return time.Since(lastActive) < time.Duration(singleton.Conf.Telemetry.OfflineThresholdSeconds)*time.Second
}

func serverOnlineFlag(server model.Server, presentation runtimeServerResponse) bool {
	if presentation.Protocol == "v2" && presentation.NodeUUID != "" {
		return presentation.HostState == model.HostStateOnline
	}
	return serverOnline(server.LastActive)
}

func writeV2List(c *gin.Context, data any, meta v2Meta) {
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": meta})
}

func parsePage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

func orderClause(c *gin.Context, allowed map[string]string, fallback string) string {
	column := fallback
	if requested := strings.TrimSpace(c.Query("sort")); requested != "" {
		if safe, ok := allowed[requested]; ok {
			column = safe
		}
	}
	direction := "DESC"
	if strings.EqualFold(c.Query("order"), "asc") {
		direction = "ASC"
	}
	return column + " " + direction
}

func requireV2PublicAccess(c *gin.Context) bool {
	if singleton.Conf.Site.ViewPassword == "" {
		return true
	}
	if _, ok := c.Get(model.CtxKeyAuthorizedUser); ok {
		return true
	}
	if _, ok := c.Get(model.CtxKeyViewPasswordVerified); ok {
		return true
	}
	writeV2Problem(c, http.StatusForbidden, "view_password_required", "公开状态页需要查看密码")
	return false
}

func isAPITokenRequest(c *gin.Context) bool {
	value, ok := c.Get(model.CtxKeyIsAPI)
	return ok && value == true
}

func publicHostView(c *gin.Context, host *model.Host) any {
	if isAPITokenRequest(c) {
		return hostAdminDTO(host)
	}
	return host
}

func v2Session(c *gin.Context) {
	state := gin.H{"authenticated": false, "csrf_token": mygin.CSRFToken(c), "login_url": "/oauth2/login", "capabilities": []string{}, "version": singleton.Version}
	if value, ok := c.Get(model.CtxKeyAuthorizedUser); ok {
		user := value.(*model.User)
		state["authenticated"] = true
		state["user"] = gin.H{"id": user.ID, "login": user.Login, "name": user.Name, "avatar_url": user.AvatarURL, "super_admin": user.SuperAdmin}
		capabilities := []string{"servers:write", "monitors:write", "notifications:write", "network:write", "telemetry:write", "settings:write"}
		if isAPI, _ := c.Get(model.CtxKeyIsAPI); isAPI == true {
			if perm, _ := c.Get(model.CtxKeyAPITokenPermission); perm == model.ApiTokenPermissionRead {
				capabilities = []string{"servers:read", "monitors:read", "notifications:read", "network:read", "telemetry:read", "settings:read"}
			}
		}
		state["capabilities"] = capabilities
	}
	writeV2Data(c, http.StatusOK, state)
}

func v2Logout(c *gin.Context) {
	value, ok := c.Get(model.CtxKeyAuthorizedUser)
	if ok {
		user := value.(*model.User)
		_ = singleton.DB.Model(user).Updates(map[string]any{"token": "", "token_expired": time.Now()}).Error
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: singleton.Conf.Site.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	c.Status(http.StatusNoContent)
}

func v2PublicBootstrap(c *gin.Context) {
	_, member := c.Get(model.CtxKeyAuthorizedUser)
	_, verified := c.Get(model.CtxKeyViewPasswordVerified)
	writeV2Data(c, http.StatusOK, gin.H{
		"brand": singleton.Conf.Site.Brand, "locale": singleton.Conf.Language, "version": singleton.Version,
		"csrf_token": mygin.CSRFToken(c),
		"logo_url":   safeAssetURL(singleton.Conf.Site.LogoURL, "/static/logo.svg"), "background_url": safeAssetURL(singleton.Conf.Site.BackgroundURL, "/static/theme-server-status/img/bg.jpg"),
		"footer_text": singleton.Conf.Site.FooterText, "primary_color": singleton.Conf.Site.PrimaryColor, "custom_css": singleton.Conf.Site.SafeCustomCSS,
		"requires_view_password": singleton.Conf.Site.ViewPassword != "", "view_password_verified": member || verified,
		"show_availability": singleton.Conf.ShowAvailabilityToGuest, "authenticated": member,
		"theme":                       model.NormalizePublicTheme(singleton.Conf.Site.Theme),
		"allow_frontend_theme_switch": !singleton.Conf.DisableSwitchTemplateInFrontend,
	})
}

type viewPasswordJSON struct {
	Password string `json:"password" binding:"required"`
}

func v2ViewPasswordSession(c *gin.Context) {
	var request viewPasswordJSON
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if request.Password != singleton.Conf.Site.ViewPassword {
		writeV2Problem(c, http.StatusForbidden, "invalid_view_password", "查看密码不正确")
		return
	}
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "password_session_failed", err.Error())
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: singleton.Conf.Site.CookieName + "-vp", Value: string(hashBytes), Path: "/", MaxAge: 86400, Secure: oauthCookieSecure(c), HttpOnly: true, SameSite: http.SameSiteLaxMode})
	c.Status(http.StatusNoContent)
}

func safeAssetURL(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if strings.HasPrefix(value, "/static/") || strings.HasPrefix(value, "data:image/") {
		return value
	}
	return fallback
}

func v2PublicServers(c *gin.Context) {
	if !requireV2PublicAccess(c) {
		return
	}
	servers := publicServerSnapshot(c)
	writeV2List(c, servers, v2Meta{Page: 1, PageSize: len(servers), Total: int64(len(servers))})
}

func v2PublicServer(c *gin.Context) {
	if !requireV2PublicAccess(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", err.Error())
		return
	}
	for _, server := range publicServerSnapshot(c) {
		if server["id"] == id {
			writeV2Data(c, http.StatusOK, server)
			return
		}
	}
	writeV2Problem(c, http.StatusNotFound, "server_not_found", "服务器不存在")
}

func publicServerSnapshot(c *gin.Context) []gin.H {
	_, member := c.Get(model.CtxKeyAuthorizedUser)
	_, verified := c.Get(model.CtxKeyViewPasswordVerified)
	tokenFull := isAPITokenRequest(c)
	singleton.SortedServerLock.RLock()
	defer singleton.SortedServerLock.RUnlock()
	source := singleton.SortedServerListForGuest
	if member || verified {
		source = singleton.SortedServerList
	}
	servers := make([]gin.H, 0, len(source))
	for _, running := range source {
		item := *running
		presentation := runtimeForServer(item)
		telemetry := gin.H{"host": presentation.HostState, "connectivity": presentation.Connectivity, "available": presentation.Availability, "coverage": presentation.Coverage}
		row := gin.H{
			"id": item.ID, "name": item.Name, "tag": item.Tag, "public_note": decodePublicNote(item.PublicNote),
			"display_index": item.DisplayIndex, "hide_for_guest": item.HideForGuest, "enable_ddns": item.EnableDDNS,
			"host": publicHostView(c, item.Host), "state": item.State, "last_active": formatOptionalTime(item.LastActive),
			"online":    serverOnlineFlag(item, presentation),
			"telemetry": telemetry,
		}
		if tokenFull {
			monitoringOptions := map[string]bool{}
			_ = json.Unmarshal([]byte(item.MonitoringOptionsRaw), &monitoringOptions)
			row["note"] = item.Note
			row["monitoring_options"] = monitoringOptions
			row["ddns_profiles"] = item.DDNSProfiles
		}
		servers = append(servers, row)
	}
	return servers
}

func v2PublicServices(c *gin.Context) {
	if !requireV2PublicAccess(c) {
		return
	}
	stats := singleton.ServiceSentinelShared.LoadStats()
	items := make([]any, 0, len(stats))
	for _, item := range stats {
		if !item.Monitor.EnableShowInService {
			continue
		}
		output, _ := snakeValue(item).(map[string]any)
		if monitor, ok := output["monitor"].(map[string]any); ok {
			output["id"], output["name"], output["type"] = monitor["id"], monitor["name"], monitor["type"]
		}
		items = append(items, output)
	}
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}

func v2PublicNetwork(c *gin.Context) {
	if !requireV2PublicAccess(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", err.Error())
		return
	}
	if !publicServerIDVisible(c, id) {
		writeV2Problem(c, http.StatusNotFound, "server_not_found", "服务器不存在")
		return
	}
	rows := singleton.MonitorAPI.GetMonitorHistories(map[string]any{"server_id": id})
	items := publicNetworkHistoryItems(rows)
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}

func v2PublicCycleTransfer(c *gin.Context) {
	if !requireV2PublicAccess(c) {
		return
	}
	query := singleton.DB.Model(&model.TrafficPolicy{}).Where("enabled = ?", true)
	if raw := strings.TrimSpace(c.Query("server_id")); raw != "" {
		serverID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || serverID == 0 {
			writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", "server_id 无效")
			return
		}
		query = query.Where("server_id = ?", serverID)
	}
	var rows []model.TrafficPolicy
	if err := query.Order("server_id ASC, id ASC").Find(&rows).Error; err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	now := time.Now()
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		item := gin.H{
			"policy_id":  row.ID,
			"server_id":  row.ServerID,
			"name":       row.Name,
			"direction":  row.Direction,
			"mode":       row.Mode,
			"cycle_unit": row.CycleUnit,
		}
		if usage, err := trafficservice.Calculate(singleton.DB, row, now); err == nil {
			item["window_start"] = usage.WindowStart.Format(time.RFC3339)
			if usage.WindowEnd != nil {
				item["window_end"] = usage.WindowEnd.Format(time.RFC3339)
				item["next_reset_at"] = usage.WindowEnd.Format(time.RFC3339)
			} else {
				item["next_reset_at"] = nil
			}
			item["used_bytes"] = usage.UsedBytes
			item["quota_bytes"] = usage.QuotaBytes
			item["usage_percent"] = usage.UsagePercent
			item["status"] = usage.Status
			item["warning_percent"] = usage.WarningPercent
			item["warning_bytes"] = publicWarningBytes(usage.QuotaBytes, usage.WarningPercent)
			item["remaining_bytes"] = publicRemainingBytes(usage.UsedBytes, usage.QuotaBytes)
		}
		items = append(items, item)
	}
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}

func v2AdminSummary(c *gin.Context) {
	var total, incidents, losses, alerts int64
	singleton.DB.Model(&model.Server{}).Count(&total)
	singleton.DB.Model(&model.AvailabilityIncident{}).Where("ended_at = 0").Count(&incidents)
	singleton.DB.Model(&model.TelemetryDataLoss{}).Count(&losses)
	singleton.DB.Model(&model.TelemetryAlert{}).Count(&alerts)
	singleton.SortedServerLock.RLock()
	probes := make([]struct {
		id         uint64
		lastActive time.Time
	}, 0, len(singleton.SortedServerList))
	for _, server := range singleton.SortedServerList {
		probes = append(probes, struct {
			id         uint64
			lastActive time.Time
		}{id: server.ID, lastActive: server.LastActive})
	}
	singleton.SortedServerLock.RUnlock()
	online := int64(0)
	threshold := time.Duration(singleton.Conf.Telemetry.OfflineThresholdSeconds) * time.Second
	for _, probe := range probes {
		if offline, ok, err := model.ServerConsensusOffline(singleton.DB, probe.id); err == nil && ok {
			if !offline {
				online++
			}
			continue
		}
		if time.Since(probe.lastActive) < threshold {
			online++
		}
	}
	connection, err := telemetry.LoadConnectionSummary(singleton.DB, time.Now())
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeV2Data(c, http.StatusOK, gin.H{
		"total_servers": total, "online_servers": online, "active_collectors": connection.CollectorsOnline,
		"active_incidents": incidents, "data_loss": losses, "telemetry_alerts": alerts, "telemetry_pending": 0,
		"collectors_offline": connection.CollectorsOffline, "paths_assigned": connection.PathsAssigned, "paths_connected": connection.PathsConnected,
	})
}

type serverV2Write struct {
	Name              string                    `json:"name" binding:"required"`
	Tag               string                    `json:"tag"`
	Note              string                    `json:"note"`
	PublicNote        map[string]any            `json:"public_note"`
	MonitoringOptions map[string]bool           `json:"monitoring_options"`
	DisplayIndex      int                       `json:"display_index"`
	HideForGuest      bool                      `json:"hide_for_guest"`
	EnableDDNS        bool                      `json:"enable_ddns"`
	DDNSProfiles      []uint64                  `json:"ddns_profiles"`
	TrafficPolicies   *[]trafficPolicyUpsertDTO `json:"traffic_policies"`
	ProbeTarget       string                    `json:"probe_target"`
	ProbeTCPPorts     string                    `json:"probe_tcp_ports"`
	ProbeEnableICMP   *bool                     `json:"probe_enable_icmp"`
	ProbeEnableTCP    *bool                     `json:"probe_enable_tcp"`
	ProbeEnableMTR    *bool                     `json:"probe_enable_mtr"`
}

type trafficPolicyUpsertDTO struct {
	ID uint64 `json:"id"`
	trafficPolicyWriteDTO
}

func hostAdminDTO(host *model.Host) any {
	if host == nil {
		return nil
	}
	encoded, err := json.Marshal(host)
	if err != nil {
		return nil
	}
	var view map[string]any
	if err := json.Unmarshal(encoded, &view); err != nil {
		return nil
	}
	if view == nil {
		view = map[string]any{}
	}
	ip := strings.TrimSpace(host.IP)
	if ip == "" {
		return view
	}
	view["ip"] = ip
	ipv4, ipv6, _ := utils.SplitIPAddr(ip)
	if ipv4 != "" {
		view["ipv4"] = ipv4
	}
	if ipv6 != "" {
		view["ipv6"] = ipv6
	}
	return view
}

func serverAdminDTO(server model.Server, reveal bool) gin.H {
	monitoringOptions := map[string]bool{}
	_ = json.Unmarshal([]byte(server.MonitoringOptionsRaw), &monitoringOptions)
	result := gin.H{"id": server.ID, "name": server.Name, "tag": server.Tag, "note": server.Note, "public_note": decodePublicNote(server.PublicNote), "monitoring_options": monitoringOptions, "display_index": server.DisplayIndex, "hide_for_guest": server.HideForGuest, "enable_ddns": server.EnableDDNS, "ddns_profiles": server.DDNSProfiles, "probe_target": server.ProbeTarget, "probe_tcp_ports": server.ProbeTCPPorts, "probe_enable_icmp": model.BoolOrTrue(server.ProbeEnableICMP), "probe_enable_tcp": model.BoolOrTrue(server.ProbeEnableTCP), "probe_enable_mtr": model.BoolOrTrue(server.ProbeEnableMTR), "last_active": formatOptionalTime(server.LastActive), "host": hostAdminDTO(server.Host), "state": server.State}
	if reveal {
		result["secret"] = server.Secret
	}
	runtime := runtimeForServer(server)
	result["online"] = serverOnlineFlag(server, runtime)
	result["telemetry"] = gin.H{"host": runtime.HostState, "connectivity": runtime.Connectivity, "available": runtime.Availability, "coverage": runtime.Coverage}
	return result
}

func v2AdminServers(c *gin.Context) {
	page, size := parsePage(c)
	query := singleton.DB.Model(&model.Server{})
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		like := "%" + q + "%"
		query = query.Where("name LIKE ? OR tag LIKE ? OR note LIKE ?", like, like, like)
	}
	var total int64
	query.Count(&total)
	var servers []model.Server
	order := orderClause(c, map[string]string{"id": "id", "name": "name", "tag": "tag", "display_index": "display_index", "last_active": "last_active"}, "display_index")
	if err := query.Order(order + ", id ASC").Offset((page - 1) * size).Limit(size).Find(&servers).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	items := make([]gin.H, 0, len(servers))
	singleton.ServerLock.RLock()
	for _, server := range servers {
		if running := singleton.ServerList[server.ID]; running != nil {
			server.CopyFromRunningServer(running)
		}
		items = append(items, serverAdminDTO(server, false))
	}
	singleton.ServerLock.RUnlock()
	if err := attachTrafficSummaries(items); err != nil {
		writeV2Problem(c, 500, "traffic_usage_failed", err.Error())
		return
	}
	writeV2List(c, items, v2Meta{Page: page, PageSize: size, Total: total})
}

func attachTrafficSummaries(items []gin.H) error {
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		id, ok := item["id"].(uint64)
		if !ok {
			continue
		}
		ids = append(ids, id)
	}
	summaries, err := trafficservice.Summaries(singleton.DB, ids, time.Now())
	if err != nil {
		return err
	}
	for _, item := range items {
		id, _ := item["id"].(uint64)
		rows := summaries[id]
		payload := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			payload = append(payload, trafficSummaryDTO(row))
		}
		item["traffic_summaries"] = payload
	}
	return nil
}

func v2AdminServer(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var server model.Server
	if err := singleton.DB.First(&server, id).Error; err != nil {
		writeV2Problem(c, 404, "server_not_found", "服务器不存在")
		return
	}
	singleton.ServerLock.RLock()
	if running := singleton.ServerList[id]; running != nil {
		server.CopyFromRunningServer(running)
	}
	singleton.ServerLock.RUnlock()
	writeV2Data(c, http.StatusOK, serverAdminDTO(server, false))
}

func v2ID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		writeV2Problem(c, http.StatusBadRequest, "invalid_id", "资源 ID 无效")
		return 0, false
	}
	return id, true
}

func v2CreateServer(c *gin.Context) { v2SaveServer(c, 0) }
func v2UpdateServer(c *gin.Context) {
	id, ok := v2ID(c)
	if ok {
		v2SaveServer(c, id)
	}
}

func v2SaveServer(c *gin.Context, id uint64) {
	var request serverV2Write
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server", err.Error())
		return
	}
	server := model.Server{Common: model.Common{ID: id}}
	created := id == 0
	if !created {
		if err := singleton.DB.First(&server, id).Error; err != nil {
			writeV2Problem(c, 404, "server_not_found", "服务器不存在")
			return
		}
	}
	if err := applyServerWriteFields(&server, request, created); err != nil {
		writeV2Problem(c, 400, "invalid_server", err.Error())
		return
	}
	policies, err := trafficPoliciesFromWrite(request.TrafficPolicies)
	if err != nil {
		writeV2Problem(c, 400, "invalid_traffic_policy", err.Error())
		return
	}
	if created {
		secret, err := utils.GenerateRandomString(18)
		if err != nil {
			writeV2Problem(c, 500, "secret_generation_failed", err.Error())
			return
		}
		server.Secret = secret
	}
	var policyErr error
	err = singleton.DB.Transaction(func(tx *gorm.DB) error {
		if created {
			if err := tx.Create(&server).Error; err != nil {
				return err
			}
		} else if err := tx.Save(&server).Error; err != nil {
			return err
		}
		if policies != nil {
			policyErr = trafficservice.Replace(tx, server.ID, policies)
			return policyErr
		}
		return nil
	})
	if err != nil {
		if policyErr != nil {
			writeV2Problem(c, 400, "invalid_traffic_policy", policyErr.Error())
			return
		}
		code := "server_update_failed"
		if created {
			code = "server_create_failed"
		}
		writeV2Problem(c, 400, code, err.Error())
		return
	}
	if created {
		server.Host, server.State = &model.Host{}, &model.HostState{}
		singleton.ServerLock.Lock()
		singleton.SecretToID[server.Secret] = server.ID
		singleton.ServerList[server.ID] = &server
		singleton.ServerTagToIDList[server.Tag] = append(singleton.ServerTagToIDList[server.Tag], server.ID)
		singleton.ServerLock.Unlock()
	} else {
		if err := singleton.RefreshObserverAssignmentsForServer(server.ID, time.Now()); err != nil {
			writeV2Problem(c, 500, "assignment_refresh_failed", err.Error())
			return
		}
		if err := singleton.RefreshProbeCollectorConfigsForServer(server.ID); err != nil {
			writeV2Problem(c, 500, "assignment_refresh_failed", err.Error())
			return
		}
		singleton.ServerLock.Lock()
		old := singleton.ServerList[server.ID]
		if old != nil {
			server.CopyFromRunningServer(old)
			if server.Tag != old.Tag {
				removeServerTag(old.Tag, server.ID)
				singleton.ServerTagToIDList[server.Tag] = append(singleton.ServerTagToIDList[server.Tag], server.ID)
			}
		}
		singleton.ServerList[server.ID] = &server
		singleton.ServerLock.Unlock()
	}
	singleton.ReSortServer()
	result := serverAdminDTO(server, created)
	attachTrafficPolicies(result, server.ID)
	writeV2Data(c, map[bool]int{true: http.StatusCreated, false: http.StatusOK}[created], result)
}

func trafficPoliciesFromWrite(request *[]trafficPolicyUpsertDTO) ([]model.TrafficPolicy, error) {
	if request == nil {
		return nil, nil
	}
	rows := make([]model.TrafficPolicy, 0, len(*request))
	for _, item := range *request {
		row := model.TrafficPolicy{Common: model.Common{ID: item.ID}}
		if err := applyTrafficPolicyWrite(&row, item.trafficPolicyWriteDTO); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err := trafficservice.ValidateAll(rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func attachTrafficPolicies(result gin.H, serverID uint64) {
	var rows []model.TrafficPolicy
	if err := singleton.DB.Where("server_id = ?", serverID).Order("id ASC").Find(&rows).Error; err != nil {
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, trafficPolicyDTO(row))
	}
	result["traffic_policies"] = items
}

func removeServerTag(tag string, id uint64) {
	values := singleton.ServerTagToIDList[tag]
	for index, value := range values {
		if value == id {
			values = append(values[:index], values[index+1:]...)
			break
		}
	}
	if len(values) == 0 {
		delete(singleton.ServerTagToIDList, tag)
	} else {
		singleton.ServerTagToIDList[tag] = values
	}
}

func v2DeleteServer(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	err := singleton.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Delete(&model.Server{}, "id = ?", id).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&model.MonitorHistory{}, "server_id = ?", id).Error
	})
	if err != nil {
		writeV2Problem(c, 500, "server_delete_failed", err.Error())
		return
	}
	singleton.ServerLock.Lock()
	if singleton.ServerList[id] != nil {
		onServerDelete(id)
	}
	singleton.ServerLock.Unlock()
	singleton.ReSortServer()
	c.Status(http.StatusNoContent)
}

func v2ResetServerSecret(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	secret, err := utils.GenerateRandomString(18)
	if err != nil {
		writeV2Problem(c, 500, "secret_generation_failed", err.Error())
		return
	}
	var stored model.Server
	if err := singleton.DB.First(&stored, id).Error; err != nil {
		writeV2Problem(c, 404, "server_not_found", "服务器不存在")
		return
	}
	stored.Secret = secret
	if err := singleton.DB.Save(&stored).Error; err != nil {
		writeV2Problem(c, 500, "secret_save_failed", err.Error())
		return
	}
	singleton.ServerLock.Lock()
	if server := singleton.ServerList[id]; server != nil {
		delete(singleton.SecretToID, server.Secret)
		server.Secret = secret
		singleton.SecretToID[secret] = id
	}
	singleton.ServerLock.Unlock()
	writeV2Data(c, http.StatusOK, gin.H{"secret": secret})
}

func v2ResetAvailability(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	deleted, err := singleton.ResetServerAvailability(id)
	if err != nil {
		writeV2Problem(c, 500, "availability_reset_failed", err.Error())
		return
	}
	writeV2Data(c, http.StatusOK, gin.H{"deleted": deleted})
}

func v2AdminServerAvailability(c *gin.Context) {
	serverID, ok := v2ID(c)
	if !ok {
		return
	}
	var server model.Server
	if err := singleton.DB.Select("id").First(&server, serverID).Error; err != nil {
		writeV2Problem(c, http.StatusNotFound, "server_not_found", "服务器不存在")
		return
	}
	var binding model.ServerNodeBinding
	if err := singleton.DB.Where("server_id = ? AND current = ?", serverID, true).Order("valid_from DESC").First(&binding).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			writeV2List(c, []any{}, v2Meta{})
			return
		}
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit < 1 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	query := singleton.DB.Where("node_uuid = ?", binding.NodeUUID)
	for name, operator := range map[string]string{"from": ">=", "to": "<="} {
		if value := strings.TrimSpace(c.Query(name)); value != "" {
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				writeV2Problem(c, http.StatusBadRequest, "invalid_"+name, name+" 必须是 RFC3339 时间")
				return
			}
			query = query.Where("bucket_start "+operator+" ?", parsed.UnixNano())
		}
	}
	if value := strings.TrimSpace(c.Query("cursor")); value != "" {
		cursor, err := strconv.ParseInt(value, 10, 64)
		if err != nil || cursor <= 0 {
			writeV2Problem(c, http.StatusBadRequest, "invalid_cursor", "cursor 无效")
			return
		}
		query = query.Where("bucket_start < ?", cursor)
	}
	var rows []model.AvailabilityBucket
	if err := query.Order("bucket_start DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	next := ""
	if len(rows) > limit {
		next = strconv.FormatInt(rows[limit-1].BucketStart, 10)
		rows = rows[:limit]
	}
	blobs := make([][]byte, len(rows))
	for i, row := range rows {
		blobs[i] = row.ObserverSummary
	}
	evidenceRows, err := telemetry.AnnotateObserverEvidence(singleton.DB, blobs)
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	result := make([]gin.H, 0, len(rows))
	for i, row := range rows {
		var available any
		switch row.ConnectivityState {
		case model.ConnectivityFull, model.ConnectivityPartial:
			available = true
		case model.ConnectivityUnavailable:
			available = false
		}
		result = append(result, gin.H{
			"node_uuid": hex.EncodeToString(row.NodeUUID), "bucket_start": time.Unix(0, row.BucketStart).UTC().Format(time.RFC3339Nano),
			"host": row.HostState, "connectivity": row.ConnectivityState, "available": available, "coverage": coverageLabel(row),
			"expected_observers": row.ExpectedObservers, "healthy_observers": row.HealthyObservers, "seen_observers": row.SeenObservers,
			"observer_evidence": evidenceRows[i], "revision": row.Revision, "finalized": row.Finalized,
			"recalculated_at": optionalRFC3339Nano(row.RecalculatedAt),
			"window_end":      optionalRFC3339Nano(availabilityWindowEnd(row)), "resolution": row.Resolution,
		})
	}
	writeV2List(c, result, v2Meta{NextCursor: next})
}

func optionalRFC3339Nano(value int64) any {
	if value <= 0 {
		return nil
	}
	return time.Unix(0, value).UTC().Format(time.RFC3339Nano)
}

func availabilityWindowEnd(row model.AvailabilityBucket) int64 {
	if row.WindowEnd > row.BucketStart {
		return row.WindowEnd
	}
	return 0
}

func optionalFloat(sampledAt int64, value float64) any {
	if sampledAt <= 0 {
		return nil
	}
	return value
}

type batchServerRequest struct {
	IDs   []uint64 `json:"ids"`
	Group string   `json:"group"`
}

type serverDisplayIndexWrite struct {
	DisplayIndex int `json:"display_index"`
}

type serverGroupRenameWrite struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func v2UpdateServerDisplayIndex(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var request serverDisplayIndexWrite
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_display_index", err.Error())
		return
	}
	var server model.Server
	if err := singleton.DB.First(&server, id).Error; err != nil {
		writeV2Problem(c, http.StatusNotFound, "server_not_found", "服务器不存在")
		return
	}
	if err := singleton.DB.Model(&server).Update("display_index", request.DisplayIndex).Error; err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "display_index_update_failed", err.Error())
		return
	}
	server.DisplayIndex = request.DisplayIndex
	singleton.ServerLock.Lock()
	if old := singleton.ServerList[id]; old != nil {
		server.CopyFromRunningServer(old)
		old.DisplayIndex = request.DisplayIndex
		singleton.ServerList[id] = old
	} else {
		singleton.ServerList[id] = &server
	}
	singleton.ServerLock.Unlock()
	singleton.ReSortServer()
	singleton.ServerLock.RLock()
	if running := singleton.ServerList[id]; running != nil {
		server = *running
	}
	singleton.ServerLock.RUnlock()
	writeV2Data(c, http.StatusOK, serverAdminDTO(server, false))
}

func v2ListServerGroups(c *gin.Context) {
	counts := map[string]int{}
	singleton.ServerLock.RLock()
	for _, server := range singleton.ServerList {
		if server == nil {
			continue
		}
		counts[server.Tag]++
	}
	singleton.ServerLock.RUnlock()
	if len(counts) == 0 {
		var tags []string
		if err := singleton.DB.Model(&model.Server{}).Pluck("tag", &tags).Error; err == nil {
			for _, tag := range tags {
				counts[tag]++
			}
		}
	}
	items := make([]gin.H, 0, len(counts))
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "" {
			return false
		}
		if names[j] == "" {
			return true
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		items = append(items, gin.H{"name": name, "count": counts[name]})
	}
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}

func v2RenameServerGroup(c *gin.Context) {
	var request serverGroupRenameWrite
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_group_rename", err.Error())
		return
	}
	from, to := strings.TrimSpace(request.From), strings.TrimSpace(request.To)
	if from == to {
		writeV2Data(c, http.StatusOK, gin.H{"updated": 0})
		return
	}
	var servers []model.Server
	if err := singleton.DB.Where("tag = ?", from).Find(&servers).Error; err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "group_rename_failed", err.Error())
		return
	}
	if len(servers) == 0 {
		writeV2Problem(c, http.StatusNotFound, "group_not_found", "分组不存在或没有服务器")
		return
	}
	if err := singleton.DB.Model(&model.Server{}).Where("tag = ?", from).Update("tag", to).Error; err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "group_rename_failed", err.Error())
		return
	}
	for _, item := range servers {
		id := item.ID
		var server model.Server
		if singleton.DB.First(&server, id).Error != nil {
			continue
		}
		singleton.ServerLock.Lock()
		if old := singleton.ServerList[id]; old != nil {
			server.CopyFromRunningServer(old)
			removeServerTag(old.Tag, id)
		}
		singleton.ServerList[id] = &server
		singleton.ServerTagToIDList[server.Tag] = append(singleton.ServerTagToIDList[server.Tag], id)
		singleton.ServerLock.Unlock()
		_ = singleton.RefreshObserverAssignmentsForServer(id, time.Now())
	}
	singleton.ReSortServer()
	writeV2Data(c, http.StatusOK, gin.H{"updated": len(servers)})
}

func v2BatchServerGroup(c *gin.Context) {
	var request batchServerRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(request.IDs) == 0 {
		writeV2Problem(c, 400, "invalid_batch", "至少选择一台服务器")
		return
	}
	if err := singleton.DB.Model(&model.Server{}).Where("id IN ?", request.IDs).Update("tag", request.Group).Error; err != nil {
		writeV2Problem(c, 500, "batch_update_failed", err.Error())
		return
	}
	for _, id := range request.IDs {
		var server model.Server
		if singleton.DB.First(&server, id).Error == nil {
			singleton.ServerLock.Lock()
			if old := singleton.ServerList[id]; old != nil {
				server.CopyFromRunningServer(old)
				removeServerTag(old.Tag, id)
			}
			singleton.ServerList[id] = &server
			singleton.ServerTagToIDList[server.Tag] = append(singleton.ServerTagToIDList[server.Tag], id)
			singleton.ServerLock.Unlock()
			_ = singleton.RefreshObserverAssignmentsForServer(id, time.Now())
		}
	}
	singleton.ReSortServer()
	writeV2Data(c, http.StatusOK, gin.H{"updated": len(request.IDs)})
}
func v2BatchServerDelete(c *gin.Context) {
	var request batchServerRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(request.IDs) == 0 {
		writeV2Problem(c, 400, "invalid_batch", "至少选择一台服务器")
		return
	}
	for _, id := range request.IDs {
		singleton.ServerLock.RLock()
		exists := singleton.ServerList[id] != nil
		singleton.ServerLock.RUnlock()
		if !exists {
			continue
		}
		if err := singleton.DB.Unscoped().Delete(&model.Server{}, "id = ?", id).Error; err != nil {
			writeV2Problem(c, 500, "batch_delete_failed", err.Error())
			return
		}
		singleton.ServerLock.Lock()
		onServerDelete(id)
		singleton.ServerLock.Unlock()
	}
	singleton.ReSortServer()
	c.Status(http.StatusNoContent)
}

func resourceModel(name string) (any, any, bool) {
	switch name {
	case "monitors":
		return &model.Monitor{}, &[]model.Monitor{}, true
	case "notifications":
		return &model.Notification{}, &[]model.Notification{}, true
	case "alert-rules":
		return &model.AlertRule{}, &[]model.AlertRule{}, true
	case "ddns":
		return &model.DDNSProfile{}, &[]model.DDNSProfile{}, true
	case "nat":
		return &model.NAT{}, &[]model.NAT{}, true
	}
	return nil, nil, false
}

func v2ListResource(c *gin.Context, name string) {
	item, list, ok := resourceModel(name)
	if !ok {
		writeV2Problem(c, 404, "resource_not_found", name)
		return
	}
	page, size := parsePage(c)
	query := singleton.DB.Model(item)
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		like := "%" + q + "%"
		query = query.Where("name LIKE ?", like)
	}
	var total int64
	query.Count(&total)
	allowed := map[string]string{"id": "id", "name": "name", "created_at": "created_at", "updated_at": "updated_at"}
	if err := query.Order(orderClause(c, allowed, "id")).Offset((page - 1) * size).Limit(size).Find(list).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	writeV2List(c, resourceOutput(name, list), v2Meta{Page: page, PageSize: size, Total: total})
}
func v2GetResource(c *gin.Context, name string) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	item, _, valid := resourceModel(name)
	if !valid {
		writeV2Problem(c, 404, "resource_not_found", name)
		return
	}
	if err := singleton.DB.First(item, id).Error; err != nil {
		writeV2Problem(c, 404, "record_not_found", "记录不存在")
		return
	}
	writeV2Data(c, 200, resourceOutput(name, item))
}
func v2CreateResource(c *gin.Context, name string) { v2SaveResource(c, name, 0) }
func v2UpdateResource(c *gin.Context, name string) {
	id, ok := v2ID(c)
	if ok {
		v2SaveResource(c, name, id)
	}
}

func v2SaveResource(c *gin.Context, name string, id uint64) {
	body := map[string]any{}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeV2Problem(c, 400, "invalid_request", err.Error())
		return
	}
	item, _, ok := resourceModel(name)
	if !ok {
		writeV2Problem(c, 404, "resource_not_found", name)
		return
	}
	if id > 0 {
		if err := singleton.DB.First(item, id).Error; err != nil {
			writeV2Problem(c, 404, "record_not_found", "记录不存在")
			return
		}
	}
	if err := decodeResource(name, body, item); err != nil {
		writeV2Problem(c, 400, "invalid_resource", err.Error())
		return
	}
	switch value := item.(type) {
	case *model.Monitor:
		value.ID = id
	case *model.Notification:
		value.ID = id
	case *model.AlertRule:
		value.ID = id
	case *model.DDNSProfile:
		value.ID = id
	case *model.NAT:
		value.ID = id
	}
	if notification, ok := item.(*model.Notification); ok && !asBool(body["skip_test"]) {
		bundle := model.NotificationServerBundle{Notification: notification, Loc: singleton.Loc}
		if err := bundle.Send("这是测试消息"); err != nil {
			writeV2Problem(c, 400, "notification_test_failed", err.Error())
			return
		}
	}
	if err := singleton.DB.Save(item).Error; err != nil {
		writeV2Problem(c, 400, "resource_save_failed", err.Error())
		return
	}
	if err := afterResourceSave(name, item); err != nil {
		writeV2Problem(c, 400, "resource_activate_failed", err.Error())
		return
	}
	status := http.StatusOK
	if id == 0 {
		status = http.StatusCreated
	}
	writeV2Data(c, status, resourceOutput(name, item))
}

func decodeResource(name string, body map[string]any, target any) error {
	converted := map[string]any{}
	for key, value := range body {
		converted[snakeToPascal(key)] = value
	}
	switch name {
	case "monitors":
		converted["SkipServers"] = idSet(body["skip_server_ids"])
		if raw, err := utils.Json.Marshal(body["skip_server_ids"]); err == nil {
			converted["SkipServersRaw"] = string(raw)
		}
	case "notifications":
		converted["VerifySSL"] = asBool(body["verify_ssl"])
	case "alert-rules":
		converted["Rules"] = body["rules"]
		converted["Enable"] = asBool(body["enable"])
	case "ddns":
		converted["EnableIPv4"] = asBool(body["enable_ipv4"])
		converted["EnableIPv6"] = asBool(body["enable_ipv6"])
		if domains, ok := body["domains"].([]any); ok {
			values := make([]string, 0, len(domains))
			for _, v := range domains {
				values = append(values, fmt.Sprint(v))
			}
			converted["Domains"] = values
			converted["DomainsRaw"] = strings.Join(values, ",")
		}
		if profile, ok := target.(*model.DDNSProfile); ok && profile.ID > 0 {
			if secret := strings.TrimSpace(fmt.Sprint(body["access_secret"])); secret == "" || secret == ddnsRedactedPlaceholder {
				converted["AccessSecret"] = profile.AccessSecret
			}
		}
	}
	encoded, err := utils.Json.Marshal(converted)
	if err != nil {
		return err
	}
	return utils.Json.Unmarshal(encoded, target)
}

func idSet(value any) map[uint64]bool {
	result := map[uint64]bool{}
	if list, ok := value.([]any); ok {
		for _, raw := range list {
			if id, err := strconv.ParseUint(fmt.Sprint(raw), 10, 64); err == nil {
				result[id] = true
			}
		}
	}
	return result
}
func asBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "on" || v == "true" || v == "1"
	case float64:
		return v != 0
	}
	return false
}

func afterResourceSave(name string, item any) error {
	switch value := item.(type) {
	case *model.Monitor:
		return singleton.ServiceSentinelShared.OnMonitorUpdate(*value)
	case *model.Notification:
		singleton.OnRefreshOrAddNotification(value)
	case *model.AlertRule:
		singleton.OnRefreshOrAddAlert(*value)
	case *model.DDNSProfile:
		singleton.OnDDNSUpdate()
	case *model.NAT:
		singleton.OnNATUpdate()
	}
	return nil
}

func v2MonitorHistory(c *gin.Context) {
	monitorID, ok := v2ID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit < 1 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	query := singleton.DB.Where("monitor_id = ?", monitorID)
	if value := strings.TrimSpace(c.Query("from")); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			query = query.Where("created_at >= ?", parsed)
		} else {
			writeV2Problem(c, 400, "invalid_from", "from 必须是 RFC3339 时间")
			return
		}
	}
	if value := strings.TrimSpace(c.Query("to")); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			query = query.Where("created_at <= ?", parsed)
		} else {
			writeV2Problem(c, 400, "invalid_to", "to 必须是 RFC3339 时间")
			return
		}
	}
	if cursor, err := strconv.ParseUint(c.Query("cursor"), 10, 64); err == nil && cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	var rows []model.MonitorHistory
	if err := query.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	next := ""
	if len(rows) > limit {
		next = strconv.FormatUint(rows[limit-1].ID, 10)
		rows = rows[:limit]
	}
	writeV2List(c, snakeValue(rows), v2Meta{NextCursor: next})
}

func v2DeleteResource(c *gin.Context, name string) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	item, _, valid := resourceModel(name)
	if !valid {
		writeV2Problem(c, 404, "resource_not_found", name)
		return
	}
	if err := singleton.DB.Unscoped().Delete(item, "id = ?", id).Error; err != nil {
		writeV2Problem(c, 500, "resource_delete_failed", err.Error())
		return
	}
	switch name {
	case "monitors":
		singleton.ServiceSentinelShared.OnMonitorDelete(id)
		_ = singleton.DB.Unscoped().Delete(&model.MonitorHistory{}, "monitor_id = ?", id).Error
	case "notifications":
		singleton.OnDeleteNotification(id)
	case "alert-rules":
		singleton.OnDeleteAlert(id)
	case "ddns":
		singleton.OnDDNSUpdate()
	case "nat":
		singleton.OnNATUpdate()
	}
	c.Status(http.StatusNoContent)
}

func resourceOutput(name string, value any) any {
	result := snakeValue(value)
	scrubResource(name, result)
	return result
}
func scrubResource(name string, value any) {
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			scrubResource(name, item)
		}
	case map[string]any:
		if name == "ddns" && current["access_secret"] != "" {
			current["access_secret"] = ddnsRedactedPlaceholder
		}
		if name == "monitors" {
			if raw, ok := current["skip_servers_raw"].(string); ok {
				var ids []uint64
				_ = utils.Json.Unmarshal([]byte(raw), &ids)
				current["skip_server_ids"] = ids
			}
		}
		if name == "alert-rules" {
			current["rules"] = parseJSONList(current["rules_raw"])
		}
	}
}
func parseJSONList(raw any) any {
	var value any = []any{}
	if text, ok := raw.(string); ok && text != "" {
		_ = utils.Json.Unmarshal([]byte(text), &value)
	}
	return value
}

func snakeValue(value any) any {
	encoded, err := utils.Json.Marshal(value)
	if err != nil {
		return nil
	}
	var raw any
	if utils.Json.Unmarshal(encoded, &raw) != nil {
		return nil
	}
	return snakeWalk(raw)
}
func snakeWalk(value any) any {
	switch current := value.(type) {
	case []any:
		for index, item := range current {
			current[index] = snakeWalk(item)
		}
		return current
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, item := range current {
			result[pascalToSnake(key)] = snakeWalk(item)
		}
		return result
	default:
		return value
	}
}
func snakeToPascal(value string) string {
	parts := strings.Split(value, "_")
	for index, part := range parts {
		if part != "" {
			parts[index] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}
func pascalToSnake(value string) string {
	runes := []rune(value)
	var out []rune
	for index, r := range runes {
		if unicode.IsUpper(r) && index > 0 && (unicode.IsLower(runes[index-1]) || (index+1 < len(runes) && unicode.IsLower(runes[index+1]))) {
			out = append(out, '_')
		}
		out = append(out, unicode.ToLower(r))
	}
	return string(out)
}

func v2TestNotification(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var notification model.Notification
	if err := singleton.DB.First(&notification, id).Error; err != nil {
		writeV2Problem(c, 404, "notification_not_found", "通知方式不存在")
		return
	}
	bundle := model.NotificationServerBundle{Notification: &notification, Loc: singleton.Loc}
	if err := bundle.Send("这是测试消息"); err != nil {
		writeV2Problem(c, 502, "notification_test_failed", err.Error())
		return
	}
	writeV2Data(c, http.StatusAccepted, gin.H{"accepted": true})
}
func v2DDNSProviders(c *gin.Context) {
	items := snakeValue(model.ProviderList)
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(model.ProviderList), Total: int64(len(model.ProviderList))})
}

func v2GetSettings(c *gin.Context) { writeV2Data(c, http.StatusOK, v2SettingsDTO()) }
func v2SettingsDTO() gin.H {
	return gin.H{
		"site_title": singleton.Conf.Site.Brand, "language": singleton.Conf.Language, "view_password": "", "view_password_configured": singleton.Conf.Site.ViewPassword != "",
		"grpc_host": singleton.Conf.GRPCHost, "proxy_grpc_port": singleton.Conf.ProxyGRPCPort, "tls": singleton.Conf.TLS, "nameservers": splitNonEmpty(singleton.Conf.DNSServers), "enable_offline_history": singleton.Conf.EnableOfflineHistory,
		"offline_threshold": singleton.Conf.OfflineThresholdSeconds, "check_interval": singleton.Conf.OfflineCheckIntervalSeconds, "merge_gap": singleton.Conf.OfflineMergeGapSeconds,
		"retention_days": singleton.Conf.OfflineHistoryRetentionDays, "notify_offline": singleton.Conf.EnableOfflineNotification, "notify_recovery": singleton.Conf.EnableRecoveryNotification,
		"plain_ip_in_notification": singleton.Conf.EnablePlainIPInNotification,
		"show_availability_guest":  singleton.Conf.ShowAvailabilityToGuest, "connectivity_notification": singleton.Conf.Telemetry.EnableConnectivityNotification,
		"correction_notification": singleton.Conf.Telemetry.EnableCorrectionNotification, "collector_offline_notification": singleton.Conf.Telemetry.EnableCollectorOfflineNotification,
		"collector_online_notification": singleton.Conf.Telemetry.EnableCollectorOnlineNotification,
		"data_loss_notification":        singleton.Conf.Telemetry.EnableDataLossNotification, "primary_color": singleton.Conf.Site.PrimaryColor, "footer_text": singleton.Conf.Site.FooterText,
		"logo_url": singleton.Conf.Site.LogoURL, "background_url": singleton.Conf.Site.BackgroundURL, "custom_css": singleton.Conf.Site.SafeCustomCSS,
		"primary_location": singleton.Conf.Site.PrimaryLocation,
		"theme":            model.NormalizePublicTheme(singleton.Conf.Site.Theme), "allow_frontend_theme_switch": !singleton.Conf.DisableSwitchTemplateInFrontend,
		"retention": snakeValue(singleton.Conf.Retention), "web_delivery": singleton.Conf.Web.Delivery,
	}
}
func splitNonEmpty(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) })
	if parts == nil {
		return []string{}
	}
	return parts
}
func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
func uintValue(value any, current uint64) uint64 {
	if value == nil {
		return current
	}
	parsed, err := strconv.ParseUint(strings.TrimSuffix(fmt.Sprint(value), ".0"), 10, 64)
	if err != nil {
		return current
	}
	return parsed
}

var forbiddenCSS = regexp.MustCompile(`(?i)@import|expression\s*\(|javascript\s*:|url\s*\(\s*['"]?\s*(?:https?:)?//|</?style`)
var safeColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func v2UpdateSettings(c *gin.Context) {
	body := map[string]any{}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeV2Problem(c, 400, "invalid_settings", err.Error())
		return
	}
	previous := *singleton.Conf
	committed := false
	defer func() {
		if !committed {
			*singleton.Conf = previous
		}
	}()
	oldOffline := singleton.Conf.EnableOfflineHistory
	if value := stringValue(body["site_title"]); value != "" {
		singleton.Conf.Site.Brand = value
	}
	if value := stringValue(body["language"]); value != "" {
		if _, ok := model.Languages[value]; !ok {
			writeV2Problem(c, 400, "invalid_language", "不支持的语言")
			return
		}
		singleton.Conf.Language = value
	}
	if asBool(body["clear_view_password"]) {
		singleton.Conf.Site.ViewPassword = ""
	} else if password, exists := body["view_password"]; exists && stringValue(password) != "" {
		singleton.Conf.Site.ViewPassword = stringValue(password)
	}
	if value, exists := body["grpc_host"]; exists {
		singleton.Conf.GRPCHost = stringValue(value)
	}
	if value, exists := body["proxy_grpc_port"]; exists {
		port := uintValue(value, uint64(singleton.Conf.ProxyGRPCPort))
		if port > 65535 {
			writeV2Problem(c, 400, "invalid_proxy_grpc_port", "公网 gRPC 端口必须在 0–65535 之间，0 表示使用监听端口")
			return
		}
		singleton.Conf.ProxyGRPCPort = uint(port)
	}
	if value, exists := body["tls"]; exists {
		singleton.Conf.TLS = asBool(value)
	}
	if raw, exists := body["nameservers"]; exists {
		switch values := raw.(type) {
		case []any:
			parts := make([]string, 0, len(values))
			for _, value := range values {
				if item := stringValue(value); item != "" {
					parts = append(parts, item)
				}
			}
			singleton.Conf.DNSServers = strings.Join(parts, ",")
		case string:
			singleton.Conf.DNSServers = values
		}
	}
	if value, exists := body["enable_offline_history"]; exists {
		singleton.Conf.EnableOfflineHistory = asBool(value)
	}
	singleton.Conf.OfflineThresholdSeconds = uintValue(body["offline_threshold"], singleton.Conf.OfflineThresholdSeconds)
	singleton.Conf.OfflineCheckIntervalSeconds = uintValue(body["check_interval"], singleton.Conf.OfflineCheckIntervalSeconds)
	singleton.Conf.OfflineMergeGapSeconds = uintValue(body["merge_gap"], singleton.Conf.OfflineMergeGapSeconds)
	singleton.Conf.OfflineHistoryRetentionDays = uintValue(body["retention_days"], singleton.Conf.OfflineHistoryRetentionDays)
	if singleton.Conf.OfflineThresholdSeconds < 10 || singleton.Conf.OfflineCheckIntervalSeconds < 5 || singleton.Conf.OfflineCheckIntervalSeconds > singleton.Conf.OfflineThresholdSeconds || singleton.Conf.OfflineHistoryRetentionDays < 1 || singleton.Conf.OfflineMergeGapSeconds > 3600 {
		writeV2Problem(c, 400, "invalid_offline_settings", "离线阈值、检测间隔或保留策略超出允许范围")
		return
	}
	if value, exists := body["notify_offline"]; exists {
		singleton.Conf.EnableOfflineNotification = asBool(value)
	}
	if value, exists := body["notify_recovery"]; exists {
		singleton.Conf.EnableRecoveryNotification = asBool(value)
	}
	if value, exists := body["plain_ip_in_notification"]; exists {
		singleton.Conf.EnablePlainIPInNotification = asBool(value)
	}
	if value, exists := body["show_availability_guest"]; exists {
		singleton.Conf.ShowAvailabilityToGuest = asBool(value)
	}
	if value, exists := body["connectivity_notification"]; exists {
		singleton.Conf.Telemetry.EnableConnectivityNotification = asBool(value)
	}
	if value, exists := body["correction_notification"]; exists {
		singleton.Conf.Telemetry.EnableCorrectionNotification = asBool(value)
	}
	if value, exists := body["collector_offline_notification"]; exists {
		singleton.Conf.Telemetry.EnableCollectorOfflineNotification = asBool(value)
	}
	if value, exists := body["collector_online_notification"]; exists {
		singleton.Conf.Telemetry.EnableCollectorOnlineNotification = asBool(value)
	}
	if value, exists := body["data_loss_notification"]; exists {
		singleton.Conf.Telemetry.EnableDataLossNotification = asBool(value)
	}
	if value, exists := body["theme"]; exists {
		singleton.Conf.Site.Theme = model.NormalizePublicTheme(stringValue(value))
	}
	if value, exists := body["allow_frontend_theme_switch"]; exists {
		singleton.Conf.DisableSwitchTemplateInFrontend = !asBool(value)
	}
	if value := stringValue(body["primary_color"]); value != "" {
		if !safeColor.MatchString(value) {
			writeV2Problem(c, 400, "invalid_primary_color", "品牌色必须使用六位十六进制颜色")
			return
		}
		singleton.Conf.Site.PrimaryColor = value
	}
	if value, exists := body["footer_text"]; exists {
		singleton.Conf.Site.FooterText = stringValue(value)
	}
	if value, exists := body["primary_location"]; exists {
		location := stringValue(value)
		if len(location) > 64 {
			writeV2Problem(c, 400, "invalid_primary_location", "主控位置不得超过 64 个字符")
			return
		}
		singleton.Conf.Site.PrimaryLocation = location
	}
	if value, exists := body["logo_url"]; exists {
		candidate := stringValue(value)
		if candidate != "" && safeAssetURL(candidate, "") == "" {
			writeV2Problem(c, 400, "unsafe_asset_url", "Logo 只允许 /static/ 或 data:image/ 地址")
			return
		}
		singleton.Conf.Site.LogoURL = candidate
	}
	if value, exists := body["background_url"]; exists {
		candidate := stringValue(value)
		if candidate != "" && safeAssetURL(candidate, "") == "" {
			writeV2Problem(c, 400, "unsafe_asset_url", "背景只允许 /static/ 或 data:image/ 地址")
			return
		}
		singleton.Conf.Site.BackgroundURL = candidate
	}
	if value, exists := body["custom_css"]; exists {
		css := fmt.Sprint(value)
		if forbiddenCSS.MatchString(css) {
			writeV2Problem(c, 400, "unsafe_custom_css", "自定义 CSS 包含被禁止的远程或可执行规则")
			return
		}
		if len(css) > 32768 {
			writeV2Problem(c, 400, "custom_css_too_large", "自定义 CSS 不得超过 32 KiB")
			return
		}
		singleton.Conf.Site.SafeCustomCSS = css
	}
	if err := singleton.Conf.Save(); err != nil {
		writeV2Problem(c, 500, "settings_save_failed", err.Error())
		return
	}
	committed = true
	singleton.InitLocalizer()
	singleton.OnNameserverUpdate()
	if oldOffline != singleton.Conf.EnableOfflineHistory {
		singleton.StartOfflineDetector()
	} else {
		singleton.ReloadOfflineDetectorConfig()
	}
	writeV2Data(c, 200, v2SettingsDTO())
}

func apiTokenDTO(token model.ApiToken, includePlain bool) gin.H {
	prefix := token.Token
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	var expires any
	if token.ExpiresAt != nil {
		expires = token.ExpiresAt.Format(time.RFC3339)
	}
	dto := gin.H{
		"id":           token.ID,
		"note":         token.Note,
		"permission":   token.NormalizedPermission(),
		"expires_at":   expires,
		"enabled":      token.Enabled,
		"expired":      token.IsExpired(),
		"token_prefix": prefix + "…",
		"created_at":   token.CreatedAt.Format(time.RFC3339),
	}
	if includePlain {
		dto["token"] = token.Token
	}
	return dto
}

func v2ListAPITokens(c *gin.Context) {
	user := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	var tokens []model.ApiToken
	if err := singleton.DB.Where("user_id = ?", user.ID).Order("created_at DESC").Find(&tokens).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	items := make([]gin.H, 0, len(tokens))
	for _, token := range tokens {
		items = append(items, apiTokenDTO(token, false))
	}
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}
func v2CreateAPIToken(c *gin.Context) {
	user := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	var request struct {
		Note       string     `json:"note" binding:"required"`
		Permission string     `json:"permission" binding:"required"`
		ExpiresAt  *time.Time `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_token", err.Error())
		return
	}
	if request.Permission != model.ApiTokenPermissionRead && request.Permission != model.ApiTokenPermissionWrite {
		writeV2Problem(c, 400, "invalid_token_permission", "permission 必须是 read 或 write")
		return
	}
	if request.ExpiresAt != nil && !request.ExpiresAt.After(time.Now()) {
		writeV2Problem(c, 400, "invalid_token_expiry", "到期时间必须晚于当前时间")
		return
	}
	plain, err := utils.GenerateRandomString(32)
	if err != nil {
		writeV2Problem(c, 500, "token_generation_failed", err.Error())
		return
	}
	hash := sha256.Sum256([]byte(plain))
	token := model.ApiToken{
		UserID:     user.ID,
		Token:      plain,
		TokenHash:  hash[:],
		Note:       request.Note,
		Permission: request.Permission,
		ExpiresAt:  request.ExpiresAt,
		Enabled:    true,
	}
	if err := singleton.DB.Create(&token).Error; err != nil {
		writeV2Problem(c, 500, "token_create_failed", err.Error())
		return
	}
	singleton.ApiLock.Lock()
	singleton.ApiTokenList[plain] = &token
	singleton.UserIDToApiTokenList[user.ID] = append(singleton.UserIDToApiTokenList[user.ID], plain)
	singleton.ApiLock.Unlock()
	writeV2Data(c, http.StatusCreated, apiTokenDTO(token, true))
}
func v2DeleteAPIToken(c *gin.Context) {
	user := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	value := c.Param("id")
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		writeV2Problem(c, 400, "invalid_token_id", "Token ID 无效")
		return
	}
	var token model.ApiToken
	if err := singleton.DB.Where("user_id = ? AND id = ?", user.ID, id).First(&token).Error; err != nil {
		writeV2Problem(c, 404, "token_not_found", "Token 不存在")
		return
	}
	if err := singleton.DB.Unscoped().Delete(&token).Error; err != nil {
		writeV2Problem(c, 500, "token_delete_failed", err.Error())
		return
	}
	singleton.ApiLock.Lock()
	delete(singleton.ApiTokenList, token.Token)
	values := singleton.UserIDToApiTokenList[user.ID]
	for index, current := range values {
		if current == token.Token {
			values = append(values[:index], values[index+1:]...)
			break
		}
	}
	singleton.UserIDToApiTokenList[user.ID] = values
	singleton.ApiLock.Unlock()
	c.Status(http.StatusNoContent)
}

func v2GetAPIToken(c *gin.Context) {
	user := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var token model.ApiToken
	if err := singleton.DB.Where("user_id = ? AND id = ?", user.ID, id).First(&token).Error; err != nil {
		writeV2Problem(c, 404, "token_not_found", "Token 不存在")
		return
	}
	writeV2Data(c, 200, apiTokenDTO(token, true))
}

func v2PatchAPIToken(c *gin.Context) {
	user := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var request struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		writeV2Problem(c, 400, "invalid_token_patch", "enabled 必填")
		return
	}
	var token model.ApiToken
	if err := singleton.DB.Where("user_id = ? AND id = ?", user.ID, id).First(&token).Error; err != nil {
		writeV2Problem(c, 404, "token_not_found", "Token 不存在")
		return
	}
	if err := singleton.DB.Model(&token).Update("enabled", *request.Enabled).Error; err != nil {
		writeV2Problem(c, 500, "token_update_failed", err.Error())
		return
	}
	token.Enabled = *request.Enabled
	singleton.ApiLock.Lock()
	if current, exists := singleton.ApiTokenList[token.Token]; exists {
		current.Enabled = *request.Enabled
	}
	singleton.ApiLock.Unlock()
	writeV2Data(c, 200, apiTokenDTO(token, false))
}

func v2OfflineHistory(c *gin.Context) {
	serverID, err := strconv.ParseUint(c.Query("server_id"), 10, 64)
	if err != nil || serverID == 0 {
		writeV2Problem(c, 400, "invalid_server_id", "server_id 无效")
		return
	}
	page, size := parsePage(c)
	query := singleton.DB.Model(&model.ServerOfflineHistory{}).Where("server_id = ?", serverID)
	var total int64
	query.Count(&total)
	var rows []model.ServerOfflineHistory
	if err := query.Order("started_at DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	writeV2List(c, snakeValue(rows), v2Meta{Page: page, PageSize: size, Total: total})
}
func v2DeleteOfflineHistory(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	result := singleton.DB.Unscoped().Delete(&model.ServerOfflineHistory{}, "id = ?", id)
	if result.Error != nil {
		writeV2Problem(c, 500, "offline_history_delete_failed", result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		writeV2Problem(c, 404, "offline_history_not_found", "离线历史不存在")
		return
	}
	c.Status(http.StatusNoContent)
}
func v2CleanupOfflineHistory(c *gin.Context) {
	var request struct {
		Before   *time.Time `json:"before"`
		ServerID uint64     `json:"server_id"`
	}
	_ = c.ShouldBindJSON(&request)
	before := time.Now().AddDate(0, 0, -int(singleton.Conf.OfflineHistoryRetentionDays))
	if request.Before != nil {
		before = *request.Before
	}
	query := singleton.DB.Unscoped().Where("ended_at IS NOT NULL AND ended_at < ?", before)
	if request.ServerID > 0 {
		query = query.Where("server_id = ?", request.ServerID)
	}
	result := query.Delete(&model.ServerOfflineHistory{})
	if result.Error != nil {
		writeV2Problem(c, 500, "offline_history_cleanup_failed", result.Error.Error())
		return
	}
	writeV2Data(c, 200, gin.H{"deleted": result.RowsAffected})
}

func v2GetDatabase(c *gin.Context) {
	if DatabaseMaintainer == nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", "数据库维护未启动")
		return
	}
	writeV2Data(c, http.StatusOK, DatabaseMaintainer.Status())
}

func v2OptimizeDatabase(c *gin.Context) {
	if DatabaseMaintainer == nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", "数据库维护未启动")
		return
	}
	if !DatabaseMaintainer.Start(true) {
		writeV2Problem(c, http.StatusConflict, "optimize_in_progress", "数据库正在优化")
		return
	}
	writeV2Data(c, http.StatusOK, DatabaseMaintainer.Status())
}

var errObserverAddressRequired = errors.New("address is required for observer collectors")
var errProbeIPFamilyRequired = errors.New("enable_ipv4 or enable_ipv6 is required")

func boolOrKeep(value *bool, created bool, current, fallback bool) bool {
	if value != nil {
		return *value
	}
	if created {
		return fallback
	}
	return current
}

func applyCollectorProbeRequest(collector *model.Collector, request collectorRequest, created bool) error {
	if request.ProbeIntervalSeconds > 0 {
		collector.ProbeIntervalSec = request.ProbeIntervalSeconds
	}
	if request.MTRIntervalSeconds > 0 {
		collector.MTRIntervalSec = request.MTRIntervalSeconds
	}
	if strings.TrimSpace(request.TCPPorts) != "" {
		collector.TCPPorts = strings.TrimSpace(request.TCPPorts)
	}
	if request.EnableICMP != nil {
		collector.EnableICMP = *request.EnableICMP
	} else if created {
		collector.EnableICMP = true
	}
	if request.EnableTCP != nil {
		collector.EnableTCP = *request.EnableTCP
	} else if created {
		collector.EnableTCP = true
	}
	if request.EnableMTR != nil {
		collector.EnableMTR = *request.EnableMTR
	} else if created {
		collector.EnableMTR = true
	}
	if request.EnableIPv4 != nil {
		collector.EnableIPv4 = request.EnableIPv4
	} else if created {
		collector.EnableIPv4 = model.BoolPtr(true)
	}
	if request.EnableIPv6 != nil {
		collector.EnableIPv6 = request.EnableIPv6
	} else if created {
		collector.EnableIPv6 = model.BoolPtr(true)
	}
	if !model.BoolOrTrue(collector.EnableIPv4) && !model.BoolOrTrue(collector.EnableIPv6) {
		return errProbeIPFamilyRequired
	}
	collector.ProbeNotify = request.Notify
	collector.NotificationTag = strings.TrimSpace(request.NotificationTag)
	collector.LatencyNotify = request.LatencyNotify
	collector.MinLatencyMs = request.MinLatencyMs
	collector.MaxLatencyMs = request.MaxLatencyMs
	if request.FailThreshold > 0 {
		collector.FailThreshold = request.FailThreshold
	}
	return nil
}

func collectorDTO(collector model.Collector) gin.H {
	var scopes []model.CollectorScope
	_ = singleton.DB.Where("collector_uuid = ?", collector.CollectorUUID).Find(&scopes).Error
	var runtime model.CollectorRuntime
	_ = singleton.DB.Where("collector_uuid = ?", collector.CollectorUUID).Limit(1).Find(&runtime).Error
	scopeItems := make([]gin.H, 0, len(scopes))
	for _, scope := range scopes {
		scopeItems = append(scopeItems, gin.H{"type": scope.ScopeType, "value": scope.ScopeValue})
	}
	return gin.H{
		"id": collector.CollectorUUID, "name": collector.Name, "address": collector.Address, "listen_port": collector.ListenPort, "tls": collector.TLS,
		"insecure_tls": collector.InsecureTLS, "location": collector.Location, "kind": model.NormalizeCollectorKind(collector.Kind),
		"probe_interval_seconds": collector.ProbeIntervalSec, "mtr_interval_seconds": collector.MTRIntervalSec, "tcp_ports": collector.TCPPorts,
		"enable_icmp": collector.EnableICMP, "enable_tcp": collector.EnableTCP, "enable_mtr": collector.EnableMTR,
		"enable_ipv4": model.BoolOrTrue(collector.EnableIPv4), "enable_ipv6": model.BoolOrTrue(collector.EnableIPv6),
		"notify": collector.ProbeNotify, "notification_tag": collector.NotificationTag, "latency_notify": collector.LatencyNotify,
		"min_latency_ms": collector.MinLatencyMs, "max_latency_ms": collector.MaxLatencyMs, "fail_threshold": collector.FailThreshold,
		"generation": collector.Generation, "config_version": collector.ConfigVersion,
		"revoked": collector.Revoked, "status": telemetry.CollectorStatus(runtime.LastSeen, time.Now()),
		"last_seen": optionalRFC3339Nano(runtime.LastSeen), "last_sync": optionalRFC3339Nano(runtime.LastSync),
		"last_primary_seen": optionalRFC3339Nano(runtime.LastPrimarySeen), "spool_size": runtime.SpoolSize,
		"pending_records": runtime.PendingRecords, "oldest_pending": optionalRFC3339Nano(runtime.OldestPending),
		"replication_cursor": runtime.ReplicationCursor, "connected_agents": runtime.ConnectedAgents,
		"protocol_version": runtime.ProtocolVersion, "software_version": runtime.SoftwareVersion,
		"heartbeat_rtt_ms":           optionalFloat(runtime.HeartbeatRttSampledAt, runtime.HeartbeatRttMs),
		"heartbeat_rtt_sampled_at":   optionalRFC3339Nano(runtime.HeartbeatRttSampledAt),
		"replication_rtt_ms":         optionalFloat(runtime.ReplicationRttSampledAt, runtime.ReplicationRttMs),
		"replication_rtt_sampled_at": optionalRFC3339Nano(runtime.ReplicationRttSampledAt), "scopes": scopeItems,
	}
}
func v2Collectors(c *gin.Context) {
	var rows []model.Collector
	if err := singleton.DB.Where("deleted = ?", false).Order("created_at DESC").Find(&rows).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, collectorDTO(row))
	}
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}
func v2Collector(c *gin.Context) {
	var collector model.Collector
	if err := singleton.DB.First(&collector, "collector_uuid = ? AND deleted = ?", c.Param("id"), false).Error; err != nil {
		writeV2Problem(c, 404, "collector_not_found", "从端不存在")
		return
	}
	writeV2Data(c, 200, collectorDTO(collector))
}
func v2CreateCollector(c *gin.Context) {
	var request collectorRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_collector", err.Error())
		return
	}
	kind := model.NormalizeCollectorKind(request.Kind)
	if kind == "" {
		writeV2Problem(c, 400, "invalid_collector", "kind must be observer or probe")
		return
	}
	if kind == model.CollectorKindObserver && strings.TrimSpace(request.Address) == "" {
		writeV2Problem(c, 400, "invalid_collector", "address is required for observer collectors")
		return
	}
	listenPort, err := normalizeCollectorListenPort(request.ListenPort, request.Address)
	if err != nil {
		writeV2Problem(c, 400, "invalid_collector", err.Error())
		return
	}
	id, err := randomCollectorID()
	if err != nil {
		writeV2Problem(c, 500, "collector_id_failed", err.Error())
		return
	}
	plain, hash, err := telemetryToken()
	if err != nil {
		writeV2Problem(c, 500, "token_generation_failed", err.Error())
		return
	}
	collector := model.Collector{CollectorUUID: id, Name: request.Name, Address: request.Address, ListenPort: listenPort, TokenHash: hash, RegistrationToken: plain, Generation: 1, ConfigVersion: singleton.CurrentTelemetryConfigVersion() + 1, TLS: request.TLS, InsecureTLS: request.InsecureTLS, Location: strings.TrimSpace(request.Location), Kind: kind}
	if err := applyCollectorProbeRequest(&collector, request, true); err != nil {
		writeV2Problem(c, 400, "invalid_probe_ip_family", err.Error())
		return
	}
	collector.ApplyProbeDefaults()
	if err := singleton.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&collector).Error; err != nil {
			return err
		}
		return replaceCollectorScopes(tx, &collector, request.Scopes, time.Now())
	}); err != nil {
		writeV2Problem(c, 500, "collector_create_failed", err.Error())
		return
	}
	writeV2Data(c, http.StatusCreated, gin.H{"collector": collectorDTO(collector), "registration_token": plain})
}
func telemetryToken() (string, []byte, error) { return telemetry.NewRegistrationToken() }
func v2UpdateCollector(c *gin.Context) {
	var request collectorRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_collector", err.Error())
		return
	}
	listenPort, err := normalizeCollectorListenPort(request.ListenPort, request.Address)
	if err != nil {
		writeV2Problem(c, 400, "invalid_collector", err.Error())
		return
	}
	var collector model.Collector
	err = singleton.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&collector, "collector_uuid = ? AND deleted = ?", c.Param("id"), false).Error; err != nil {
			return err
		}
		if model.NormalizeCollectorKind(collector.Kind) == model.CollectorKindObserver && strings.TrimSpace(request.Address) == "" {
			return errObserverAddressRequired
		}
		collector.Name, collector.Address, collector.ListenPort, collector.TLS, collector.InsecureTLS, collector.Location = request.Name, request.Address, listenPort, request.TLS, request.InsecureTLS, strings.TrimSpace(request.Location)
		if err := applyCollectorProbeRequest(&collector, request, false); err != nil {
			return err
		}
		collector.ApplyProbeDefaults()
		collector.ConfigVersion++
		if err := tx.Save(&collector).Error; err != nil {
			return err
		}
		return replaceCollectorScopes(tx, &collector, request.Scopes, time.Now())
	})
	if err != nil {
		if errors.Is(err, errObserverAddressRequired) {
			writeV2Problem(c, 400, "invalid_collector", err.Error())
			return
		}
		if errors.Is(err, errProbeIPFamilyRequired) {
			writeV2Problem(c, 400, "invalid_probe_ip_family", err.Error())
			return
		}
		writeV2Problem(c, 404, "collector_update_failed", err.Error())
		return
	}
	writeV2Data(c, 200, collectorDTO(collector))
}
func v2DeleteCollector(c *gin.Context) {
	if err := disableCollector(c.Param("id"), true); err != nil {
		writeV2Problem(c, 404, "collector_delete_failed", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}
func v2RotateCollector(c *gin.Context) {
	plain, hash, err := telemetryToken()
	if err != nil {
		writeV2Problem(c, 500, "token_generation_failed", err.Error())
		return
	}
	var collector model.Collector
	if err := singleton.DB.First(&collector, "collector_uuid = ? AND deleted = ?", c.Param("id"), false).Error; err != nil {
		writeV2Problem(c, 404, "collector_not_found", "从端不存在")
		return
	}
	collector.TokenHash = hash
	collector.RegistrationToken = plain
	collector.Revoked = false
	collector.ConfigVersion++
	if err := singleton.DB.Save(&collector).Error; err != nil {
		writeV2Problem(c, 500, "collector_token_save_failed", err.Error())
		return
	}
	writeV2Data(c, 200, gin.H{
		"collector_id":       collector.CollectorUUID,
		"registration_token": plain,
		"revoked":            collector.Revoked,
	})
}

func v2CollectorToken(c *gin.Context) {
	var collector model.Collector
	if err := singleton.DB.First(&collector, "collector_uuid = ? AND deleted = ?", c.Param("id"), false).Error; err != nil {
		writeV2Problem(c, 404, "collector_not_found", "从端不存在")
		return
	}
	writeV2Data(c, 200, gin.H{"collector_id": collector.CollectorUUID, "registration_token": collector.RegistrationToken, "revoked": collector.Revoked})
}
func v2CollectorInstallPreview(c *gin.Context) {
	var collector model.Collector
	if err := singleton.DB.First(&collector, "collector_uuid = ? AND deleted = ?", c.Param("id"), false).Error; err != nil {
		writeV2Problem(c, 404, "collector_not_found", "从端不存在")
		return
	}
	if strings.TrimSpace(collector.RegistrationToken) == "" {
		writeV2Problem(c, 400, "invalid_collector", "从端注册 Token 不可用，请先轮换 Token")
		return
	}
	var request collectorInstallPreviewDTO
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_install_preview", err.Error())
		return
	}
	endpoint := strings.TrimSpace(request.PrimaryEndpoint)
	if endpoint == "" {
		host := singleton.Conf.GRPCHost
		if host == "" {
			host = "127.0.0.1"
		}
		endpoint = fmt.Sprintf("%s:%d", host, publicGRPCPort())
	}
	grpcPort, err := resolveCollectorInstallPort(collector.ListenPort, collector.Address, request.GRPCPort)
	if err != nil {
		writeV2Problem(c, 400, "invalid_collector", err.Error())
		return
	}
	if grpcPort == 0 {
		grpcPort = 5556
	}
	if grpcPort < 1 || grpcPort > 65535 {
		writeV2Problem(c, 400, "invalid_install_preview", "grpc_port must be between 1 and 65535")
		return
	}
	script := singleton.Conf.InstallScript.Collector
	if script == "" {
		script = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_collector.sh"
	}
	command := buildCollectorInstallCommand(script, endpoint, collector.RegistrationToken, grpcPort, request.PrimaryTLS, request.PrimaryInsecureTLS)
	defaultTLS := false
	if singleton.Conf != nil {
		defaultTLS = singleton.Conf.GRPCTLS.Enabled
	}
	writeV2Data(c, 200, gin.H{
		"command":              command,
		"primary_endpoint":     endpoint,
		"grpc_port":            grpcPort,
		"primary_tls":          request.PrimaryTLS,
		"primary_insecure_tls": request.PrimaryInsecureTLS,
		"default_primary_tls":  defaultTLS,
	})
}
func v2RevokeCollector(c *gin.Context) {
	if err := disableCollector(c.Param("id"), false); err != nil {
		writeV2Problem(c, 404, "collector_revoke_failed", err.Error())
		return
	}
	var collector model.Collector
	if err := singleton.DB.First(&collector, "collector_uuid = ? AND deleted = ?", c.Param("id"), false).Error; err != nil {
		writeV2Problem(c, 404, "collector_not_found", "从端不存在")
		return
	}
	writeV2Data(c, 200, collectorDTO(collector))
}
func v2UpdateCollectorScope(c *gin.Context) {
	var request struct {
		Scopes []collectorScopeRequest `json:"scopes"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, 400, "invalid_scope", err.Error())
		return
	}
	var collector model.Collector
	err := singleton.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&collector, "collector_uuid = ? AND deleted = ?", c.Param("id"), false).Error; err != nil {
			return err
		}
		collector.ConfigVersion++
		if err := tx.Save(&collector).Error; err != nil {
			return err
		}
		return replaceCollectorScopes(tx, &collector, request.Scopes, time.Now())
	})
	if err != nil {
		writeV2Problem(c, 400, "scope_update_failed", err.Error())
		return
	}
	writeV2Data(c, 200, collectorDTO(collector))
}
func v2TelemetryOverview(c *gin.Context) {
	var collectors, agents, assignments, losses, alerts int64
	singleton.DB.Model(&model.Collector{}).Where("deleted = ?", false).Count(&collectors)
	singleton.DB.Model(&model.AgentTelemetryRuntime{}).Count(&agents)
	singleton.DB.Model(&model.ObserverAssignment{}).Where("valid_to = 0").Count(&assignments)
	singleton.DB.Model(&model.TelemetryDataLoss{}).Count(&losses)
	singleton.DB.Model(&model.TelemetryAlert{}).Count(&alerts)
	writeV2Data(c, 200, gin.H{"collectors": collectors, "agents": agents, "active_assignments": assignments, "data_loss": losses, "alerts": alerts})
}

func v2ConnectionSummary(c *gin.Context) {
	summary, err := telemetry.LoadConnectionSummary(singleton.DB, time.Now())
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeV2Data(c, http.StatusOK, gin.H{
		"collectors_total": summary.CollectorsTotal, "collectors_online": summary.CollectorsOnline,
		"collectors_offline": summary.CollectorsOffline, "collectors_unknown": summary.CollectorsUnknown,
		"paths_assigned": summary.PathsAssigned, "paths_connected": summary.PathsConnected, "paths_seen": summary.PathsSeen,
	})
}

func v2ConnectionPaths(c *gin.Context) {
	var filter telemetry.PathFilter
	if raw := strings.TrimSpace(c.Query("server_id")); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", "server_id 无效")
			return
		}
		filter.ServerID = id
	}
	filter.ObserverID = strings.TrimSpace(c.Query("observer_id"))
	paths, err := telemetry.LoadConnectionPaths(singleton.DB, filter)
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	items := make([]gin.H, 0, len(paths))
	for _, path := range paths {
		items = append(items, gin.H{
			"server_id": path.ServerID, "server_name": path.ServerName, "display_index": path.DisplayIndex, "tag": path.Tag,
			"node_uuid":   path.NodeUUID,
			"observer_id": path.ObserverID, "observer_kind": path.ObserverKind, "observer_name": path.ObserverName,
			"assigned": path.Assigned, "last_seen": optionalRFC3339Nano(path.LastSeen),
			"sink": gin.H{
				"connected": path.Sink.Connected, "pending_events": path.Sink.PendingEvents,
				"last_error": path.Sink.LastError, "ack_through": path.Sink.AckThrough,
				"last_rtt_ms":    optionalFloat(path.Sink.RttSampledAt, path.Sink.LastRttMs),
				"rtt_sampled_at": optionalRFC3339Nano(path.Sink.RttSampledAt),
			},
		})
	}
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}

func v2ConnectionLatency(c *gin.Context) {
	var filter telemetry.LatencyFilter
	filter.Kind = strings.TrimSpace(c.Query("kind"))
	filter.CollectorUUID = strings.TrimSpace(c.Query("collector_id"))
	filter.ObserverID = strings.TrimSpace(c.Query("observer_id"))
	if raw := strings.TrimSpace(c.Query("server_id")); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", "server_id 无效")
			return
		}
		filter.ServerID = id
	}
	page, size := parsePage(c)
	rows, total, err := telemetry.ListConnectionLatency(singleton.DB, filter, (page-1)*size, size)
	if err != nil {
		if strings.Contains(err.Error(), "unknown connection latency kind") {
			writeV2Problem(c, http.StatusBadRequest, "invalid_latency_kind", err.Error())
			return
		}
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, gin.H{
			"kind": row.Kind, "collector_id": row.CollectorUUID, "server_id": row.ServerID, "server_name": row.ServerName,
			"node_uuid": row.NodeUUID, "observer_id": row.ObserverID, "bucket_start": optionalRFC3339Nano(row.BucketStart),
			"min_ms": row.MinMs, "avg_ms": row.AvgMs, "max_ms": row.MaxMs, "count": row.Count,
		})
	}
	writeV2List(c, items, v2Meta{Page: page, PageSize: size, Total: total})
}

func v2ProbeSummary(c *gin.Context) {
	summary, err := telemetry.LoadProbeSummary(singleton.DB, time.Now())
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeV2Data(c, http.StatusOK, gin.H{
		"collectors_total": summary.CollectorsTotal, "collectors_online": summary.CollectorsOnline,
		"collectors_offline": summary.CollectorsOffline, "collectors_unknown": summary.CollectorsUnknown,
		"paths_assigned": summary.PathsAssigned, "paths_reachable": summary.PathsReachable,
		"paths_down": summary.PathsDown, "paths_no_target": summary.PathsNoTarget,
	})
}

func v2ProbePaths(c *gin.Context) {
	var filter telemetry.ProbePathFilter
	filter.CollectorID = strings.TrimSpace(c.Query("collector_id"))
	if raw := strings.TrimSpace(c.Query("server_id")); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", "server_id 无效")
			return
		}
		filter.ServerID = id
	}
	paths, err := telemetry.LoadProbePaths(singleton.DB, filter)
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	items := make([]gin.H, 0, len(paths))
	for _, path := range paths {
		tcp := make([]gin.H, 0, len(path.TCP))
		for _, item := range path.TCP {
			tcp = append(tcp, gin.H{"port": item.Port, "ok": item.OK, "rtt_ms": item.RttMs, "error": item.Error})
		}
		item := gin.H{
			"server_id": path.ServerID, "server_name": path.ServerName, "display_index": path.DisplayIndex, "tag": path.Tag,
			"collector_id": path.CollectorID, "collector_name": path.CollectorName,
			"target":    gin.H{"source": path.TargetSource, "hostname": path.Hostname, "ipv4": path.IPv4, "ipv6": path.IPv6},
			"reachable": path.Reachable, "has_trace": path.HasTrace, "last_error": path.LastError,
			"icmp": gin.H{"ok": path.ICMPOk, "rtt_ms": path.ICMPRttMs, "loss": path.ICMPLoss, "packets_sent": path.ICMPSent, "packets_received": path.ICMPRecv},
			"tcp":  tcp,
		}
		if path.SampledAt > 0 {
			item["sampled_at"] = optionalRFC3339Nano(path.SampledAt)
			item["display_rtt_ms"] = optionalFloat(path.SampledAt, path.DisplayRttMs)
		}
		if path.MTR.HopCount > 0 {
			mtr := gin.H{"loss": path.MTR.Loss, "hop_count": path.MTR.HopCount}
			if path.MTR.SampledAt > 0 {
				mtr["sampled_at"] = optionalRFC3339Nano(path.MTR.SampledAt)
			}
			item["mtr"] = mtr
		}
		items = append(items, item)
	}
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}

func v2ProbeSamples(c *gin.Context) {
	var filter telemetry.ProbeSampleFilter
	filter.CollectorID = strings.TrimSpace(c.Query("collector_id"))
	filter.Kind = strings.TrimSpace(c.Query("kind"))
	if raw := strings.TrimSpace(c.Query("server_id")); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", "server_id 无效")
			return
		}
		filter.ServerID = id
	}
	if raw := strings.TrimSpace(c.Query("port")); raw != "" {
		port, err := strconv.ParseUint(raw, 10, 16)
		if err != nil {
			writeV2Problem(c, http.StatusBadRequest, "invalid_port", "port 无效")
			return
		}
		filter.Port = uint(port)
	}
	page, size := parsePage(c)
	rows, total, err := telemetry.ListProbeSamples(singleton.DB, filter, (page-1)*size, size)
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, gin.H{
			"collector_id": row.CollectorID, "server_id": row.ServerID, "server_name": row.ServerName,
			"kind": row.Kind, "port": row.Port, "bucket_start": optionalRFC3339Nano(row.BucketStart),
			"min_ms": row.MinMs, "avg_ms": row.AvgMs, "max_ms": row.MaxMs, "loss": row.Loss,
			"success_count": row.SuccessCount, "fail_count": row.FailCount,
		})
	}
	writeV2List(c, items, v2Meta{Page: page, PageSize: size, Total: total})
}

func v2ProbeTrace(c *gin.Context) {
	collectorID := strings.TrimSpace(c.Query("collector_id"))
	raw := strings.TrimSpace(c.Query("server_id"))
	if collectorID == "" || raw == "" {
		writeV2Problem(c, http.StatusBadRequest, "invalid_probe_trace", "collector_id 与 server_id 必填")
		return
	}
	serverID, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || serverID == 0 {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", "server_id 无效")
		return
	}
	trace, err := telemetry.GetProbeTrace(singleton.DB, collectorID, serverID)
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	if trace == nil {
		writeV2Data(c, http.StatusOK, nil)
		return
	}
	hops := make([]gin.H, 0, len(trace.Hops))
	for _, hop := range trace.Hops {
		hops = append(hops, gin.H{
			"ttl": hop.TTL, "address": hop.Address, "loss": hop.Loss,
			"avg_ms": durationMilliseconds(hop.Avg), "sent": hop.Sent,
		})
	}
	writeV2Data(c, http.StatusOK, gin.H{
		"collector_id": trace.CollectorID, "server_id": trace.ServerID,
		"sampled_at": optionalRFC3339Nano(trace.SampledAt), "destination": trace.Destination, "hops": hops,
	})
}

func durationMilliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}

func v2TelemetryDataset(c *gin.Context) {
	page, size := parsePage(c)
	offset := (page - 1) * size
	var (
		rows  any
		total int64
		err   error
	)
	switch c.Param("dataset") {
	case "observer-assignments", "assignments":
		rows, total, err = telemetry.ListObserverAssignments(singleton.DB, offset, size)
	case "agents":
		rows, total, err = telemetry.ListAgentReliability(singleton.DB, offset, size)
	case "incidents":
		rows, total, err = telemetry.ListIncidents(singleton.DB, offset, size)
	case "incident-revisions":
		rows, total, err = telemetry.ListIncidentRevisions(singleton.DB, offset, size)
	case "data-loss":
		rows, total, err = telemetry.ListDataLoss(singleton.DB, offset, size)
	case "alerts":
		rows, total, err = telemetry.ListAlerts(singleton.DB, offset, size)
	default:
		writeV2Problem(c, 404, "dataset_not_found", "探测数据集不存在")
		return
	}
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeV2List(c, rows, v2Meta{Page: page, PageSize: size, Total: total})
}
