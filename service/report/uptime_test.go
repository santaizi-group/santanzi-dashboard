package report

import (
	"bytes"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCollectUptimeUsesClosedBuckets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AvailabilityBucket{}, &model.ServerNodeBinding{}); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	start := base.Add(-2 * time.Hour)
	nodeA := bytes.Repeat([]byte{1}, 16)
	nodeC := bytes.Repeat([]byte{3}, 16)
	nodeD := bytes.Repeat([]byte{4}, 16)
	nodeE := bytes.Repeat([]byte{5}, 16)
	for _, row := range []model.ServerNodeBinding{
		{ServerID: 1, NodeUUID: nodeA, Current: true, Reason: "test"},
		{ServerID: 3, NodeUUID: nodeC, Current: true, Reason: "test"},
		{ServerID: 4, NodeUUID: nodeD, Current: true, Reason: "test"},
		{ServerID: 5, NodeUUID: nodeE, Current: true, Reason: "test"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	put := func(node []byte, at time.Time, dur time.Duration, state string) {
		t.Helper()
		row := model.AvailabilityBucket{
			NodeUUID: node, BucketStart: at.UnixNano(), WindowEnd: at.Add(dur).UnixNano(),
			HostState: model.HostStateOnline, ConnectivityState: state, Resolution: model.AvailabilityResolutionRaw,
		}
		if state == model.ConnectivityUnavailable {
			row.HostState = model.HostStateOffline
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	put(nodeA, base.Add(-2*time.Minute), 30*time.Second, model.ConnectivityUnknown)
	put(nodeA, base.Add(-90*time.Second), 30*time.Second, model.ConnectivityFull)
	put(nodeA, base.Add(-60*time.Second), 30*time.Second, model.ConnectivityPartial)
	put(nodeA, base.Add(-30*time.Second), 30*time.Second, model.ConnectivityUnavailable)
	// 未结束的桶不参与。
	if err := db.Create(&model.AvailabilityBucket{
		NodeUUID: nodeA, BucketStart: base.Add(-15 * time.Second).UnixNano(), WindowEnd: base.Add(time.Minute).UnixNano(),
		HostState: model.HostStateOnline, ConnectivityState: model.ConnectivityFull, Resolution: model.AvailabilityResolutionRaw,
	}).Error; err != nil {
		t.Fatal(err)
	}
	t0 := start.Add(10 * time.Minute)
	put(nodeD, t0, 30*time.Second, model.ConnectivityUnavailable)
	put(nodeD, t0.Add(30*time.Second), 30*time.Second, model.ConnectivityUnavailable)
	put(nodeD, t0.Add(60*time.Second), 30*time.Second, model.ConnectivityFull)
	put(nodeD, t0.Add(90*time.Second), 30*time.Second, model.ConnectivityUnavailable)
	put(nodeE, start.Add(-30*time.Minute), 40*time.Minute, model.ConnectivityUnavailable)

	hosts := []HostRow{
		{ID: 1, Name: "a"},
		{ID: 2, Name: "b"},
		{ID: 3, Name: "c"},
		{ID: 4, Name: "d"},
		{ID: 5, Name: "e"},
	}
	got := collectUptime(db, hosts, start, base, base)
	if len(got) != 5 {
		t.Fatalf("len=%d", len(got))
	}
	byName := map[string]UptimeRow{}
	for _, row := range got {
		byName[row.Name] = row
	}
	a := byName["a"]
	if !a.HasData || a.Percent != 66.66 || a.OfflineSec != 30 {
		t.Fatalf("a=%#v", a)
	}
	if byName["b"].HasData || byName["c"].HasData {
		t.Fatalf("missing data looked like uptime: b=%#v c=%#v", byName["b"], byName["c"])
	}
	d := byName["d"]
	if !d.HasData || d.Percent != 25 || d.OfflineSec != 90 {
		t.Fatalf("d=%#v", d)
	}
	e := byName["e"]
	if !e.HasData || e.Percent != 0 || e.OfflineSec != 600 {
		t.Fatalf("e=%#v", e)
	}
	if got[0].Name != "e" || got[1].Name != "d" || got[2].Name != "a" {
		t.Fatalf("order=%s %s %s", got[0].Name, got[1].Name, got[2].Name)
	}
	if got[3].HasData || got[4].HasData {
		t.Fatal("no-data rows should sort last")
	}
	fillUptimeLongest(db, got, start, base, base)
	for _, row := range got {
		if row.Name == "d" && row.LongestSec != 60 {
			t.Fatalf("longest=%d", row.LongestSec)
		}
	}
}
