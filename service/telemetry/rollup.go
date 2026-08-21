package telemetry

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RetentionPolicy struct {
	StateRaw        time.Duration
	StateOneMinute  time.Duration
	StateOneHour    time.Duration
	Observation     time.Duration
	Evidence        time.Duration
	Lifecycle       time.Duration
	Receipt         time.Duration
	BatchSize       int
	MaxRuntime      time.Duration
	CompactMinBytes int64
	AutoCompact     bool
}

const (
	DefaultRetentionBatch  = 5000
	DefaultRetentionBudget = 20 * time.Second
	DefaultReceiptRetain   = 7 * 24 * time.Hour
	DefaultEvidenceRetain  = 48 * time.Hour
	DefaultCompactMinBytes = int64(64 << 20)
)

func PolicyFromConfig(config model.RetentionConfig) RetentionPolicy {
	return NormalizeRetentionPolicy(RetentionPolicy{
		StateRaw:        time.Duration(config.StateRawHours) * time.Hour,
		StateOneMinute:  time.Duration(config.StateOneMinuteDays) * 24 * time.Hour,
		StateOneHour:    time.Duration(config.StateOneHourDays) * 24 * time.Hour,
		Observation:     time.Duration(config.ObservationDays) * 24 * time.Hour,
		Evidence:        time.Duration(config.EvidenceHours) * time.Hour,
		Lifecycle:       time.Duration(config.LifecycleDays) * 24 * time.Hour,
		Receipt:         time.Duration(config.ReceiptDays) * 24 * time.Hour,
		BatchSize:       config.BatchSize,
		MaxRuntime:      time.Duration(config.MaxRuntimeMs) * time.Millisecond,
		CompactMinBytes: config.CompactMinBytes,
		AutoCompact:     model.BoolOrTrue(config.AutoCompact),
	})
}

func NormalizeRetentionPolicy(policy RetentionPolicy) RetentionPolicy {
	if policy.StateRaw <= 0 {
		policy.StateRaw = 6 * time.Hour
	}
	if policy.StateOneMinute <= 0 {
		policy.StateOneMinute = 30 * 24 * time.Hour
	}
	if policy.StateOneHour <= 0 {
		policy.StateOneHour = 365 * 24 * time.Hour
	}
	if policy.Observation <= 0 {
		policy.Observation = 30 * 24 * time.Hour
	}
	if policy.Evidence <= 0 {
		policy.Evidence = DefaultEvidenceRetain
	}
	if policy.Lifecycle <= 0 {
		policy.Lifecycle = 10 * 365 * 24 * time.Hour
	}
	if policy.Receipt <= 0 {
		policy.Receipt = DefaultReceiptRetain
	}
	if policy.BatchSize <= 0 {
		policy.BatchSize = DefaultRetentionBatch
	}
	if policy.MaxRuntime <= 0 {
		policy.MaxRuntime = DefaultRetentionBudget
	}
	if policy.CompactMinBytes <= 0 {
		policy.CompactMinBytes = DefaultCompactMinBytes
	}
	return policy
}

type RollupWorker struct {
	db        *gorm.DB
	retention RetentionPolicy
	states    *StateSampleBuffer
}

func NewRollupWorker(db *gorm.DB, policy RetentionPolicy) *RollupWorker {
	return &RollupWorker{db: db, retention: NormalizeRetentionPolicy(policy), states: liveStateBuffer()}
}

func (w *RollupWorker) Run(ctx context.Context) {
	rollupTicker := time.NewTicker(time.Minute)
	defer rollupTicker.Stop()
	_ = w.RollupPending(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-rollupTicker.C:
			_ = w.RollupPending(ctx, now)
		}
	}
}

func (w *RollupWorker) RollupPending(ctx context.Context, now time.Time) error {
	minuteEnd := now.Truncate(time.Minute)
	if err := w.rollupMemoryBefore(ctx, minuteEnd.UnixNano()); err != nil {
		return err
	}
	if err := w.rollupRawWindow(ctx, minuteEnd.Add(-time.Minute), minuteEnd); err != nil {
		return err
	}
	hourEnd := now.Truncate(time.Hour)
	return w.rollupHourWindow(ctx, hourEnd.Add(-time.Hour), hourEnd)
}

