package bot

import (
	"testing"

	"github.com/hi2shark/santaizi-dashboard/service/report"
)

func TestCompareVerAndNumericFilters(t *testing.T) {
	if compareVer("1.2.0", "1.10.0") >= 0 {
		t.Fatal("1.2.0 should be < 1.10.0")
	}
	if compareVer("v1.0.9", "1.0.9") != 0 {
		t.Fatal("v prefix")
	}
	item := evalHost{Host: report.HostRow{CPU: 90, Name: "hk-1", Tag: "hk"}}
	if !matchSnapshot(item, []Filter{{Field: "tag", Op: "=", Values: []string{"hk", "jp"}}}) {
		t.Fatal("tag")
	}
	if matchHistory(item, Query{Filters: []Filter{{Field: "cpu", Op: ">", Number: 95}}}) {
		t.Fatal("cpu filter")
	}
	if !matchHistory(item, Query{Filters: []Filter{{Field: "cpu", Op: ">", Number: 80}}}) {
		t.Fatal("cpu pass")
	}
}

func TestSortEvalStableByName(t *testing.T) {
	items := []evalHost{
		{Host: report.HostRow{Name: "b", CPU: 10}},
		{Host: report.HostRow{Name: "a", CPU: 50}},
		{Host: report.HostRow{Name: "c", CPU: 50}},
	}
	sortEval(items, Query{Sort: "-cpu"})
	if items[0].Host.Name != "a" || items[1].Host.Name != "c" {
		t.Fatalf("%s %s", items[0].Host.Name, items[1].Host.Name)
	}
}

func TestDeltaLabelMissingPrev(t *testing.T) {
	if deltaLabel(10, 0, 0) != "无对照" {
		t.Fatal(deltaLabel(10, 0, 0))
	}
	if deltaLabel(150, 100, 1) != "+50%" {
		t.Fatal(deltaLabel(150, 100, 1))
	}
}
