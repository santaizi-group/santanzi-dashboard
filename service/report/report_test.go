package report

import (
	"strings"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
)

func TestFormatBytesAndDuration(t *testing.T) {
	if FormatBytes(512) != "512 B" {
		t.Fatal(FormatBytes(512))
	}
	if !strings.Contains(FormatBytes(1536), "KiB") {
		t.Fatal(FormatBytes(1536))
	}
	if FormatDuration(30) != "30s" || FormatDuration(120) != "2m" {
		t.Fatalf("%s %s", FormatDuration(30), FormatDuration(120))
	}
}

func TestWindowStartFor(t *testing.T) {
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	if !windowStartFor(model.BotPeriodDaily, now).Equal(now.AddDate(0, 0, -1)) {
		t.Fatal("daily")
	}
	if !windowStartFor(model.BotPeriodWeekly, now).Equal(now.AddDate(0, 0, -7)) {
		t.Fatal("weekly")
	}
}

func TestWantSections(t *testing.T) {
	if !want(nil, "status") {
		t.Fatal("empty means all")
	}
	if want(map[string]bool{"traffic": true}, "alerts") {
		t.Fatal("filtered")
	}
}
