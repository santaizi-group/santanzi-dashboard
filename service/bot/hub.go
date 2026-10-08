package bot

import (
	"context"
	"fmt"
	"image/color"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	"github.com/hi2shark/santaizi-dashboard/service/report"
	"github.com/hi2shark/santaizi-dashboard/service/report/chart"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

type Hub struct {
	mu        sync.Mutex
	cancel    context.CancelFunc
	client    *tgbot.Bot
	sender    *Sender
	authz     *Authz
	limiter   *chatLimiter
	aiLimiter *chatLimiter
	queries   *queryCache
	aiMu      sync.Mutex
	aiTurns   map[int64]aiTurn
}

var (
	sharedHub      = &Hub{authz: NewAuthz(), limiter: newChatLimiter(), aiLimiter: newChatLimiter(), queries: newQueryCache()}
	processStarted = time.Now()
)

func Shared() *Hub { return sharedHub }

func (h *Hub) Run(ctx context.Context) {
	h.Reload()
	<-ctx.Done()
	h.Stop()
}

func (h *Hub) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopLocked()
}

func (h *Hub) stopLocked() {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	h.client = nil
	h.sender = nil
	singleton.AfterNotificationDelivered = nil
}

func (h *Hub) Reload() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopLocked()
	if singleton.Conf == nil || singleton.Conf.Mode != "primary" {
		return
	}
	conf := singleton.Conf.Bot
	if !conf.Enabled || strings.TrimSpace(conf.Token) == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	opts := []tgbot.Option{
		tgbot.WithMiddlewares(h.authMiddleware),
		tgbot.WithDefaultHandler(h.onUpdate),
		tgbot.WithHTTPClient(25*time.Second, utils.HttpClient),
		tgbot.WithCheckInitTimeout(25 * time.Second),
	}
	if endpoint := strings.TrimSpace(conf.APIEndpoint); endpoint != "" {
		opts = append(opts, tgbot.WithServerURL(endpoint))
	}
	if conf.Mode == model.BotModeWebhook && strings.TrimSpace(conf.WebhookSecret) != "" {
		opts = append(opts, tgbot.WithWebhookSecretToken(conf.WebhookSecret))
	}
	client, err := tgbot.New(conf.Token, opts...)
	if err != nil {
		log.Println("SANTAIZI>> bot init:", classifyBotAPIError(err))
		cancel()
		h.cancel = nil
		return
	}
	h.client = client
	h.sender = newSender(client)
	h.authz.Reload()
	go h.sender.Run(ctx)
	singleton.AfterNotificationDelivered = h.forwardNotification
	_, _ = client.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: defaultCommands()})
	if conf.Mode == model.BotModeWebhook {
		base := strings.TrimRight(strings.TrimSpace(conf.WebhookBaseURL), "/")
		if base != "" {
			_, err = client.SetWebhook(ctx, &tgbot.SetWebhookParams{
				URL:         base + "/api/v2/bot/telegram/webhook",
				SecretToken: conf.WebhookSecret,
			})
			if err != nil {
				log.Println("SANTAIZI>> bot setWebhook:", err)
			}
		}
		go client.StartWebhook(ctx)
		log.Println("SANTAIZI>> Telegram Bot webhook 已启动")
		return
	}
	_, _ = client.DeleteWebhook(ctx, &tgbot.DeleteWebhookParams{})
	go client.Start(ctx)
	log.Println("SANTAIZI>> Telegram Bot polling 已启动")
}

func (h *Hub) ServeWebhook(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	client := h.client
	mode := ""
	if singleton.Conf != nil {
		mode = singleton.Conf.Bot.Mode
	}
	h.mu.Unlock()
	if client == nil || mode != model.BotModeWebhook {
		http.NotFound(w, r)
		return
	}
	client.WebhookHandler().ServeHTTP(w, r)
}

func (h *Hub) Reply(chatID int64, text string) {
	h.ReplyMarkup(chatID, text, nil)
}

func (h *Hub) ReplyMarkup(chatID int64, text string, markup *models.InlineKeyboardMarkup) {
	h.enqueue(outbound{chatID: chatID, text: text, markup: markup})
}

func (h *Hub) ReplyPhoto(chatID int64, png []byte, caption string) {
	h.ReplyPhotoMarkup(chatID, 0, png, caption, nil, false)
}

func (h *Hub) ReplyPhotoMarkup(chatID int64, messageID int, png []byte, caption string, markup *models.InlineKeyboardMarkup, edit bool) {
	if len(png) == 0 {
		return
	}
	h.enqueue(outbound{chatID: chatID, messageID: messageID, photo: png, caption: caption, markup: markup, edit: edit})
}

func (h *Hub) EditMarkup(chatID int64, messageID int, text string, markup *models.InlineKeyboardMarkup) {
	if messageID <= 0 {
		h.ReplyMarkup(chatID, text, markup)
		return
	}
	h.enqueue(outbound{chatID: chatID, messageID: messageID, text: text, markup: markup, edit: true})
}

func (h *Hub) enqueue(msg outbound) {
	if h == nil {
		return
	}
	msg.group = h.pacedGroup(msg.chatID)
	h.mu.Lock()
	sender := h.sender
	h.mu.Unlock()
	if sender == nil {
		return
	}
	sender.Enqueue(msg)
}

func (h *Hub) pacedGroup(chatID int64) bool {
	if h == nil || h.authz == nil {
		return false
	}
	rec := h.authz.Lookup(chatID)
	if rec == nil {
		return false
	}
	switch rec.Kind {
	case model.BotChatGroup, model.BotChatSupergroup, model.BotChatChannel:
		return true
	default:
		return false
	}
}