func (w *RollupWorker) rollupMemoryBefore(ctx context.Context, cutoff int64) error {
	samples := w.states.TakeBefore(cutoff)
	if len(samples) == 0 {
		return nil
	}
	type windowKey struct {
		node  string
		start int64
	}
	grouped := map[windowKey][]liveStateSample{}
	nodes := map[string][]byte{}
	for _, sample := range samples {
		start := sample.collectedAt / int64(time.Minute) * int64(time.Minute)
		key := windowKey{node: string(sample.nodeUUID), start: start}
		grouped[key] = append(grouped[key], sample)
		nodes[key.node] = sample.nodeUUID
	}
	for key, window := range grouped {
		sort.Slice(window, func(i, j int) bool { return window[i].collectedAt < window[j].collectedAt })
		states := make([]*pb.State, 0, len(window))
		for _, sample := range window {
			states = append(states, sample.state)
		}
		start := time.Unix(0, key.start)
		end := start.Add(time.Minute)
		payload := aggregateStates(states, start, end)
		if payload == nil {
			continue
		}
		encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
		if err != nil {
			return err
		}
		row := model.StateRollup{
			NodeUUID: nodes[key.node], Resolution: "1m", WindowStart: start.UnixNano(), WindowEnd: end.UnixNano(),
			SampleCount: payload.GetSampleCount(), Payload: encoded, NetInTotal: payload.GetNetInTotal(), NetOutTotal: payload.GetNetOutTotal(),
		}
		if err := w.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_uuid"}, {Name: "resolution"}, {Name: "window_start"}}, UpdateAll: true,
		}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (w *RollupWorker) rollupRawWindow(ctx context.Context, start, end time.Time) error {
	var nodes [][]byte
	if err := w.db.WithContext(ctx).Model(&model.TelemetryEvent{}).
		Where("event_type = ? AND collected_at >= ? AND collected_at < ? AND payload_retained = ?", pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE, start.UnixNano(), end.UnixNano(), true).
		Distinct().Pluck("node_uuid", &nodes).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		var events []model.TelemetryEvent
		if err := w.db.WithContext(ctx).Where("node_uuid = ? AND event_type = ? AND collected_at >= ? AND collected_at < ? AND payload_retained = ?",
			node, pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE, start.UnixNano(), end.UnixNano(), true).Order("collected_at ASC").Find(&events).Error; err != nil {
			return err
		}
		states := make([]*pb.State, 0, len(events))
		for _, row := range events {
			event := new(pb.TelemetryEvent)
			if err := proto.Unmarshal(row.Payload, event); err != nil {
				return err
			}
			if event.GetState() != nil {
				states = append(states, event.GetState())
			}
		}
		payload := aggregateStates(states, start, end)
		if payload == nil {
			continue
		}
		encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
		if err != nil {
			return err
		}
		row := model.StateRollup{
			NodeUUID: node, Resolution: "1m", WindowStart: start.UnixNano(), WindowEnd: end.UnixNano(),
			SampleCount: payload.GetSampleCount(), Payload: encoded, NetInTotal: payload.GetNetInTotal(), NetOutTotal: payload.GetNetOutTotal(),
		}
		if err := w.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_uuid"}, {Name: "resolution"}, {Name: "window_start"}}, UpdateAll: true,
		}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (w *RollupWorker) rollupHourWindow(ctx context.Context, start, end time.Time) error {
	var nodes [][]byte
	if err := w.db.WithContext(ctx).Model(&model.StateRollup{}).Where("resolution = ? AND window_start >= ? AND window_start < ?", "1m", start.UnixNano(), end.UnixNano()).Distinct().Pluck("node_uuid", &nodes).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		var rows []model.StateRollup
		if err := w.db.WithContext(ctx).Where("node_uuid = ? AND resolution = ? AND window_start >= ? AND window_start < ?", node, "1m", start.UnixNano(), end.UnixNano()).Order("window_start ASC").Find(&rows).Error; err != nil {
			return err
		}
		payload := aggregateRollups(rows, start, end)
		if payload == nil {
			continue
		}
		encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
		if err != nil {
			return err
		}
		result := model.StateRollup{
			NodeUUID: node, Resolution: "1h", WindowStart: start.UnixNano(), WindowEnd: end.UnixNano(),
			SampleCount: payload.GetSampleCount(), Payload: encoded, NetInTotal: payload.GetNetInTotal(), NetOutTotal: payload.GetNetOutTotal(),
		}
		if err := w.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_uuid"}, {Name: "resolution"}, {Name: "window_start"}}, UpdateAll: true,
		}).Create(&result).Error; err != nil {
			return err
		}
	}
	return nil
}

