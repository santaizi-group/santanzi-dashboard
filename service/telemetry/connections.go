package telemetry

import (
	"encoding/hex"
	"strings"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PrimaryObserverID = "primary"
	CollectorTimeout  = 90 * time.Second
)

const (
	CollectorStatusOnline  = "online"
	CollectorStatusOffline = "offline"
	CollectorStatusUnknown = "unknown"
	ObserverKindPrimary    = "primary"
	ObserverKindCollector  = "collector"
)

func CollectorStatus(lastSeen int64, now time.Time) string {
	if lastSeen <= 0 {
		return CollectorStatusUnknown
	}
	if now.UnixNano()-lastSeen <= int64(CollectorTimeout) {
		return CollectorStatusOnline
	}
	return CollectorStatusOffline
}

func CollectorRuntimeFromProto(collectorUUID string, runtime *pb.CollectorRuntime, receivedAt time.Time, includeLastSync bool) model.CollectorRuntime {
	row := model.CollectorRuntime{
		CollectorUUID: collectorUUID, Status: CollectorStatusOnline, LastSeen: receivedAt.UnixNano(),
		SpoolSize: runtime.GetSpoolSize(), PendingRecords: runtime.GetPendingRecords(), OldestPending: runtime.GetOldestPendingUnixNano(),
		ReplicationCursor: runtime.GetReplicationCursor(), ConnectedAgents: runtime.GetConnectedAgents(),
		ProtocolVersion: runtime.GetProtocolVersion(), SoftwareVersion: runtime.GetSoftwareVersion(),
		LastPrimarySeen: runtime.GetLastPrimarySeenUnixNano(),
		HeartbeatRttMs:  runtime.GetHeartbeatRttMs(), HeartbeatRttSampledAt: runtime.GetHeartbeatRttSampledAtUnixNano(),
		ReplicationRttMs: runtime.GetReplicationRttMs(), ReplicationRttSampledAt: runtime.GetReplicationRttSampledAtUnixNano(),
	}
	if includeLastSync {
		row.LastSync = receivedAt.UnixNano()
	}
	return row
}

func UpsertCollectorRuntime(db *gorm.DB, row model.CollectorRuntime, includeLastSync bool) error {
	columns := []string{
		"status", "last_seen", "spool_size", "pending_records", "oldest_pending", "replication_cursor",
		"connected_agents", "protocol_version", "last_primary_seen", "heartbeat_rtt_ms", "heartbeat_rtt_sampled_at",
		"replication_rtt_ms", "replication_rtt_sampled_at", "updated_at",
	}
	if includeLastSync {
		columns = append(columns, "last_sync")
	}
	if strings.TrimSpace(row.SoftwareVersion) != "" {
		columns = append(columns, "software_version")
	}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "collector_uuid"}},
		DoUpdates: clause.AssignmentColumns(columns),
	}).Create(&row).Error; err != nil {
		return err
	}
	return RecordCollectorLatency(db, row)
}

type ConnectionSummary struct {
	CollectorsTotal   int64
	CollectorsOnline  int64
	CollectorsOffline int64
	CollectorsUnknown int64
	PathsAssigned     int64
	PathsConnected    int64
	PathsSeen         int64
}

type PathSink struct {
	Connected     bool
	PendingEvents uint64
	LastError     string
	AckThrough    uint64
	LastRttMs     float64
	RttSampledAt  int64
}

type ConnectionPath struct {
	ServerID     uint64
	ServerName   string
	DisplayIndex int
	Tag          string
	NodeUUID     string
	ObserverID   string
	ObserverKind string
	ObserverName string
	Assigned     bool
	LastSeen     int64
	Sink         PathSink
}

type PathFilter struct {
	ServerID   uint64
	ObserverID string
}

func LoadConnectionSummary(db *gorm.DB, now time.Time) (ConnectionSummary, error) {
	var summary ConnectionSummary
	var collectors []model.Collector
	if err := db.Where("deleted = ? AND revoked = ? AND kind != ?", false, false, model.CollectorKindProbe).Find(&collectors).Error; err != nil {
		return summary, err
	}
	summary.CollectorsTotal = int64(len(collectors))
	if len(collectors) > 0 {
		ids := make([]string, 0, len(collectors))
		for _, collector := range collectors {
			ids = append(ids, collector.CollectorUUID)
		}
		var runtimes []model.CollectorRuntime
		if err := db.Where("collector_uuid IN ?", ids).Find(&runtimes).Error; err != nil {
			return summary, err
		}
		byID := map[string]int64{}
		for _, runtime := range runtimes {
			byID[runtime.CollectorUUID] = runtime.LastSeen
		}
		for _, collector := range collectors {
			switch CollectorStatus(byID[collector.CollectorUUID], now) {
			case CollectorStatusOnline:
				summary.CollectorsOnline++
			case CollectorStatusOffline:
				summary.CollectorsOffline++
			default:
				summary.CollectorsUnknown++
			}
		}
	}
	paths, err := loadConnectionPaths(db, PathFilter{}, now)
	if err != nil {
		return summary, err
	}
	summary.PathsAssigned = int64(len(paths))
	for _, path := range paths {
		if path.Sink.Connected {
			summary.PathsConnected++
		}
		if path.LastSeen > 0 {
			summary.PathsSeen++
		}
	}
	return summary, nil
}

