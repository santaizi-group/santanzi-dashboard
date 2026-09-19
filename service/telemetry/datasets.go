package telemetry

import (
	"encoding/json"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"gorm.io/gorm"
)

func normalizePage(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = 20
	}
	return offset, limit
}

func listPage[T any](db *gorm.DB, order string, offset, limit int) ([]T, int64, error) {
	offset, limit = normalizePage(offset, limit)
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []T
	if err := db.Order(order).Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	if rows == nil {
		rows = []T{}
	}
	return rows, total, nil
}

type ObserverEvidenceItem struct {
	ObserverID   string `json:"observer_id"`
	ObserverKind string `json:"observer_kind,omitempty"`
	ObserverName string `json:"observer_name,omitempty"`
	Healthy      bool   `json:"healthy"`
	Seen         bool   `json:"seen"`
}

type AgentSink struct {
	EndpointID    string  `json:"endpoint_id"`
	ObserverKind  string  `json:"observer_kind,omitempty"`
	ObserverName  string  `json:"observer_name,omitempty"`
	Connected     bool    `json:"connected"`
	PendingEvents uint64  `json:"pending_events"`
	LastError     string  `json:"last_error,omitempty"`
	AckThrough    uint64  `json:"ack_through"`
	LastRttMs     float64 `json:"last_rtt_ms,omitempty"`
	RttSampledAt  *string `json:"rtt_sampled_at,omitempty"`
}

type ObserverAssignmentRecord struct {
	ServerID      uint64  `json:"server_id,omitempty"`
	ServerName    string  `json:"server_name,omitempty"`
	NodeUUID      string  `json:"node_uuid"`
	ObserverID    string  `json:"observer_id"`
	ObserverKind  string  `json:"observer_kind"`
	ObserverName  string  `json:"observer_name,omitempty"`
	ValidFrom     *string `json:"valid_from"`
	ValidTo       *string `json:"valid_to"`
	Generation    uint64  `json:"generation"`
	ConfigVersion uint64  `json:"config_version"`
}

type AgentReliabilityRecord struct {
	ServerID        uint64      `json:"server_id,omitempty"`
	ServerName      string      `json:"server_name,omitempty"`
	NodeUUID        string      `json:"node_uuid"`
	WalPressure     string      `json:"wal_pressure"`
	WalBytes        uint64      `json:"wal_bytes"`
	PendingEvents   uint64      `json:"pending_events"`
	OldestPending   *string     `json:"oldest_pending"`
	ClockUntrusted  bool        `json:"clock_untrusted"`
	ProtocolVersion string      `json:"protocol_version,omitempty"`
	AgentVersion    string      `json:"agent_version,omitempty"`
	UpdatedAt       *string     `json:"updated_at"`
	Sinks           []AgentSink `json:"sinks"`
}

type IncidentRecord struct {
	ID                    uint64                 `json:"id"`
	ServerID              uint64                 `json:"server_id,omitempty"`
	ServerName            string                 `json:"server_name,omitempty"`
	NodeUUID              string                 `json:"node_uuid"`
	InitialClassification string                 `json:"initial_classification,omitempty"`
	CurrentClassification string                 `json:"current_classification"`
	Revision              uint64                 `json:"revision"`
	StartedAt             *string                `json:"started_at"`
	EndedAt               *string                `json:"ended_at"`
	RecalculatedAt        *string                `json:"recalculated_at"`
	Reason                string                 `json:"reason,omitempty"`
	ObserverEvidence      []ObserverEvidenceItem `json:"observer_evidence"`
}

type IncidentRevisionRecord struct {
	ID               uint64                 `json:"id"`
	IncidentID       uint64                 `json:"incident_id"`
	Revision         uint64                 `json:"revision"`
	Classification   string                 `json:"classification"`
	Reason           string                 `json:"reason,omitempty"`
	RecalculatedAt   *string                `json:"recalculated_at"`
	CreatedAt        *string                `json:"created_at"`
	ObserverEvidence []ObserverEvidenceItem `json:"observer_evidence"`
}

type DataLossRecord struct {
	FactID       string  `json:"fact_id"`
	ComponentID  string  `json:"component_id,omitempty"`
	OccurredAt   *string `json:"occurred_at"`
	Reason       string  `json:"reason"`
	FirstSpoolID uint64  `json:"first_spool_id,omitempty"`
	LastSpoolID  uint64  `json:"last_spool_id,omitempty"`
	LostRecords  uint64  `json:"lost_records"`
	Detail       string  `json:"detail,omitempty"`
	CreatedAt    *string `json:"created_at"`
}

