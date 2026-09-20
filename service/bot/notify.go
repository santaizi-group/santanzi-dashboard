package bot

import (
	"fmt"

	"github.com/go-telegram/bot/models"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

func (h *Hub) forwardNotification(tag, desc string, server *model.Server) {
	if h == nil || h.authz == nil {
		return
	}
	if server != nil && server.ID != 0 && singleton.IsBotMuted(server.ID) {
		return
	}
	subs := h.authz.Subscribers(tag)
	if len(subs) == 0 {
		return
	}
	text := Bold("三太子监控") + "\n" + Escape(desc)
	var markup *models.InlineKeyboardMarkup
	if server != nil && server.ID != 0 {
		markup = &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: "主机详情", CallbackData: fmt.Sprintf("h:%d", server.ID)},
			{Text: "静音 1 小时", CallbackData: fmt.Sprintf("m:1h:%d", server.ID)},
		}}}
	}
	for _, chat := range subs {
		if chat.Role < model.BotRoleViewer {
			continue
		}
		h.ReplyMarkup(chat.ChatID, text, markup)
	}
}
