package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/service/report"
)

func TestFormatHostOmitsIP(t *testing.T) {
	text := FormatHost(report.HostRow{
		ID: 3, Name: "hk-1", Tag: "hk", Online: true,
		CPU: 12, MemPct: 40, DiskPct: 50, Load1: 0.8,
		NetIn: 1000, NetOut: 2000, NetInSpeed: 10, NetOutSpeed: 20,
		Platform: "linux", PlatformVer: "6", AgentVersion: "1.0.9", Uptime: 3600,
	})
	if strings.Contains(text, "IP") || strings.Contains(strings.ToLower(text), "127.0.0.1") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "hk-1") || !strings.Contains(text, "负载") {
		t.Fatal(text)
	}
}

func TestParseKindQueryTopAndUptimeDays(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	q, err := parseKindQuery("top", "cpu 7d tag=hk", now)
	if err != nil {
		t.Fatal(err)
	}
	if q.Kind != "top" || q.Metric != "cpu" || q.Sort != "-cpu" || q.Range.Kind != "7d" {
		t.Fatalf("%#v", q)
	}
	u, err := parseKindQuery("uptime", "14", now)
	if err != nil {
		t.Fatal(err)
	}
	// 裸数字统一为主机 ID；天数须带 d。
	if u.Filters == nil || len(u.Filters) != 1 || u.Filters[0].Field != "id" || u.Filters[0].Values[0] != "14" {
		t.Fatalf("uptime bare number should resolve to host id filter: %#v", u.Filters)
	}
	if u.Range.Kind != "7d" {
		t.Fatalf("uptime default range = %#v", u.Range)
	}
	d, err := parseKindQuery("uptime", "14d", now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Range.Kind != "14d" {
		t.Fatalf("%#v", d.Range)
	}
	m, err := parseKindQuery("find", "cpu>50 90m", now)
	if err != nil {
		t.Fatal(err)
	}
	if m.Range.Kind != "90m" || !m.Range.To.After(m.Range.From) {
		t.Fatalf("minute span: %#v", m.Range)
	}
}

func TestNavCallbackSkipsWrites(t *testing.T) {
	if !isNavCallback("m:home") || !isNavCallback("q:abc123:r=7d") {
		t.Fatal("nav")
	}
	if isNavCallback("c:mute:1") || isNavCallback("m:1h:9") {
		t.Fatal("write must count")
	}
}
