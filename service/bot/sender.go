package bot

import (
	"bytes"
	"context"
	"log"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type outbound struct {
	chatID  int64
	text    string
	photo   []byte
	caption string
	markup  *models.InlineKeyboardMarkup
}

type Sender struct {
	client *tgbot.Bot
	ch     chan outbound
}

func newSender(client *tgbot.Bot) *Sender {
	return &Sender{client: client, ch: make(chan outbound, 256)}
}

func (s *Sender) Run(ctx context.Context) {
	limiter := time.NewTicker(40 * time.Millisecond)
	defer limiter.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.ch:
			<-limiter.C
			s.dispatch(ctx, msg)
		}
	}
}

func (s *Sender) Enqueue(msg outbound) {
	if s == nil {
		return
	}
	select {
	case s.ch <- msg:
	default:
		log.Println("SANTAIZI>> bot send queue full, drop")
	}
}

func (s *Sender) dispatch(ctx context.Context, msg outbound) {
	if s.client == nil {
		return
	}
	var err error
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
		_, err = s.client.SendPhoto(ctx, params)
	} else {
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
				break
			}
		}
	}
	if err == nil {
		return
	}
	if tgbot.IsTooManyRequestsError(err) {
		wait := 1
		if too, ok := err.(*tgbot.TooManyRequestsError); ok && too.RetryAfter > 0 {
			wait = too.RetryAfter
		}
		time.Sleep(time.Duration(wait) * time.Second)
		s.Enqueue(msg)
		return
	}
	log.Println("SANTAIZI>> bot send:", err)
}
