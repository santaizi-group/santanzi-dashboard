package controller

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	botservice "github.com/hi2shark/santaizi-dashboard/service/bot"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

func registerBotRoutes(root gin.IRouter, admin *gin.RouterGroup) {
	root.POST("/v2/bot/telegram/webhook", v2TelegramWebhook)
	admin.GET("/bot/settings", v2GetBotSettings)
	admin.PATCH("/bot/settings", v2PatchBotSettings)
	admin.POST("/bot/test", v2TestBot)
	admin.GET("/bot/chats", v2ListBotChats)
	admin.GET("/bot/chats/:id", v2GetBotChat)
	admin.PATCH("/bot/chats/:id", v2PatchBotChat)
	admin.DELETE("/bot/chats/:id", v2DeleteBotChat)
	admin.GET("/bot/bind-codes", v2ListBotBindCodes)
	admin.POST("/bot/bind-codes", v2CreateBotBindCode)
	admin.DELETE("/bot/bind-codes/:id", v2DeleteBotBindCode)
	admin.GET("/bot/reports", v2ListBotReports)
	admin.POST("/bot/reports", v2CreateBotReport)
	admin.GET("/bot/reports/:id", v2GetBotReport)
	admin.PATCH("/bot/reports/:id", v2PatchBotReport)
	admin.DELETE("/bot/reports/:id", v2DeleteBotReport)
	admin.POST("/bot/reports/:id/run", v2RunBotReport)
}

func v2TelegramWebhook(c *gin.Context) {
	botservice.Shared().ServeWebhook(c.Writer, c.Request)
}

func botSettingsDTO() gin.H {
	conf := singleton.Conf.Bot
	tokenSet := strings.TrimSpace(conf.Token) != ""
	suffix := ""
	if tokenSet {
		token := conf.Token
		if len(token) > 4 {
			suffix = token[len(token)-4:]
		}
	}
	secretSet := strings.TrimSpace(conf.WebhookSecret) != ""
	return gin.H{
		"enabled": conf.Enabled, "provider": conf.Provider, "mode": conf.Mode,
		"api_endpoint": conf.APIEndpoint, "webhook_base_url": conf.WebhookBaseURL,
		"charts": conf.Charts, "language": conf.Language, "rate_per_minute": conf.RatePerMinute,
		"token_set": tokenSet, "token_suffix": suffix, "webhook_secret_set": secretSet,
	}
}

func v2GetBotSettings(c *gin.Context) { writeV2Data(c, http.StatusOK, botSettingsDTO()) }

type botSettingsWrite struct {
	Enabled        *bool   `json:"enabled"`
	Provider       *string `json:"provider"`
	Token          *string `json:"token"`
	APIEndpoint    *string `json:"api_endpoint"`
	Mode           *string `json:"mode"`
	WebhookBaseURL *string `json:"webhook_base_url"`
	WebhookSecret  *string `json:"webhook_secret"`
	Charts         *bool   `json:"charts"`
	Language       *string `json:"language"`
	RatePerMinute  *int    `json:"rate_per_minute"`
}

func v2PatchBotSettings(c *gin.Context) {
	var body botSettingsWrite
	if err := c.ShouldBindJSON(&body); err != nil {
		writeV2Problem(c, 400, "invalid_bot_settings", err.Error())
		return
	}
	conf := &singleton.Conf.Bot
	if body.Enabled != nil {
		conf.Enabled = *body.Enabled
	}
	if body.Provider != nil {
		conf.Provider = strings.TrimSpace(*body.Provider)
	}
	if body.Token != nil && strings.TrimSpace(*body.Token) != "" {
		conf.Token = strings.TrimSpace(*body.Token)
	}
	if body.APIEndpoint != nil {
		conf.APIEndpoint = strings.TrimSpace(*body.APIEndpoint)
	}
	if body.Mode != nil {
		conf.Mode = strings.TrimSpace(*body.Mode)
	}
	if body.WebhookBaseURL != nil {
		conf.WebhookBaseURL = strings.TrimSpace(*body.WebhookBaseURL)
	}
	if body.WebhookSecret != nil && strings.TrimSpace(*body.WebhookSecret) != "" {
		conf.WebhookSecret = strings.TrimSpace(*body.WebhookSecret)
	}
	if body.Charts != nil {
		conf.Charts = *body.Charts
	}
	if body.Language != nil {
		conf.Language = strings.TrimSpace(*body.Language)
	}
	if body.RatePerMinute != nil {
		conf.RatePerMinute = *body.RatePerMinute
	}
	conf.Normalize(singleton.Conf.Language)
	if conf.Mode == model.BotModeWebhook && conf.WebhookSecret == "" {
		secret, err := utils.GenerateRandomString(32)
		if err != nil {
			writeV2Problem(c, 500, "bot_settings_save_failed", err.Error())
			return
		}
		conf.WebhookSecret = secret
	}
	if err := singleton.Conf.Save(); err != nil {
		writeV2Problem(c, 500, "bot_settings_save_failed", err.Error())
		return
	}
	go botservice.Shared().Reload()
	writeV2Data(c, 200, botSettingsDTO())
}