func (h *Hub) respond(ctx context.Context, text string, markup *models.InlineKeyboardMarkup) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	if id := messageIDFrom(ctx); id > 0 {
		h.EditMarkup(chat.ChatID, id, text, markup)
		return
	}
	h.ReplyMarkup(chat.ChatID, text, markup)
}

func (h *Hub) respondPhoto(ctx context.Context, png []byte, caption string, markup *models.InlineKeyboardMarkup) {
	chat := chatFrom(ctx)
	if chat == nil || len(png) == 0 {
		if chat != nil {
			h.respond(ctx, caption, markup)
		}
		return
	}
	id := messageIDFrom(ctx)
	head, rest := splitCaption(caption)
	h.ReplyPhotoMarkup(chat.ChatID, id, png, head, markup, id > 0)
	if rest != "" {
		h.Reply(chat.ChatID, rest)
	}
}

func (h *Hub) chartsOn() bool {
	return singleton.Conf != nil && singleton.Conf.Bot.Charts
}

func chartPalette() chart.Palette {
	if singleton.Conf != nil && singleton.Conf.Bot.ChartTheme == model.BotChartThemeLight {
		return chart.LightPalette()
	}
	return chart.DarkPalette()
}

func (h *Hub) Authz() *Authz { return h.authz }

func TestToken(ctx context.Context, token, endpoint string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", classifyBotAPIError(fmt.Errorf("token 为空"))
	}
	opts := []tgbot.Option{tgbot.WithHTTPClient(25*time.Second, utils.HttpClient), tgbot.WithCheckInitTimeout(25 * time.Second)}
	if strings.TrimSpace(endpoint) != "" {
		opts = append(opts, tgbot.WithServerURL(endpoint))
	}
	client, err := tgbot.New(token, opts...)
	if err != nil {
		return "", classifyBotAPIError(err)
	}
	me, err := client.GetMe(ctx)
	if err != nil {
		return "", classifyBotAPIError(err)
	}
	if me.Username != "" {
		return me.Username, nil
	}
	return me.FirstName, nil
}

func (h *Hub) SendReport(row *model.BotReport, force bool) error {
	if row == nil {
		return fmt.Errorf("报告不存在")
	}
	now := time.Now()
	if singleton.Loc != nil {
		now = now.In(singleton.Loc)
	}
	key := PeriodKey(row.Period, now)
	opts := report.Options{
		Period:   row.Period,
		Cover:    row.Cover,
		Ignore:   row.IgnoreIDs(),
		Sections: row.SectionSet(),
		Now:      now,
	}
	snap, err := report.Collect(opts)
	if err != nil {
		return err
	}
	text := FormatSnapshot(snap, row.SectionSet())
	ids := model.ParseInt64CSV(row.ChatIDs)
	if len(ids) == 0 {
		return fmt.Errorf("未选择会话")
	}
	var png []byte
	if row.ChartsEnabled() && singleton.Conf != nil && singleton.Conf.Bot.Charts {
		png, _ = snapshotChart(snap)
	}
	for _, id := range ids {
		if png != nil {
			h.ReplyPhoto(id, png, "三太子监控 · 周期报告")
		}
		h.Reply(id, text)
	}
	if !force {
		row.LastPeriodKey = key
	}
	done := now
	row.LastRunAt = &done
	row.LastStatus = "ok"
	if singleton.DB != nil {
		_ = singleton.DB.Model(row).Updates(map[string]any{
			"last_period_key": row.LastPeriodKey,
			"last_run_at":     row.LastRunAt,
			"last_status":     row.LastStatus,
		}).Error
	}
	return nil
}

func snapshotChart(snap report.Snapshot) ([]byte, error) {
	p := chartPalette()
	items := make([]chart.BarItem, 0, 10)
	src := snap.Uptime
	if len(src) == 0 {
		for _, host := range snap.Hosts {
			items = append(items, chart.BarItem{Label: host.Name, Value: host.CPU})
			if len(items) >= 10 {
				break
			}
		}
		return chart.BarFormat("CPU %", items, chart.AxisNumber, p)
	}
	for i, row := range src {
		if i >= 10 {
			break
		}
		items = append(items, chart.BarItem{Label: row.Name, Value: row.Percent, Color: uptimeColor(p, row.Percent)})
	}
	return chart.BarFormat("Uptime %", items, chart.AxisNumber, p)
}

// uptimeColor 越低越坏：99.9 以上绿、95 以上琥珀、其余红。
func uptimeColor(p chart.Palette, percent float64) color.RGBA {
	switch {
	case percent >= 99.9:
		return p.Success
	case percent >= 95:
		return p.Warning
	default:
		return p.Danger
	}
}

// defaultCommands 从命令注册表派生 Telegram 命令面板；
// 只暴露 viewer 及以下可见的命令（start/bind 走绑定流程仍保留）。
func defaultCommands() []models.BotCommand {
	out := make([]models.BotCommand, 0, len(commandSpecs))
	for i := range commandSpecs {
		spec := &commandSpecs[i]
		if spec.Hidden || spec.MinRole > model.BotRoleViewer {
			continue
		}
		out = append(out, models.BotCommand{Command: spec.Name, Description: spec.Desc})
	}
	return out
}

func StartScheduler() {
	if singleton.Cron == nil {
		return
	}
	if _, err := singleton.Cron.AddFunc("0 * * * * *", ScanReports); err != nil {
		log.Println("SANTAIZI>> bot report cron:", err)
	}
}