type AlertRecord struct {
	ID          uint64  `json:"id"`
	AlertType   string  `json:"alert_type"`
	Severity    string  `json:"severity,omitempty"`
	ServerID    uint64  `json:"server_id,omitempty"`
	ServerName  string  `json:"server_name,omitempty"`
	NodeUUID    string  `json:"node_uuid,omitempty"`
	ComponentID string  `json:"component_id,omitempty"`
	OccurredAt  *string `json:"occurred_at"`
	CreatedAt   *string `json:"created_at"`
	Notified    bool    `json:"notified"`
	Message     string  `json:"message,omitempty"`
	DedupKey    string  `json:"dedup_key,omitempty"`
}

func ListObserverAssignments(db *gorm.DB, offset, limit int) ([]ObserverAssignmentRecord, int64, error) {
	out := []ObserverAssignmentRecord{}
	rows, total, err := listPage[model.ObserverAssignment](db.Model(&model.ObserverAssignment{}).Where("valid_to = ?", 0), "valid_from DESC", offset, limit)
	if err != nil {
		return nil, 0, err
	}
	nodeIDs := uniqueBytes(rows, func(row model.ObserverAssignment) []byte { return row.NodeUUID })
	observerIDs := uniqueStrings(rows, func(row model.ObserverAssignment) string { return row.ObserverID })
	idx, err := loadHostIndex(db, nodeIDs, observerIDs)
	if err != nil {
		return nil, 0, err
	}
	for _, row := range rows {
		serverID, serverName := idx.host(row.NodeUUID)
		kind, name := idx.observer(row.ObserverID)
		out = append(out, ObserverAssignmentRecord{
			ServerID: serverID, ServerName: serverName, NodeUUID: HexUUID(row.NodeUUID),
			ObserverID: row.ObserverID, ObserverKind: kind, ObserverName: name,
			ValidFrom: RFC3339NanoPtr(row.ValidFrom), ValidTo: RFC3339NanoPtr(row.ValidTo),
			Generation: row.Generation, ConfigVersion: row.ConfigVersion,
		})
	}
	return out, total, nil
}

func ListAgentReliability(db *gorm.DB, offset, limit int) ([]AgentReliabilityRecord, int64, error) {
	out := []AgentReliabilityRecord{}
	rows, total, err := listPage[model.AgentTelemetryRuntime](db.Model(&model.AgentTelemetryRuntime{}), "updated_at DESC", offset, limit)
	if err != nil {
		return nil, 0, err
	}
	nodeIDs := make([][]byte, 0, len(rows))
	observerIDs := make([]string, 0)
	decoded := make([]*pb.AgentRuntime, len(rows))
	for i, row := range rows {
		nodeIDs = append(nodeIDs, row.NodeUUID)
		decoded[i] = DecodeAgentRuntime(row.SinkCursors)
		if decoded[i] == nil {
			continue
		}
		for _, sink := range decoded[i].GetSinks() {
			observerIDs = append(observerIDs, sink.GetEndpointId())
		}
	}
	idx, err := loadHostIndex(db, uniqueByteSlices(nodeIDs), observerIDs)
	if err != nil {
		return nil, 0, err
	}
	for i, row := range rows {
		serverID, serverName := idx.host(row.NodeUUID)
		record := AgentReliabilityRecord{
			ServerID: serverID, ServerName: serverName, NodeUUID: HexUUID(row.NodeUUID),
			WalPressure: walPressureLabel(row.WalPressure), WalBytes: row.WalBytes,
			PendingEvents: row.PendingEvents, OldestPending: RFC3339NanoPtr(row.OldestPending),
			ClockUntrusted: row.ClockUntrusted, ProtocolVersion: row.ProtocolVersion,
			AgentVersion: row.AgentVersion, UpdatedAt: RFC3339TimePtr(row.UpdatedAt), Sinks: []AgentSink{},
		}
		if agent := decoded[i]; agent != nil {
			for _, sink := range agent.GetSinks() {
				kind, name := idx.observer(sink.GetEndpointId())
				record.Sinks = append(record.Sinks, AgentSink{
					EndpointID: sink.GetEndpointId(), ObserverKind: kind, ObserverName: name,
					Connected: sink.GetConnected(), PendingEvents: sink.GetPendingEvents(),
					LastError: sink.GetLastError(), AckThrough: sink.GetAckThrough(),
					LastRttMs: sink.GetLastRttMs(), RttSampledAt: RFC3339NanoPtr(sink.GetRttSampledAtUnixNano()),
				})
			}
		}
		out = append(out, record)
	}
	return out, total, nil
}

