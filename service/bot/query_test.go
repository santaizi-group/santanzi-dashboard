package bot

import (
	"strings"
	"testing"
	"time"
)

func TestParseQueryFiltersUnitsAndRange(t *testing.T) {
	now := time.Date(2026, 9, 20, 15, 30, 0, 0, time.FixedZone("CST", 8*3600))
	q, err := ParseQuery([]string{"cpu>80", "tag=hk|jp", "7d", "sort=-cpu", "top=5"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Filters) != 2 || q.Filters[0].Field != "cpu" || q.Filters[0].Number != 80 {
		t.Fatalf("filters=%#v", q.Filters)
	}
	if len(q.Filters[1].Values) != 2 || q.Filters[1].Values[0] != "hk" {
		t.Fatalf("tag=%#v", q.Filters[1])
	}
	if q.Range.Kind != "7d" || q.Sort != "-cpu" || q.Limit != 5 {
		t.Fatalf("q=%#v", q)
	}
	if q.Range.From.Day() != 14 || q.Range.To.Day() != 20 {
		t.Fatalf("range=%s %s", q.Range.From, q.Range.To)
	}
}

func TestParseQueryNetUnitsAndCustomDates(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	q, err := ParseQuery([]string{"net>10m", "total>500g", "2026-09-01..2026-09-10"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if q.Filters[0].Number != 10*1024*1024 {
		t.Fatalf("net=%v", q.Filters[0].Number)
	}
	if q.Filters[1].Number != 500*1024*1024*1024 {
		t.Fatalf("total=%v", q.Filters[1].Number)
	}
	if q.Range.Kind != "custom" || q.Range.From.Day() != 1 || q.Range.To.Day() != 11 {
		t.Fatalf("custom=%#v", q.Range)
	}
}

func TestParseQueryAvgPrefixAndBareName(t *testing.T) {
	q, err := ParseQuery([]string{"avg:cpu>50", "name~edge", "offline"}, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if q.Agg != AggAvg || q.Filters[0].Field != "cpu" || q.Filters[1].Op != "~" || q.Filters[2].Field != "offline" {
		t.Fatalf("q=%#v", q)
	}
}

func TestParseQueryRejectsUnknownAndTooLarge(t *testing.T) {
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	if _, err := ParseQuery([]string{"foo>1"}, now); err == nil {
		t.Fatal("expected unknown token")
	}
	if _, err := ParseQuery([]string{"cpu>1", "40d"}, now); err == nil {
		t.Fatal("expected too large")
	}
	q, err := ParseQuery([]string{"total>1", "40d"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if q.Range.Kind != "40d" {
		t.Fatalf("traffic span should allow 40d: %#v", q.Range)
	}
	if _, err := ParseQuery([]string{"cpu>1", "91d"}, now); err == nil {
		t.Fatal("91d must exceed cap")
	}
}

func TestParseQueryVersionAndVsPrev(t *testing.T) {
	q, err := ParseQuery([]string{"ver<1.2.0", "vs=prev", "today"}, time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if q.Compare != "prev" || q.Filters[0].Field != "ver" || q.Filters[0].Values[0] != "1.2.0" {
		t.Fatalf("%#v", q)
	}
	if !strings.Contains(q.Range.Label(), "今日") && q.Range.Kind != "today" {
		t.Fatal(q.Range.Label())
	}
}

func TestApplyOverlayReplacesRangeSortGroupPage(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	q, err := ParseQuery([]string{"cpu>10", "today"}, now)
	if err != nil {
		t.Fatal(err)
	}
	got := applyOverlay(q, "r=7d,s=-mem,g=tag,p=2")
	if got.Range.Kind != "7d" || got.Sort != "-mem" || got.GroupBy != "tag" || got.Page != 2 {
		t.Fatalf("%#v", got)
	}
	if q.Range.Kind != "today" {
		t.Fatal("clone must not mutate original")
	}
}

func TestParseQueryBareDigitsAreID(t *testing.T) {
	q, err := ParseQuery([]string{"12"}, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Filters) != 1 || q.Filters[0].Field != "id" || q.Filters[0].Values[0] != "12" {
		t.Fatalf("%#v", q.Filters)
	}
}

func TestParseQueryBareNameIsHostAuto(t *testing.T) {
	q, err := ParseQuery([]string{"hk-1"}, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Filters) != 1 || q.Filters[0].Field != "host" || q.Filters[0].Op != "auto" {
		t.Fatalf("%#v", q.Filters)
	}
}

func TestParseKindQueryCmpSkipsRangeToken(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	q, err := parseKindQuery("cmp", "7d hk-1 hk-2", now)
	if err != nil {
		t.Fatal(err)
	}
	if q.Range.Kind != "7d" {
		t.Fatalf("range=%#v", q.Range)
	}
	if len(q.Left) != 1 || q.Left[0].Values[0] != "hk-1" {
		t.Fatalf("left=%#v", q.Left)
	}
	if len(q.Right) != 1 || q.Right[0].Values[0] != "hk-2" {
		t.Fatalf("right=%#v", q.Right)
	}
}

func TestParseKindQueryChartSkipsRangeThenMetric(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	q, err := parseKindQuery("chart", "24h id=1 cpu", now)
	if err != nil {
		t.Fatal(err)
	}
	if q.Metric != "cpu" || q.Range.Kind != "24h" {
		t.Fatalf("%#v", q)
	}
	if len(q.Filters) != 1 || q.Filters[0].Field != "id" || q.Filters[0].Values[0] != "1" {
		t.Fatalf("filters=%#v", q.Filters)
	}
}

func TestParseKindQueryChartMetricIsNotHost(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	q, err := parseKindQuery("chart", "cpu 24h", now)
	if err != nil {
		t.Fatal(err)
	}
	if q.Metric != "cpu" || q.Range.Kind != "24h" || len(q.Filters) != 0 {
		t.Fatalf("%#v", q)
	}
}

func TestApplyOverlayTagDrilldown(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	q, err := ParseQuery([]string{"7d"}, now)
	if err != nil {
		t.Fatal(err)
	}
	q.Kind = "groups"
	q.GroupBy = "tag"
	got := applyOverlay(q, "t=hk")
	if got.Kind != "servers" || got.GroupBy != "" || got.Range.Kind != "7d" || got.Page != 1 {
		t.Fatalf("%#v", got)
	}
	if len(got.Filters) != 1 || got.Filters[0].Field != "tag" || got.Filters[0].Values[0] != "hk" {
		t.Fatalf("filters=%#v", got.Filters)
	}
}
