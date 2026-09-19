package telemetry

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"google.golang.org/protobuf/proto"
)

func TestNormalizeLabel(t *testing.T) {
	if got := NormalizeLabel("CONNECTIVITY_DEGRADED"); got != "connectivity_degraded" {
		t.Fatalf("classification: %s", got)
	}
	if got := NormalizeLabel("availability evidence"); got != "availability_evidence" {
		t.Fatalf("reason: %s", got)
	}
	if got := NormalizeLabel("  late evidence correction "); got != "late_evidence_correction" {
		t.Fatalf("late reason: %s", got)
	}
}

func TestListAgentReliabilityDecodesSinksAndTimes(t *testing.T) {
	db := newConnectionDB(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	node := bytes.Repeat([]byte{0xda, 0xce, 0xe8, 0x92}, 4)
	server := model.Server{Name: "edge-a", Secret: "secret"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ServerNodeBinding{ServerID: server.ID, NodeUUID: node, Current: true, Reason: "test", ValidFrom: now.UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	blob, err := proto.Marshal(&pb.AgentRuntime{Sinks: []*pb.SinkRuntime{
		{EndpointId: PrimaryObserverID, Connected: true, PendingEvents: 2, AckThrough: 11},
		{EndpointId: "collector-east", Connected: false, LastError: "dial timeout", PendingEvents: 4},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentTelemetryRuntime{
		NodeUUID: node, WalPressure: int32(pb.WalPressure_WAL_PRESSURE_HEALTHY), WalBytes: 2048,
		PendingEvents: 6, OldestPending: now.UnixNano(), SinkCursors: blob, ClockUntrusted: true,
		ProtocolVersion: "v2", AgentVersion: "1.0.0-rs", UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	rows, _, err := ListAgentReliability(db, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	row := rows[0]
	if row.ServerName != "edge-a" || row.WalPressure != "healthy" || !row.ClockUntrusted || row.OldestPending == nil || row.AgentVersion != "1.0.0-rs" {
		t.Fatalf("row=%#v json=%s", row, mustJSON(t, row))
	}
	if !strings.Contains(*row.OldestPending, "2023-11-14") {
		t.Fatalf("oldest_pending=%s", *row.OldestPending)
	}
	encoded, _ := json.Marshal(row)
	if bytes.Contains(encoded, []byte("Cgk")) || bytes.Contains(encoded, blob) {
		t.Fatalf("raw blob leaked: %s", encoded)
	}
	if len(row.Sinks) != 2 || !row.Sinks[0].Connected || row.Sinks[0].ObserverKind != ObserverKindPrimary || row.Sinks[1].LastError != "dial timeout" {
		t.Fatalf("sinks=%#v", row.Sinks)
	}
}

func TestListIncidentsDecodesEvidenceAndClassification(t *testing.T) {
	db := newConnectionDB(t)
	if err := db.AutoMigrate(&model.AvailabilityIncident{}, &model.IncidentRevision{}, &model.TelemetryDataLoss{}, &model.TelemetryAlert{}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	node := bytes.Repeat([]byte{0xab}, 16)
	server := model.Server{Name: "edge-b", Secret: "secret"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ServerNodeBinding{ServerID: server.ID, NodeUUID: node, Current: true, Reason: "test", ValidFrom: now.UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	evidence, err := json.Marshal([]rawEvidence{{ObserverID: PrimaryObserverID, Healthy: true, Seen: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AvailabilityIncident{
		NodeUUID: node, InitialClassification: "HOST_OFFLINE", CurrentClassification: "CONNECTIVITY_DEGRADED",
		Revision: 2, StartedAt: now.UnixNano(), EndedAt: 0, RecalculatedAt: now.UnixNano(),
		Reason: "availability evidence", ObserverEvidence: evidence,
	}).Error; err != nil {
		t.Fatal(err)
	}

	rows, _, err := ListIncidents(db, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	row := rows[0]
	if row.ServerName != "edge-b" || row.CurrentClassification != "connectivity_degraded" || row.Reason != "availability_evidence" || row.EndedAt != nil {
		t.Fatalf("row=%#v", row)
	}
	if len(row.ObserverEvidence) != 1 || row.ObserverEvidence[0].ObserverKind != ObserverKindPrimary || !row.ObserverEvidence[0].Seen {
		t.Fatalf("evidence=%#v", row.ObserverEvidence)
	}
	encoded, _ := json.Marshal(row)
	if bytes.Contains(encoded, []byte(`"observer_evidence":"`)) {
		t.Fatalf("raw evidence blob leaked: %s", encoded)
	}
}

func TestListDataLossAndAlertsNormalizeEnums(t *testing.T) {
	db := newConnectionDB(t)
	if err := db.AutoMigrate(&model.TelemetryDataLoss{}, &model.TelemetryAlert{}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	node := bytes.Repeat([]byte{0x11}, 16)
	fact := bytes.Repeat([]byte{0x22}, 16)
	server := model.Server{Name: "edge-c", Secret: "secret"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ServerNodeBinding{ServerID: server.ID, NodeUUID: node, Current: true, Reason: "test", ValidFrom: now.UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelemetryDataLoss{
		FactID: fact, ComponentID: "wal", OccurredAt: now.UnixNano(),
		Reason: int32(pb.GapReason_GAP_REASON_HARD_LIMIT_DATA_LOSS), LostRecords: 8, Detail: "truncated",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelemetryAlert{
		DedupKey: "HOST_OFFLINE/node/1", AlertType: "HOST_OFFLINE", Severity: "warning",
		NodeUUID: node, OccurredAt: now.UnixNano(), Message: "HOST_OFFLINE node incident", Notified: true,
	}).Error; err != nil {
		t.Fatal(err)
	}

	loss, _, err := ListDataLoss(db, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(loss) != 1 || loss[0].Reason != "hard_limit_data_loss" || loss[0].OccurredAt == nil || loss[0].FactID == "" {
		t.Fatalf("loss=%#v", loss)
	}
	alerts, _, err := ListAlerts(db, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].AlertType != "host_offline" || alerts[0].ServerName != "edge-c" || alerts[0].Message == "" {
		t.Fatalf("alerts=%#v", alerts)
	}
}

func TestListObserverAssignmentsMapsHostAndOngoing(t *testing.T) {
	db := newConnectionDB(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	node := bytes.Repeat([]byte{0x09}, 16)
	server := model.Server{Name: "edge-a", Secret: "secret"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ServerNodeBinding{ServerID: server.ID, NodeUUID: node, Current: true, Reason: "test", ValidFrom: now.UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ObserverAssignment{
		NodeUUID: node, ObserverID: PrimaryObserverID, ValidFrom: now.UnixNano(), ValidTo: 0, ConfigVersion: 3, Generation: 2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	rows, _, err := ListObserverAssignments(db, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ServerName != "edge-a" || rows[0].ObserverKind != ObserverKindPrimary || rows[0].ValidTo != nil || rows[0].ValidFrom == nil {
		t.Fatalf("rows=%#v", rows)
	}
}

func TestListIncidentRevisionsPaginates(t *testing.T) {
	db := newConnectionDB(t)
	if err := db.AutoMigrate(&model.IncidentRevision{}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i < 25; i++ {
		if err := db.Create(&model.IncidentRevision{
			IncidentID: uint64(i + 1), Revision: 1, Classification: "HOST_OFFLINE",
			RecalculatedAt: now.UnixNano(), CreatedAt: now.Add(time.Duration(i) * time.Second),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	first, total, err := ListIncidentRevisions(db, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 25 || len(first) != 10 {
		t.Fatalf("first page: len=%d total=%d", len(first), total)
	}
	if first[0].IncidentID != 25 || first[9].IncidentID != 16 {
		t.Fatalf("first page order: %#v", first)
	}
	second, total, err := ListIncidentRevisions(db, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 25 || len(second) != 10 || second[0].IncidentID != 15 || second[9].IncidentID != 6 {
		t.Fatalf("second page: len=%d total=%d first=%d last=%d", len(second), total, second[0].IncidentID, second[9].IncidentID)
	}
}

func TestAnnotateObserverEvidenceResolvesCollectorNames(t *testing.T) {
	db := newConnectionDB(t)
	createCollector(t, db, "collector-east", "East")
	blob, err := json.Marshal([]rawEvidence{
		{ObserverID: PrimaryObserverID, Healthy: true, Seen: true},
		{ObserverID: "collector-east", Healthy: true, Seen: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := AnnotateObserverEvidence(db, [][]byte{blob, nil})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if len(rows[0]) != 2 || rows[0][0].ObserverKind != ObserverKindPrimary || rows[0][1].ObserverName != "East" || rows[0][1].ObserverKind != ObserverKindCollector {
		t.Fatalf("evidence=%#v", rows[0])
	}
	if len(rows[1]) != 0 {
		t.Fatalf("empty blob=%#v", rows[1])
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
