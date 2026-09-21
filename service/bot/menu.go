package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/report"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	trafficservice "github.com/hi2shark/santaizi-dashboard/service/traffic"
)

func (h *Hub) cmdMenu(ctx context.Context) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	h.respond(ctx, Bold("三太子监控")+"\n"+Escape(roleLabel(chat.Role)), menuKeyboard(chat.Role, false))
}

func menuKeyboard(role uint8, more bool) *models.InlineKeyboardMarkup {
	if more {
		rows := [][]models.InlineKeyboardButton{
			{btn("规则", "m:rules"), btn("从端", "m:collectors")},
			{btn("探针版本", "m:agents"), btn("帮助", "m:help")},
		}
		if role >= model.BotRoleAdmin {
			rows = append(rows, []models.InlineKeyboardButton{btn("操作日志", "m:audit"), btn("自检", "m:health")})
			rows = append(rows, []models.InlineKeyboardButton{btn("会话", "m:chats")})
		}
		rows = append(rows, []models.InlineKeyboardButton{btn("返回", "m:home")})
		return markup(rows...)
	}
	return markup(
		[]models.InlineKeyboardButton{btn("总览", "m:status"), btn("主机", "m:servers")},
		[]models.InlineKeyboardButton{btn("统计", "m:usage"), btn("服务监控", "m:services")},
		[]models.InlineKeyboardButton{btn("排行", "m:top"), btn("分组", "m:groups")},
		[]models.InlineKeyboardButton{btn("可用率", "m:uptime"), btn("探针", "m:probes")},
		[]models.InlineKeyboardButton{btn("连通异常", "m:alerts"), btn("更多", "m:more")},
	)
}

func (h *Hub) cmdServers(ctx context.Context, arg string, page int) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	q, err := parseKindQuery("servers", arg, singletonNow())
	if err != nil {
		h.respond(ctx, Escape(err.Error()), markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	q.Page = page
	h.renderList(ctx, q)
}

func (h *Hub) cmdServer(ctx context.Context, query string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	hosts := report.FilterHosts(query)
	if strings.TrimSpace(query) == "" || len(hosts) == 0 {
		h.respond(ctx, "未找到主机。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	if len(hosts) > 1 {
		h.respond(ctx, pickHostText(hosts), pickHostMarkup(hosts, "h"))
		return
	}
	h.showHost(ctx, hosts[0].ID)
}

func (h *Hub) searchHosts(ctx context.Context, text string) {
	hosts := report.FilterHosts(text)
	switch len(hosts) {
	case 0:
		h.Reply(chatFrom(ctx).ChatID, "未找到主机。")
	case 1:
		h.showHost(ctx, hosts[0].ID)
	default:
		if len(hosts) > 8 {
			hosts = hosts[:8]
		}
		h.ReplyMarkup(chatFrom(ctx).ChatID, pickHostText(hosts), pickHostMarkup(hosts, "h"))
	}
}

func pickHostText(hosts []report.HostRow) string {
	return Bold("选择主机") + fmt.Sprintf("\n共 %d 台", len(hosts))
}

func pickHostMarkup(hosts []report.HostRow, prefix string) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(hosts)+1)
	for _, host := range hosts {
		label := host.Name
		if host.Tag != "" {
			label = host.Name + " · " + host.Tag
		}
		rows = append(rows, []models.InlineKeyboardButton{btn(label, fmt.Sprintf("%s:%d", prefix, host.ID))})
	}
	rows = append(rows, []models.InlineKeyboardButton{navHome()})
	return markup(rows...)
}

func (h *Hub) showHost(ctx context.Context, id uint64) {
	host, ok := report.HostByID(id)
	if !ok {
		h.respond(ctx, "未找到主机。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	text := FormatHost(host)
	if singleton.DB != nil {
		summaries, err := trafficservice.Summaries(singleton.DB, []uint64{host.ID}, singletonNow())
		if err == nil {
			for _, item := range summaries[host.ID] {
				text += fmt.Sprintf("\n策略 %s %s/%s %.0f%%", Escape(item.Name), report.FormatBytes(item.UsedBytes), report.FormatBytes(item.QuotaBytes), item.UsagePercent)
			}
		}
	}
	h.respond(ctx, text, hostKeyboard(host.ID))
}

func hostKeyboard(id uint64) *models.InlineKeyboardMarkup {
	return markup(
		[]models.InlineKeyboardButton{btn("曲线", fmt.Sprintf("ch:%d:cpu:24h", id)), btn("统计", fmt.Sprintf("u:today:%d", id))},
		[]models.InlineKeyboardButton{btn("可用率", fmt.Sprintf("up:%d", id)), btn("离线记录", fmt.Sprintf("of:%d", id))},
		[]models.InlineKeyboardButton{btn("对比", fmt.Sprintf("cm:%d", id)), btn("返回列表", "m:servers")},
		[]models.InlineKeyboardButton{navHome()},
	)
}

func (h *Hub) cmdStatus(ctx context.Context) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	snap, err := report.Collect(report.Options{Sections: map[string]bool{"status": true, "probes": true}})
	if err != nil {
		h.respond(ctx, "读取总览失败。", markup([]models.InlineKeyboardButton{navHome()}))
		return
	}
	h.respond(ctx, FormatSnapshot(snap, map[string]bool{"status": true, "probes": true}), markup([]models.InlineKeyboardButton{navHome()}))
}
