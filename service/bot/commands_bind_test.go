package bot

import (
	"context"
	"testing"

	"github.com/hi2shark/santaizi-dashboard/model"
)

func TestBindAndStartReplyWhenUnauthorized(t *testing.T) {
	h := &Hub{sender: &Sender{ch: make(chan outbound, 8)}}
	ctx := context.WithValue(context.Background(), ctxChat, &model.BotChat{ChatID: 42, Kind: model.BotChatPrivate})

	h.cmdStart(ctx, 42, "")
	assertReply(t, h, "在管理后台生成绑定码后，发送 /bind <绑定码>。")

	h.cmdBind(ctx, 42, "")
	assertReply(t, h, "用法：/bind <绑定码> 或 /start <绑定码>")

	h.cmdBind(ctx, 42, "EXPIRED")
	assertReply(t, h, "绑定码无效或已过期。")
}

func assertReply(t *testing.T, h *Hub, want string) {
	t.Helper()
	select {
	case msg := <-h.sender.ch:
		if msg.text != want {
			t.Fatalf("reply=%q want %q", msg.text, want)
		}
	default:
		t.Fatal("expected a reply")
	}
}
