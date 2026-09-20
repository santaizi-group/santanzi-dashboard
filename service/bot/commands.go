package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/report"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

func (h *Hub) onUpdate(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.CallbackQuery != nil {
		h.onCallback(ctx, b, update)
		return
	}
	if update.Message == nil {
		return
	}
	cmd, arg := commandName(update.Message.Text)
	switch cmd {
	case "start":
		h.cmdStart(ctx, update.Message.Chat.ID, arg)
	case "bind":
		h.cmdBind(ctx, update.Message.Chat.ID, arg)
	case "help":
		h.cmdHelp(ctx, arg)
	case "menu":
		h.cmdMenu(ctx)
	case "whoami":
		h.cmdWhoami(ctx)
	case "status":
		h.cmdStatus(ctx)
	case "servers":
		h.cmdServers(ctx, arg, 1)
	case "server":
		h.cmdServer(ctx, arg)
	case "find":
		h.runKind(ctx, "find", arg)
	case "top":
		h.runKind(ctx, "top", arg)
	case "usage":
		h.runKind(ctx, "usage", arg)
	case "traffic":
		h.cmdTraffic(ctx)
	case "uptime":
		h.runKind(ctx, "uptime", arg)
	case "groups":
		h.runKind(ctx, "groups", arg)
	case "chart", "cpu", "net":
		if cmd == "cpu" {
			arg = strings.TrimSpace(arg + " cpu")
		}
		if cmd == "net" {
			arg = strings.TrimSpace(arg + " net")
		}
		h.runKind(ctx, "chart", arg)
	case "cmp":
		h.runKind(ctx, "cmp", arg)
	case "services":
		h.cmdServices(ctx)
	case "rules":
		h.cmdRules(ctx)
	case "collectors":
		h.cmdCollectors(ctx)
	case "agents":
		h.cmdAgents(ctx)
	case "offline":
		h.cmdOffline(ctx, arg)
	case "probes":
		h.cmdProbes(ctx)
	case "alerts":
		h.cmdAlerts(ctx)
	case "audit":
		h.cmdAudit(ctx)
	case "health":
		h.cmdHealth(ctx)
	case "report":
		h.cmdReport(ctx, arg)
	case "mute":
		h.cmdMute(ctx, arg, false)
	case "unmute":
		h.cmdMute(ctx, arg, true)
	case "rule":
		h.cmdRule(ctx, arg, false)
	case "chats":
		h.cmdChats(ctx)
	case "role":
		h.cmdRole(ctx, arg)
	case "revoke":
		h.cmdRevoke(ctx, arg)
	default:
		if cmd != "" {
			h.Reply(update.Message.Chat.ID, "未知命令。发 /help 查看。")
			return
		}
		chat := chatFrom(ctx)
		if chat != nil && chat.Kind == model.BotChatPrivate && strings.TrimSpace(update.Message.Text) != "" {
			h.searchHosts(ctx, update.Message.Text)
		}
	}
}

func (h *Hub) cmdStart(ctx context.Context, chatID int64, arg string) {
	if arg != "" {
		h.cmdBind(ctx, chatID, arg)
		return
	}
	chat := chatFrom(ctx)
	if chat != nil && chat.ID != 0 && chat.IsEnabled() {
		h.cmdMenu(ctx)
		return
	}
	h.Reply(chatID, "在管理后台生成绑定码后，发送 /bind <绑定码>。")
}

func (h *Hub) cmdBind(ctx context.Context, chatID int64, arg string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	chat.ChatID = chatID
	if strings.TrimSpace(arg) == "" {
		h.Reply(chatID, "用法：/bind <绑定码> 或 /start <绑定码>")
		return
	}
	_, err := ConsumeBindCode(arg, chat)
	if err != nil {
		h.audit(ctx, "bind", arg, "denied")
		h.Reply(chatID, "绑定码无效或已过期。")
		return
	}
	h.authz.Upsert(chat)
	notice := "已绑定为" + roleLabel(chat.Role) + "。"
	if chat.Kind != model.BotChatPrivate {
		notice += "\n群内成员都将获得该角色。需要收紧时，在面板填写允许的用户 ID。"
	}
	h.Reply(chatID, notice+"\n发 /menu 打开菜单。")
	h.audit(ctx, "bind", arg, "ok")
}

