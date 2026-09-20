package bot

import (
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
)

func TestPeriodKeyAndDue(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, loc)
	if PeriodKey(model.BotPeriodDaily, now) != "2026-09-20" {
		t.Fatal(PeriodKey(model.BotPeriodDaily, now))
	}
	if PeriodKey(model.BotPeriodMonthly, now) != "2026-09" {
		t.Fatal(PeriodKey(model.BotPeriodMonthly, now))
	}
	row := model.BotReport{Period: model.BotPeriodDaily, HourLocal: 8, Minute: 0, Enabled: model.BoolPtr(true)}
	if !ReportDue(row, now) {
		t.Fatal("should be due after 08:00")
	}
	row.LastPeriodKey = "2026-09-20"
	if ReportDue(row, now) {
		t.Fatal("idempotent")
	}
	early := time.Date(2026, 9, 20, 7, 0, 0, 0, loc)
	row.LastPeriodKey = ""
	if ReportDue(row, early) {
		t.Fatal("not yet")
	}
}

func TestCommandName(t *testing.T) {
	cmd, arg := commandName("/status@bot extra")
	if cmd != "status" || arg != "extra" {
		t.Fatalf("%s %s", cmd, arg)
	}
}

func TestEscapeSplit(t *testing.T) {
	if Escape("<b>") != "&lt;b&gt;" {
		t.Fatal(Escape("<b>"))
	}
	if len(SplitChunks("a")) != 1 {
		t.Fatal(SplitChunks("a"))
	}
}

func TestCommandMinRole(t *testing.T) {
	if commandMinRole("status") != model.BotRoleViewer {
		t.Fatal("viewer")
	}
	if commandMinRole("mute") != model.BotRoleOperator {
		t.Fatal("operator")
	}
	if commandMinRole("revoke") != model.BotRoleAdmin {
		t.Fatal("admin")
	}
}