func aggregateStates(states []*pb.State, start, end time.Time) *pb.StateRollupPayload {
	if len(states) == 0 {
		return nil
	}
	minimum := cloneState(states[0])
	maximum := cloneState(states[0])
	average := new(pb.State)
	for _, state := range states {
		minimum.Cpu = math.Min(minimum.GetCpu(), state.GetCpu())
		minimum.MemUsed = min(minimum.GetMemUsed(), state.GetMemUsed())
		minimum.SwapUsed = min(minimum.GetSwapUsed(), state.GetSwapUsed())
		minimum.DiskUsed = min(minimum.GetDiskUsed(), state.GetDiskUsed())
		minimum.Load1 = math.Min(minimum.GetLoad1(), state.GetLoad1())
		minimum.Load5 = math.Min(minimum.GetLoad5(), state.GetLoad5())
		minimum.Load15 = math.Min(minimum.GetLoad15(), state.GetLoad15())
		minimum.TcpConnCount = min(minimum.GetTcpConnCount(), state.GetTcpConnCount())
		minimum.UdpConnCount = min(minimum.GetUdpConnCount(), state.GetUdpConnCount())
		minimum.ProcessCount = min(minimum.GetProcessCount(), state.GetProcessCount())
		minimum.NetInSpeed = min(minimum.GetNetInSpeed(), state.GetNetInSpeed())
		minimum.NetOutSpeed = min(minimum.GetNetOutSpeed(), state.GetNetOutSpeed())
		maximum.Cpu = math.Max(maximum.GetCpu(), state.GetCpu())
		maximum.MemUsed = max(maximum.GetMemUsed(), state.GetMemUsed())
		maximum.SwapUsed = max(maximum.GetSwapUsed(), state.GetSwapUsed())
		maximum.DiskUsed = max(maximum.GetDiskUsed(), state.GetDiskUsed())
		maximum.Load1 = math.Max(maximum.GetLoad1(), state.GetLoad1())
		maximum.Load5 = math.Max(maximum.GetLoad5(), state.GetLoad5())
		maximum.Load15 = math.Max(maximum.GetLoad15(), state.GetLoad15())
		maximum.TcpConnCount = max(maximum.GetTcpConnCount(), state.GetTcpConnCount())
		maximum.UdpConnCount = max(maximum.GetUdpConnCount(), state.GetUdpConnCount())
		maximum.ProcessCount = max(maximum.GetProcessCount(), state.GetProcessCount())
		maximum.NetInSpeed = max(maximum.GetNetInSpeed(), state.GetNetInSpeed())
		maximum.NetOutSpeed = max(maximum.GetNetOutSpeed(), state.GetNetOutSpeed())
		average.Cpu += state.GetCpu()
		average.MemUsed += state.GetMemUsed()
		average.SwapUsed += state.GetSwapUsed()
		average.DiskUsed += state.GetDiskUsed()
		average.Load1 += state.GetLoad1()
		average.Load5 += state.GetLoad5()
		average.Load15 += state.GetLoad15()
		average.TcpConnCount += state.GetTcpConnCount()
		average.UdpConnCount += state.GetUdpConnCount()
		average.ProcessCount += state.GetProcessCount()
		average.NetInSpeed += state.GetNetInSpeed()
		average.NetOutSpeed += state.GetNetOutSpeed()
	}
	count := uint64(len(states))
	average.Cpu /= float64(count)
	average.MemUsed /= count
	average.SwapUsed /= count
	average.DiskUsed /= count
	average.Load1 /= float64(count)
	average.Load5 /= float64(count)
	average.Load15 /= float64(count)
	average.TcpConnCount /= count
	average.UdpConnCount /= count
	average.ProcessCount /= count
	average.NetInSpeed /= count
	average.NetOutSpeed /= count
	netIn, netOut := counterDeltas(states)
	return &pb.StateRollupPayload{
		WindowStartUnixNano: start.UnixNano(), WindowEndUnixNano: end.UnixNano(), SampleCount: uint32(len(states)),
		Minimum: minimum, Average: average, Maximum: maximum, NetInTotal: netIn, NetOutTotal: netOut,
	}
}