func (h *Hub) cmdHelp(ctx context.Context, arg string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	if strings.TrimSpace(strings.ToLower(arg)) == "query" {
		h.Reply(chat.ChatID, helpQueryText())
		return
	}
	text := Bold("三太子监控") + "\n" +
		"/menu 菜单\n/status 总览\n/servers 主机列表\n/server &lt;id|名称&gt; 主机详情\n/find 条件筛选\n/top 排行\n/usage 范围流量\n/traffic 流量配额\n/uptime 可用率\n/groups 分组\n/chart 曲线\n/cmp 对比\n/services 服务监控\n/rules 告警规则\n/collectors 从端\n/agents 探针版本\n/offline 离线记录\n/probes 探针观察\n/alerts 连通异常\n/report now 立即报告\n/whoami 当前权限\n/help query 查询语法"
	if chat.Role >= model.BotRoleOperator {
		text += "\n/mute &lt;主机&gt; [时长]\n/unmute &lt;主机&gt;\n/rule &lt;id&gt; on|off"
	}
	if chat.Role >= model.BotRoleAdmin {
		text += "\n/chats\n/role &lt;chatID&gt; &lt;角色&gt;\n/revoke &lt;chatID&gt;\n/audit\n/health"
	}
	h.respond(ctx, text, markup([]models.InlineKeyboardButton{navHome()}))
}

func helpQueryText() string {
	return Bold("查询语法") + "\n" +
		"字段 cpu mem disk net total uptime load tag name ver\n" +
		"范围 today yesterday 7d 30d month 24h 2026-09-01..2026-09-20\n" +
		"聚合 avg: max: min:　排序 sort=-cpu　分组 group=tag　环比 vs=prev\n" +
		Code("/find cpu>80 tag=hk") + "\n" +
		Code("/find cpu>80 7d") + "\n" +
		Code("/top cpu 7d tag=hk") + "\n" +
		Code("/usage month group=tag") + "\n" +
		Code("/cmp tag=hk tag=jp 7d") + "\n" +
		Code("/chart hk-1 cpu 24h")
}

func (h *Hub) cmdWhoami(ctx context.Context) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	h.Reply(chat.ChatID, fmt.Sprintf("会话 %s\n角色 %s", Code(fmt.Sprintf("%d", chat.ChatID)), Escape(roleLabel(chat.Role))))
}

func (h *Hub) cmdTraffic(ctx context.Context) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	snap, err := report.Collect(report.Options{Sections: map[string]bool{"traffic": true}})
	if err != nil {
		h.Reply(chat.ChatID, "读取流量失败。")
		return
	}
	if len(snap.Traffic) == 0 {
		h.Reply(chat.ChatID, "暂无流量策略。")
		return
	}
	h.Reply(chat.ChatID, FormatSnapshot(snap, map[string]bool{"traffic": true}))
}

func (h *Hub) cmdReport(ctx context.Context, arg string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	fields := strings.Fields(arg)
	if len(fields) == 0 || fields[0] != "now" {
		h.Reply(chat.ChatID, "用法：/report now [daily|weekly|monthly]")
		return
	}
	period := model.BotPeriodDaily
	if len(fields) > 1 {
		switch fields[1] {
		case model.BotPeriodWeekly, model.BotPeriodMonthly, model.BotPeriodDaily:
			period = fields[1]
		}
	}
	row := &model.BotReport{
		ChatIDs:  fmt.Sprintf("%d", chat.ChatID),
		Period:   period,
		Sections: "status,servers,traffic,uptime,probes,alerts",
		Enabled:  model.BoolPtr(true),
	}
	if err := h.SendReport(row, true); err != nil {
		h.Reply(chat.ChatID, "生成报告失败。")
		h.audit(ctx, "report", period, "error")
		return
	}
	h.audit(ctx, "report", period, "ok")
}

