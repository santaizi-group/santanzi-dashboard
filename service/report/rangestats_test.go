package report

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRangeStatsMergesPayloadExtremaAndDoesNotNPlusOne(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.StateRollup{}, &model.ServerNodeBinding{}); err != nil {
		t.Fatal(err)
	}
	node := bytes.Repeat([]byte{4}, 16)
	from := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 20, 12, 20, 0, 0, time.UTC)
	hour := from
	if err := db.Create(&model.StateRollup{
		NodeUUID: node, Resolution: "1h", WindowStart: hour.UnixNano(), WindowEnd: hour.Add(time.Hour).UnixNano(),
		SampleCount: 2, Payload: mustRollup(t, 10, 80, 40, 100, 400), NetInTotal: 100, NetOutTotal: 50,
	}).Error; err != nil {
		t.Fatal(err)
	}
	cur := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	if err := db.Create(&model.StateRollup{
		NodeUUID: node, Resolution: "1m", WindowStart: cur.UnixNano(), WindowEnd: cur.Add(time.Minute).UnixNano(),
		SampleCount: 1, Payload: mustRollup(t, 90, 90, 90, 800, 800), NetInTotal: 7, NetOutTotal: 3,
	}).Error; err != nil {
		t.Fatal(err)
	}
	stats, err := RangeStats(db, [][]byte{node}, from, to)
	if err != nil {
		t.Fatal(err)
	}
	got := stats[string(node)]
	if got.CPUMin != 10 || got.CPUMax != 90 || got.CPUAvg < 50 {
		t.Fatalf("cpu=%#v", got)
	}
	if got.MemUsedMax != 800 || got.MemUsedMin != 100 {
		t.Fatalf("mem=%#v", got)
	}
	if got.Total != 160 {
		t.Fatalf("total=%d", got.Total)
	}
}

func TestRangeStatsRejectsSpanOver30Days(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := RangeStats(nil, [][]byte{bytes.Repeat([]byte{1}, 16)}, from, from.Add(31*24*time.Hour))
	if !errors.Is(err, ErrRangeTooLarge) {
		t.Fatalf("err=%v", err)
	}
}

func TestCurrentBindingsIgnoresEmpty(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ServerNodeBinding{}); err != nil {
		t.Fatal(err)
	}
	node := bytes.Repeat([]byte{5}, 16)
	if err := db.Create(&model.ServerNodeBinding{ServerID: 8, NodeUUID: node, Current: true, Reason: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	got, err := CurrentBindings(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[8]) != 16 {
		t.Fatalf("got=%v", got)
	}
}

func mustRollup(t *testing.T, minCPU, maxCPU, avgCPU float64, minMem, maxMem uint64) []byte {
	t.Helper()
	payload := &pb.StateRollupPayload{
		SampleCount: 2,
		Minimum:     &pb.State{Cpu: minCPU, MemUsed: minMem, DiskUsed: 10, Load1: 0.1},
		Maximum:     &pb.State{Cpu: maxCPU, MemUsed: maxMem, DiskUsed: 20, Load1: 1.2},
		Average:     &pb.State{Cpu: avgCPU, MemUsed: (minMem + maxMem) / 2, DiskUsed: 15, Load1: 0.6, NetInSpeed: 100},
		NetInTotal:  1,
		NetOutTotal: 1,
	}
	encoded, err := proto.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