func aggregateRollups(rows []model.StateRollup, start, end time.Time) *pb.StateRollupPayload {
	var states []*pb.State
	var netIn, netOut uint64
	var minimum, maximum *pb.State
	for _, row := range rows {
		payload := new(pb.StateRollupPayload)
		if proto.Unmarshal(row.Payload, payload) != nil || payload.GetAverage() == nil {
			continue
		}
		if payload.GetMinimum() != nil && payload.GetMaximum() != nil {
			if minimum == nil {
				minimum = cloneState(payload.GetMinimum())
				maximum = cloneState(payload.GetMaximum())
			} else {
				mergeStateExtrema(minimum, maximum, payload.GetMinimum(), payload.GetMaximum())
			}
		}
		for index := uint32(0); index < payload.GetSampleCount(); index++ {
			states = append(states, payload.GetAverage())
		}
		netIn += payload.GetNetInTotal()
		netOut += payload.GetNetOutTotal()
	}
	result := aggregateStates(states, start, end)
	if result != nil {
		if minimum != nil {
			result.Minimum = minimum
			result.Maximum = maximum
		}
		result.NetInTotal = netIn
		result.NetOutTotal = netOut
	}
	return result
}

func mergeStateExtrema(minimum, maximum, candidateMinimum, candidateMaximum *pb.State) {
	minimum.Cpu = math.Min(minimum.GetCpu(), candidateMinimum.GetCpu())
	minimum.MemUsed = min(minimum.GetMemUsed(), candidateMinimum.GetMemUsed())
	minimum.SwapUsed = min(minimum.GetSwapUsed(), candidateMinimum.GetSwapUsed())
	minimum.DiskUsed = min(minimum.GetDiskUsed(), candidateMinimum.GetDiskUsed())
	minimum.Load1 = math.Min(minimum.GetLoad1(), candidateMinimum.GetLoad1())
	minimum.Load5 = math.Min(minimum.GetLoad5(), candidateMinimum.GetLoad5())
	minimum.Load15 = math.Min(minimum.GetLoad15(), candidateMinimum.GetLoad15())
	minimum.TcpConnCount = min(minimum.GetTcpConnCount(), candidateMinimum.GetTcpConnCount())
	minimum.UdpConnCount = min(minimum.GetUdpConnCount(), candidateMinimum.GetUdpConnCount())
	minimum.ProcessCount = min(minimum.GetProcessCount(), candidateMinimum.GetProcessCount())
	minimum.NetInSpeed = min(minimum.GetNetInSpeed(), candidateMinimum.GetNetInSpeed())
	minimum.NetOutSpeed = min(minimum.GetNetOutSpeed(), candidateMinimum.GetNetOutSpeed())
	maximum.Cpu = math.Max(maximum.GetCpu(), candidateMaximum.GetCpu())
	maximum.MemUsed = max(maximum.GetMemUsed(), candidateMaximum.GetMemUsed())
	maximum.SwapUsed = max(maximum.GetSwapUsed(), candidateMaximum.GetSwapUsed())
	maximum.DiskUsed = max(maximum.GetDiskUsed(), candidateMaximum.GetDiskUsed())
	maximum.Load1 = math.Max(maximum.GetLoad1(), candidateMaximum.GetLoad1())
	maximum.Load5 = math.Max(maximum.GetLoad5(), candidateMaximum.GetLoad5())
	maximum.Load15 = math.Max(maximum.GetLoad15(), candidateMaximum.GetLoad15())
	maximum.TcpConnCount = max(maximum.GetTcpConnCount(), candidateMaximum.GetTcpConnCount())
	maximum.UdpConnCount = max(maximum.GetUdpConnCount(), candidateMaximum.GetUdpConnCount())
	maximum.ProcessCount = max(maximum.GetProcessCount(), candidateMaximum.GetProcessCount())
	maximum.NetInSpeed = max(maximum.GetNetInSpeed(), candidateMaximum.GetNetInSpeed())
	maximum.NetOutSpeed = max(maximum.GetNetOutSpeed(), candidateMaximum.GetNetOutSpeed())
}

func counterDeltas(states []*pb.State) (uint64, uint64) {
	var inbound, outbound uint64
	for index := 1; index < len(states); index++ {
		previous, current := states[index-1], states[index]
		continuousBoot := current.GetUptime() >= previous.GetUptime()
		inbound += safeCounterDelta(previous.GetNetInTransfer(), current.GetNetInTransfer(), continuousBoot)
		outbound += safeCounterDelta(previous.GetNetOutTransfer(), current.GetNetOutTransfer(), continuousBoot)
	}
	return inbound, outbound
}

