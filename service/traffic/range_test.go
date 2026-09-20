package traffic

import (
	"bytes"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRangeUsageMatchesSumRollupAndSplitsDirections(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.StateRollup{}); err != nil {
		t.Fatal(err)
	}
	node := bytes.Repeat([]byte{3}, 16)
	start := time.Date(2026, 8, 11, 10, 35, 0, 0, time.UTC)
	end := time.Date(2026, 8, 11, 12, 20, 0, 0, time.UTC)
	rows := []model.StateRollup{
		{NodeUUID: node, Resolution: "1m", WindowStart: start.UnixNano(), WindowEnd: start.Add(time.Minute).UnixNano(), Payload: []byte{1}, NetInTotal: 5, NetOutTotal: 1},
		{NodeUUID: node, Resolution: "1h", WindowStart: time.Date(2026, 8, 11, 11, 0, 0, 0, time.UTC).UnixNano(), WindowEnd: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC).UnixNano(), Payload: []byte{1}, NetInTotal: 100, NetOutTotal: 40},
		{NodeUUID: node, Resolution: "1m", WindowStart: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC).UnixNano(), WindowEnd: time.Date(2026, 8, 11, 12, 1, 0, 0, time.UTC).UnixNano(), Payload: []byte{1}, NetInTotal: 7, NetOutTotal: 2},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	used, err := RangeUsage(db, node, start, end, model.TrafficDirectionInbound)
	if err != nil {
		t.Fatal(err)
	}
	if used != 112 {
		t.Fatalf("in=%d", used)
	}
	split, err := RangeUsageSplit(db, node, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if split.In != 112 || split.Out != 43 || split.Total != 155 {
		t.Fatalf("split=%#v", split)
	}
	many, err := RangeUsageMany(db, [][]byte{node, bytes.Repeat([]byte{9}, 16)}, start, end)
	if err != nil {
		t.Fatal(err)
	}
	got := many[string(node)]
	if got.Total != 155 {
		t.Fatalf("many=%#v", many)
	}
	if many[string(bytes.Repeat([]byte{9}, 16))].Total != 0 {
		t.Fatal("missing node must stay zero")
	}
}

func TestRangeUsageEmptyNodeIsZero(t *testing.T) {
	used, err := RangeUsage(nil, nil, time.Now(), time.Now().Add(time.Hour), "")
	if err != nil || used != 0 {
		t.Fatalf("used=%d err=%v", used, err)
	}
}
