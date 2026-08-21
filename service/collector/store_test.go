package collector

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/telemetry"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "collector.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close collector store: %v", err)
		}
	})
	return store
}

func closeTestGorm(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := closeGormDB(db); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
}

func collectorEvent(t *testing.T, node, session []byte, sequence uint64) *pb.TelemetryEvent {
	t.Helper()
	eventID, err := telemetry.EventID(node, session, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return &pb.TelemetryEvent{
		EventId: eventID, NodeUuid: node, SessionId: session, Sequence: sequence,
		EventType:           pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_HEARTBEAT,
		Priority:            pb.TelemetryPriority_TELEMETRY_PRIORITY_P0_CRITICAL,
		CollectedAtUnixNano: time.Now().UnixNano(),
		SourceProtocol:      pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2,
		Reliability:         pb.Reliability_RELIABILITY_RELIABLE_REPLAY,
		Payload:             &pb.TelemetryEvent_Heartbeat{Heartbeat: &pb.HeartbeatPayload{}},
	}
}

func TestCollectorIngestCommitsFactsOutboxAndCursorTogether(t *testing.T) {
	store := openTestStore(t)
	node, session := bytes.Repeat([]byte{1}, 16), bytes.Repeat([]byte{2}, 16)
	batch := &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{
		{Record: &pb.TelemetryRecord_Event{Event: collectorEvent(t, node, session, 1)}},
		{Record: &pb.TelemetryRecord_Gap{Gap: &pb.SequenceGap{
			GapId: bytes.Repeat([]byte{3}, 16), NodeUuid: node, SessionId: session,
			StartSequence: 2, EndSequence: 3, Reason: pb.GapReason_GAP_REASON_COMPACTED,
		}}},
		{Record: &pb.TelemetryRecord_Event{Event: collectorEvent(t, node, session, 4)}},
	}}
	for index := 0; index < 2; index++ {
		result, err := store.Ingest(context.Background(), batch, "collector-a", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Acks) != 1 || result.Acks[0].GetAckThrough() != 4 {
			t.Fatalf("acks=%#v", result.Acks)
		}
		if index == 0 && result.Enqueued != 5 {
			t.Fatalf("first ingest enqueued=%d want 5", result.Enqueued)
		}
		if index == 1 && result.Enqueued != 0 {
			t.Fatalf("duplicate ingest enqueued=%d want 0", result.Enqueued)
		}
	}
	var events, observations, gaps, outbox int64
	store.db.Model(&model.CollectorStoredEvent{}).Count(&events)
	store.db.Model(&model.CollectorStoredObservation{}).Count(&observations)
	store.db.Model(&model.CollectorStoredGap{}).Count(&gaps)
	store.db.Model(&model.CollectorOutbox{}).Count(&outbox)
	if events != 2 || observations != 2 || gaps != 1 || outbox != 5 {
		t.Fatalf("events=%d observations=%d gaps=%d outbox=%d", events, observations, gaps, outbox)
	}
	spool, err := store.ReadOutbox(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(spool.Events) != 2 || len(spool.Observations) != 2 || len(spool.Gaps) != 1 || spool.Through == 0 {
		t.Fatalf("spool=%#v", spool)
	}
	if err := store.CommitReplicationAck(context.Background(), spool.Through); err != nil {
		t.Fatal(err)
	}
	store.db.Model(&model.CollectorOutbox{}).Count(&outbox)
	if outbox != 0 {
		t.Fatalf("outbox after ack=%d", outbox)
	}
	store.db.Model(&model.CollectorStoredEvent{}).Count(&events)
	store.db.Model(&model.CollectorStoredObservation{}).Count(&observations)
	store.db.Model(&model.CollectorStoredGap{}).Count(&gaps)
	if events != 0 || observations != 0 || gaps != 0 {
		t.Fatalf("acknowledged local facts not cleaned: events=%d observations=%d gaps=%d", events, observations, gaps)
	}
}

func TestCollectorAuthorizationCacheHonorsAssignmentAndRevocation(t *testing.T) {
	store := openTestStore(t)
	node := bytes.Repeat([]byte{9}, 16)
	now := time.Now()
	config := &pb.CollectorAuthorizationConfig{
		ConfigVersion: 3, PrimaryPublicKey: bytes.Repeat([]byte{4}, 32), KeyId: bytes.Repeat([]byte{5}, 16),
		Assignments: []*pb.NodeAssignment{{NodeUuid: node, ObserverId: "collector-a", ValidFromUnixNano: now.Add(-time.Minute).UnixNano(), Generation: 1, ConfigVersion: 3}},
	}
	if err := store.SaveAuthorization(context.Background(), "collector-a", config, now); err != nil {
		t.Fatal(err)
	}
	if allowed, err := store.IsNodeAuthorized(context.Background(), node, now); err != nil || !allowed {
		t.Fatalf("allowed=%t err=%v", allowed, err)
	}
	config.RevokedNodeUuids = [][]byte{node}
	if err := store.SaveAuthorization(context.Background(), "collector-a", config, now); err != nil {
		t.Fatal(err)
	}
	if allowed, err := store.IsNodeAuthorized(context.Background(), node, now); err != nil || allowed {
		t.Fatalf("revoked allowed=%t err=%v", allowed, err)
	}
}

func TestCollectorAuthorizationCachePersistsAgentCA(t *testing.T) {
	store := openTestStore(t)
	now := time.Now()
	pem := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
	config := &pb.CollectorAuthorizationConfig{
		ConfigVersion: 4, PrimaryPublicKey: bytes.Repeat([]byte{4}, 32), KeyId: bytes.Repeat([]byte{5}, 16),
		AgentCaCertificatePem: pem,
	}
	if err := store.SaveAuthorization(context.Background(), "collector-a", config, now); err != nil {
		t.Fatal(err)
	}
	cache, err := store.Authorization(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cache.AgentCACertificatePEM != pem {
		t.Fatalf("agent CA pem=%q", cache.AgentCACertificatePEM)
	}
	config.AgentCaCertificatePem = ""
	if err := store.SaveAuthorization(context.Background(), "collector-a", config, now); err != nil {
		t.Fatal(err)
	}
	cache, err = store.Authorization(context.Background())
	if err != nil || cache.AgentCACertificatePEM != pem {
		t.Fatalf("empty update wiped agent CA: %#v err=%v", cache, err)
	}
}

func TestCollectorHardLimitCreatesReplicableGapAndDataLoss(t *testing.T) {
	store := openTestStore(t)
	node, session := bytes.Repeat([]byte{6}, 16), bytes.Repeat([]byte{7}, 16)
	batch := &pb.TelemetryBatch{Records: []*pb.TelemetryRecord{{
		Record: &pb.TelemetryRecord_Event{Event: collectorEvent(t, node, session, 1)},
	}}}
	if _, err := store.Ingest(context.Background(), batch, "collector-a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.EnforceSpoolPolicy(context.Background(), "collector-a", 1, 30*24*time.Hour, time.Now()); err != nil {
		t.Fatal(err)
	}
	outbox, err := store.ReadOutbox(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox.DataLoss) != 1 || len(outbox.Gaps) == 0 {
		t.Fatalf("data_loss=%d gaps=%d", len(outbox.DataLoss), len(outbox.Gaps))
	}
	if outbox.Gaps[0].GetReason() != pb.GapReason_GAP_REASON_HARD_LIMIT_DATA_LOSS {
		t.Fatalf("gap reason=%s", outbox.Gaps[0].GetReason())
	}
}

func TestCollectorRejectsUnversionedExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collector.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE existing_data (id INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	closeTestGorm(t, db)
	if _, err := OpenStore(path, false); err == nil || !strings.Contains(err.Error(), "without collector_schema_migrations") {
		t.Fatalf("expected unversioned database rejection, got %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("rejected open left the sqlite file locked: %v", err)
	}
}

func TestSaveAuthorizationStoresMTRProbes(t *testing.T) {
	store := openTestStore(t)
	now := time.Now()
	config := &pb.CollectorAuthorizationConfig{
		ConfigVersion: 1, PrimaryPublicKey: bytes.Repeat([]byte{4}, 32), KeyId: bytes.Repeat([]byte{5}, 16),
		Kind:  pb.CollectorKind_COLLECTOR_KIND_PROBE,
		Probe: &pb.ProbeConfig{MtrProbes: 8},
		Targets: []*pb.ProbeTarget{
			{ServerId: 1, ServerName: "explicit", Ipv4: "192.0.2.1", EnableIcmp: true, EnableMtr: true, MtrProbes: 10},
			{ServerId: 2, ServerName: "fallback", Ipv4: "192.0.2.2", EnableIcmp: true, EnableMtr: true},
		},
	}
	if err := store.SaveAuthorization(context.Background(), "collector-a", config, now); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ProbeTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uint64]model.CollectorCachedProbeTarget{}
	for _, row := range rows {
		byID[row.ServerID] = row
	}
	if got := byID[1]; got.MTRProbes != 10 {
		t.Fatalf("explicit probes=%d", got.MTRProbes)
	}
	if got := byID[2]; got.MTRProbes != 8 {
		t.Fatalf("config fallback probes=%d", got.MTRProbes)
	}
}

func TestMigrateV7AddsCachedMTRProbes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v6.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestGorm(t, db) })
	if err := db.AutoMigrate(&model.CollectorSchemaMigration{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE collector_cached_probe_targets (
		server_id INTEGER PRIMARY KEY,
		server_name TEXT,
		ipv4 TEXT,
		ipv6 TEXT,
		hostname TEXT,
		tcp_ports TEXT,
		enable_icmp INTEGER,
		enable_tcp INTEGER,
		enable_mtr INTEGER,
		enable_ipv4 INTEGER,
		enable_ipv6 INTEGER,
		interval_sec INTEGER,
		mtr_interval_sec INTEGER,
		route_interval_sec INTEGER,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorSchemaMigration{Version: 6, AppliedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasColumn(&model.CollectorCachedProbeTarget{}, "mtr_probes") {
		t.Fatal("fixture should omit mtr_probes")
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn(&model.CollectorCachedProbeTarget{}, "mtr_probes") {
		t.Fatal("v7 should add mtr_probes")
	}
	var version uint64
	if err := db.Model(&model.CollectorSchemaMigration{}).Select("COALESCE(MAX(version), 0)").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version != 7 {
		t.Fatalf("version = %d", version)
	}
}