func (h *Hub) cmdMute(ctx context.Context, arg string, unmute bool) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		h.Reply(chat.ChatID, "用法：/mute &lt;主机&gt; [时长]")
		return
	}
	server := report.FindServer(fields[0])
	if server == nil {
		h.Reply(chat.ChatID, "未找到主机。")
		return
	}
	if unmute {
		h.ReplyMarkup(chat.ChatID, "确认恢复 "+Escape(server.Name)+" 的告警？", confirmKeyboard("unmute", server.ID, 0))
		return
	}
	sec := int64(3600)
	if len(fields) > 1 {
		if d, ok := parseDurationSeconds(fields[1]); ok {
			sec = d
		}
	}
	h.ReplyMarkup(chat.ChatID, fmt.Sprintf("确认静音 %s %s？", Escape(server.Name), report.FormatDuration(uint64(sec))), confirmKeyboard("mute", server.ID, sec))
}

func (h *Hub) cmdRule(ctx context.Context, arg string, confirmed bool) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	fields := strings.Fields(arg)
	if len(fields) < 2 {
		h.Reply(chat.ChatID, "用法：/rule &lt;id&gt; on|off")
		return
	}
	id, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || id == 0 {
		h.Reply(chat.ChatID, "规则 ID 无效。")
		return
	}
	enable := fields[1] == "on"
	if !confirmed {
		action := "停用"
		if enable {
			action = "启用"
		}
		flag := int64(0)
		if enable {
			flag = 1
		}
		h.ReplyMarkup(chat.ChatID, fmt.Sprintf("确认%s规则 %s？", action, Code(fmt.Sprintf("%d", id))), confirmKeyboard("rule", id, flag))
		return
	}
	h.applyRule(ctx, id, enable)
}

func (h *Hub) applyMute(ctx context.Context, serverID uint64, sec int64, unmute bool) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	if unmute {
		singleton.BotUnmuteServer(serverID)
		h.Reply(chat.ChatID, "已恢复告警。")
		h.audit(ctx, "unmute", fmt.Sprintf("%d", serverID), "ok")
		return
	}
	if sec <= 0 {
		sec = 3600
	}
	singleton.BotMuteServer(serverID, time.Duration(sec)*time.Second)
	h.Reply(chat.ChatID, "已静音 "+report.FormatDuration(uint64(sec))+"。")
	h.audit(ctx, "mute", fmt.Sprintf("%d", serverID), "ok")
}

func (h *Hub) applyRule(ctx context.Context, id uint64, enable bool) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	var row model.AlertRule
	if singleton.DB.First(&row, id).Error != nil {
		h.Reply(chat.ChatID, "规则不存在。")
		return
	}
	row.Enable = model.BoolPtr(enable)
	if err := singleton.DB.Save(&row).Error; err != nil {
		h.Reply(chat.ChatID, "保存失败。")
		h.audit(ctx, "rule", fmt.Sprintf("%d", id), "error")
		return
	}
	singleton.OnRefreshOrAddAlert(row)
	state := "已停用"
	if enable {
		state = "已启用"
	}
	h.Reply(chat.ChatID, state+" "+Escape(row.Name))
	h.audit(ctx, "rule", fmt.Sprintf("%d", id), "ok")
}

func (h *Hub) cmdChats(ctx context.Context) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	var rows []model.BotChat
	if singleton.DB.Order("id desc").Limit(20).Find(&rows).Error != nil {
		h.Reply(chat.ChatID, "读取会话失败。")
		return
	}
	if len(rows) == 0 {
		h.Reply(chat.ChatID, "暂无授权会话。")
		return
	}
	var b strings.Builder
	b.WriteString(Bold("授权会话"))
	b.WriteString("\n")
	for _, row := range rows {
		state := "停用"
		if row.IsEnabled() {
			state = "启用"
		}
		title := row.Title
		if title == "" {
			title = "-"
		}
		b.WriteString(fmt.Sprintf("%s %s %s %s\n", Code(fmt.Sprintf("%d", row.ChatID)), Escape(title), Escape(roleLabel(row.Role)), Escape(state)))
	}
	h.respond(ctx, strings.TrimRight(b.String(), "\n"), markup([]models.InlineKeyboardButton{navHome()}))
}