func v2TestBot(c *gin.Context) {
	conf := singleton.Conf.Bot
	name, err := botservice.TestToken(c.Request.Context(), conf.Token, conf.APIEndpoint)
	if err != nil {
		writeV2Problem(c, 400, "bot_test_failed", err.Error())
		return
	}
	writeV2Data(c, 200, gin.H{"ok": true, "username": name})
}

func botChatDTO(row model.BotChat) gin.H {
	return gin.H{
		"id": row.ID, "chat_id": row.ChatID, "kind": row.Kind, "title": row.Title,
		"role": model.BotRoleName(row.Role), "allowed_user_ids": model.ParseInt64CSV(row.AllowedUserIDs),
		"subscribe_tags": model.ParseStringCSV(row.SubscribeTags), "note": row.Note,
		"bound_by": row.BoundBy, "last_seen_at": formatOptionalTime(row.LastSeenAt),
		"enabled": row.Enabled != nil && *row.Enabled, "created_at": row.CreatedAt.Format(time.RFC3339),
		"updated_at": row.UpdatedAt.Format(time.RFC3339),
	}
}

func v2ListBotChats(c *gin.Context) {
	page, size := parsePage(c)
	query := singleton.DB.Model(&model.BotChat{})
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		query = query.Where("title LIKE ? OR CAST(chat_id AS TEXT) LIKE ?", "%"+q+"%", "%"+q+"%")
	}
	var total int64
	query.Count(&total)
	var rows []model.BotChat
	if err := query.Order(orderClause(c, map[string]string{"id": "id", "created_at": "created_at", "updated_at": "updated_at"}, "id")).Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, botChatDTO(row))
	}
	writeV2List(c, out, v2Meta{Page: page, PageSize: size, Total: total})
}

func v2GetBotChat(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.BotChat
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "bot_chat_not_found", "授权会话不存在")
		return
	}
	writeV2Data(c, 200, botChatDTO(row))
}

type botChatWrite struct {
	Role           string   `json:"role"`
	AllowedUserIDs *[]int64 `json:"allowed_user_ids"`
	SubscribeTags  *[]string `json:"subscribe_tags"`
	Note           *string  `json:"note"`
	Enabled        *bool    `json:"enabled"`
}

func v2PatchBotChat(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.BotChat
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "bot_chat_not_found", "授权会话不存在")
		return
	}
	var body botChatWrite
	if err := c.ShouldBindJSON(&body); err != nil {
		writeV2Problem(c, 400, "invalid_bot_chat", err.Error())
		return
	}
	if body.Role != "" {
		role, ok := model.ParseBotRole(body.Role)
		if !ok {
			writeV2Problem(c, 400, "invalid_bot_chat", "角色无效")
			return
		}
		row.Role = role
	}
	if body.Enabled != nil {
		row.Enabled = body.Enabled
	}
	if body.AllowedUserIDs != nil {
		row.AllowedUserIDs = model.JoinInt64CSV(*body.AllowedUserIDs)
	}
	if body.SubscribeTags != nil {
		row.SubscribeTags = model.JoinStringCSV(*body.SubscribeTags)
	}
	if body.Note != nil {
		row.Note = strings.TrimSpace(*body.Note)
	}
	if err := singleton.DB.Save(&row).Error; err != nil {
		writeV2Problem(c, 400, "bot_chat_save_failed", err.Error())
		return
	}
	botservice.Shared().Authz().Upsert(&row)
	writeV2Data(c, 200, botChatDTO(row))
}

