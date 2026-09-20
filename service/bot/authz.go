package bot

import (
	"sync"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

type Authz struct {
	mu    sync.RWMutex
	chats map[int64]*model.BotChat
}

func NewAuthz() *Authz {
	return &Authz{chats: map[int64]*model.BotChat{}}
}

func (a *Authz) Reload() {
	if singleton.DB == nil {
		return
	}
	var rows []model.BotChat
	_ = singleton.DB.Find(&rows).Error
	next := make(map[int64]*model.BotChat, len(rows))
	for i := range rows {
		row := rows[i]
		next[row.ChatID] = &row
	}
	a.mu.Lock()
	a.chats = next
	a.mu.Unlock()
}

func (a *Authz) Lookup(chatID int64) *model.BotChat {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.chats[chatID]
}

func (a *Authz) Upsert(chat *model.BotChat) {
	if chat == nil {
		return
	}
	a.mu.Lock()
	a.chats[chat.ChatID] = chat
	a.mu.Unlock()
}

func (a *Authz) Delete(chatID int64) {
	a.mu.Lock()
	delete(a.chats, chatID)
	a.mu.Unlock()
}

func (a *Authz) Subscribers(tag string) []*model.BotChat {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*model.BotChat, 0)
	for _, chat := range a.chats {
		if chat.Subscribes(tag) {
			out = append(out, chat)
		}
	}
	return out
}

func commandMinRole(cmd string) uint8 {
	switch cmd {
	case "start", "bind":
		return 0
	case "mute", "unmute", "rule":
		return model.BotRoleOperator
	case "chats", "role", "revoke", "audit", "health":
		return model.BotRoleAdmin
	default:
		return model.BotRoleViewer
	}
}
