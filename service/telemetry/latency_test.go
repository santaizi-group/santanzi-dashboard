package telemetry

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecordLatencySampleDedupesAndAggregatesMinute(t *testing.T) {
	db := newConnectionDB(t)
	node := bytes.Repeat([]byte{0x11}, 16)
	first := time.Unix(1_700_000_010, 0)
	if err := RecordLatencySample(db, LatencySample{
		Kind: LatencyKindPath, NodeUUID: node, ObserverID: PrimaryObserverID, RttMs: 10, SampledAt: first.UnixNano(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := RecordLatencySample(db, LatencySample{
		Kind: LatencyKindPath, NodeUUID: node, ObserverID: PrimaryObserverID, RttMs: 99, SampledAt: first.UnixNano(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := RecordLatencySample(db, LatencySample{
		Kind: LatencyKindPath, NodeUUID: node, ObserverID: PrimaryObserverID, RttMs: 30, SampledAt: first.Add(20 * time.Second).UnixNano(),
	}); err != nil {
		t.Fatal(err)
	}
	rows, total, err := ListConnectionLatency(db, LatencyFilter{Kind: LatencyKindPath, ObserverID: PrimaryObserverID}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].Count != 2 || rows[0].MinMs != 10 || rows[0].MaxMs != 30 || rows[0].AvgMs != 20 {
		t.Fatalf("bucket=%#v total=%d", rows, total)
	}
}

func TestRecordAgentSinkLatencyWritesPathBucket(t *testing.T) {
	db := newConnectionDB(t)
	node := bytes.Repeat([]byte{0x22}, 16)
	sampled := time.Unix(1_700_000_000, 0).UnixNano()
	if err := RecordAgentSinkLatency(db, node, &pb.AgentRuntime{Sinks: []*pb.SinkRuntime{
		{EndpointId: PrimaryObserverID, Connected: true, LastRttMs: 7.5, RttSampledAtUnixNano: sampled},
	}}); err != nil {
		t.Fatal(err)
	}
	rows, total, err := ListConnectionLatency(db, LatencyFilter{Kind: LatencyKindPath}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || rows[0].AvgMs != 7.5 || rows[0].ObserverID != PrimaryObserverID {
		t.Fatalf("rows=%#v total=%d", rows, total)
	}
}

func TestRecordAgentSinkLatencySkipsUnhandshaked(t *testing.T) {
	db := newConnectionDB(t)
	now := time.Unix(1_700_000_090, 0)
	node := bytes.Repeat([]byte{0x33}, 16)
	sampled := now.UnixNano()
	createCollector(t, db, "collector-stale", "Stale")
	if err := db.Create(&model.CollectorRuntime{CollectorUUID: "collector-stale", Status: "online", LastSeen: now.Add(-2 * time.Minute).UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := recordAgentSinkLatency(db, node, &pb.AgentRuntime{Sinks: []*pb.SinkRuntime{
		{EndpointId: PrimaryObserverID, Connected: false, LastRttMs: 7.5, RttSampledAtUnixNano: sampled},
		{EndpointId: "collector-stale", Connected: true, LastRttMs: 40, RttSampledAtUnixNano: sampled},
	}}, now); err != nil {
		t.Fatal(err)
	}
	_, total, err := ListConnectionLatency(db, LatencyFilter{Kind: LatencyKindPath}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("unhandshaked sinks should not write buckets, total=%d", total)
	}
}

func TestApplyRetentionDeletesOldConnectionLatency(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TelemetryEvent{}, &model.StateRollup{}, &model.TelemetryObservation{}, &model.TelemetryGap{}, &model.AvailabilityBucket{}, &model.AvailabilityIncident{}, &model.ConnectionLatencyBucket{}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-49 * time.Hour).UnixNano()
	fresh := time.Now().Add(-time.Hour).UnixNano()
	if err := db.Create(&model.ConnectionLatencyBucket{Kind: LatencyKindCollectorHeartbeat, CollectorUUID: "c1", NodeUUID: latencyNodeKey(nil), BucketStart: old, MinMs: 1, MaxMs: 1, SumMs: 1, Count: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ConnectionLatencyBucket{Kind: LatencyKindCollectorHeartbeat, CollectorUUID: "c1", NodeUUID: latencyNodeKey(nil), BucketStart: fresh, MinMs: 2, MaxMs: 2, SumMs: 2, Count: 1}).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewRollupWorker(db, RetentionPolicy{BatchSize: 100})
	if err := worker.ApplyRetention(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.ConnectionLatencyBucket{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("retained=%d", count)
	}
}

func TestConnectionLatencyKeepExcludesProbeAndRevoked(t *testing.T) {
	db := newConnectionDB(t)
	createCollector(t, db, "obs-a", "A")
	createCollector(t, db, "obs-b", "B")
	if err := db.Create(&model.Collector{
		CollectorUUID: "probe-c", Name: "Probe", Address: "probe-c:5556",
		TokenHash: bytes.Repeat([]byte{3}, 32), RegistrationToken: "token-probe-c", Kind: model.CollectorKindProbe,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Collector{
		CollectorUUID: "revoked-c", Name: "Revoked", Address: "revoked-c:5556",
		TokenHash: bytes.Repeat([]byte{3}, 32), RegistrationToken: "token-revoked-c", Revoked: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	keep, err := connectionLatencyKeep(db)
	if err != nil {
		t.Fatal(err)
	}
	if keep != 2*ConnectionLatencyKeepMultiplier {
		t.Fatalf("keep=%d", keep)
	}
	if err := db.Model(&model.Collector{}).Where("collector_uuid IN ?", []string{"obs-a", "obs-b"}).Update("deleted", true).Error; err != nil {
		t.Fatal(err)
	}
	keep, err = connectionLatencyKeep(db)
	if err != nil {
		t.Fatal(err)
	}
	if keep != ConnectionLatencyKeepMultiplier {
		t.Fatalf("floor keep=%d", keep)
	}
}

func TestPruneConnectionLatencyByCountKeepsLatestPerGroup(t *testing.T) {
	db := newConnectionDB(t)
	nodeA := bytes.Repeat([]byte{0x41}, 16)
	nodeB := bytes.Repeat([]byte{0x42}, 16)
	base := time.Unix(1_700_000_000, 0)
	insertLatencyBuckets(t, db, LatencyKindPath, "", nodeA, PrimaryObserverID, base, 105)
	insertLatencyBuckets(t, db, LatencyKindPath, "", nodeB, PrimaryObserverID, base, 3)
	insertLatencyBuckets(t, db, LatencyKindCollectorHeartbeat, "c1", latencyNodeKey(nil), "", base, 105)

	deleted, err := PruneConnectionLatencyByCount(context.Background(), db, time.Now().Add(time.Minute), 50)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 10 {
		t.Fatalf("deleted=%d", deleted)
	}

	assertLatestBuckets(t, db, LatencyKindPath, "", nodeA, PrimaryObserverID, 100, base.Add(5*time.Minute).UnixNano())
	assertLatestBuckets(t, db, LatencyKindPath, "", nodeB, PrimaryObserverID, 3, base.UnixNano())
	assertLatestBuckets(t, db, LatencyKindCollectorHeartbeat, "c1", latencyNodeKey(nil), "", 100, base.Add(5*time.Minute).UnixNano())
}

func insertLatencyBuckets(t *testing.T, db *gorm.DB, kind, collectorUUID string, nodeUUID []byte, observerID string, start time.Time, n int) {
	t.Helper()
	rows := make([]model.ConnectionLatencyBucket, n)
	for i := 0; i < n; i++ {
		rows[i] = model.ConnectionLatencyBucket{
			Kind: kind, CollectorUUID: collectorUUID, NodeUUID: latencyNodeKey(nodeUUID),
			ObserverID: observerID, BucketStart: start.Add(time.Duration(i) * time.Minute).UnixNano(),
			MinMs: 1, MaxMs: 1, SumMs: 1, Count: 1,
		}
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
}

func assertLatestBuckets(t *testing.T, db *gorm.DB, kind, collectorUUID string, nodeUUID []byte, observerID string, want int, oldest int64) {
	t.Helper()
	var rows []model.ConnectionLatencyBucket
	if err := db.Where("kind = ? AND collector_uuid = ? AND node_uuid = ? AND observer_id = ?",
		kind, collectorUUID, latencyNodeKey(nodeUUID), observerID).
		Order("bucket_start ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != want {
		t.Fatalf("kind=%s remaining=%d want=%d", kind, len(rows), want)
	}
	if rows[0].BucketStart != oldest {
		t.Fatalf("kind=%s oldest=%d want=%d", kind, rows[0].BucketStart, oldest)
	}
}
