package bot

import (
	"fmt"

	"github.com/go-telegram/bot/models"
)

func btn(text, data string) models.InlineKeyboardButton {
	if len(data) > 64 {
		data = data[:64]
	}
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

func markup(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	cleaned := make([][]models.InlineKeyboardButton, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		cleaned = append(cleaned, row)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: cleaned}
}

func navHome() models.InlineKeyboardButton {
	return btn("菜单", "m:home")
}

func queryCallback(token, overlay string) string {
	if overlay == "" {
		return "q:" + token
	}
	return fmt.Sprintf("q:%s:%s", token, overlay)
}

func resultChips(token string, q Query, page, total int) *models.InlineKeyboardMarkup {
	rangeRow := []models.InlineKeyboardButton{
		btn("今日", queryCallback(token, "r=today")),
		btn("7天", queryCallback(token, "r=7d")),
		btn("30天", queryCallback(token, "r=30d")),
		btn("本月", queryCallback(token, "r=month")),
	}
	sortRow := []models.InlineKeyboardButton{
		btn("CPU", queryCallback(token, "s=-cpu")),
		btn("内存", queryCallback(token, "s=-mem")),
		btn("磁盘", queryCallback(token, "s=-disk")),
		btn("流量", queryCallback(token, "s=-total")),
	}
	groupRow := []models.InlineKeyboardButton{
		btn("按主机", queryCallback(token, "g=host")),
		btn("按分组", queryCallback(token, "g=tag")),
	}
	var pageRow []models.InlineKeyboardButton
	if page > 1 {
		pageRow = append(pageRow, btn("上一页", queryCallback(token, fmt.Sprintf("p=%d", page-1))))
	}
	if page < total {
		pageRow = append(pageRow, btn("下一页", queryCallback(token, fmt.Sprintf("p=%d", page+1))))
	}
	pageRow = append(pageRow, navHome())
	_ = q
	return markup(rangeRow, sortRow, groupRow, pageRow)
}
