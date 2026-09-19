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

func newTelemetryStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.TelemetryEvent{}, &model.TelemetryObservation{}, &model.TelemetryGap{}, &model.TelemetryIngestCursor{},
		&model.TelemetryDataLoss{}, &model.ObserverHealthBucket{}, &model.ObserverPathBucket{}, &model.ObserverAssignment{},
		&model.AvailabilityRecomputeQueue{}, &model.CollectorReplicationReceipt{}, &model.CollectorRuntime{},
	); err != nil {
		t.Fatal(err)
	}
	return NewStore(db), db
}

func event(t *testing.T, node, session []byte, sequence uint64, collected time.Time) *pb.TelemetryEvent {
	t.Helper()
	id, err := EventID(node, session, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return &pb.TelemetryEvent{
		EventId: id, NodeUuid: node, SessionId: session, Sequence: sequence,
		EventType:           pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_HEARTBEAT,
		Priority:            pb.TelemetryPriority_TELEMETRY_PRIORITY_P0_CRITICAL,
		CollectedAtUnixNano: collected.UnixNano(),
		SourceProtocol:      pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2,
		Reliability:         pb.Reliability_RELIABILITY_RELIABLE_REPLAY,
		Payload:             &pb.TelemetryEvent_Heartbeat{Heartbeat: &pb.HeartbeatPayload{}},
	}
}

func hostEvent(t *testing.T, node, session []byte, sequence uint64, collected time.Time) *pb.TelemetryEvent {
	t.Helper()
	item := event(t, node, session, sequence, collected)
	item.EventType = pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_HOST
	item.Priority = pb.TelemetryPriority_TELEMETRY_PRIORITY_P1_IMPORTANT
	item.Payload = &pb.TelemetryEvent_Host{Host: &pb.Host{Platform: "linux"}}
	return item
}

func TestIngestDeduplicatesAndAcknowledgesContiguousEvents(t *testing.T) {
	store, db := newTelemetryStore(t)
	now := time.Now()
	node, session := bytes.Repeat([]byte{1}, 16), bytes.Repeat([]byte{2}, 16)
	batch := &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{
		{Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 1, now)}},
		{Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 2, now)}},
	}}
	for i := 0; i < 2; i++ {
		result, err := store.Ingest(context.Background(), batch, "primary", now)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Acks) != 1 || result.Acks[0].GetAckThrough() != 2 {
			t.Fatalf("acks=%#v", result.Acks)
		}
	}
	var eventCount, observationCount, paths int64
	db.Model(&model.TelemetryEvent{}).Count(&eventCount)
	db.Model(&model.TelemetryObservation{}).Count(&observationCount)
	db.Model(&model.ObserverPathBucket{}).Count(&paths)
	if eventCount != 0 || observationCount != 0 || paths != 1 {
		t.Fatalf("event=%d observation=%d paths=%d", eventCount, observationCount, paths)
	}
}

func TestIngestPersistsHostFacts(t *testing.T) {
	store, db := newTelemetryStore(t)
	now := time.Now()
	node, session := bytes.Repeat([]byte{0x11}, 16), bytes.Repeat([]byte{0x12}, 16)
	if _, err := store.Ingest(context.Background(), &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{
		{Record: &pb.TelemetryRecord_Event{Event: hostEvent(t, node, session, 1, now)}},
	}}, "primary", now); err != nil {
		t.Fatal(err)
	}
	var eventCount, observationCount int64
	db.Model(&model.TelemetryEvent{}).Count(&eventCount)
	db.Model(&model.TelemetryObservation{}).Count(&observationCount)
	if eventCount != 1 || observationCount != 1 {
		t.Fatalf("event=%d observation=%d", eventCount, observationCount)
	}
}

func TestGapAllowsCursorToAdvance(t *testing.T) {
	store, _ := newTelemetryStore(t)
	now := time.Now()
	node, session := bytes.Repeat([]byte{3}, 16), bytes.Repeat([]byte{4}, 16)
	batch := &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{
		{Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 1, now)}},
		{Record: &pb.TelemetryRecord_Gap{Gap: &pb.SequenceGap{GapId: bytes.Repeat([]byte{9}, 16), NodeUuid: node, SessionId: session, StartSequence: 2, EndSequence: 3, Reason: pb.GapReason_GAP_REASON_COMPACTED}}},
		{Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 4, now)}},
	}}
	result, err := store.Ingest(context.Background(), batch, "primary", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Acks[0].GetAckThrough(); got != 4 {
		t.Fatalf("ack=%d", got)
	}
}