func v2DeleteBotChat(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.BotChat
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "bot_chat_not_found", "授权会话不存在")
		return
	}
	if err := singleton.DB.Unscoped().Delete(&row).Error; err != nil {
		writeV2Problem(c, 500, "bot_chat_delete_failed", err.Error())
		return
	}
	botservice.Shared().Authz().Delete(row.ChatID)
	c.Status(204)
}

func botBindDTO(row model.BotBindCode) gin.H {
	used := ""
	if row.UsedAt != nil {
		used = row.UsedAt.Format(time.RFC3339)
	}
	return gin.H{
		"id": row.ID, "code": row.Code, "role": model.BotRoleName(row.Role),
		"expires_at": row.ExpiresAt.Format(time.RFC3339), "used_by_chat_id": row.UsedByChatID,
		"used_at": used, "created_by": row.CreatedBy, "created_at": row.CreatedAt.Format(time.RFC3339),
	}
}

func v2ListBotBindCodes(c *gin.Context) {
	page, size := parsePage(c)
	query := singleton.DB.Model(&model.BotBindCode{})
	var total int64
	query.Count(&total)
	var rows []model.BotBindCode
	if err := query.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		writeV2Problem(c, 500, "database_error", err.Error())
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, botBindDTO(row))
	}
	writeV2List(c, out, v2Meta{Page: page, PageSize: size, Total: total})
}

type botBindWrite struct {
	Role           string `json:"role"`
	TTLSeconds     int    `json:"ttl_seconds"`
}

func v2CreateBotBindCode(c *gin.Context) {
	var body botBindWrite
	_ = c.ShouldBindJSON(&body)
	role, ok := model.ParseBotRole(body.Role)
	if !ok {
		role = model.BotRoleViewer
	}
	ttl := time.Duration(body.TTLSeconds) * time.Second
	row, err := botservice.IssueBindCode(role, ttl, "admin")
	if err != nil {
		writeV2Problem(c, 400, "invalid_bot_bind_code", err.Error())
		return
	}
	writeV2Data(c, 201, botBindDTO(*row))
}

func v2DeleteBotBindCode(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	result := singleton.DB.Unscoped().Delete(&model.BotBindCode{}, "id = ?", id)
	if result.RowsAffected == 0 {
		writeV2Problem(c, 404, "bot_bind_code_not_found", "绑定码不存在")
		return
	}
	c.Status(204)
}

func botReportDTO(row model.BotReport) gin.H {
	lastRun := ""
	if row.LastRunAt != nil {
		lastRun = row.LastRunAt.Format(time.RFC3339)
	}
	return gin.H{
		"id": row.ID, "name": row.Name, "chat_ids": model.ParseInt64CSV(row.ChatIDs),
		"period": row.Period, "hour_local": row.HourLocal, "minute": row.Minute,
		"weekday": row.Weekday, "day_of_month": row.DayOfMonth,
		"sections": model.ParseStringCSV(row.Sections),
		"cover": coverName(row.Cover, row.Ignore), "ignore_ids": model.ParseUint64CSV(row.Ignore),
		"with_charts": row.ChartsEnabled(), "enabled": row.IsEnabled(),
		"last_period_key": row.LastPeriodKey, "last_run_at": lastRun, "last_status": row.LastStatus,
		"created_at": row.CreatedAt.Format(time.RFC3339), "updated_at": row.UpdatedAt.Format(time.RFC3339),
	}
}

func coverName(cover uint8, ignore string) string {
	if cover == model.RuleCoverIgnoreAll {
		return "include"
	}
	if strings.TrimSpace(ignore) != "" {
		return "exclude"
	}
	return "all"
}

func parseCover(value string) uint8 {
	switch value {
	case "include":
		return model.RuleCoverIgnoreAll
	default:
		return model.RuleCoverAll
	}
}

