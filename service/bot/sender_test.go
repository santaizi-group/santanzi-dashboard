package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/hi2shark/santaizi-dashboard/model"
)

func TestEnqueueOverflowReplacesWithNotice(t *testing.T) {
	s := &Sender{}
	for i := 0; i < outboundQueueSize; i++ {
		s.Enqueue(outbound{chatID: 1, text: "body"})
	}
	s.Enqueue(outbound{chatID: 2, text: "dropped body"})
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) != outboundQueueSize {
		t.Fatalf("len=%d", len(s.queue))
	}
	last := s.queue[len(s.queue)-1]
	if last.chatID != 2 || last.text != outboundBusyText || !last.fallback {
		t.Fatalf("last=%#v", last)
	}
}

func TestPushFrontKeepsRetryAhead(t *testing.T) {
	s := &Sender{}
	s.Enqueue(outbound{text: "a"})
	s.Enqueue(outbound{text: "b"})
	s.pushFront(outbound{text: "retry"})
	msg, ok := s.Next()
	if !ok || msg.text != "retry" {
		t.Fatalf("front=%q ok=%v", msg.text, ok)
	}
}

func TestGroupPaceYieldsToPrivate(t *testing.T) {
	s := &Sender{lastGroup: map[int64]time.Time{9: time.Now()}}
	s.queue = []outbound{{chatID: 9, text: "group", group: true}, {chatID: 1, text: "private"}}
	idx, wait := s.nextIndexLocked(time.Now())
	if idx != 1 || wait != 0 {
		t.Fatalf("idx=%d wait=%s", idx, wait)
	}
	s.queue = []outbound{{chatID: 9, text: "group", group: true}}
	idx, wait = s.nextIndexLocked(time.Now())
	if idx != -1 || wait <= 0 || wait > groupSendGap {
		t.Fatalf("idx=%d wait=%s", idx, wait)
	}
}

func TestTextRateLimitReplies(t *testing.T) {
	h := &Hub{authz: NewAuthz(), limiter: newChatLimiter(), sender: &Sender{}}
	enabled := true
	h.authz.Upsert(&model.BotChat{ChatID: 7, Role: model.BotRoleViewer, Enabled: &enabled})
	for i := 0; i < 20; i++ {
		if !h.limiter.Allow(7, 20) {
			t.Fatal("prefill")
		}
	}
	update := &models.Update{Message: &models.Message{
		Text: "/status",
		Chat: models.Chat{ID: 7, Type: models.ChatTypePrivate},
		From: &models.User{ID: 7},
	}}
	called := false
	h.authMiddleware(func(context.Context, *tgbot.Bot, *models.Update) { called = true })(context.Background(), nil, update)
	if called {
		t.Fatal("limited command should not run")
	}
	assertReply(t, h, "操作过于频繁，请稍后再试。")
}

func TestSplitCaptionKeepsButtonsOnHead(t *testing.T) {
	long := strings.Repeat("测", 600) + "\n" + strings.Repeat("量", 600)
	head, rest := splitCaption(long)
	if len([]rune(head)) > telegramCaptionLimit {
		t.Fatalf("head runes=%d", len([]rune(head)))
	}
	if !strings.Contains(rest, "量") || strings.Contains(head, "量") {
		t.Fatalf("head=%q rest=%q", head[:20], rest[:20])
	}
	line := strings.Repeat("行", 1200)
	head, rest = splitCaption(line)
	if len([]rune(strings.TrimSuffix(head, "…"))) != telegramCaptionLimit || rest == "" {
		t.Fatalf("head=%d rest=%d", len([]rune(head)), len([]rune(rest)))
	}
}
