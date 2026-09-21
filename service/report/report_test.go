package report

import (
	"strings"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
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

func TestFilterHostsPrefersExactID(t *testing.T) {
	restore := swapHosts([]*model.Server{
		{Common: model.Common{ID: 1}, Name: "web-12", Tag: "hk"},
		{Common: model.Common{ID: 12}, Name: "edge", Tag: "jp"},
	})
	t.Cleanup(restore)

	got := FilterHosts("12")
	if len(got) != 1 || got[0].ID != 12 || got[0].Name != "edge" {
		t.Fatalf("%#v", got)
	}
	host, ok := HostByID(12)
	if !ok || host.Name != "edge" {
		t.Fatalf("%v %#v", ok, host)
	}
	if _, ok := HostByID(99); ok {
		t.Fatal("missing id")
	}
}

func TestFilterHostsExactNameBeatsSubstring(t *testing.T) {
	restore := swapHosts([]*model.Server{
		{Common: model.Common{ID: 2}, Name: "hk", Tag: "other"},
		{Common: model.Common{ID: 3}, Name: "hk-edge", Tag: "hk"},
	})
	t.Cleanup(restore)

	got := FilterHosts("hk")
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("%#v", got)
	}
}

func swapHosts(list []*model.Server) func() {
	singleton.SortedServerLock.Lock()
	prev := singleton.SortedServerList
	singleton.SortedServerList = list
	singleton.SortedServerLock.Unlock()
	return func() {
		singleton.SortedServerLock.Lock()
		singleton.SortedServerList = prev
		singleton.SortedServerLock.Unlock()
	}
}
