package bot

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/report"
	"github.com/hi2shark/santaizi-dashboard/service/report/chart"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"github.com/hi2shark/santaizi-dashboard/service/telemetry"
)

func (h *Hub) cmdServices(ctx context.Context) {
	if singleton.ServiceSentinelShared == nil {
		h.respond(ctx, "暂无服务监控。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	stats := singleton.ServiceSentinelShared.LoadStats()
	if len(stats) == 0 {
		h.respond(ctx, "暂无服务监控。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	ids := make([]uint64, 0, len(stats))
	for id := range stats {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var b strings.Builder
	b.WriteString(Bold("服务监控") + "\n")
	rows := make([][]models.InlineKeyboardButton, 0, len(ids)+1)
	for _, id := range ids {
		item := stats[id]
		name := fmt.Sprintf("#%d", id)
		if item.Monitor != nil && item.Monitor.Name != "" {
			name = item.Monitor.Name
		}
		b.WriteString(fmt.Sprintf("%s 可用 %.1f%% 延迟 %.0fms  up %d down %d\n",
			Escape(name), item.TotalUptime(), avgDelay(item), item.CurrentUp, item.CurrentDown))
		rows = append(rows, []models.InlineKeyboardButton{btn(name, fmt.Sprintf("svc:%d", id))})
	}
	rows = append(rows, []models.InlineKeyboardButton{navHome()})
	h.respond(ctx, strings.TrimRight(b.String(), "\n"), markup(rows...))
}

func avgDelay(item *model.ServiceItemResponse) float32 {
	if item == nil || item.Delay == nil {
		return 0
	}
	var sum float32
	var n int
	for _, d := range item.Delay {
		if d > 0 {
			sum += d
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float32(n)
}

func (h *Hub) showService(ctx context.Context, id uint64) {
	if singleton.ServiceSentinelShared == nil {
		h.respond(ctx, "暂无服务监控。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	stats := singleton.ServiceSentinelShared.LoadStats()
	item := stats[id]
	if item == nil || item.Monitor == nil {
		h.respond(ctx, "未找到该监控。", markup([]models.InlineKeyboardButton{btn("返回", "m:services"), navHome()}))
		return
	}
	var b strings.Builder
	b.WriteString(Bold(item.Monitor.Name) + "\n")
	b.WriteString(fmt.Sprintf("30 天可用率 %.2f%%　当前 up %d down %d\n", item.TotalUptime(), item.CurrentUp, item.CurrentDown))
	if item.Up != nil && item.Down != nil {
		for i := 0; i < 30; i++ {
			if item.Up[i] == 0 && item.Down[i] == 0 {
				continue
			}
			delay := float32(0)
			if item.Delay != nil {
				delay = item.Delay[i]
			}
			b.WriteString(fmt.Sprintf("D%d up %d down %d  %.0fms\n", i+1, item.Up[i], item.Down[i], delay))
		}
	}
	caption := strings.TrimRight(b.String(), "\n")
	back := markup([]models.InlineKeyboardButton{btn("返回", "m:services"), navHome()})
	if h.chartsOn() && item.Delay != nil {
		points := make([]float64, 0, 30)
		for _, d := range item.Delay {
			points = append(points, float64(d))
		}
		png, err := chart.Line("delay", []chart.Series{{Name: "ms", Points: points}}, nil)
		if err == nil && len(png) > 0 {
			h.respondPhoto(ctx, png, caption, back)
			return
		}
	}
	h.respond(ctx, caption, back)
}

func (h *Hub) cmdRules(ctx context.Context) {
	if singleton.DB == nil {
		h.respond(ctx, "读取失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	var rows []model.AlertRule
	if singleton.DB.Order("id asc").Limit(30).Find(&rows).Error != nil {
		h.respond(ctx, "读取规则失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	if len(rows) == 0 {
		h.respond(ctx, "暂无告警规则。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	var b strings.Builder
	b.WriteString(Bold("告警规则") + "\n")
	for _, row := range rows {
		state := "停用"
		if row.Enabled() {
			state = "启用"
		}
		summary := row.RulesSummary()
		if summary == "" {
			summary = "-"
		}
		tag := row.NotificationTag
		if tag == "" {
			tag = "default"
		}
		b.WriteString(fmt.Sprintf("%s %s %s\n%s　%s\n", Code(fmt.Sprintf("%d", row.ID)), Escape(row.Name), Escape(state), Escape(summary), Escape(tag)))
	}
	h.respond(ctx, strings.TrimRight(b.String(), "\n"), markup([]models.InlineKeyboardButton{navHome()}))
}

func (h *Hub) cmdCollectors(ctx context.Context) {
	if singleton.DB == nil {
		h.respond(ctx, "读取失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	now := singletonNow()
	var collectors []model.Collector
	if singleton.DB.Where("deleted = ? AND revoked = ?", false, false).Order("name asc").Find(&collectors).Error != nil {
		h.respond(ctx, "读取从端失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	if len(collectors) == 0 {
		h.respond(ctx, "暂无从端。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	ids := make([]string, 0, len(collectors))
	for _, collector := range collectors {
		ids = append(ids, collector.CollectorUUID)
	}
	var runtimes []model.CollectorRuntime
	_ = singleton.DB.Where("collector_uuid IN ?", ids).Find(&runtimes).Error
	seen := map[string]int64{}
	for _, runtime := range runtimes {
		seen[runtime.CollectorUUID] = runtime.LastSeen
	}
	var b strings.Builder
	b.WriteString(Bold("从端") + "\n")
	for _, collector := range collectors {
		kind := "观测型"
		if collector.IsProbe() {
			kind = "探测型"
		}
		status := statusLabel(telemetry.CollectorStatus(seen[collector.CollectorUUID], now))
		last := "从未"
		if seen[collector.CollectorUUID] > 0 {
			last = time.Unix(0, seen[collector.CollectorUUID]).In(now.Location()).Format("01-02 15:04")
		}
		b.WriteString(fmt.Sprintf("%s %s %s　%s\n", Escape(collector.Name), Escape(kind), Escape(status), Escape(last)))
	}
	h.respond(ctx, strings.TrimRight(b.String(), "\n"), markup([]models.InlineKeyboardButton{navHome()}))
}

func statusLabel(status string) string {
	switch status {
	case telemetry.CollectorStatusOnline:
		return "在线"
	case telemetry.CollectorStatusOffline:
		return "离线"
	default:
		return "未知"
	}
}

func (h *Hub) cmdAgents(ctx context.Context) {
	hosts := report.AllHosts()
	if len(hosts) == 0 {
		h.respond(ctx, "暂无主机。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	byVer := map[string][]string{}
	byOS := map[string]int{}
	latest := ""
	for _, host := range hosts {
		ver := host.AgentVersion
		if ver == "" {
			ver = "未知"
		}
		byVer[ver] = append(byVer[ver], host.Name)
		if latest == "" || compareVer(ver, latest) > 0 {
			latest = ver
		}
		os := strings.TrimSpace(host.Platform + " " + host.Arch)
		if os == "" {
			os = "未知"
		}
		byOS[os]++
	}
	vers := make([]string, 0, len(byVer))
	for ver := range byVer {
		vers = append(vers, ver)
	}
	sort.Slice(vers, func(i, j int) bool { return compareVer(vers[i], vers[j]) > 0 })
	var b strings.Builder
	b.WriteString(Bold("探针版本") + "\n")
	for _, ver := range vers {
		b.WriteString(fmt.Sprintf("%s　%d 台\n", Escape(ver), len(byVer[ver])))
	}
	if latest != "" && latest != "未知" {
		var behind []string
		for ver, names := range byVer {
			if ver == "未知" || compareVer(ver, latest) < 0 {
				behind = append(behind, names...)
			}
		}
		if len(behind) > 0 {
			if len(behind) > 8 {
				behind = behind[:8]
			}
			b.WriteString("落后 " + Escape(strings.Join(behind, "、")) + "\n")
		}
	}
	b.WriteString(Bold("系统") + "\n")
	oss := make([]string, 0, len(byOS))
	for os := range byOS {
		oss = append(oss, os)
	}
	sort.Strings(oss)
	for _, os := range oss {
		b.WriteString(fmt.Sprintf("%s　%d\n", Escape(os), byOS[os]))
	}
	h.respond(ctx, strings.TrimRight(b.String(), "\n"), markup([]models.InlineKeyboardButton{navHome()}))
}

func (h *Hub) cmdOffline(ctx context.Context, arg string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	query := strings.TrimSpace(arg)
	if query == "" {
		h.respond(ctx, "用法：/offline &lt;主机&gt;", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	hosts := report.FilterHosts(query)
	if len(hosts) == 0 {
		h.respond(ctx, "未找到主机。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	if len(hosts) > 1 {
		h.respond(ctx, pickHostText(hosts), pickHostMarkup(hosts, "of"))
		return
	}
	h.showOffline(ctx, hosts[0].ID)
}

func (h *Hub) showOffline(ctx context.Context, id uint64) {
	hosts := report.FilterHosts(fmt.Sprintf("%d", id))
	if len(hosts) == 0 {
		h.respond(ctx, "未找到主机。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	if singleton.DB == nil {
		h.respond(ctx, "读取失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	var rows []model.ServerOfflineHistory
	if singleton.DB.Where("server_id = ?", id).Order("started_at desc").Limit(10).Find(&rows).Error != nil {
		h.respond(ctx, "读取离线记录失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	var b strings.Builder
	b.WriteString(Bold(hosts[0].Name) + " 离线记录\n")
	if len(rows) == 0 {
		b.WriteString("暂无离线记录。")
		h.respond(ctx, b.String(), markup([]models.InlineKeyboardButton{btn("主机", fmt.Sprintf("h:%d", id)), navHome()}))
		return
	}
	for _, row := range rows {
		ended := "进行中"
		if row.EndedAt != nil {
			ended = row.EndedAt.Format("01-02 15:04")
		}
		reason := offlineReasonLabel(row)
		dur := row.DurationSeconds
		if dur == 0 && row.EndedAt != nil {
			dur = uint64(row.EndedAt.Sub(row.StartedAt).Seconds())
		}
		b.WriteString(fmt.Sprintf("%s → %s　%s　%s\n",
			Escape(row.StartedAt.Format("01-02 15:04")), Escape(ended),
			Escape(report.FormatDuration(dur)), Escape(reason)))
	}
	h.respond(ctx, strings.TrimRight(b.String(), "\n"), markup([]models.InlineKeyboardButton{btn("主机", fmt.Sprintf("h:%d", id)), navHome()}))
}

func offlineReasonLabel(row model.ServerOfflineHistory) string {
	reason := row.Reason
	if reason == "" || reason == model.OfflineReasonUnknown {
		reason = singleton.DetectOfflineReason(row.LastBootTime, row.RecoveredBootTime, row.LastUptime, row.RecoveredUptime)
	}
	switch reason {
	case model.OfflineReasonMachineReboot:
		return "重启"
	case model.OfflineReasonNetworkDisconnect:
		return "网络中断"
	case model.OfflineReasonAgentRestart:
		return "探针重启"
	case model.OfflineReasonDashboardRestart:
		return "面板重启"
	case model.OfflineReasonManual:
		return "手动"
	default:
		return "未知"
	}
}

func (h *Hub) cmdProbes(ctx context.Context) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	snap, err := report.Collect(report.Options{Sections: map[string]bool{"probes": true}})
	if err != nil {
		h.respond(ctx, "读取探针观察失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	text := FormatSnapshot(snap, map[string]bool{"probes": true})
	if singleton.DB != nil {
		paths, err := telemetry.LoadProbePaths(singleton.DB, telemetry.ProbePathFilter{})
		if err == nil {
			var down []telemetry.ProbePath
			for _, path := range paths {
				if path.TargetSource == "none" || path.SampledAt == 0 || path.Reachable {
					continue
				}
				down = append(down, path)
				if len(down) >= 8 {
					break
				}
			}
			if len(down) > 0 {
				text += "\n" + Bold("异常路径") + "\n"
				now := singletonNow()
				for _, path := range down {
					when := time.Unix(0, path.SampledAt).In(now.Location()).Format("15:04")
					target := path.Hostname
					if target == "" {
						target = path.ServerName
					}
					text += fmt.Sprintf("%s %s　%s\n", Escape(path.CollectorName), Escape(target), Escape(when))
				}
				text = strings.TrimRight(text, "\n")
			}
		}
	}
	h.respond(ctx, text, markup([]models.InlineKeyboardButton{navHome()}))
}

func (h *Hub) cmdAlerts(ctx context.Context) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	snap, err := report.Collect(report.Options{Sections: map[string]bool{"alerts": true}})
	if err != nil {
		h.respond(ctx, "读取连通异常失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	if len(snap.Alerts) == 0 {
		h.respond(ctx, "当前无连通异常。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	h.respond(ctx, FormatSnapshot(snap, map[string]bool{"alerts": true}), markup([]models.InlineKeyboardButton{navHome()}))
}

func (h *Hub) cmdAudit(ctx context.Context) {
	if singleton.DB == nil {
		h.respond(ctx, "读取失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	var rows []model.BotAuditLog
	if singleton.DB.Order("id desc").Limit(10).Find(&rows).Error != nil {
		h.respond(ctx, "读取操作日志失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	if len(rows) == 0 {
		h.respond(ctx, "暂无操作日志。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	var b strings.Builder
	b.WriteString(Bold("操作日志") + "\n")
	for _, row := range rows {
		b.WriteString(fmt.Sprintf("%s %s %s %s %s\n",
			Escape(row.CreatedAt.Format("01-02 15:04")),
			Code(fmt.Sprintf("%d", row.ChatID)),
			Escape(roleLabel(row.Role)),
			Escape(row.Command),
			Escape(row.Result)))
	}
	h.respond(ctx, strings.TrimRight(b.String(), "\n"), markup([]models.InlineKeyboardButton{navHome()}))
}

func (h *Hub) cmdHealth(ctx context.Context) {
	now := singletonNow()
	var b strings.Builder
	b.WriteString(Bold("面板自检") + "\n")
	b.WriteString("版本 " + Escape(singleton.Version) + "\n")
	b.WriteString("运行 " + Escape(report.FormatDuration(uint64(now.Sub(processStarted).Seconds()))) + "\n")
	if singleton.DB != nil {
		var latest int64
		_ = singleton.DB.Model(&model.StateRollup{}).Select("MAX(window_end)").Scan(&latest).Error
		if latest > 0 {
			at := time.Unix(0, latest).In(now.Location())
			lag := now.Sub(at)
			if lag < 0 {
				lag = 0
			}
			b.WriteString(fmt.Sprintf("聚合 %s　滞后 %s\n", Escape(at.Format("15:04:05")), Escape(report.FormatDuration(uint64(lag.Seconds())))))
		} else {
			b.WriteString("聚合 无数据\n")
		}
		var reports []model.BotReport
		_ = singleton.DB.Order("id desc").Limit(5).Find(&reports).Error
		if len(reports) == 0 {
			b.WriteString("周期报告 未配置")
		} else {
			b.WriteString(Bold("周期报告") + "\n")
			for _, row := range reports {
				when := "从未"
				if row.LastRunAt != nil {
					when = row.LastRunAt.Format("01-02 15:04")
				}
				status := row.LastStatus
				if status == "" {
					status = "-"
				}
				name := row.Name
				if name == "" {
					name = row.Period
				}
				b.WriteString(fmt.Sprintf("%s %s %s\n", Escape(name), Escape(when), Escape(status)))
			}
		}
	}
	h.respond(ctx, strings.TrimRight(b.String(), "\n"), markup([]models.InlineKeyboardButton{navHome()}))
}

func parseID(raw string) uint64 {
	id, _ := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	return id
}