func ListIncidents(db *gorm.DB, offset, limit int) ([]IncidentRecord, int64, error) {
	out := []IncidentRecord{}
	rows, total, err := listPage[model.AvailabilityIncident](db.Model(&model.AvailabilityIncident{}), "started_at DESC", offset, limit)
	if err != nil {
		return nil, 0, err
	}
	nodeIDs := make([][]byte, 0, len(rows))
	observerIDs := make([]string, 0)
	evidence := make([][]ObserverEvidenceItem, len(rows))
	for i, row := range rows {
		nodeIDs = append(nodeIDs, row.NodeUUID)
		raw := decodeEvidence(row.ObserverEvidence)
		evidence[i] = raw
		for _, item := range raw {
			observerIDs = append(observerIDs, item.ObserverID)
		}
	}
	idx, err := loadHostIndex(db, uniqueByteSlices(nodeIDs), observerIDs)
	if err != nil {
		return nil, 0, err
	}
	for i, row := range rows {
		serverID, serverName := idx.host(row.NodeUUID)
		out = append(out, IncidentRecord{
			ID: row.ID, ServerID: serverID, ServerName: serverName, NodeUUID: HexUUID(row.NodeUUID),
			InitialClassification: NormalizeLabel(row.InitialClassification),
			CurrentClassification: NormalizeLabel(row.CurrentClassification),
			Revision:              row.Revision, StartedAt: RFC3339NanoPtr(row.StartedAt), EndedAt: RFC3339NanoPtr(row.EndedAt),
			RecalculatedAt: RFC3339NanoPtr(row.RecalculatedAt), Reason: NormalizeLabel(row.Reason),
			ObserverEvidence: annotateEvidence(evidence[i], idx),
		})
	}
	return out, total, nil
}

func ListIncidentRevisions(db *gorm.DB, offset, limit int) ([]IncidentRevisionRecord, int64, error) {
	out := []IncidentRevisionRecord{}
	rows, total, err := listPage[model.IncidentRevision](db.Model(&model.IncidentRevision{}), "created_at DESC", offset, limit)
	if err != nil {
		return nil, 0, err
	}
	observerIDs := make([]string, 0)
	evidence := make([][]ObserverEvidenceItem, len(rows))
	for i, row := range rows {
		raw := decodeEvidence(row.Evidence)
		evidence[i] = raw
		for _, item := range raw {
			observerIDs = append(observerIDs, item.ObserverID)
		}
	}
	idx, err := loadHostIndex(db, nil, observerIDs)
	if err != nil {
		return nil, 0, err
	}
	for i, row := range rows {
		out = append(out, IncidentRevisionRecord{
			ID: row.ID, IncidentID: row.IncidentID, Revision: row.Revision,
			Classification: NormalizeLabel(row.Classification), Reason: NormalizeLabel(row.Reason),
			RecalculatedAt: RFC3339NanoPtr(row.RecalculatedAt), CreatedAt: RFC3339TimePtr(row.CreatedAt),
			ObserverEvidence: annotateEvidence(evidence[i], idx),
		})
	}
	return out, total, nil
}

func ListDataLoss(db *gorm.DB, offset, limit int) ([]DataLossRecord, int64, error) {
	out := []DataLossRecord{}
	rows, total, err := listPage[model.TelemetryDataLoss](db.Model(&model.TelemetryDataLoss{}), "occurred_at DESC", offset, limit)
	if err != nil {
		return nil, 0, err
	}
	for _, row := range rows {
		out = append(out, DataLossRecord{
			FactID: HexUUID(row.FactID), ComponentID: row.ComponentID, OccurredAt: RFC3339NanoPtr(row.OccurredAt),
			Reason: gapReasonLabel(row.Reason), FirstSpoolID: row.FirstSpoolID, LastSpoolID: row.LastSpoolID,
			LostRecords: row.LostRecords, Detail: row.Detail, CreatedAt: RFC3339TimePtr(row.CreatedAt),
		})
	}
	return out, total, nil
}

