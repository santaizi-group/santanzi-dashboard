package bot

import (
	"bytes"
	"context"
	"log"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	outboundQueueSize = 1024
	sendGap           = 40 * time.Millisecond
	groupSendGap      = time.Second
	outboundBusyText  = "消息过多，请稍后再查。"
)

type outbound struct {
	chatID    int64
	messageID int
	text      string
	photo     []byte
	caption   string
	markup    *models.InlineKeyboardMarkup
	edit      bool
	group     bool
	fallback  bool
}

type Sender struct {
	client    *tgbot.Bot
	mu        sync.Mutex
	queue     []outbound
	wake      chan struct{}
	lastSend  time.Time
	lastGroup map[int64]time.Time
}

func newSender(client *tgbot.Bot) *Sender {
	return &Sender{client: client, wake: make(chan struct{}, 1), lastGroup: map[int64]time.Time{}}
}

func (s *Sender) Run(ctx context.Context) {
	var pauseUntil time.Time
	for {
		msg, ok := s.waitNext(ctx, &pauseUntil)
		if !ok {
			return
		}
		err := s.dispatch(ctx, msg)
		if err == nil {
			now := time.Now()
			s.mu.Lock()
			s.lastSend = now
			if msg.group {
				if s.lastGroup == nil {
					s.lastGroup = map[int64]time.Time{}
				}
				s.lastGroup[msg.chatID] = now
			}
			s.mu.Unlock()
			continue
		}
		if tgbot.IsTooManyRequestsError(err) {
			wait := time.Second
			if too, ok := err.(*tgbot.TooManyRequestsError); ok && too.RetryAfter > 0 {
				wait = time.Duration(too.RetryAfter) * time.Second
			}
			pauseUntil = time.Now().Add(wait)
			s.pushFront(msg)
			continue
		}
		log.Println("SANTAIZI>> bot send:", err)
	}
}

func (s *Sender) Enqueue(msg outbound) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureWakeLocked()
	if len(s.queue) >= outboundQueueSize {
		if msg.fallback {
			log.Println("SANTAIZI>> bot send queue full, drop")
			return
		}
		log.Println("SANTAIZI>> bot send queue full, drop body")
		if len(s.queue) > 0 {
			s.queue = s.queue[1:]
		}
		msg = outbound{chatID: msg.chatID, text: outboundBusyText, fallback: true, group: msg.group}
		if len(s.queue) >= outboundQueueSize {
			log.Println("SANTAIZI>> bot send queue full, drop")
			return
		}
	}
	s.queue = append(s.queue, msg)
	s.wakeLocked()
}

func (s *Sender) pushFront(msg outbound) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureWakeLocked()
	s.queue = append([]outbound{msg}, s.queue...)
	s.wakeLocked()
}

func (s *Sender) Next() (outbound, bool) {
	if s == nil {
		return outbound{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return outbound{}, false
	}
	msg := s.queue[0]
	s.queue = s.queue[1:]
	return msg, true
}

func (s *Sender) ensureWakeLocked() {
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
}

func (s *Sender) wakeLocked() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Sender) waitNext(ctx context.Context, pauseUntil *time.Time) (outbound, bool) {
	for {
		if err := ctx.Err(); err != nil {
			return outbound{}, false
		}
		now := time.Now()
		var pause time.Duration
		if pauseUntil != nil && !pauseUntil.IsZero() && now.Before(*pauseUntil) {
			pause = time.Until(*pauseUntil)
		}
		s.mu.Lock()
		idx, wait := -1, time.Duration(0)
		if pause == 0 {
			idx, wait = s.nextIndexLocked(now)
		}
		var msg outbound
		if idx >= 0 {
			msg = s.queue[idx]
			s.queue = append(s.queue[:idx], s.queue[idx+1:]...)
		}
		empty := len(s.queue) == 0 && idx < 0
		wake := s.wake
		s.mu.Unlock()
		if idx >= 0 {
			return msg, true
		}
		if pause == 0 && wait == 0 && empty {
			select {
			case <-ctx.Done():
				return outbound{}, false
			case <-wake:
			}
			continue
		}
		delay := pause
		if wait > delay {
			delay = wait
		}
		if delay <= 0 {
			delay = sendGap
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return outbound{}, false
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// nextIndexLocked 返回可以立刻发送的下标。群聊同一会话至少间隔 1 秒，
// 冷却中的群消息会让位于后面的私聊，避免一条群把告警拖住。
func (s *Sender) nextIndexLocked(now time.Time) (int, time.Duration) {
	if len(s.queue) == 0 {
		return -1, 0
	}
	if !s.lastSend.IsZero() {
		if d := sendGap - now.Sub(s.lastSend); d > 0 {
			return -1, d
		}
	}
	var soonest time.Duration
	for i, msg := range s.queue {
		if !msg.group {
			return i, 0
		}
		last := s.lastGroup[msg.chatID]
		if last.IsZero() || now.Sub(last) >= groupSendGap {
			return i, 0
		}
		wait := groupSendGap - now.Sub(last)
		if soonest == 0 || wait < soonest {
			soonest = wait
		}
	}
	return -1, soonest
}

func (s *Sender) dispatch(ctx context.Context, msg outbound) error {
	if s.client == nil {
		return nil
	}
	if msg.edit && msg.messageID > 0 {
		err := s.edit(ctx, msg)
		if err == nil {
			return nil
		}
		if tgbot.IsTooManyRequestsError(err) {
			return err
		}
		msg.edit = false
	}
	if len(msg.photo) > 0 {
		params := &tgbot.SendPhotoParams{
			ChatID:    msg.chatID,
			Photo:     &models.InputFileUpload{Filename: "chart.png", Data: bytes.NewReader(msg.photo)},
			Caption:   msg.caption,
			ParseMode: models.ParseModeHTML,
		}
		if msg.markup != nil {
			params.ReplyMarkup = msg.markup
		}
		_, err := s.client.SendPhoto(ctx, params)
		return err
	}
	var err error
	for i, chunk := range SplitChunks(msg.text) {
		params := &tgbot.SendMessageParams{
			ChatID:    msg.chatID,
			Text:      chunk,
			ParseMode: models.ParseModeHTML,
		}
		if i == 0 && msg.markup != nil {
			params.ReplyMarkup = msg.markup
		}
		_, err = s.client.SendMessage(ctx, params)
		if err != nil {
			return err
		}
	}
	return err
}

func (s *Sender) edit(ctx context.Context, msg outbound) error {
	if len(msg.photo) > 0 {
		media := &models.InputMediaPhoto{
			Media:           "attach://chart.png",
			Caption:         msg.caption,
			ParseMode:       models.ParseModeHTML,
			MediaAttachment: bytes.NewReader(msg.photo),
		}
		params := &tgbot.EditMessageMediaParams{
			ChatID:    msg.chatID,
			MessageID: msg.messageID,
			Media:     media,
		}
		if msg.markup != nil {
			params.ReplyMarkup = msg.markup
		}
		_, err := s.client.EditMessageMedia(ctx, params)
		return err
	}
	text := msg.text
	if text == "" {
		text = msg.caption
	}
	chunks := SplitChunks(text)
	body := " "
	if len(chunks) > 0 && chunks[0] != "" {
		body = chunks[0]
	}
	params := &tgbot.EditMessageTextParams{
		ChatID:    msg.chatID,
		MessageID: msg.messageID,
		Text:      body,
		ParseMode: models.ParseModeHTML,
	}
	if msg.markup != nil {
		params.ReplyMarkup = msg.markup
	}
	_, err := s.client.EditMessageText(ctx, params)
	return err
}