func LoadConnectionPaths(db *gorm.DB, filter PathFilter) ([]ConnectionPath, error) {
	return loadConnectionPaths(db, filter, time.Now())
}

func loadConnectionPaths(db *gorm.DB, filter PathFilter, now time.Time) ([]ConnectionPath, error) {
	query := db.Where("valid_to = ?", 0)
	if filter.ServerID > 0 {
		var binding model.ServerNodeBinding
		if err := db.Where("server_id = ? AND current = ?", filter.ServerID, true).Limit(1).Find(&binding).Error; err != nil {
			return nil, err
		}
		if binding.ServerID == 0 {
			return []ConnectionPath{}, nil
		}
		query = query.Where("node_uuid = ?", binding.NodeUUID)
	}
	if filter.ObserverID != "" {
		query = query.Where("observer_id = ?", filter.ObserverID)
	}
	var assignments []model.ObserverAssignment
	if err := query.Find(&assignments).Error; err != nil {
		return nil, err
	}
	if len(assignments) == 0 {
		return []ConnectionPath{}, nil
	}

	nodeIDs := uniqueBytes(assignments, func(row model.ObserverAssignment) []byte { return row.NodeUUID })
	observerIDs := uniqueStrings(assignments, func(row model.ObserverAssignment) string { return row.ObserverID })
	idx, err := loadHostIndex(db, nodeIDs, observerIDs)
	if err != nil {
		return nil, err
	}

	type pathLastSeen struct {
		NodeUUID   []byte
		ObserverID string
		LastSeenAt int64
	}
	var lastSeenRows []pathLastSeen
	if err := db.Model(&model.ObserverPathBucket{}).
		Select("node_uuid, observer_id, MAX(last_seen_at) as last_seen_at").
		Where("seen = ? AND node_uuid IN ? AND observer_id IN ?", true, nodeIDs, observerIDs).
		Group("node_uuid, observer_id").Scan(&lastSeenRows).Error; err != nil {
		return nil, err
	}
	lastSeen := map[string]int64{}
	for _, row := range lastSeenRows {
		lastSeen[pathKey(row.NodeUUID, row.ObserverID)] = row.LastSeenAt
	}

	var runtimes []model.AgentTelemetryRuntime
	if err := db.Where("node_uuid IN ?", nodeIDs).Find(&runtimes).Error; err != nil {
		return nil, err
	}
	sinks := map[string]PathSink{}
	for _, runtime := range runtimes {
		agent := DecodeAgentRuntime(runtime.SinkCursors)
		if agent == nil {
			continue
		}
		for _, sink := range agent.GetSinks() {
			sinks[pathKey(runtime.NodeUUID, sink.GetEndpointId())] = PathSink{
				Connected: sink.GetConnected(), PendingEvents: sink.GetPendingEvents(),
				LastError: sink.GetLastError(), AckThrough: sink.GetAckThrough(),
				LastRttMs: sink.GetLastRttMs(), RttSampledAt: sink.GetRttSampledAtUnixNano(),
			}
		}
	}

	observerLastSeen, err := collectorLastSeenByID(db, observerIDs)
	if err != nil {
		return nil, err
	}

	paths := make([]ConnectionPath, 0, len(assignments))
	for _, assignment := range assignments {
		if idx.isProbeObserver(assignment.ObserverID) {
			continue
		}
		server := idx.lookupServer(assignment.NodeUUID)
		kind, name := idx.observer(assignment.ObserverID)
		sink := sinks[pathKey(assignment.NodeUUID, assignment.ObserverID)]
		if !sinkHandshaked(assignment.ObserverID, sink, observerLastSeen, now) {
			clearUnhandshakedSink(&sink)
		}
		paths = append(paths, ConnectionPath{
			ServerID: server.ID, ServerName: server.Name, DisplayIndex: server.DisplayIndex, Tag: server.Tag,
			NodeUUID:   hex.EncodeToString(assignment.NodeUUID),
			ObserverID: assignment.ObserverID, ObserverKind: kind, ObserverName: name, Assigned: true,
			LastSeen: lastSeen[pathKey(assignment.NodeUUID, assignment.ObserverID)],
			Sink:     sink,
		})
	}
	return paths, nil
}

func collectorLastSeenByID(db *gorm.DB, ids []string) (map[string]int64, error) {
	out := map[string]int64{}
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || id == PrimaryObserverID {
			continue
		}
		filtered = append(filtered, id)
	}
	if len(filtered) == 0 {
		return out, nil
	}
	var runtimes []model.CollectorRuntime
	if err := db.Where("collector_uuid IN ?", filtered).Find(&runtimes).Error; err != nil {
		return nil, err
	}
	for _, runtime := range runtimes {
		out[runtime.CollectorUUID] = runtime.LastSeen
	}
	return out, nil
}

