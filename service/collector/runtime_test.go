package collector

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNoteSyncRTTRecordsHeartbeatSample(t *testing.T) {
	runtime := &Runtime{}
	now := time.Unix(1_800_000_000, 0)
	runtime.markSyncSent(now.Add(-20 * time.Millisecond))
	runtime.noteSyncRTT(now)
	if runtime.heartbeatRttMs < 19 || runtime.heartbeatRttMs > 21 || runtime.heartbeatRttAt != now.UnixNano() || !runtime.syncSentAt.IsZero() {
		t.Fatalf("heartbeat rtt=%v sampled=%d sent=%v", runtime.heartbeatRttMs, runtime.heartbeatRttAt, runtime.syncSentAt)
	}
	runtime.noteSyncRTT(now.Add(time.Second))
	if runtime.heartbeatRttMs < 19 || runtime.heartbeatRttMs > 21 {
		t.Fatalf("empty outstanding send should not overwrite, got %v", runtime.heartbeatRttMs)
	}
}

func TestNoteReplicationRTTRecordsSample(t *testing.T) {
	runtime := &Runtime{}
	now := time.Unix(1_800_000_000, 0)
	runtime.noteReplicationRTT(now.Add(-8*time.Millisecond), now)
	if runtime.replicationRttMs < 7.5 || runtime.replicationRttMs > 8.5 || runtime.replicationRttAt != now.UnixNano() {
		t.Fatalf("replication rtt=%v sampled=%d", runtime.replicationRttMs, runtime.replicationRttAt)
	}
}

func TestNextReplicationRetryBacksOffToTenSeconds(t *testing.T) {
	got := nextReplicationRetry(replicationRetryMin)
	if got != 2*time.Second {
		t.Fatalf("after 1s got %v", got)
	}
	got = nextReplicationRetry(got)
	if got != 5*time.Second {
		t.Fatalf("after 2s got %v", got)
	}
	got = nextReplicationRetry(got)
	if got != replicationRetryMax {
		t.Fatalf("after 5s got %v", got)
	}
	if nextReplicationRetry(replicationRetryMax) != replicationRetryMax {
		t.Fatalf("cap should stay at %v", replicationRetryMax)
	}
}

type ackReplicationStream struct {
	mu      sync.Mutex
	last    *pb.ReplicationBatch
	batches int
}

func (s *ackReplicationStream) Send(batch *pb.ReplicationBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = batch
	s.batches++
	return nil
}

func (s *ackReplicationStream) Recv() (*pb.ReplicationAck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return nil, context.Canceled
	}
	return &pb.ReplicationAck{
		CollectorUuid: s.last.GetCollectorUuid(), ReplicationSession: s.last.GetReplicationSession(),
		BatchSequence: s.last.GetBatchSequence(), CommittedSpoolThroughId: s.last.GetSpoolThroughId(),
	}, nil
}

func TestFlushOutboxDrainsBacklogWithoutIdleWait(t *testing.T) {
	store := openTestStore(t)
	node, session := bytes.Repeat([]byte{0x21}, 16), bytes.Repeat([]byte{0x22}, 16)
	records := make([]*pb.TelemetryRecord, 0, 300)
	for sequence := uint64(1); sequence <= 300; sequence++ {
		records = append(records, &pb.TelemetryRecord{Record: &pb.TelemetryRecord_Event{Event: collectorEvent(t, node, session, sequence)}})
	}
	result, err := store.Ingest(context.Background(), &pb.TelemetryBatch{Records: records}, "collector-drain", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Enqueued != 600 {
		t.Fatalf("enqueued=%d want 600", result.Enqueued)
	}
	runtime := &Runtime{
		store: store, ctx: context.Background(), collectorUUID: "collector-drain",
		replicationSession: bytes.Repeat([]byte{0x23}, 16), nextBatchSequence: 1,
		replicateWake: make(chan struct{}, 1),
	}
	stream := &ackReplicationStream{}
	started := time.Now()
	flushed, err := runtime.flushOutbox(stream)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if flushed < 2 {
		t.Fatalf("flushed=%d want at least 2 batches", flushed)
	}
	if elapsed >= replicationIdleWait {
		t.Fatalf("draining backlog slept %v, want under idle wait %v", elapsed, replicationIdleWait)
	}
	pending, err := store.ReadOutbox(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Through != 0 {
		t.Fatalf("outbox still has spool through %d", pending.Through)
	}
}

func TestMatchIngestCertificateRequiresAgentWhenForced(t *testing.T) {
	runtime := &Runtime{forceAgentIngest: true}
	node := bytes.Repeat([]byte{1}, 16)
	err := runtime.matchIngestCertificate(context.Background(), node, node)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("forced ingest without cert: %v", err)
	}
	runtime.forceAgentIngest = false
	if err := runtime.matchIngestCertificate(context.Background(), node, node); err != nil {
		t.Fatal(err)
	}
}

func TestProbeRuntimeOmitsOutboxStats(t *testing.T) {
	store := openTestStore(t)
	if err := store.RecordHealth(context.Background(), &pb.ObserverHealthSample{
		ObserverId: "probe-a", SampledAtUnixNano: time.Now().UnixNano(), Healthy: true, ProcessSession: "sess",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{store: store, ctx: context.Background(), collectorUUID: "probe-a", kind: model.CollectorKindProbe}
	snap := runtime.runtimeSnapshot()
	if snap.GetPendingRecords() != 0 || snap.GetSpoolSize() != 0 || snap.GetOldestPendingUnixNano() != 0 || snap.GetConnectedAgents() != 0 {
		t.Fatalf("probe snapshot pending=%d spool=%d oldest=%d agents=%d", snap.GetPendingRecords(), snap.GetSpoolSize(), snap.GetOldestPendingUnixNano(), snap.GetConnectedAgents())
	}
	runtime.discardProbeOutbox()
	stats, err := store.RuntimeStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Pending != 0 || stats.SpoolBytes != 0 {
		t.Fatalf("discard left pending=%d spool=%d", stats.Pending, stats.SpoolBytes)
	}
}