func v2ListBotReports(c *gin.Context) {
	v2ListNamed(c, &model.BotReport{}, func(rows []model.BotReport) []gin.H {
		out := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			out = append(out, botReportDTO(row))
		}
		return out
	})
}

func v2GetBotReport(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.BotReport
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "bot_report_not_found", "周期报告不存在")
		return
	}
	writeV2Data(c, 200, botReportDTO(row))
}

type botReportWrite struct {
	Name        string   `json:"name" binding:"required"`
	ChatIDs     []int64  `json:"chat_ids"`
	Period      string   `json:"period"`
	HourLocal   int      `json:"hour_local"`
	Minute      int      `json:"minute"`
	Weekday     int      `json:"weekday"`
	DayOfMonth  int      `json:"day_of_month"`
	Sections    []string `json:"sections"`
	Cover       string   `json:"cover"`
	IgnoreIDs   []uint64 `json:"ignore_ids"`
	WithCharts  bool     `json:"with_charts"`
	Enabled     bool     `json:"enabled"`
}

func applyBotReportWrite(row *model.BotReport, body botReportWrite) error {
	row.Name = strings.TrimSpace(body.Name)
	if row.Name == "" {
		return errors.New("name is required")
	}
	row.ChatIDs = model.JoinInt64CSV(body.ChatIDs)
	switch body.Period {
	case model.BotPeriodWeekly, model.BotPeriodMonthly:
		row.Period = body.Period
	default:
		row.Period = model.BotPeriodDaily
	}
	row.HourLocal = body.HourLocal
	row.Minute = body.Minute
	if row.HourLocal < 0 || row.HourLocal > 23 {
		row.HourLocal = 9
	}
	if row.Minute < 0 || row.Minute > 59 {
		row.Minute = 0
	}
	row.Weekday = body.Weekday
	row.DayOfMonth = body.DayOfMonth
	row.Sections = model.JoinStringCSV(body.Sections)
	row.Cover = parseCover(body.Cover)
	row.Ignore = model.JoinUint64CSV(body.IgnoreIDs)
	row.WithCharts = model.BoolPtr(body.WithCharts)
	row.Enabled = model.BoolPtr(body.Enabled)
	return nil
}

func v2CreateBotReport(c *gin.Context) { v2SaveBotReport(c, 0) }
func v2PatchBotReport(c *gin.Context) {
	id, ok := v2ID(c)
	if ok {
		v2SaveBotReport(c, id)
	}
}

func v2SaveBotReport(c *gin.Context, id uint64) {
	var body botReportWrite
	if err := c.ShouldBindJSON(&body); err != nil {
		writeV2Problem(c, 400, "invalid_bot_report", err.Error())
		return
	}
	row := model.BotReport{Common: model.Common{ID: id}}
	if id > 0 && singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "bot_report_not_found", "周期报告不存在")
		return
	}
	if err := applyBotReportWrite(&row, body); err != nil {
		writeV2Problem(c, 400, "invalid_bot_report", "名称不能为空")
		return
	}
	if err := singleton.DB.Save(&row).Error; err != nil {
		writeV2Problem(c, 400, "bot_report_save_failed", err.Error())
		return
	}
	status := 200
	if id == 0 {
		status = 201
	}
	writeV2Data(c, status, botReportDTO(row))
}

func v2DeleteBotReport(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	result := singleton.DB.Unscoped().Delete(&model.BotReport{}, "id = ?", id)
	if result.RowsAffected == 0 {
		writeV2Problem(c, 404, "bot_report_not_found", "周期报告不存在")
		return
	}
	c.Status(204)
}

func v2RunBotReport(c *gin.Context) {
	id, ok := v2ID(c)
	if !ok {
		return
	}
	var row model.BotReport
	if singleton.DB.First(&row, id).Error != nil {
		writeV2Problem(c, 404, "bot_report_not_found", "周期报告不存在")
		return
	}
	if err := botservice.Shared().SendReport(&row, true); err != nil {
		writeV2Problem(c, 400, "bot_report_run_failed", err.Error())
		return
	}
	writeV2Data(c, 202, botReportDTO(row))
}
