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

func TestResolveAutoHostPrefersExactThenTagThenLoose(t *testing.T) {
	hosts := []report.HostRow{
		{ID: 1, Name: "hk-1", Tag: "hk"},
		{ID: 12, Name: "edge-12", Tag: "jp"},
		{ID: 3, Name: "web", Tag: ""},
	}
	got := resolveHostFilters(hosts, []Filter{{Field: "host", Op: "auto", Values: []string{"hk-1"}}})
	if len(got) != 1 || got[0].Field != "name" || got[0].Op != "=" {
		t.Fatalf("exact name %#v", got)
	}
	got = resolveHostFilters(hosts, []Filter{{Field: "host", Op: "auto", Values: []string{"hk"}}})
	if len(got) != 1 || got[0].Field != "tag" || got[0].Values[0] != "hk" {
		t.Fatalf("exact tag %#v", got)
	}
	got = resolveHostFilters(hosts, []Filter{{Field: "host", Op: "auto", Values: []string{"default"}}})
	if len(got) != 1 || got[0].Field != "tag" || got[0].Values[0] != "default" {
		t.Fatalf("empty tag %#v", got)
	}
	got = resolveHostFilters(hosts, []Filter{{Field: "host", Op: "auto", Values: []string{"edge"}}})
	if len(got) != 1 || got[0].Field != "host" || got[0].Op != "~" {
		t.Fatalf("loose %#v", got)
	}
	item := evalHost{Host: hosts[1]}
	if !matchSnapshot(item, got) {
		t.Fatal("loose should match edge-12")
	}
	if matchSnapshot(evalHost{Host: hosts[0]}, got) {
		t.Fatal("loose should not match hk-1")
	}
}
