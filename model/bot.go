package model

import (
	"strconv"
	"strings"
	"time"
)

const (
	BotRoleBlocked  uint8 = 0
	BotRoleViewer   uint8 = 1
	BotRoleOperator uint8 = 2
	BotRoleAdmin    uint8 = 3

	BotChatPrivate     = "private"
	BotChatGroup       = "group"
	BotChatSupergroup  = "supergroup"
	BotChatChannel     = "channel"

	BotPeriodDaily   = "daily"
	BotPeriodWeekly  = "weekly"
	BotPeriodMonthly = "monthly"

	BotModePolling = "polling"
	BotModeWebhook = "webhook"
)

type BotConfig struct {
	Enabled        bool   `koanf:"enabled" yaml:"enabled"`
	Provider       string `koanf:"provider" yaml:"provider"`
	Token          string `koanf:"token" yaml:"token"`
	APIEndpoint    string `koanf:"api_endpoint" yaml:"api_endpoint"`
	Mode           string `koanf:"mode" yaml:"mode"`
	WebhookBaseURL string `koanf:"webhook_base_url" yaml:"webhook_base_url"`
	WebhookSecret  string `koanf:"webhook_secret" yaml:"webhook_secret"`
	Charts         bool   `koanf:"charts" yaml:"charts"`
	Language       string `koanf:"language" yaml:"language"`
	RatePerMinute  int    `koanf:"rate_per_minute" yaml:"rate_per_minute"`
}

func (c *BotConfig) Normalize(fallbackLanguage string) {
	if strings.TrimSpace(c.Provider) == "" {
		c.Provider = "telegram"
	}
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case BotModeWebhook:
		c.Mode = BotModeWebhook
	default:
		c.Mode = BotModePolling
	}
	if c.RatePerMinute <= 0 {
		c.RatePerMinute = 20
	}
	if c.RatePerMinute > 60 {
		c.RatePerMinute = 60
	}
	if strings.TrimSpace(c.Language) == "" {
		c.Language = fallbackLanguage
	}
}

func BotRoleName(role uint8) string {
	switch role {
	case BotRoleAdmin:
		return "admin"
	case BotRoleOperator:
		return "operator"
	case BotRoleViewer:
		return "viewer"
	default:
		return "blocked"
	}
}

func ParseBotRole(value string) (uint8, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "blocked", "0":
		return BotRoleBlocked, true
	case "viewer", "1", "观察":
		return BotRoleViewer, true
	case "operator", "2", "运维":
		return BotRoleOperator, true
	case "admin", "3", "管理":
		return BotRoleAdmin, true
	default:
		return 0, false
	}
}

type BotChat struct {
	Common
	ChatID         int64 `gorm:"uniqueIndex;not null"`
	Kind           string
	Title          string
	Role           uint8
	AllowedUserIDs string
	SubscribeTags  string
	Note           string
	BoundBy        int64
	LastSeenAt     time.Time
	Enabled        *bool
}

func (c *BotChat) IsEnabled() bool {
	return c != nil && c.Enabled != nil && *c.Enabled && c.Role > BotRoleBlocked
}

func (c *BotChat) UserAllowed(userID int64) bool {
	if c == nil {
		return false
	}
	ids := ParseInt64CSV(c.AllowedUserIDs)
	if len(ids) == 0 {
		return true
	}
	for _, id := range ids {
		if id == userID {
			return true
		}
	}
	return false
}

func (c *BotChat) Subscribes(tag string) bool {
	if c == nil || !c.IsEnabled() {
		return false
	}
	want := strings.TrimSpace(tag)
	if want == "" {
		want = "default"
	}
	tags := ParseStringCSV(c.SubscribeTags)
	if len(tags) == 0 {
		return false
	}
	for _, item := range tags {
		if item == want || item == "*" {
			return true
		}
	}
	return false
}

type BotBindCode struct {
	Common
	Code         string `gorm:"uniqueIndex;size:32;not null"`
	Role         uint8
	ExpiresAt    time.Time
	UsedByChatID int64
	UsedAt       *time.Time
	CreatedBy    string
}

func (c *BotBindCode) Consumed() bool {
	return c != nil && c.UsedAt != nil
}

type BotReport struct {
	Common
	Name          string
	ChatIDs       string
	Period        string
	HourLocal     int
	Minute        int
	Weekday       int
	DayOfMonth    int
	Sections      string
	Cover         uint8
	Ignore        string
	WithCharts    *bool
	Enabled       *bool
	LastPeriodKey string
	LastRunAt     *time.Time
	LastStatus    string
}

func (r *BotReport) IsEnabled() bool {
	return r != nil && r.Enabled != nil && *r.Enabled
}

func (r *BotReport) ChartsEnabled() bool {
	return r != nil && r.WithCharts != nil && *r.WithCharts
}

func (r *BotReport) SectionSet() map[string]bool {
	out := map[string]bool{}
	for _, item := range ParseStringCSV(r.Sections) {
		out[item] = true
	}
	if len(out) == 0 {
		for _, item := range []string{"status", "servers", "traffic", "uptime", "probes", "alerts"} {
			out[item] = true
		}
	}
	return out
}

func (r *BotReport) IgnoreIDs() map[uint64]bool {
	out := map[uint64]bool{}
	for _, id := range ParseUint64CSV(r.Ignore) {
		out[id] = true
	}
	return out
}

type BotAuditLog struct {
	ID        uint64 `gorm:"primaryKey"`
	ChatID    int64  `gorm:"index"`
	TGUserID  int64
	Role      uint8
	Command   string `gorm:"size:64"`
	Target    string
	Result    string `gorm:"size:32"`
	CreatedAt time.Time `gorm:"index"`
}

func ParseStringCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}

func ParseInt64CSV(value string) []int64 {
	parts := ParseStringCSV(value)
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		out = append(out, id)
	}
	return out
}

func ParseUint64CSV(value string) []uint64 {
	parts := ParseStringCSV(value)
	out := make([]uint64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		out = append(out, id)
	}
	return out
}

func JoinInt64CSV(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

func JoinUint64CSV(ids []uint64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		parts = append(parts, strconv.FormatUint(id, 10))
	}
	return strings.Join(parts, ",")
}

func JoinStringCSV(values []string) string {
	return strings.Join(ParseStringCSV(strings.Join(values, ",")), ",")
}

func ServerInBotScope(id uint64, cover uint8, ignore map[uint64]bool) bool {
	if cover == RuleCoverIgnoreAll {
		return ignore[id]
	}
	return !ignore[id]
}