func TestReplicationAckLossRetryIsIdempotent(t *testing.T) {
	store, db := newTelemetryStore(t)
	now := time.Now()
	node, session := bytes.Repeat([]byte{5}, 16), bytes.Repeat([]byte{6}, 16)
	fact := event(t, node, session, 1, now)
	batch := &pb.ReplicationBatch{
		CollectorUuid: "collector-a", ReplicationSession: bytes.Repeat([]byte{7}, 16),
		BatchSequence: 1, SpoolThroughId: 42, Events: []*pb.TelemetryEvent{fact},
		Observations: []*pb.TelemetryObservation{{
			EventId: fact.GetEventId(), ObserverId: "collector-a", ReceivedAtUnixNano: now.UnixNano(),
		}},
	}
	// The first ACK is deliberately treated as lost by immediately replaying the
	// exact same batch identity.
	for attempt := 0; attempt < 2; attempt++ {
		committed, err := store.Replicate(context.Background(), batch, now.Add(time.Duration(attempt)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if committed != 42 {
			t.Fatalf("committed spool cursor=%d", committed)
		}
	}
	for table, modelValue := range map[string]any{
		"events": &model.TelemetryEvent{}, "observations": &model.TelemetryObservation{},
		"receipts": &model.CollectorReplicationReceipt{},
	} {
		var count int64
		if err := db.Model(modelValue).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		want := int64(0)
		if table == "receipts" {
			want = 1
		}
		if count != want {
			t.Fatalf("%s count=%d want=%d", table, count, want)
		}
	}
	var paths int64
	if err := db.Model(&model.ObserverPathBucket{}).Count(&paths).Error; err != nil {
		t.Fatal(err)
	}
	if paths != 1 {
		t.Fatalf("paths=%d", paths)
	}
}

func TestHoleDoesNotAdvanceBeforeGrace(t *testing.T) {
	store, db := newTelemetryStore(t)
	now := time.Now()
	node, session := bytes.Repeat([]byte{0x21}, 16), bytes.Repeat([]byte{0x22}, 16)
	result, err := store.Ingest(context.Background(), &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{
		{Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 1, now)}},
		{Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 3, now)}},
	}}, "primary", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Acks[0].GetAckThrough(); got != 1 {
		t.Fatalf("ack=%d", got)
	}
	var cursor model.TelemetryIngestCursor
	if err := db.First(&cursor, "receiver_id = ?", "primary").Error; err != nil {
		t.Fatal(err)
	}
	if cursor.HoleSequence != 2 || cursor.HoleSince == 0 {
		t.Fatalf("hole=%d since=%d", cursor.HoleSequence, cursor.HoleSince)
	}
	var gaps, losses int64
	db.Model(&model.TelemetryGap{}).Count(&gaps)
	db.Model(&model.TelemetryDataLoss{}).Count(&losses)
	if gaps != 0 || losses != 0 {
		t.Fatalf("gaps=%d losses=%d", gaps, losses)
	}
}

func TestHoleHealsAfterGrace(t *testing.T) {
	store, db := newTelemetryStore(t)
	now := time.Now()
	node, session := bytes.Repeat([]byte{0x23}, 16), bytes.Repeat([]byte{0x24}, 16)
	if _, err := store.Ingest(context.Background(), &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{
		{Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 1, now)}},
		{Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 3, now)}},
	}}, "primary", now); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TelemetryIngestCursor{}).Where("receiver_id = ?", "primary").
		Update("hole_since", now.Add(-6*time.Minute).UnixNano()).Error; err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Minute)
	result, err := store.Ingest(context.Background(), &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{
		{Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 3, later)}},
	}}, "primary", later)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Acks[0].GetAckThrough(); got != 3 {
		t.Fatalf("ack=%d", got)
	}
	var gap model.TelemetryGap
	if err := db.First(&gap).Error; err != nil {
		t.Fatal(err)
	}
	if gap.StartSequence != 2 || gap.EndSequence != 2 || gap.Reason != int32(pb.GapReason_GAP_REASON_COMPACTED) {
		t.Fatalf("gap=%#v", gap)
	}
	var loss model.TelemetryDataLoss
	if err := db.First(&loss).Error; err != nil {
		t.Fatal(err)
	}
	if loss.LostRecords != 1 || loss.ComponentID != "primary" {
		t.Fatalf("loss=%#v", loss)
	}
	var cursor model.TelemetryIngestCursor
	if err := db.First(&cursor, "receiver_id = ?", "primary").Error; err != nil {
		t.Fatal(err)
	}
	if cursor.HoleSequence != 0 || cursor.HoleSince != 0 {
		t.Fatalf("hole not cleared: %#v", cursor)
	}
}