func ListAlerts(db *gorm.DB, offset, limit int) ([]AlertRecord, int64, error) {
	out := []AlertRecord{}
	rows, total, err := listPage[model.TelemetryAlert](db.Model(&model.TelemetryAlert{}), "occurred_at DESC", offset, limit)
	if err != nil {
		return nil, 0, err
	}
	nodeIDs := make([][]byte, 0, len(rows))
	observerIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row.NodeUUID) > 0 {
			nodeIDs = append(nodeIDs, row.NodeUUID)
		}
		if row.ComponentID != "" {
			observerIDs = append(observerIDs, row.ComponentID)
		}
	}
	idx, err := loadHostIndex(db, uniqueByteSlices(nodeIDs), observerIDs)
	if err != nil {
		return nil, 0, err
	}
	for _, row := range rows {
		serverID, serverName := idx.host(row.NodeUUID)
		out = append(out, AlertRecord{
			ID: row.ID, AlertType: NormalizeLabel(row.AlertType), Severity: NormalizeLabel(row.Severity),
			ServerID: serverID, ServerName: serverName, NodeUUID: HexUUID(row.NodeUUID),
			ComponentID: row.ComponentID, OccurredAt: RFC3339NanoPtr(row.OccurredAt),
			CreatedAt: RFC3339TimePtr(row.CreatedAt), Notified: row.Notified,
			Message: row.Message, DedupKey: row.DedupKey,
		})
	}
	return out, total, nil
}

func walPressureLabel(value int32) string {
	switch pb.WalPressure(value) {
	case pb.WalPressure_WAL_PRESSURE_HEALTHY:
		return "healthy"
	case pb.WalPressure_WAL_PRESSURE_P3_DOWNSAMPLED:
		return "downsampled"
	case pb.WalPressure_WAL_PRESSURE_ROLLUP:
		return "rollup"
	case pb.WalPressure_WAL_PRESSURE_CRITICAL:
		return "critical"
	case pb.WalPressure_WAL_PRESSURE_DATA_LOSS:
		return "telemetry_data_loss"
	default:
		return "unknown"
	}
}

func gapReasonLabel(value int32) string {
	switch pb.GapReason(value) {
	case pb.GapReason_GAP_REASON_COMPACTED:
		return "compacted"
	case pb.GapReason_GAP_REASON_HARD_LIMIT_DATA_LOSS:
		return "hard_limit_data_loss"
	case pb.GapReason_GAP_REASON_CORRUPTION:
		return "corruption"
	default:
		return "unknown"
	}
}

type rawEvidence struct {
	ObserverID string `json:"observer_id"`
	Healthy    bool   `json:"healthy"`
	Seen       bool   `json:"seen"`
}

func decodeEvidence(blob []byte) []ObserverEvidenceItem {
	if len(blob) == 0 {
		return []ObserverEvidenceItem{}
	}
	var raw []rawEvidence
	if json.Unmarshal(blob, &raw) != nil {
		return []ObserverEvidenceItem{}
	}
	out := make([]ObserverEvidenceItem, 0, len(raw))
	for _, item := range raw {
		out = append(out, ObserverEvidenceItem{
			ObserverID: item.ObserverID, Healthy: item.Healthy, Seen: item.Seen,
		})
	}
	return out
}

func annotateEvidence(items []ObserverEvidenceItem, idx hostIndex) []ObserverEvidenceItem {
	if items == nil {
		return []ObserverEvidenceItem{}
	}
	out := make([]ObserverEvidenceItem, 0, len(items))
	for _, item := range items {
		kind, name := idx.observer(item.ObserverID)
		item.ObserverKind = kind
		item.ObserverName = name
		out = append(out, item)
	}
	return out
}

func AnnotateObserverEvidence(db *gorm.DB, blobs [][]byte) ([][]ObserverEvidenceItem, error) {
	out := make([][]ObserverEvidenceItem, len(blobs))
	if len(blobs) == 0 {
		return out, nil
	}
	decoded := make([][]ObserverEvidenceItem, len(blobs))
	observerIDs := make([]string, 0)
	for i, blob := range blobs {
		raw := decodeEvidence(blob)
		decoded[i] = raw
		for _, item := range raw {
			if item.ObserverID != "" {
				observerIDs = append(observerIDs, item.ObserverID)
			}
		}
	}
	idx, err := loadHostIndex(db, nil, observerIDs)
	if err != nil {
		return nil, err
	}
	for i, items := range decoded {
		out[i] = annotateEvidence(items, idx)
	}
	return out, nil
}