func sinkHandshaked(observerID string, sink PathSink, lastSeen map[string]int64, now time.Time) bool {
	if !sink.Connected {
		return false
	}
	if observerID == PrimaryObserverID {
		return true
	}
	return CollectorStatus(lastSeen[observerID], now) == CollectorStatusOnline
}

func clearUnhandshakedSink(sink *PathSink) {
	sink.Connected = false
	sink.LastRttMs = 0
	sink.RttSampledAt = 0
}

func pathKey(nodeUUID []byte, observerID string) string {
	return string(nodeUUID) + "\x00" + observerID
}

func uniqueBytes(rows []model.ObserverAssignment, pick func(model.ObserverAssignment) []byte) [][]byte {
	seen := map[string]bool{}
	out := make([][]byte, 0, len(rows))
	for _, row := range rows {
		value := pick(row)
		key := string(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func uniqueStrings(rows []model.ObserverAssignment, pick func(model.ObserverAssignment) string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		value := pick(row)
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func DecodeAgentRuntime(blob []byte) *pb.AgentRuntime {
	if len(blob) == 0 {
		return nil
	}
	var agent pb.AgentRuntime
	if proto.Unmarshal(blob, &agent) != nil {
		return nil
	}
	return &agent
}

func HexUUID(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	return hex.EncodeToString(value)
}

func RFC3339NanoPtr(value int64) *string {
	if value <= 0 {
		return nil
	}
	text := time.Unix(0, value).UTC().Format(time.RFC3339Nano)
	return &text
}

func RFC3339TimePtr(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	text := value.UTC().Format(time.RFC3339Nano)
	return &text
}

func NormalizeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(value), "_"))
}

type hostIndex struct {
	serverByNode map[string]uint64
	servers      map[uint64]model.Server
	collectors   map[string]model.Collector
}

func (idx hostIndex) host(nodeUUID []byte) (uint64, string) {
	server := idx.lookupServer(nodeUUID)
	return server.ID, server.Name
}

func (idx hostIndex) lookupServer(nodeUUID []byte) model.Server {
	id := idx.serverByNode[string(nodeUUID)]
	if server, ok := idx.servers[id]; ok {
		return server
	}
	return model.Server{Common: model.Common{ID: id}}
}

func (idx hostIndex) observer(id string) (kind, name string) {
	if id == PrimaryObserverID {
		return ObserverKindPrimary, ""
	}
	return ObserverKindCollector, idx.collectors[id].Name
}

func (idx hostIndex) isProbeObserver(id string) bool {
	if id == "" || id == PrimaryObserverID {
		return false
	}
	collector, ok := idx.collectors[id]
	return ok && collector.IsProbe()
}

func loadHostIndex(db *gorm.DB, nodeIDs [][]byte, observerIDs []string) (hostIndex, error) {
	idx := hostIndex{
		serverByNode: map[string]uint64{},
		servers:      map[uint64]model.Server{},
		collectors:   map[string]model.Collector{},
	}
	if len(nodeIDs) > 0 {
		var bindings []model.ServerNodeBinding
		if err := db.Where("current = ? AND node_uuid IN ?", true, nodeIDs).Find(&bindings).Error; err != nil {
			return idx, err
		}
		serverIDs := make([]uint64, 0, len(bindings))
		for _, binding := range bindings {
			idx.serverByNode[string(binding.NodeUUID)] = binding.ServerID
			serverIDs = append(serverIDs, binding.ServerID)
		}
		missing := make([][]byte, 0)
		for _, nodeID := range nodeIDs {
			if _, ok := idx.serverByNode[string(nodeID)]; !ok {
				missing = append(missing, nodeID)
			}
		}
		if len(missing) > 0 {
			var historical []model.ServerNodeBinding
			if err := db.Where("node_uuid IN ?", missing).Order("valid_to DESC, valid_from DESC").Find(&historical).Error; err != nil {
				return idx, err
			}
			for _, binding := range historical {
				key := string(binding.NodeUUID)
				if _, ok := idx.serverByNode[key]; ok {
					continue
				}
				idx.serverByNode[key] = binding.ServerID
				serverIDs = append(serverIDs, binding.ServerID)
			}
		}
		if len(serverIDs) > 0 {
			var rows []model.Server
			if err := db.Select("id", "name", "tag", "display_index").Where("id IN ?", serverIDs).Find(&rows).Error; err != nil {
				return idx, err
			}
			for _, row := range rows {
				idx.servers[row.ID] = row
			}
		}
	}
	collectorIDs := uniqueStringValues(observerIDs)
	filtered := make([]string, 0, len(collectorIDs))
	for _, id := range collectorIDs {
		if id != "" && id != PrimaryObserverID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) > 0 {
		var rows []model.Collector
		if err := db.Where("collector_uuid IN ?", filtered).Find(&rows).Error; err != nil {
			return idx, err
		}
		for _, row := range rows {
			idx.collectors[row.CollectorUUID] = row
		}
	}
	return idx, nil
}

func uniqueByteSlices(values [][]byte) [][]byte {
	seen := map[string]bool{}
	out := make([][]byte, 0, len(values))
	for _, value := range values {
		key := string(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func uniqueStringValues(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