func safeCounterDelta(previous, current uint64, continuousBoot bool) uint64 {
	const maximumPlausibleDelta = uint64(1 << 50) // 1 PiB between adjacent samples is treated as corrupt evidence.
	if !continuousBoot {
		return 0
	}
	if current >= previous {
		delta := current - previous
		if delta > maximumPlausibleDelta {
			return 0
		}
		return delta
	}
	if previous > math.MaxUint64-(1<<32) {
		delta := math.MaxUint64 - previous + current + 1
		if delta <= maximumPlausibleDelta {
			return delta
		}
	}
	return 0
}

func cloneState(state *pb.State) *pb.State {
	return proto.Clone(state).(*pb.State)
}

func (w *RollupWorker) ApplyRetention(ctx context.Context, now time.Time) error {
	_, err := DrainRetention(ctx, w.db, w.retention, now)
	return err
}

func DrainRetention(ctx context.Context, db *gorm.DB, policy RetentionPolicy, now time.Time) (int64, error) {
	policy = NormalizeRetentionPolicy(policy)
	deadline := now.Add(policy.MaxRuntime)
	batch := policy.BatchSize
	var deleted int64
	add := func(n int64, err error) error {
		deleted += n
		return err
	}

	if sqliteTableExists(db, "probe_route_jobs") {
		if err := expireStaleProbeRouteJobs(db, now); err != nil {
			return deleted, err
		}
	}
	if sqliteTableExists(db, "probe_routes") {
		if err := PruneAllProbeRoutes(db); err != nil {
			return deleted, err
		}
	}
	if sqliteTableExists(db, "connection_latency_buckets") {
		if err := add(PruneConnectionLatencyByCount(ctx, db, deadline, batch)); err != nil {
			return deleted, err
		}
	}

	completedMinute := now.Truncate(time.Minute).UnixNano()
	if sqliteTableExists(db, "telemetry_events") {
		if err := add(drainUntil(ctx, db, deadline, batch, "telemetry_observations",
			"event_id IN (SELECT event_id FROM telemetry_events WHERE event_type IN ("+highFrequencyEventTypesSQL()+") AND collected_at < ?)", completedMinute)); err != nil {
			return deleted, err
		}
		if err := add(drainUntil(ctx, db, deadline, batch, "telemetry_events",
			"collected_at < ? AND event_type IN ("+highFrequencyEventTypesSQL()+")", completedMinute)); err != nil {
			return deleted, err
		}
		stripped, err := updateUntil(ctx, db, deadline, batch, `UPDATE telemetry_events SET payload = NULL, payload_retained = 0
		WHERE rowid IN (SELECT rowid FROM telemetry_events
		WHERE event_type = ? AND payload_retained = 1 AND collected_at < ? LIMIT ?)`,
			pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE, completedMinute)
		if err != nil {
			return deleted, err
		}
		deleted += stripped
	}

	observationBefore := now.Add(-policy.Observation).UnixNano()
	evidenceBefore := now.Add(-policy.Evidence).UnixNano()
	if err := add(drainUntil(ctx, db, deadline, batch, "telemetry_observations", "received_at < ?", observationBefore)); err != nil {
		return deleted, err
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "telemetry_events", "collected_at < ? AND event_type != "+itoa(int(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_LIFECYCLE)), observationBefore)); err != nil {
		return deleted, err
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "observer_path_buckets", "bucket_start < ?", evidenceBefore)); err != nil {
		return deleted, err
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "observer_health_buckets", "bucket_start < ?", evidenceBefore)); err != nil {
		return deleted, err
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "availability_recompute_queues", "bucket_start < ?", evidenceBefore)); err != nil {
		return deleted, err
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "telemetry_alerts", "occurred_at < ?", observationBefore)); err != nil {
		return deleted, err
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "telemetry_data_losses", "occurred_at < ?", observationBefore)); err != nil {
		return deleted, err
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "collector_replication_receipts", "created_at < ?", now.Add(-policy.Receipt))); err != nil {
		return deleted, err
	}
	compacted, err := compactAvailabilitySpans(ctx, db, deadline, batch, evidenceBefore)
	if err != nil {
		return deleted, err
	}
	deleted += compacted

	lifecycleBefore := now.Add(-policy.Lifecycle).UnixNano()
	if err := add(drainUntil(ctx, db, deadline, batch, "incident_revisions", "incident_id IN (SELECT id FROM availability_incidents WHERE started_at < ?)", lifecycleBefore)); err != nil {
		return deleted, err
	}
	for _, item := range []struct {
		table     string
		condition string
		before    int64
	}{
		{"telemetry_events", "collected_at < ? AND event_type = " + itoa(int(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_LIFECYCLE)), lifecycleBefore},
		{"telemetry_gaps", "created_at_unix_nano < ?", lifecycleBefore},
		{"availability_buckets", "bucket_start < ?", lifecycleBefore},
		{"availability_incidents", "started_at < ?", lifecycleBefore},
		{"state_rollups", "resolution = '1m' AND window_start < ?", now.Add(-policy.StateOneMinute).UnixNano()},
		{"state_rollups", "resolution = '1h' AND window_start < ?", now.Add(-policy.StateOneHour).UnixNano()},
		{"connection_latency_buckets", "bucket_start < ?", evidenceBefore},
		{"probe_sample_buckets", "bucket_start < ?", evidenceBefore},
		{"probe_latests", "sampled_at < ?", evidenceBefore},
		{"probe_traces", "sampled_at < ?", evidenceBefore},
	} {
		if err := add(drainUntil(ctx, db, deadline, batch, item.table, item.condition, item.before)); err != nil {
			return deleted, err
		}
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "monitor_histories", "server_id != 0 AND created_at < ?", now.Add(-24*time.Hour))); err != nil {
		return deleted, err
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "monitor_histories", "created_at < ?", now.Add(-30*24*time.Hour))); err != nil {
		return deleted, err
	}
	if sqliteTableExists(db, "monitors") {
		if err := add(drainUntil(ctx, db, deadline, batch, "monitor_histories", "monitor_id NOT IN (SELECT id FROM monitors)")); err != nil {
			return deleted, err
		}
	}
	if err := add(drainUntil(ctx, db, deadline, batch, "transfers", "created_at < ?", now.Add(-48*time.Hour))); err != nil {
		return deleted, err
	}
	if sqliteTableExists(db, "servers") {
		if err := add(drainUntil(ctx, db, deadline, batch, "transfers", "server_id NOT IN (SELECT id FROM servers)")); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func drainUntil(ctx context.Context, db *gorm.DB, deadline time.Time, limit int, table, condition string, args ...any) (int64, error) {
	if !retentionTableAllowed(table) {
		return 0, errors.New("retention table is not allowlisted")
	}
	if !sqliteTableExists(db, table) {
		return 0, nil
	}
	var total int64
	query := "DELETE FROM " + table + " WHERE rowid IN (SELECT rowid FROM " + table + " WHERE " + condition + " LIMIT ?)"
	bound := append(append([]any{}, args...), limit)
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return total, nil
		}
		result := db.WithContext(ctx).Exec(query, bound...)
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if result.RowsAffected == 0 {
			return total, nil
		}
	}
}