func (h *Hub) cmdRole(ctx context.Context, arg string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	fields := strings.Fields(arg)
	if len(fields) < 2 {
		h.Reply(chat.ChatID, "用法：/role &lt;chatID&gt; &lt;viewer|operator|admin|blocked&gt;")
		return
	}
	id, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || id == 0 {
		h.Reply(chat.ChatID, "chatID 无效。")
		return
	}
	role, ok := model.ParseBotRole(fields[1])
	if !ok {
		h.Reply(chat.ChatID, "角色无效。")
		return
	}
	var row model.BotChat
	if singleton.DB.Where("chat_id = ?", id).First(&row).Error != nil {
		h.Reply(chat.ChatID, "会话不存在。")
		return
	}
	row.Role = role
	if role == model.BotRoleBlocked {
		row.Enabled = model.BoolPtr(false)
	}
	_ = singleton.DB.Save(&row).Error
	h.authz.Upsert(&row)
	h.Reply(chat.ChatID, "已更新角色。")
	h.audit(ctx, "role", fields[0], "ok")
}

func (h *Hub) cmdRevoke(ctx context.Context, arg string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if err != nil || id == 0 {
		h.Reply(chat.ChatID, "用法：/revoke &lt;chatID&gt;")
		return
	}
	result := singleton.DB.Unscoped().Where("chat_id = ?", id).Delete(&model.BotChat{})
	if result.RowsAffected == 0 {
		h.Reply(chat.ChatID, "会话不存在。")
		return
	}
	h.authz.Delete(id)
	h.Reply(chat.ChatID, "已撤销。")
	h.audit(ctx, "revoke", arg, "ok")
}

func (h *Hub) onCallback(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	data := update.CallbackQuery.Data
	_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID})
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	switch {
	case data == "x:" || strings.HasPrefix(data, "x:"):
		h.respond(ctx, "已取消。", nil)
	case strings.HasPrefix(data, "q:"):
		h.onQueryCallback(ctx, data)
	case strings.HasPrefix(data, "m:"):
		h.onMenuCallback(ctx, data)
	case strings.HasPrefix(data, "h:"):
		h.showHost(ctx, parseID(strings.TrimPrefix(data, "h:")))
	case strings.HasPrefix(data, "svc:"):
		h.showService(ctx, parseID(strings.TrimPrefix(data, "svc:")))
	case strings.HasPrefix(data, "of:"):
		h.showOffline(ctx, parseID(strings.TrimPrefix(data, "of:")))
	case strings.HasPrefix(data, "up:"):
		h.runKind(ctx, "uptime", fmt.Sprintf("id=%s 7d", strings.TrimPrefix(data, "up:")))
	case strings.HasPrefix(data, "u:"):
		h.onUsageCallback(ctx, data)
	case strings.HasPrefix(data, "ch:"):
		h.onChartCallback(ctx, data)
	case strings.HasPrefix(data, "cm:"):
		h.onCmpPick(ctx, data)
	case strings.HasPrefix(data, "c:mute:"):
		parts := strings.Split(data, ":")
		id := parseID(parts[2])
		sec := int64(3600)
		if len(parts) > 3 {
			sec, _ = strconv.ParseInt(parts[3], 10, 64)
		}
		h.applyMute(ctx, id, sec, false)
	case strings.HasPrefix(data, "c:unmute:"):
		parts := strings.Split(data, ":")
		h.applyMute(ctx, parseID(parts[2]), 0, true)
	case strings.HasPrefix(data, "c:rule:"):
		parts := strings.Split(data, ":")
		enable := len(parts) > 3 && parts[3] == "1"
		h.applyRule(ctx, parseID(parts[2]), enable)
	case strings.HasPrefix(data, "m:1h:"):
		h.applyMute(ctx, parseID(strings.TrimPrefix(data, "m:1h:")), 3600, false)
	}
}

