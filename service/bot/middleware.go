package bot

import (
	"context"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

type ctxKey int

const (
	ctxChat ctxKey = iota
	ctxUserID
	ctxMessageID
)

type chatLimiter struct {
	mu     sync.Mutex
	window map[int64][]time.Time
}

func newChatLimiter() *chatLimiter {
	return &chatLimiter{window: map[int64][]time.Time{}}
}

func (l *chatLimiter) Allow(chatID int64, perMinute int) bool {
	if perMinute <= 0 {
		perMinute = 20
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	times := l.window[chatID]
	cut := now.Add(-time.Minute)
	kept := times[:0]
	for _, t := range times {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= perMinute {
		l.window[chatID] = kept
		return false
	}
	l.window[chatID] = append(kept, now)
	return true
}

func (h *Hub) authMiddleware(next tgbot.HandlerFunc) tgbot.HandlerFunc {
	return func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
		chatID, userID, kind, title, text, isCallback := extractUpdate(update)
		if chatID == 0 {
			return
		}
		cmd, _ := commandName(text)
		if isCallback {
			cmd = callbackCommand(update.CallbackQuery.Data)
		}
		rec := h.authz.Lookup(chatID)
		if rec == nil || !rec.IsEnabled() {
			if cmd == "start" || cmd == "bind" {
				next(context.WithValue(context.WithValue(ctx, ctxUserID, userID), ctxChat, &model.BotChat{
					ChatID: chatID, Kind: kind, Title: title, BoundBy: userID,
				}), b, update)
			}
			return
		}
		if !rec.UserAllowed(userID) {
			return
		}
		need := commandMinRole(cmd)
		if rec.Role < need {
			if isCallback {
				_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
					CallbackQueryID: update.CallbackQuery.ID, Text: "权限不足。",
				})
			} else {
				h.Reply(chatID, "权限不足。")
			}
			return
		}
		limit := 20
		if singleton.Conf != nil {
			limit = singleton.Conf.Bot.RatePerMinute
		}
		nav := isCallback && isNavCallback(update.CallbackQuery.Data)
		if !nav && !h.limiter.Allow(chatID, limit) {
			if isCallback {
				_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
					CallbackQueryID: update.CallbackQuery.ID, Text: "操作过于频繁，请稍后再试。",
				})
			}
			return
		}
		rec.LastSeenAt = time.Now()
		if rec.ID != 0 && singleton.DB != nil {
			seen := rec.LastSeenAt
			go singleton.DB.Model(&model.BotChat{}).Where("id = ?", rec.ID).Update("last_seen_at", seen)
		}
		next(context.WithValue(context.WithValue(context.WithValue(ctx, ctxUserID, userID), ctxChat, rec), ctxMessageID, callbackMessageID(update)), b, update)
	}
}

func extractUpdate(update *models.Update) (chatID, userID int64, kind, title, text string, callback bool) {
	if update == nil {
		return
	}
	if update.Message != nil {
		chatID = update.Message.Chat.ID
		kind = string(update.Message.Chat.Type)
		title = chatTitle(update.Message.Chat)
		text = update.Message.Text
		if update.Message.From != nil {
			userID = update.Message.From.ID
		}
		return
	}
	if update.CallbackQuery != nil {
		callback = true
		userID = update.CallbackQuery.From.ID
		if update.CallbackQuery.Message.Message != nil {
			chatID = update.CallbackQuery.Message.Message.Chat.ID
			kind = string(update.CallbackQuery.Message.Message.Chat.Type)
			title = chatTitle(update.CallbackQuery.Message.Message.Chat)
		}
		return
	}
	return
}

func chatTitle(chat models.Chat) string {
	if chat.Title != "" {
		return chat.Title
	}
	name := strings.TrimSpace(chat.FirstName + " " + chat.LastName)
	if name != "" {
		return name
	}
	return chat.Username
}

func callbackCommand(data string) string {
	switch {
	case strings.HasPrefix(data, "c:mute"), strings.HasPrefix(data, "m:1h"):
		return "mute"
	case strings.HasPrefix(data, "c:unmute"):
		return "unmute"
	case strings.HasPrefix(data, "c:rule"):
		return "rule"
	case strings.HasPrefix(data, "au:"):
		return "audit"
	case strings.HasPrefix(data, "hl:"):
		return "health"
	default:
		return "status"
	}
}

func isNavCallback(data string) bool {
	if strings.HasPrefix(data, "c:") || strings.HasPrefix(data, "m:1h") {
		return false
	}
	return true
}

func callbackMessageID(update *models.Update) int {
	if update == nil || update.CallbackQuery == nil {
		return 0
	}
	if update.CallbackQuery.Message.Message != nil {
		return update.CallbackQuery.Message.Message.ID
	}
	if update.CallbackQuery.Message.InaccessibleMessage != nil {
		return update.CallbackQuery.Message.InaccessibleMessage.MessageID
	}
	return 0
}

func chatFrom(ctx context.Context) *model.BotChat {
	chat, _ := ctx.Value(ctxChat).(*model.BotChat)
	return chat
}

func userFrom(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxUserID).(int64)
	return id
}

func messageIDFrom(ctx context.Context) int {
	id, _ := ctx.Value(ctxMessageID).(int)
	return id
}

func (h *Hub) audit(ctx context.Context, command, target, result string) {
	chat := chatFrom(ctx)
	if chat == nil || singleton.DB == nil {
		return
	}
	_ = singleton.DB.Create(&model.BotAuditLog{
		ChatID: chat.ChatID, TGUserID: userFrom(ctx), Role: chat.Role,
		Command: command, Target: target, Result: result, CreatedAt: time.Now(),
	}).Error
}