func sqliteTableExists(db *gorm.DB, table string) bool {
	var count int64
	if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func updateUntil(ctx context.Context, db *gorm.DB, deadline time.Time, limit int, query string, args ...any) (int64, error) {
	var total int64
	bound := append(append([]any{}, args...), limit)
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return total, nil
		}
		result := db.WithContext(ctx).Exec(query, bound...)
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if result.RowsAffected == 0 {
			return total, nil
		}
	}
}

func retentionTableAllowed(table string) bool {
	switch table {
	case "telemetry_observations", "telemetry_events", "telemetry_gaps",
		"availability_buckets", "availability_incidents", "incident_revisions",
		"state_rollups", "connection_latency_buckets",
		"observer_path_buckets", "observer_health_buckets",
		"collector_replication_receipts", "telemetry_alerts", "telemetry_data_losses",
		"probe_sample_buckets", "probe_latests", "probe_traces", "monitor_histories", "transfers", "availability_recompute_queues":
		return true
	default:
		return false
	}
}

func highFrequencyEventTypesSQL() string {
	return itoa(int(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_HEARTBEAT)) + "," +
		itoa(int(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE)) + "," +
		itoa(int(pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE_ROLLUP))
}

func deleteBatch(db *gorm.DB, table, condition string, before int64, limit int) error {
	_, err := drainUntil(context.Background(), db, time.Time{}, limit, table, condition, before)
	return err
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 10)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
