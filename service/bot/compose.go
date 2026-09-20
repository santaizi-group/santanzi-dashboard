package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/hi2shark/santaizi-dashboard/service/report"
	"github.com/hi2shark/santaizi-dashboard/service/report/chart"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

func (h *Hub) runKind(ctx context.Context, kind, arg string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	q, err := parseKindQuery(kind, arg, singletonNow())
	if err != nil {
		h.respond(ctx, Escape(err.Error()), markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	h.renderQuery(ctx, q)
}

func (h *Hub) renderQuery(ctx context.Context, q Query) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	if h.queries == nil {
		h.queries = newQueryCache()
	}
	switch q.Kind {
	case "chart":
		h.renderChart(ctx, q)
		return
	case "cmp":
		h.renderCmp(ctx, q)
		return
	}
	items, missing, err := h.evalQuery(q)
	if err != nil {
		h.respond(ctx, queryError(err), markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	if q.Limit > 0 && q.Kind == "top" && len(items) > q.Limit {
		items = items[:q.Limit]
	}
	token := h.queries.Put(chat.ChatID, q)
	if q.GroupBy == "tag" {
		h.renderGroups(ctx, q, groupByTag(items), token, missing)
		return
	}
	pageSize := 6
	if q.Kind == "top" || q.Kind == "usage" {
		pageSize = 8
	}
	pageItems, page, total := pageSlice(items, q.Page, pageSize)
	text := formatEvalList(q, items, pageItems, page, total, missing)
	rows := evalHostButtons(pageItems)
	if q.Kind == "servers" || q.Kind == "find" {
		rows = append([][]models.InlineKeyboardButton{{
			btn("全部", "m:servers"), btn("在线", "m:servers:online"), btn("离线", "m:servers:offline"),
		}}, rows...)
	}
	chips := resultChips(token, q, page, total)
	all := append([][]models.InlineKeyboardButton{}, rows...)
	if chips != nil {
		all = append(all, chips.InlineKeyboard...)
	}
	h.respond(ctx, text, markup(all...))
	if h.chartsOn() && q.Kind == "top" && len(items) > 0 {
		png, err := barFor(q, items)
		if err == nil && len(png) > 0 {
			h.ReplyPhoto(chat.ChatID, png, "三太子监控 · 排行")
		}
	}
	if h.chartsOn() && q.Kind == "usage" && q.Range.To.Sub(q.Range.From) > 24*time.Hour {
		h.sendDailyUsageChart(ctx, q, items)
	}
}

func (h *Hub) renderList(ctx context.Context, q Query) {
	h.renderQuery(ctx, q)
}

func (h *Hub) renderGroups(ctx context.Context, q Query, groups []groupRow, token string, missing int) {
	var b strings.Builder
	b.WriteString(Bold("分组"))
	b.WriteString("　" + Escape(q.Range.Label()) + "\n")
	if len(groups) == 0 {
		b.WriteString("暂无分组")
		h.respond(ctx, b.String(), markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	for _, row := range groups {
		b.WriteString(fmt.Sprintf("%s %d/%d CPU %.0f%% MEM %.0f%% 流量 %s\n",
			Escape(row.Tag), row.Online, row.Count, row.CPU, row.Mem, report.FormatBytes(row.Total)))
	}
	if missing > 0 {
		b.WriteString(fmt.Sprintf("%d 台无上报\n", missing))
	}
	rows := make([][]models.InlineKeyboardButton, 0, len(groups)+1)
	for _, row := range groups {
		rows = append(rows, []models.InlineKeyboardButton{btn(row.Tag, "m:servers:"+row.Tag)})
	}
	chips := resultChips(token, q, 1, 1)
	if chips != nil {
		rows = append(rows, chips.InlineKeyboard...)
	}
	h.respond(ctx, strings.TrimRight(b.String(), "\n"), markup(rows...))
}

func formatEvalList(q Query, all, page []evalHost, pageNo, total, missing int) string {
	var b strings.Builder
	title := "主机"
	switch q.Kind {
	case "find":
		title = "筛选"
	case "top":
		title = "排行"
	case "usage":
		title = "统计"
	case "uptime":
		title = "可用率"
	}
	b.WriteString(Bold(title))
	b.WriteString("　" + Escape(q.Range.Label()))
	if q.Agg != "" && !q.Range.IsNow() {
		b.WriteString("　" + Escape(q.Agg))
	}
	b.WriteString(fmt.Sprintf("\n%d/%d　共 %d 台\n", pageNo, total, len(all)))
	if q.Kind == "usage" && len(all) > 0 {
		var in, out, totalBytes uint64
		var prev uint64
		var prevOK bool
		for _, item := range all {
			in += item.In
			out += item.Out
			totalBytes += item.Total
			if q.Compare == "prev" {
				prev += item.PrevTotal
				prevOK = true
			}
		}
		b.WriteString(fmt.Sprintf("入站 %s　出站 %s　合计 %s\n", report.FormatBytes(in), report.FormatBytes(out), report.FormatBytes(totalBytes)))
		if prevOK {
			b.WriteString("环比 " + Escape(deltaLabel(totalBytes, prev, 1)) + "\n")
		}
	}
	if len(page) == 0 {
		b.WriteString("没有符合条件的主机。")
		return b.String()
	}
	for _, item := range page {
		b.WriteString(formatEvalLine(q, item))
		b.WriteByte('\n')
	}
	if missing > 0 {
		b.WriteString(fmt.Sprintf("%d 台无上报", missing))
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatEvalLine(q Query, item evalHost) string {
	state := "离线"
	if item.Host.Online {
		state = "在线"
	}
	switch q.Kind {
	case "usage":
		line := fmt.Sprintf("%s %s %s", Escape(item.Host.Name), report.FormatBytes(item.Total), Escape(state))
		if q.Compare == "prev" {
			line += " " + Escape(deltaLabel(item.Total, item.PrevTotal, item.Prev.Samples))
		}
		return line
	case "uptime":
		longest := ""
		if item.Uptime.LongestSec > 0 {
			longest = " 最长 " + report.FormatDuration(item.Uptime.LongestSec)
		}
		return fmt.Sprintf("%s %.2f%% 离线 %s%s", Escape(item.Host.Name), item.Uptime.Percent, report.FormatDuration(item.Uptime.OfflineSec), longest)
	case "top":
		return fmt.Sprintf("%s %s %s", Escape(item.Host.Name), Escape(metricText(q.Metric, item)), Escape(state))
	default:
		return fmt.Sprintf("%s %s CPU %.0f%% MEM %.0f%% %s", Escape(item.Host.Name), Escape(state), item.Host.CPU, item.Host.MemPct, Escape(item.Host.Tag))
	}
}

func metricText(metric string, item evalHost) string {
	switch metric {
	case "mem":
		return fmt.Sprintf("MEM %.0f%%", item.Host.MemPct)
	case "disk":
		return fmt.Sprintf("磁盘 %.0f%%", item.Host.DiskPct)
	case "net":
		return "网速 " + report.FormatBytes(item.Host.NetInSpeed+item.Host.NetOutSpeed) + "/s"
	case "total":
		return report.FormatBytes(item.Total)
	case "load":
		return fmt.Sprintf("负载 %.2f", item.Host.Load1)
	default:
		return fmt.Sprintf("CPU %.0f%%", item.Host.CPU)
	}
}

func evalHostButtons(items []evalHost) [][]models.InlineKeyboardButton {
	rows := make([][]models.InlineKeyboardButton, 0, len(items))
	for _, item := range items {
		rows = append(rows, []models.InlineKeyboardButton{btn(item.Host.Name, fmt.Sprintf("h:%d", item.Host.ID))})
	}
	return rows
}

func barFor(q Query, items []evalHost) ([]byte, error) {
	n := len(items)
	if n > 8 {
		n = 8
	}
	bars := make([]chart.BarItem, 0, n)
	for i := 0; i < n; i++ {
		bars = append(bars, chart.BarItem{Label: items[i].Host.Name, Value: metricValue(items[i], q.Metric)})
	}
	title := q.Metric
	if title == "" {
		title = "cpu"
	}
	return chart.Bar(title, bars)
}

func (h *Hub) renderChart(ctx context.Context, q Query) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	items, _, err := h.evalQuery(q)
	if err != nil {
		h.respond(ctx, queryError(err), markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	if len(items) == 0 {
		h.respond(ctx, "未找到主机。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	resolution := "1h"
	if q.Range.Kind == "24h" || q.Range.To.Sub(q.Range.From) <= 25*time.Hour {
		resolution = "1m"
	}
	nodes := make([][]byte, 0, len(items))
	byNode := map[string]evalHost{}
	for _, item := range items {
		if len(item.Host.NodeUUID) == 0 {
			continue
		}
		nodes = append(nodes, item.Host.NodeUUID)
		byNode[string(item.Host.NodeUUID)] = item
	}
	if len(nodes) == 0 {
		h.respond(ctx, "这台主机暂无上报。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	seriesMap, err := report.RangeSeries(singleton.DB, nodes, q.Range.From, q.Range.To, resolution)
	if err != nil {
		h.respond(ctx, queryError(err), markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	metric := q.Metric
	if metric == "" {
		metric = "cpu"
	}
	var lines []chart.Series
	var summary strings.Builder
	summary.WriteString(Bold("曲线") + "　" + Escape(metric) + "　" + Escape(q.Range.Label()) + "\n")
	limit := 4
	averaged := false
	if len(nodes) > limit {
		averaged = true
	}
	if averaged {
		points := averageSeries(seriesMap, items, metric)
		points = downsample(points, 96)
		lines = append(lines, chart.Series{Name: fmt.Sprintf("avg %d", len(nodes)), Points: points})
		summary.WriteString(fmt.Sprintf("组内 %d 台平均\n", len(nodes)))
	} else {
		count := 0
		for _, item := range items {
			pts := seriesMap[string(item.Host.NodeUUID)]
			values := metricSeries(pts, item, metric)
			values = downsample(values, 96)
			if len(values) == 0 {
				continue
			}
			lines = append(lines, chart.Series{Name: item.Host.Name, Points: values})
			mn, avg, mx := minAvgMax(values)
			summary.WriteString(fmt.Sprintf("%s min %.0f avg %.0f max %.0f\n", Escape(item.Host.Name), mn, avg, mx))
			count++
			if count >= limit {
				break
			}
		}
	}
	caption := strings.TrimRight(summary.String(), "\n")
	if h.chartsOn() && len(lines) > 0 {
		png, err := chart.Line("chart", lines, nil)
		if err == nil && len(png) > 0 {
			h.respondPhoto(ctx, png, caption, markup([]models.InlineKeyboardButton{navHome()}))
			return
		}
	}
	h.respond(ctx, caption, markup([]models.InlineKeyboardButton{navHome()}))
}

func (h *Hub) renderCmp(ctx context.Context, q Query) {
	if len(q.Left) == 0 || len(q.Right) == 0 {
		h.respond(ctx, "用法：/cmp &lt;主机或分组&gt; &lt;主机或分组&gt; [范围]", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	leftQ := q.Clone()
	leftQ.Kind = "find"
	leftQ.Filters = q.Left
	rightQ := q.Clone()
	rightQ.Kind = "find"
	rightQ.Filters = q.Right
	left, _, err := h.evalQuery(leftQ)
	if err != nil {
		h.respond(ctx, queryError(err), markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	right, _, err := h.evalQuery(rightQ)
	if err != nil {
		h.respond(ctx, queryError(err), markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	h.respond(ctx, formatCmp(q, left, right), markup([]models.InlineKeyboardButton{navHome()}))
}

func formatCmp(q Query, left, right []evalHost) string {
	var b strings.Builder
	b.WriteString(Bold("对比") + "　" + Escape(q.Range.Label()) + "\n")
	b.WriteString(formatCmpSide("A", left) + "\n")
	b.WriteString(formatCmpSide("B", right))
	return b.String()
}

func formatCmpSide(label string, items []evalHost) string {
	if len(items) == 0 {
		return label + " 无主机"
	}
	var cpu, mem, disk float64
	var net, total uint64
	var uptime float64
	online := 0
	for _, item := range items {
		cpu += item.Host.CPU
		mem += item.Host.MemPct
		disk += item.Host.DiskPct
		net += item.Host.NetInSpeed + item.Host.NetOutSpeed
		total += item.Total
		uptime += item.Uptime.Percent
		if item.Host.Online {
			online++
		}
	}
	n := float64(len(items))
	name := items[0].Host.Name
	if len(items) > 1 {
		name = fmt.Sprintf("%d 台", len(items))
	}
	up := uptime / n
	return fmt.Sprintf("%s %s\n在线 %d/%d CPU %.0f%% MEM %.0f%% 磁盘 %.0f%%\n网速 %s/s 流量 %s 可用率 %.2f%%",
		label, Escape(name), online, len(items), cpu/n, mem/n, disk/n,
		report.FormatBytes(net/uint64(len(items))), report.FormatBytes(total), up)
}

func metricSeries(points []report.SeriesPoint, item evalHost, metric string) []float64 {
	out := make([]float64, 0, len(points))
	for _, point := range points {
		switch metric {
		case "mem":
			out = append(out, pctOf(uint64(point.Mem), item.Host.MemTotal))
		case "disk":
			out = append(out, pctOf(uint64(point.Disk), item.Host.DiskTotal))
		case "net":
			out = append(out, point.NetInSpeed+point.NetOutSpeed)
		case "load":
			out = append(out, point.Load)
		default:
			out = append(out, point.CPU)
		}
	}
	return out
}

func averageSeries(series map[string][]report.SeriesPoint, items []evalHost, metric string) []float64 {
	var longest []report.SeriesPoint
	for _, pts := range series {
		if len(pts) > len(longest) {
			longest = pts
		}
	}
	if len(longest) == 0 {
		return nil
	}
	sums := make([]float64, len(longest))
	counts := make([]float64, len(longest))
	byName := map[string]evalHost{}
	for _, item := range items {
		byName[string(item.Host.NodeUUID)] = item
	}
	for key, pts := range series {
		item := byName[key]
		for i, point := range pts {
			if i >= len(sums) {
				break
			}
			var value float64
			switch metric {
			case "mem":
				value = pctOf(uint64(point.Mem), item.Host.MemTotal)
			case "disk":
				value = pctOf(uint64(point.Disk), item.Host.DiskTotal)
			case "net":
				value = point.NetInSpeed + point.NetOutSpeed
			default:
				value = point.CPU
			}
			sums[i] += value
			counts[i]++
		}
	}
	out := make([]float64, 0, len(sums))
	for i, sum := range sums {
		if counts[i] == 0 {
			out = append(out, 0)
			continue
		}
		out = append(out, sum/counts[i])
	}
	return out
}

func downsample(values []float64, max int) []float64 {
	if max <= 0 || len(values) <= max {
		return values
	}
	out := make([]float64, max)
	for i := 0; i < max; i++ {
		start := i * len(values) / max
		end := (i + 1) * len(values) / max
		if end <= start {
			end = start + 1
		}
		if end > len(values) {
			end = len(values)
		}
		var sum float64
		peak := values[start]
		for _, v := range values[start:end] {
			sum += v
			if v > peak {
				peak = v
			}
		}
		out[i] = peak
		_ = sum
	}
	return out
}

func minAvgMax(values []float64) (min, avg, max float64) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	min, max = values[0], values[0]
	var sum float64
	for _, v := range values {
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, sum / float64(len(values)), max
}

func queryError(err error) string {
	if err == nil {
		return "查询失败。"
	}
	if err == report.ErrRangeTooLarge || err == errQueryTooLarge {
		return errQueryTooLarge.Error()
	}
	return "读取失败。"
}

func (h *Hub) sendDailyUsageChart(ctx context.Context, q Query, items []evalHost) {
	if singleton.DB == nil {
		return
	}
	nodes := make([][]byte, 0, len(items))
	for _, item := range items {
		if len(item.Host.NodeUUID) > 0 {
			nodes = append(nodes, item.Host.NodeUUID)
		}
	}
	if len(nodes) == 0 {
		return
	}
	points, err := report.RangeDailyTraffic(singleton.DB, nodes, q.Range.From, q.Range.To, singleton.Loc)
	if err != nil || len(points) < 2 {
		return
	}
	bars := make([]chart.BarItem, 0, len(points))
	for _, point := range points {
		if len(bars) >= 12 {
			break
		}
		bars = append(bars, chart.BarItem{Label: point.Start.Format("01-02"), Value: float64(point.Bytes)})
	}
	png, err := chart.Bar("traffic", bars)
	if err != nil || len(png) == 0 {
		return
	}
	chat := chatFrom(ctx)
	if chat != nil {
		h.ReplyPhoto(chat.ChatID, png, "三太子监控 · 流量")
	}
}