func TestStoreUsesConfiguredAvailabilityBucket(t *testing.T) {
	_, db := newTelemetryStore(t)
	store := NewStoreWithBucketSize(db, 10*time.Second)
	collected := time.Unix(1_800_000_017, 0)
	node, session := bytes.Repeat([]byte{8}, 16), bytes.Repeat([]byte{9}, 16)
	if _, err := store.Ingest(context.Background(), &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{{
		Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 1, collected)},
	}}}, "primary", collected); err != nil {
		t.Fatal(err)
	}
	var bucket model.ObserverPathBucket
	if err := db.First(&bucket, "node_uuid = ? AND observer_id = ?", node, "primary").Error; err != nil {
		t.Fatal(err)
	}
	expected := collected.UnixNano() / int64(10*time.Second) * int64(10*time.Second)
	if bucket.BucketStart != expected {
		t.Fatalf("bucket=%d want=%d", bucket.BucketStart, expected)
	}
}

func TestClockRollbackUsesReceiveTimeAndCannotBecomeFreshRuntime(t *testing.T) {
	store, db := newTelemetryStore(t)
	received := time.Now()
	collected := received.Add(-time.Hour)
	node, session := bytes.Repeat([]byte{0x61}, 16), bytes.Repeat([]byte{0x62}, 16)
	result, err := store.Ingest(context.Background(), &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{{
		Record: &pb.TelemetryRecord_Event{Event: event(t, node, session, 1, collected)},
	}}}, "primary", received)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FreshEvents) != 0 {
		t.Fatal("clock-rolled-back history was classified as fresh runtime")
	}
	var paths int64
	if err := db.Model(&model.TelemetryEvent{}).Count(&paths).Error; err != nil {
		t.Fatal(err)
	}
	if paths != 0 {
		t.Fatal("heartbeat should not persist an evidence row")
	}
	var path model.ObserverPathBucket
	if err := db.First(&path, "node_uuid = ?", node).Error; err != nil {
		t.Fatal(err)
	}
	expectedBucket := received.UnixNano() / int64(30*time.Second) * int64(30*time.Second)
	if path.BucketStart != expectedBucket {
		t.Fatalf("path bucket=%d want receive-time bucket %d", path.BucketStart, expectedBucket)
	}
}

func TestReplicateSkipsObservationWhenEventGone(t *testing.T) {
	store, db := newTelemetryStore(t)
	now := time.Now()
	node, session := bytes.Repeat([]byte{0x71}, 16), bytes.Repeat([]byte{0x72}, 16)
	missing := event(t, node, session, 9, now)
	batch := &pb.ReplicationBatch{
		CollectorUuid: "collector-b", ReplicationSession: bytes.Repeat([]byte{0x73}, 16),
		BatchSequence: 1, SpoolThroughId: 7,
		Observations: []*pb.TelemetryObservation{{
			EventId: missing.GetEventId(), ObserverId: "collector-b", ReceivedAtUnixNano: now.UnixNano(),
		}},
	}
	committed, err := store.Replicate(context.Background(), batch, now)
	if err != nil {
		t.Fatal(err)
	}
	if committed != 7 {
		t.Fatalf("committed=%d", committed)
	}
	var observations int64
	if err := db.Model(&model.TelemetryObservation{}).Count(&observations).Error; err != nil {
		t.Fatal(err)
	}
	if observations != 0 {
		t.Fatalf("observations=%d", observations)
	}
}