func (h *Hub) onMenuCallback(ctx context.Context, data string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	target := strings.TrimPrefix(data, "m:")
	if strings.HasPrefix(target, "1h:") {
		h.applyMute(ctx, parseID(strings.TrimPrefix(target, "1h:")), 3600, false)
		return
	}
	name, arg, _ := strings.Cut(target, ":")
	switch name {
	case "home":
		h.cmdMenu(ctx)
	case "more":
		h.respond(ctx, Bold("三太子监控")+"\n"+Escape(roleLabel(chat.Role)), menuKeyboard(chat.Role, true))
	case "status":
		h.cmdStatus(ctx)
	case "servers":
		h.cmdServers(ctx, arg, 1)
	case "usage":
		h.runKind(ctx, "usage", "today")
	case "services":
		h.cmdServices(ctx)
	case "top":
		h.runKind(ctx, "top", "cpu")
	case "groups":
		h.runKind(ctx, "groups", "")
	case "uptime":
		h.runKind(ctx, "uptime", "7d")
	case "probes":
		h.cmdProbes(ctx)
	case "alerts":
		h.cmdAlerts(ctx)
	case "rules":
		h.cmdRules(ctx)
	case "collectors":
		h.cmdCollectors(ctx)
	case "agents":
		h.cmdAgents(ctx)
	case "help":
		h.cmdHelp(ctx, "")
	case "audit":
		h.cmdAudit(ctx)
	case "health":
		h.cmdHealth(ctx)
	case "chats":
		h.cmdChats(ctx)
	}
}

func (h *Hub) onQueryCallback(ctx context.Context, data string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	rest := strings.TrimPrefix(data, "q:")
	token, overlay, _ := strings.Cut(rest, ":")
	if h.queries == nil {
		h.respond(ctx, "查询已过期，请重发命令。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	q, _, ok := h.queries.Patch(token, chat.ChatID, overlay)
	if !ok {
		h.respond(ctx, "查询已过期，请重发命令。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	h.renderQuery(ctx, q)
}

func (h *Hub) onUsageCallback(ctx context.Context, data string) {
	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		h.runKind(ctx, "usage", "today")
		return
	}
	rng := parts[1]
	arg := rng
	if len(parts) > 2 {
		arg = rng + " id=" + parts[2]
	}
	h.runKind(ctx, "usage", arg)
}

func (h *Hub) onChartCallback(ctx context.Context, data string) {
	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return
	}
	id := parts[1]
	metric := "cpu"
	rng := "24h"
	if len(parts) > 2 {
		metric = parts[2]
	}
	if len(parts) > 3 {
		rng = parts[3]
	}
	h.runKind(ctx, "chart", fmt.Sprintf("id=%s %s %s", id, metric, rng))
}

func (h *Hub) onCmpPick(ctx context.Context, data string) {
	rest := strings.TrimPrefix(data, "cm:")
	left, right, ok := strings.Cut(rest, ":")
	if !ok || right == "" {
		hosts := report.AllHosts()
		if len(hosts) > 8 {
			hosts = hosts[:8]
		}
		h.respond(ctx, "选择对比的另一台主机。", pickHostMarkup(hosts, "cm:"+rest))
		return
	}
	h.runKind(ctx, "cmp", fmt.Sprintf("id=%s id=%s", left, right))
}

func confirmKeyboard(action string, id uint64, extra int64) *models.InlineKeyboardMarkup {
	ok := fmt.Sprintf("c:%s:%d:%d", action, id, extra)
	if extra == 0 {
		ok = fmt.Sprintf("c:%s:%d", action, id)
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: "确认", CallbackData: ok},
		{Text: "取消", CallbackData: "x:" + action},
	}}}
}

func parseDurationSeconds(value string) (int64, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	mult := int64(1)
	switch {
	case strings.HasSuffix(value, "h"):
		mult = 3600
		value = strings.TrimSuffix(value, "h")
	case strings.HasSuffix(value, "m"):
		mult = 60
		value = strings.TrimSuffix(value, "m")
	case strings.HasSuffix(value, "d"):
		mult = 86400
		value = strings.TrimSuffix(value, "d")
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n * mult, true
}

func roleLabel(role uint8) string {
	switch role {
	case model.BotRoleAdmin:
		return "管理"
	case model.BotRoleOperator:
		return "运维"
	case model.BotRoleViewer:
		return "观察"
	default:
		return "已停用"
	}
}
