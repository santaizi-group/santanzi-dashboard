package telemetry

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newRetentionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.TelemetryEvent{}, &model.TelemetryObservation{}, &model.TelemetryGap{},
		&model.StateRollup{}, &model.AvailabilityBucket{}, &model.AvailabilityIncident{},
		&model.IncidentRevision{}, &model.ConnectionLatencyBucket{},
		&model.ObserverPathBucket{}, &model.ObserverHealthBucket{},
		&model.CollectorReplicationReceipt{}, &model.TelemetryAlert{}, &model.TelemetryDataLoss{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDrainRetentionDeletesMoreThanOneBatch(t *testing.T) {
	db := newRetentionDB(t)
	now := time.Now()
	old := now.Add(-40 * 24 * time.Hour).UnixNano()
	node := bytes.Repeat([]byte{9}, 16)
	rows := make([]model.TelemetryObservation, 1500)
	for i := range rows {
		eventID := make([]byte, 16)
		eventID[0] = byte(i)
		eventID[1] = byte(i >> 8)
		rows[i] = model.TelemetryObservation{
			EventID: eventID, ObserverID: "primary", NodeUUID: node, ReceivedAt: old,
		}
	}
	if err := db.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatal(err)
	}
	deleted, err := DrainRetention(context.Background(), db, RetentionPolicy{BatchSize: 100, MaxRuntime: time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted < 1500 {
		t.Fatalf("deleted=%d", deleted)
	}
	var count int64
	if err := db.Model(&model.TelemetryObservation{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("remaining=%d", count)
	}
}

func TestDrainRetentionKeepsFreshRowsAndStripsOldStatePayload(t *testing.T) {
	db := newRetentionDB(t)
	now := time.Now().Truncate(time.Minute).Add(30 * time.Second)
	node, session := bytes.Repeat([]byte{1}, 16), bytes.Repeat([]byte{2}, 16)
	oldAt := now.Add(-8 * time.Hour)
	freshAt := now
	oldID, _ := EventID(node, session, 1)
	freshID, _ := EventID(node, session, 2)
	hostID, _ := EventID(node, session, 3)
	beatID, _ := EventID(node, session, 4)
	event := &pb.TelemetryEvent{EventId: oldID, NodeUuid: node, SessionId: session, Sequence: 1, EventType: pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE, Payload: &pb.TelemetryEvent_State{State: &pb.State{Cpu: 10}}}
	encoded, _ := proto.Marshal(event)
	if err := db.Create(&model.TelemetryEvent{
		EventID: oldID, NodeUUID: node, SessionID: session, Sequence: 1,
		EventType: int32(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE), CollectedAt: oldAt.UnixNano(),
		Payload: encoded, PayloadRetained: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelemetryEvent{
		EventID: freshID, NodeUUID: node, SessionID: session, Sequence: 2,
		EventType: int32(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE), CollectedAt: freshAt.UnixNano(),
		Payload: encoded, PayloadRetained: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelemetryEvent{
		EventID: hostID, NodeUUID: node, SessionID: session, Sequence: 3,
		EventType: int32(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_HOST), CollectedAt: oldAt.UnixNano(),
		Payload: encoded, PayloadRetained: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelemetryEvent{
		EventID: beatID, NodeUUID: node, SessionID: session, Sequence: 4,
		EventType: int32(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_HEARTBEAT), CollectedAt: oldAt.UnixNano(),
		Payload: encoded, PayloadRetained: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelemetryObservation{EventID: oldID, ObserverID: "primary", NodeUUID: node, ReceivedAt: oldAt.UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelemetryObservation{EventID: freshID, ObserverID: "primary", NodeUUID: node, ReceivedAt: freshAt.UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	staleBucket := now.Add(-40 * 24 * time.Hour).UnixNano()
	if err := db.Create(&model.ObserverPathBucket{NodeUUID: node, ObserverID: "primary", BucketStart: staleBucket}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ObserverPathBucket{NodeUUID: node, ObserverID: "primary", BucketStart: freshAt.UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ObserverHealthBucket{ObserverID: "primary", BucketStart: staleBucket}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ObserverHealthBucket{ObserverID: "primary", BucketStart: freshAt.UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorReplicationReceipt{
		CollectorUUID: "c1", ReplicationSession: bytes.Repeat([]byte{3}, 16), BatchSequence: 1, CreatedAt: now.Add(-10 * 24 * time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorReplicationReceipt{
		CollectorUUID: "c1", ReplicationSession: bytes.Repeat([]byte{4}, 16), BatchSequence: 2, CreatedAt: now.Add(-time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DrainRetention(context.Background(), db, RetentionPolicy{BatchSize: 50, MaxRuntime: time.Minute}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&model.TelemetryEvent{}, "event_id = ?", oldID).Error; err == nil {
		t.Fatal("8h-old STATE row should be deleted")
	}
	if err := db.First(&model.TelemetryEvent{}, "event_id = ?", beatID).Error; err == nil {
		t.Fatal("8h-old HEARTBEAT row should be deleted")
	}
	var freshEvent, hostEvent model.TelemetryEvent
	if err := db.First(&freshEvent, "event_id = ?", freshID).Error; err != nil {
		t.Fatal(err)
	}
	if !freshEvent.PayloadRetained {
		t.Fatal("current-minute payload stripped")
	}
	if err := db.First(&hostEvent, "event_id = ?", hostID).Error; err != nil {
		t.Fatal(err)
	}
	if hostEvent.PayloadRetained || len(hostEvent.Payload) != 0 {
		t.Fatal("8h-old HOST payload should be stripped")
	}
	var paths, health, receipts, observations int64
	if err := db.Model(&model.ObserverPathBucket{}).Count(&paths).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ObserverHealthBucket{}).Count(&health).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CollectorReplicationReceipt{}).Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TelemetryObservation{}).Count(&observations).Error; err != nil {
		t.Fatal(err)
	}
	if paths != 1 || health != 1 || receipts != 1 || observations != 1 {
		t.Fatalf("paths=%d health=%d receipts=%d observations=%d", paths, health, receipts, observations)
	}
}

func TestPolicyFromConfigUsesDocumentedDefaults(t *testing.T) {
	policy := PolicyFromConfig(model.RetentionConfig{})
	if policy.BatchSize != DefaultRetentionBatch || policy.MaxRuntime != DefaultRetentionBudget {
		t.Fatalf("batch=%d runtime=%s", policy.BatchSize, policy.MaxRuntime)
	}
	if policy.Receipt != DefaultReceiptRetain || policy.Evidence != DefaultEvidenceRetain || policy.CompactMinBytes != DefaultCompactMinBytes || !policy.AutoCompact {
		t.Fatalf("receipt=%s evidence=%s compact=%d auto=%v", policy.Receipt, policy.Evidence, policy.CompactMinBytes, policy.AutoCompact)
	}
	off := false
	if PolicyFromConfig(model.RetentionConfig{AutoCompact: &off}).AutoCompact {
		t.Fatal("explicit auto_compact=false must stick")
	}
}

func TestPayloadRetentionSkipsCurrentMinute(t *testing.T) {
	db := newRetentionDB(t)
	now := time.Now().Truncate(time.Minute).Add(30 * time.Second)
	node, session := bytes.Repeat([]byte{5}, 16), bytes.Repeat([]byte{6}, 16)
	eventID, _ := EventID(node, session, 1)
	event := &pb.TelemetryEvent{EventId: eventID, Payload: &pb.TelemetryEvent_State{State: &pb.State{Cpu: 1}}}
	encoded, _ := proto.Marshal(event)
	if err := db.Create(&model.TelemetryEvent{
		EventID: eventID, NodeUUID: node, SessionID: session, Sequence: 1,
		EventType:   int32(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE),
		CollectedAt: now.UnixNano(), Payload: encoded, PayloadRetained: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DrainRetention(context.Background(), db, RetentionPolicy{StateRaw: time.Nanosecond, MaxRuntime: time.Minute}, now); err != nil {
		t.Fatal(err)
	}
	var row model.TelemetryEvent
	if err := db.First(&row, "event_id = ?", eventID).Error; err != nil {
		t.Fatal(err)
	}
	if !row.PayloadRetained {
		t.Fatal("current-minute payload should stay")
	}
}

func TestDrainRetentionDeletesCompletedMinuteHighFreqRows(t *testing.T) {
	db := newRetentionDB(t)
	now := time.Now().Truncate(time.Minute).Add(30 * time.Second)
	node, session := bytes.Repeat([]byte{7}, 16), bytes.Repeat([]byte{8}, 16)
	eventID, _ := EventID(node, session, 1)
	encoded, _ := proto.Marshal(&pb.TelemetryEvent{EventId: eventID, Payload: &pb.TelemetryEvent_State{State: &pb.State{Cpu: 1}}})
	if err := db.Create(&model.TelemetryEvent{
		EventID: eventID, NodeUUID: node, SessionID: session, Sequence: 1,
		EventType:   int32(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE),
		CollectedAt: now.Add(-2 * time.Minute).UnixNano(), Payload: encoded, PayloadRetained: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DrainRetention(context.Background(), db, RetentionPolicy{MaxRuntime: time.Minute}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&model.TelemetryEvent{}, "event_id = ?", eventID).Error; err == nil {
		t.Fatal("completed-minute STATE row should be deleted")
	}
}

func TestDrainRetentionDeletesStaleMonitorHistory(t *testing.T) {
	db := newRetentionDB(t)
	if err := db.AutoMigrate(&model.MonitorHistory{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rows := []model.MonitorHistory{
		{MonitorID: 1, ServerID: 9, CreatedAt: now.Add(-36 * time.Hour), AvgDelay: 1},
		{MonitorID: 1, ServerID: 9, CreatedAt: now.Add(-time.Hour), AvgDelay: 2},
		{MonitorID: 1, ServerID: 0, CreatedAt: now.Add(-2 * 24 * time.Hour), AvgDelay: 3},
		{MonitorID: 1, ServerID: 0, CreatedAt: now.Add(-40 * 24 * time.Hour), AvgDelay: 4},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DrainRetention(context.Background(), db, RetentionPolicy{MaxRuntime: time.Minute}, now); err != nil {
		t.Fatal(err)
	}
	var remaining []model.MonitorHistory
	if err := db.Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("remaining=%d %#v", len(remaining), remaining)
	}
}

func TestDrainRetentionDropsPathBucketsOutsideEvidenceWindow(t *testing.T) {
	db := newRetentionDB(t)
	now := time.Now()
	node := bytes.Repeat([]byte{0x21}, 16)
	stale := now.Add(-72 * time.Hour).UnixNano()
	fresh := now.Add(-time.Hour).UnixNano()
	if err := db.Create(&model.ObserverPathBucket{NodeUUID: node, ObserverID: "primary", BucketStart: stale}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ObserverPathBucket{NodeUUID: node, ObserverID: "primary", BucketStart: fresh}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DrainRetention(context.Background(), db, RetentionPolicy{MaxRuntime: time.Minute}, now); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.ObserverPathBucket{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("paths=%d", count)
	}
}

func TestDrainRetentionDeletesStaleEndpointSnapshots(t *testing.T) {
	db := newRetentionDB(t)
	if err := db.AutoMigrate(&model.ProbeLatest{}, &model.ProbeTrace{}, &model.ProbeSampleBucket{}, &model.ConnectionLatencyBucket{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	stale := now.Add(-49 * time.Hour).UnixNano()
	fresh := now.Add(-47 * time.Hour).UnixNano()
	if err := db.Create(&model.ProbeLatest{CollectorUUID: "c1", ServerID: 1, SampledAt: stale}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeLatest{CollectorUUID: "c1", ServerID: 2, SampledAt: fresh}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeTrace{CollectorUUID: "c1", ServerID: 1, SampledAt: stale}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeTrace{CollectorUUID: "c1", ServerID: 2, SampledAt: fresh}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeSampleBucket{CollectorUUID: "c1", ServerID: 1, Kind: "icmp", BucketStart: stale, Count: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeSampleBucket{CollectorUUID: "c1", ServerID: 2, Kind: "icmp", BucketStart: fresh, Count: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ConnectionLatencyBucket{Kind: LatencyKindPath, ObserverID: "primary", NodeUUID: latencyNodeKey(nil), BucketStart: stale, Count: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ConnectionLatencyBucket{Kind: LatencyKindPath, ObserverID: "primary", NodeUUID: latencyNodeKey(nil), BucketStart: fresh, Count: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DrainRetention(context.Background(), db, RetentionPolicy{MaxRuntime: time.Minute}, now); err != nil {
		t.Fatal(err)
	}
	var latest, traces, samples, latency int64
	if err := db.Model(&model.ProbeLatest{}).Count(&latest).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProbeTrace{}).Count(&traces).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProbeSampleBucket{}).Count(&samples).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ConnectionLatencyBucket{}).Count(&latency).Error; err != nil {
		t.Fatal(err)
	}
	if latest != 1 || traces != 1 || samples != 1 || latency != 1 {
		t.Fatalf("latest=%d traces=%d samples=%d latency=%d", latest, traces, samples, latency)
	}
}

func TestDrainRetentionCompactsOldAvailabilityRuns(t *testing.T) {
	db := newRetentionDB(t)
	now := time.Now()
	hour := now.Add(-72 * time.Hour).Truncate(time.Hour)
	node := bytes.Repeat([]byte{0x22}, 16)
	step := int64(30 * time.Second)
	for i := 0; i < 4; i++ {
		start := hour.UnixNano() + int64(i)*step
		if err := db.Create(&model.AvailabilityBucket{
			NodeUUID: node, BucketStart: start, WindowEnd: start + step,
			Resolution: model.AvailabilityResolutionRaw, HostState: model.HostStateOnline,
			ConnectivityState: model.ConnectivityFull, ExpectedObservers: 1, HealthyObservers: 1, SeenObservers: 1,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	recent := now.Add(-time.Hour).UnixNano()
	if err := db.Create(&model.AvailabilityBucket{
		NodeUUID: node, BucketStart: recent, WindowEnd: recent + step,
		Resolution: model.AvailabilityResolutionRaw, HostState: model.HostStateOnline,
		ConnectivityState: model.ConnectivityFull, ExpectedObservers: 1, HealthyObservers: 1, SeenObservers: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DrainRetention(context.Background(), db, RetentionPolicy{MaxRuntime: time.Minute}, now); err != nil {
		t.Fatal(err)
	}
	var rows []model.AvailabilityBucket
	if err := db.Order("bucket_start ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d %#v", len(rows), rows)
	}
	if rows[0].Resolution != model.AvailabilityResolutionSpan || rows[0].WindowEnd != hour.UnixNano()+4*step {
		t.Fatalf("compacted=%#v", rows[0])
	}
	if rows[1].BucketStart != recent || rows[1].Resolution != model.AvailabilityResolutionRaw {
		t.Fatalf("recent=%#v", rows[1])
	}
}

func TestDrainRetentionPrunesRouteJobsAndIngestCursors(t *testing.T) {
	db := newRetentionDB(t)
	if err := db.AutoMigrate(&model.ProbeRouteJob{}, &model.TelemetryIngestCursor{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-40 * 24 * time.Hour)
	jobs := []model.ProbeRouteJob{
		{CollectorUUID: "c", ServerID: 1, Protocol: "icmp", Status: model.ProbeRouteJobDone, RequestedAt: old.UnixNano(), UpdatedAt: old},
		{CollectorUUID: "c", ServerID: 1, Protocol: "tcp", Status: model.ProbeRouteJobFailed, RequestedAt: old.UnixNano(), UpdatedAt: old},
		{CollectorUUID: "c", ServerID: 2, Protocol: "icmp", Status: model.ProbeRouteJobDone, RequestedAt: now.UnixNano(), UpdatedAt: now},
		{CollectorUUID: "c", ServerID: 3, Protocol: "icmp", Status: model.ProbeRouteJobPending, RequestedAt: old.UnixNano(), UpdatedAt: old},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	cursors := []model.TelemetryIngestCursor{
		{ReceiverID: "primary", NodeUUID: bytes.Repeat([]byte{1}, 16), SessionID: bytes.Repeat([]byte{2}, 16), AckThrough: 9, UpdatedAt: old},
		{ReceiverID: "primary", NodeUUID: bytes.Repeat([]byte{1}, 16), SessionID: bytes.Repeat([]byte{3}, 16), AckThrough: 9, UpdatedAt: now},
	}
	if err := db.Create(&cursors).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DrainRetention(context.Background(), db, RetentionPolicy{BatchSize: 100, MaxRuntime: time.Minute}, now); err != nil {
		t.Fatal(err)
	}
	var remainingJobs []model.ProbeRouteJob
	if err := db.Find(&remainingJobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(remainingJobs) != 1 || remainingJobs[0].ServerID != 2 {
		t.Fatalf("jobs=%#v", remainingJobs)
	}
	var remainingCursors []model.TelemetryIngestCursor
	if err := db.Find(&remainingCursors).Error; err != nil {
		t.Fatal(err)
	}
	if len(remainingCursors) != 1 || !bytes.Equal(remainingCursors[0].SessionID, bytes.Repeat([]byte{3}, 16)) {
		t.Fatalf("cursors=%#v", remainingCursors)
	}
}
