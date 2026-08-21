package collector

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/telemetry"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	outboxEvent       = "event"
	outboxObservation = "observation"
	outboxGap         = "gap"
	outboxHealth      = "health"
	outboxDataLoss    = "data_loss"
)

type Store struct {
	db   *gorm.DB
	path string
}

func OpenStore(path string, debug bool) (*Store, error) {
	if path == "" {
		return nil, errors.New("collector database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{CreateBatchSize: 200})
	if err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			_ = closeGormDB(db)
		}
	}()
	if debug {
		db = db.Debug()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=NORMAL"} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	ready = true
	return &Store{db: db, path: path}, nil
}

func closeGormDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	// WAL checkpoint then Close: Windows cannot unlink a still-mapped sqlite file.
	_, _ = sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return sqlDB.Close()
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	err := closeGormDB(s.db)
	s.db = nil
	return err
}

func migrate(db *gorm.DB) error {
	var migrationTableCount int64
	if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'collector_schema_migrations'").Scan(&migrationTableCount).Error; err != nil {
		return err
	}
	if migrationTableCount == 0 {
		var userTableCount int64
		if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'").Scan(&userTableCount).Error; err != nil {
			return err
		}
		if userTableCount != 0 {
			return errors.New("existing collector database without collector_schema_migrations is unsupported; configure an empty database")
		}
		if err := db.AutoMigrate(&model.CollectorSchemaMigration{}); err != nil {
			return err
		}
	}
	var current uint64
	if err := db.Model(&model.CollectorSchemaMigration{}).Select("COALESCE(MAX(version), 0)").Scan(&current).Error; err != nil {
		return err
	}
	if current < 1 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(
				&model.CollectorMeta{}, &model.CollectorStoredEvent{}, &model.CollectorStoredObservation{},
				&model.CollectorStoredGap{}, &model.CollectorAgentCursor{}, &model.CollectorOutbox{},
				&model.CollectorReplicationCursor{}, &model.CollectorAuthorizationCache{},
				&model.CollectorCachedAssignment{}, &model.CollectorCachedRevocation{},
				&model.CollectorHealthEvidence{}, &model.CollectorDataLoss{},
			); err != nil {
				return err
			}
			if err := tx.Create(&model.CollectorReplicationCursor{ID: 1, NextBatchSequence: 1}).Error; err != nil {
				return err
			}
			return tx.Create(&model.CollectorSchemaMigration{Version: 1, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 1
	}
	if current < 2 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorDataLoss{}); err != nil {
				return err
			}
			return tx.Create(&model.CollectorSchemaMigration{Version: 2, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 2
	}
	if current < 3 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorAuthorizationCache{}); err != nil {
				return err
			}
			return tx.Create(&model.CollectorSchemaMigration{Version: 3, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 3
	}
	if current < 4 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorAuthorizationCache{}, &model.CollectorCachedProbeTarget{}); err != nil {
				return err
			}
			return tx.Create(&model.CollectorSchemaMigration{Version: 4, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 4
	}
	if current < 5 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorCachedProbeTarget{}); err != nil {
				return err
			}
			return tx.Create(&model.CollectorSchemaMigration{Version: 5, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 5
	}
	if current < 6 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorCachedProbeTarget{}); err != nil {
				return err
			}
			return tx.Create(&model.CollectorSchemaMigration{Version: 6, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 6
	}
	if current < 7 {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorCachedProbeTarget{}); err != nil {
				return err
			}
			return tx.Create(&model.CollectorSchemaMigration{Version: 7, AppliedAt: time.Now().UTC()}).Error
		})
	}
	return nil
}

func (s *Store) DB() *gorm.DB { return s.db }

type IngestResult struct {
	Acks     []*pb.SessionAck
	Enqueued int
}

func (s *Store) Ingest(ctx context.Context, batch *pb.TelemetryBatch, observerID string, receivedAt time.Time) (*IngestResult, error) {
	if batch == nil || observerID == "" {
		return nil, errors.New("telemetry batch and observer ID are required")
	}
	result := &IngestResult{}
	maxBySession := make(map[string]uint64)
	nodeBySession := make(map[string][]byte)
	sessionIDs := make(map[string][]byte)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, record := range batch.GetRecords() {
			switch body := record.GetRecord().(type) {
			case *pb.TelemetryRecord_Event:
				event := body.Event
				if err := validateEvent(event); err != nil {
					return err
				}
				encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
				if err != nil {
					return err
				}
				created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CollectorStoredEvent{
					EventID: event.GetEventId(), NodeUUID: event.GetNodeUuid(), SessionID: event.GetSessionId(),
					Sequence: event.GetSequence(), EventType: int32(event.GetEventType()), Priority: int32(event.GetPriority()),
					CollectedAt: event.GetCollectedAtUnixNano(), Payload: encoded,
				})
				if created.Error != nil {
					return created.Error
				}
				if created.RowsAffected > 0 {
					if err := enqueue(tx, outboxEvent, encoded); err != nil {
						return err
					}
					result.Enqueued++
				}
				observation := &pb.TelemetryObservation{
					EventId: event.GetEventId(), ObserverId: observerID, ReceivedAtUnixNano: receivedAt.UnixNano(),
				}
				observationBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(observation)
				if err != nil {
					return err
				}
				observed := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CollectorStoredObservation{
					EventID: event.GetEventId(), ObserverID: observerID, NodeUUID: event.GetNodeUuid(), ReceivedAt: receivedAt.UnixNano(),
				})
				if observed.Error != nil {
					return observed.Error
				}
				if observed.RowsAffected > 0 {
					if err := enqueue(tx, outboxObservation, observationBytes); err != nil {
						return err
					}
					result.Enqueued++
				}
				key := sessionKey(event.GetNodeUuid(), event.GetSessionId())
				maxBySession[key] = max(maxBySession[key], event.GetSequence())
				nodeBySession[key] = append([]byte(nil), event.GetNodeUuid()...)
				sessionIDs[key] = append([]byte(nil), event.GetSessionId()...)
			case *pb.TelemetryRecord_Gap:
				gap := body.Gap
				if err := validateGap(gap); err != nil {
					return err
				}
				encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(gap)
				if err != nil {
					return err
				}
				created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CollectorStoredGap{
					GapID: gap.GetGapId(), NodeUUID: gap.GetNodeUuid(), SessionID: gap.GetSessionId(),
					StartSequence: gap.GetStartSequence(), EndSequence: gap.GetEndSequence(), Reason: int32(gap.GetReason()),
					Payload: encoded, CreatedAtUnixNano: gap.GetCreatedAtUnixNano(),
				})
				if created.Error != nil {
					return created.Error
				}
				if created.RowsAffected > 0 {
					if err := enqueue(tx, outboxGap, encoded); err != nil {
						return err
					}
					result.Enqueued++
				}
				key := sessionKey(gap.GetNodeUuid(), gap.GetSessionId())
				maxBySession[key] = max(maxBySession[key], gap.GetEndSequence())
				nodeBySession[key] = append([]byte(nil), gap.GetNodeUuid()...)
				sessionIDs[key] = append([]byte(nil), gap.GetSessionId()...)
			default:
				return errors.New("empty telemetry record")
			}
		}
		keys := make([]string, 0, len(maxBySession))
		for key := range maxBySession {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			ack, err := advanceCursor(tx, nodeBySession[key], sessionIDs[key], maxBySession[key])
			if err != nil {
				return err
			}
			result.Acks = append(result.Acks, &pb.SessionAck{NodeUuid: nodeBySession[key], SessionId: sessionIDs[key], AckThrough: ack})
		}
		return nil
	})
	return result, err
}

func enqueue(tx *gorm.DB, recordType string, payload []byte) error {
	return tx.Create(&model.CollectorOutbox{RecordType: recordType, Payload: payload}).Error
}

func validateEvent(event *pb.TelemetryEvent) error {
	if event == nil || len(event.GetEventId()) != 16 || len(event.GetNodeUuid()) != 16 || len(event.GetSessionId()) != 16 || event.GetSequence() == 0 || event.GetPayload() == nil {
		return errors.New("invalid telemetry event")
	}
	expected, err := telemetry.EventID(event.GetNodeUuid(), event.GetSessionId(), event.GetSequence())
	if err != nil || !bytes.Equal(expected, event.GetEventId()) {
		return errors.New("telemetry event ID does not match identity tuple")
	}
	return nil
}

func validateGap(gap *pb.SequenceGap) error {
	if gap == nil || len(gap.GetGapId()) != 16 || len(gap.GetNodeUuid()) != 16 || len(gap.GetSessionId()) != 16 || gap.GetStartSequence() == 0 || gap.GetEndSequence() < gap.GetStartSequence() || gap.GetReason() == pb.GapReason_GAP_REASON_UNSPECIFIED {
		return errors.New("invalid telemetry sequence gap")
	}
	return nil
}

func advanceCursor(tx *gorm.DB, nodeUUID, sessionID []byte, maxSequence uint64) (uint64, error) {
	var cursor model.CollectorAgentCursor
	err := tx.Where("node_uuid = ? AND session_id = ?", nodeUUID, sessionID).First(&cursor).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cursor = model.CollectorAgentCursor{NodeUUID: append([]byte(nil), nodeUUID...), SessionID: append([]byte(nil), sessionID...)}
	}
	if maxSequence <= cursor.AckThrough {
		return cursor.AckThrough, nil
	}
	var sequences []uint64
	if err := tx.Model(&model.CollectorStoredEvent{}).Where("node_uuid = ? AND session_id = ? AND sequence > ? AND sequence <= ?", nodeUUID, sessionID, cursor.AckThrough, maxSequence).Pluck("sequence", &sequences).Error; err != nil {
		return 0, err
	}
	present := make(map[uint64]bool, len(sequences))
	for _, sequence := range sequences {
		present[sequence] = true
	}
	var gaps []model.CollectorStoredGap
	if err := tx.Where("node_uuid = ? AND session_id = ? AND end_sequence > ? AND start_sequence <= ?", nodeUUID, sessionID, cursor.AckThrough, maxSequence).Find(&gaps).Error; err != nil {
		return 0, err
	}
	for cursor.AckThrough < maxSequence {
		next := cursor.AckThrough + 1
		if present[next] {
			cursor.AckThrough = next
			continue
		}
		advanced := false
		for _, gap := range gaps {
			if gap.StartSequence <= next && gap.EndSequence >= next {
				cursor.AckThrough = gap.EndSequence
				advanced = true
				break
			}
		}
		if !advanced {
			break
		}
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "node_uuid"}, {Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"ack_through", "updated_at"}),
	}).Create(&cursor).Error; err != nil {
		return 0, err
	}
	return cursor.AckThrough, nil
}

func sessionKey(node, session []byte) string { return fmt.Sprintf("%x/%x", node, session) }

type OutboxBatch struct {
	Through      uint64
	Events       []*pb.TelemetryEvent
	Observations []*pb.TelemetryObservation
	Gaps         []*pb.SequenceGap
	Health       []*pb.ObserverHealthSample
	DataLoss     []*pb.CollectorDataLossFact
}

func (s *Store) ReadOutbox(ctx context.Context, limit int) (*OutboxBatch, error) {
	if limit <= 0 {
		limit = 256
	}
	var rows []model.CollectorOutbox
	if err := s.db.WithContext(ctx).Order("spool_id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	batch := &OutboxBatch{}
	for _, row := range rows {
		batch.Through = row.SpoolID
		switch row.RecordType {
		case outboxEvent:
			item := new(pb.TelemetryEvent)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return nil, err
			}
			batch.Events = append(batch.Events, item)
		case outboxObservation:
			item := new(pb.TelemetryObservation)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return nil, err
			}
			batch.Observations = append(batch.Observations, item)
		case outboxGap:
			item := new(pb.SequenceGap)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return nil, err
			}
			batch.Gaps = append(batch.Gaps, item)
		case outboxHealth:
			item := new(pb.ObserverHealthSample)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return nil, err
			}
			batch.Health = append(batch.Health, item)
		case outboxDataLoss:
			item := new(pb.CollectorDataLossFact)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return nil, err
			}
			batch.DataLoss = append(batch.DataLoss, item)
		default:
			return nil, fmt.Errorf("unknown collector outbox record type %q", row.RecordType)
		}
	}
	return batch, nil
}

func (s *Store) RecordHealth(ctx context.Context, sample *pb.ObserverHealthSample) error {
	if sample == nil || sample.GetObserverId() == "" || sample.GetSampledAtUnixNano() == 0 {
		return errors.New("invalid observer health sample")
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(sample)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model.CollectorHealthEvidence{
			ObserverID: sample.GetObserverId(), SampledAt: sample.GetSampledAtUnixNano(), Healthy: sample.GetHealthy(),
			ProcessSession: sample.GetProcessSession(),
		}).Error; err != nil {
			return err
		}
		return enqueue(tx, outboxHealth, payload)
	})
}

func (s *Store) TouchPrimarySeen(ctx context.Context, seenAt time.Time) error {
	return s.db.WithContext(ctx).Model(&model.CollectorAuthorizationCache{}).Where("id = 1").Updates(map[string]any{
		"last_primary_seen_nano": seenAt.UnixNano(), "updated_at": seenAt,
	}).Error
}

func (s *Store) CommitReplicationAck(ctx context.Context, through uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var acknowledged []model.CollectorOutbox
		if err := tx.Where("spool_id <= ?", through).Find(&acknowledged).Error; err != nil {
			return err
		}
		if err := cleanupLocalFacts(tx, acknowledged); err != nil {
			return err
		}
		if err := tx.Where("spool_id <= ?", through).Delete(&model.CollectorOutbox{}).Error; err != nil {
			return err
		}
		return tx.Model(&model.CollectorReplicationCursor{}).Where("id = 1").Updates(map[string]any{
			"committed_spool_through_id": through, "updated_at": time.Now(),
		}).Error
	})
}

func (s *Store) SaveAuthorization(ctx context.Context, collectorUUID string, config *pb.CollectorAuthorizationConfig, seenAt time.Time) error {
	if config == nil || collectorUUID == "" || len(config.GetPrimaryPublicKey()) == 0 {
		return errors.New("invalid collector authorization config")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		kind := model.CollectorKindObserver
		if config.GetKind() == pb.CollectorKind_COLLECTOR_KIND_PROBE {
			kind = model.CollectorKindProbe
		}
		var probeJSON []byte
		if config.GetProbe() != nil {
			probeJSON, _ = proto.Marshal(config.GetProbe())
		}
		cache := model.CollectorAuthorizationCache{
			ID: 1, CollectorUUID: collectorUUID, PrimaryPublicKey: config.GetPrimaryPublicKey(), KeyID: config.GetKeyId(),
			ConfigVersion: config.GetConfigVersion(), LastPrimarySeenNano: seenAt.UnixNano(),
			AgentCACertificatePEM: config.GetAgentCaCertificatePem(), Kind: kind, ProbeConfigJSON: probeJSON,
		}
		var existing model.CollectorAuthorizationCache
		if err := tx.First(&existing, "id = 1").Error; err == nil && cache.AgentCACertificatePEM == "" {
			cache.AgentCACertificatePEM = existing.AgentCACertificatePEM
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(&cache).Error; err != nil {
			return err
		}
		if err := tx.Where("1 = 1").Delete(&model.CollectorCachedAssignment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("1 = 1").Delete(&model.CollectorCachedRevocation{}).Error; err != nil {
			return err
		}
		for _, assignment := range config.GetAssignments() {
			if err := tx.Create(&model.CollectorCachedAssignment{
				NodeUUID: assignment.GetNodeUuid(), ObserverID: assignment.GetObserverId(), ValidFrom: assignment.GetValidFromUnixNano(),
				ValidTo: assignment.GetValidToUnixNano(), Generation: assignment.GetGeneration(), ConfigVersion: assignment.GetConfigVersion(),
			}).Error; err != nil {
				return err
			}
		}
		for _, nodeUUID := range config.GetRevokedNodeUuids() {
			if err := tx.Create(&model.CollectorCachedRevocation{NodeUUID: nodeUUID, ConfigVersion: config.GetConfigVersion()}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("1 = 1").Delete(&model.CollectorCachedProbeTarget{}).Error; err != nil {
			return err
		}
		enable4, enable6 := telemetry.ProbeConfigIPFamilies(config.GetProbe())
		routeInterval := uint(0)
		mtrProbesFallback := uint(0)
		if probe := config.GetProbe(); probe != nil {
			routeInterval = uint(probe.GetRouteIntervalSeconds())
			mtrProbesFallback = uint(probe.GetMtrProbes())
		}
		for _, target := range config.GetTargets() {
			ports := make([]string, 0, len(target.GetTcpPorts()))
			for _, port := range target.GetTcpPorts() {
				ports = append(ports, fmt.Sprintf("%d", port))
			}
			interval := uint(target.GetRouteIntervalSeconds())
			if interval == 0 {
				interval = routeInterval
			}
			mtrProbes := uint(target.GetMtrProbes())
			if mtrProbes == 0 {
				mtrProbes = mtrProbesFallback
			}
			if err := tx.Create(&model.CollectorCachedProbeTarget{
				ServerID: target.GetServerId(), ServerName: target.GetServerName(), IPv4: target.GetIpv4(), IPv6: target.GetIpv6(),
				Hostname: target.GetHostname(), TCPPorts: strings.Join(ports, ","), EnableICMP: target.GetEnableIcmp(),
				EnableTCP: target.GetEnableTcp(), EnableMTR: target.GetEnableMtr(), EnableIPv4: enable4, EnableIPv6: enable6,
				IntervalSec: uint(target.GetIntervalSeconds()), MTRIntervalSec: uint(target.GetMtrIntervalSeconds()),
				MTRProbes: model.NormalizeMTRProbes(mtrProbes), RouteIntervalSec: interval,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ProbeTargets(ctx context.Context) ([]model.CollectorCachedProbeTarget, error) {
	var rows []model.CollectorCachedProbeTarget
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) Authorization(ctx context.Context) (*model.CollectorAuthorizationCache, error) {
	var cache model.CollectorAuthorizationCache
	if err := s.db.WithContext(ctx).First(&cache, "id = 1").Error; err != nil {
		return nil, err
	}
	return &cache, nil
}

func (s *Store) IsNodeAuthorized(ctx context.Context, nodeUUID []byte, at time.Time) (bool, error) {
	var revoked int64
	if err := s.db.WithContext(ctx).Model(&model.CollectorCachedRevocation{}).Where("node_uuid = ?", nodeUUID).Count(&revoked).Error; err != nil {
		return false, err
	}
	if revoked > 0 {
		return false, nil
	}
	var assigned int64
	if err := s.db.WithContext(ctx).Model(&model.CollectorCachedAssignment{}).
		Where("node_uuid = ? AND valid_from <= ? AND (valid_to = 0 OR valid_to > ?)", nodeUUID, at.UnixNano(), at.UnixNano()).Count(&assigned).Error; err != nil {
		return false, err
	}
	return assigned > 0, nil
}

type RuntimeStats struct {
	SpoolBytes    uint64
	Pending       uint64
	OldestPending int64
}

func (s *Store) RuntimeStats(ctx context.Context) (RuntimeStats, error) {
	var stats RuntimeStats
	var spoolBytes int64
	if err := s.db.WithContext(ctx).Model(&model.CollectorOutbox{}).Select("COALESCE(SUM(length(payload)), 0)").Scan(&spoolBytes).Error; err != nil {
		return stats, err
	}
	stats.SpoolBytes = uint64(spoolBytes)
	var pending int64
	if err := s.db.WithContext(ctx).Model(&model.CollectorOutbox{}).Count(&pending).Error; err != nil {
		return stats, err
	}
	stats.Pending = uint64(pending)
	var oldest model.CollectorOutbox
	if err := s.db.WithContext(ctx).Order("spool_id ASC").First(&oldest).Error; err == nil {
		stats.OldestPending = oldest.CreatedAt.UnixNano()
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return stats, err
	}
	return stats, nil
}

func (s *Store) EnforceSpoolPolicy(ctx context.Context, collectorUUID string, maxBytes uint64, maxAge time.Duration, now time.Time) error {
	if collectorUUID == "" || maxBytes == 0 || maxAge <= 0 {
		return errors.New("collector spool policy is invalid")
	}
	var rows []model.CollectorOutbox
	if err := s.db.WithContext(ctx).Order("spool_id ASC").Find(&rows).Error; err != nil {
		return err
	}
	var total uint64
	for _, row := range rows {
		total += uint64(len(row.Payload))
	}
	cutoff := now.Add(-maxAge)
	target := maxBytes * 9 / 10
	var selected []model.CollectorOutbox
	for _, row := range rows {
		tooOld := row.CreatedAt.Before(cutoff)
		overLimit := total > maxBytes
		if !tooOld && !overLimit {
			continue
		}
		selected = append(selected, row)
		if total >= uint64(len(row.Payload)) {
			total -= uint64(len(row.Payload))
		}
		if !tooOld && total <= target {
			break
		}
	}
	// Event and Observation are enqueued consecutively. Never discard only the
	// Event half of an unsynchronized pair, otherwise the remaining Observation
	// would permanently reference an Event that Primary never received.
	if len(selected) > 0 && len(selected) < len(rows) && selected[len(selected)-1].RecordType == outboxEvent && rows[len(selected)].RecordType == outboxObservation {
		selected = append(selected, rows[len(selected)])
	}
	if len(selected) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := make([]uint64, 0, len(selected))
		var first, last uint64
		for _, row := range selected {
			ids = append(ids, row.SpoolID)
			if first == 0 {
				first = row.SpoolID
			}
			last = row.SpoolID
			switch row.RecordType {
			case outboxEvent:
				event := new(pb.TelemetryEvent)
				if proto.Unmarshal(row.Payload, event) == nil {
					if err := enqueueLossGap(tx, event.GetNodeUuid(), event.GetSessionId(), event.GetSequence(), event.GetSequence(), now); err != nil {
						return err
					}
				}
			case outboxGap:
				gap := new(pb.SequenceGap)
				if proto.Unmarshal(row.Payload, gap) == nil {
					if err := enqueueLossGap(tx, gap.GetNodeUuid(), gap.GetSessionId(), gap.GetStartSequence(), gap.GetEndSequence(), now); err != nil {
						return err
					}
				}
			}
		}
		if err := cleanupLocalFacts(tx, selected); err != nil {
			return err
		}
		if err := tx.Where("spool_id IN ?", ids).Delete(&model.CollectorOutbox{}).Error; err != nil {
			return err
		}
		factID := make([]byte, 16)
		if _, err := rand.Read(factID); err != nil {
			return err
		}
		fact := &pb.CollectorDataLossFact{
			FactId: factID, CollectorUuid: collectorUUID, OccurredAtUnixNano: now.UnixNano(),
			Reason: pb.GapReason_GAP_REASON_HARD_LIMIT_DATA_LOSS, FirstSpoolId: first, LastSpoolId: last,
			LostRecords: uint64(len(selected)), Detail: "collector spool hard limit or retention exceeded",
		}
		payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(fact)
		if err != nil {
			return err
		}
		if err := tx.Create(&model.CollectorDataLoss{
			FactID: factID, OccurredAt: now.UnixNano(), Reason: "HARD_LIMIT_DATA_LOSS", FirstSpoolID: first,
			LastSpoolID: last, LostRecords: uint64(len(selected)), Detail: fact.GetDetail(),
		}).Error; err != nil {
			return err
		}
		return enqueue(tx, outboxDataLoss, payload)
	})
}

func cleanupLocalFacts(tx *gorm.DB, rows []model.CollectorOutbox) error {
	for _, row := range rows {
		switch row.RecordType {
		case outboxEvent:
			item := new(pb.TelemetryEvent)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return err
			}
			if err := tx.Delete(&model.CollectorStoredEvent{}, "event_id = ?", item.GetEventId()).Error; err != nil {
				return err
			}
		case outboxObservation:
			item := new(pb.TelemetryObservation)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return err
			}
			if err := tx.Delete(&model.CollectorStoredObservation{}, "event_id = ? AND observer_id = ?", item.GetEventId(), item.GetObserverId()).Error; err != nil {
				return err
			}
		case outboxGap:
			item := new(pb.SequenceGap)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return err
			}
			if err := tx.Delete(&model.CollectorStoredGap{}, "gap_id = ?", item.GetGapId()).Error; err != nil {
				return err
			}
		case outboxHealth:
			item := new(pb.ObserverHealthSample)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return err
			}
			if err := tx.Delete(&model.CollectorHealthEvidence{},
				"observer_id = ? AND sampled_at = ? AND process_session = ?", item.GetObserverId(), item.GetSampledAtUnixNano(), item.GetProcessSession()).Error; err != nil {
				return err
			}
		case outboxDataLoss:
			item := new(pb.CollectorDataLossFact)
			if err := proto.Unmarshal(row.Payload, item); err != nil {
				return err
			}
			if err := tx.Delete(&model.CollectorDataLoss{}, "fact_id = ?", item.GetFactId()).Error; err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown collector outbox record type %q", row.RecordType)
		}
	}
	return nil
}

func enqueueLossGap(tx *gorm.DB, nodeUUID, sessionID []byte, start, end uint64, now time.Time) error {
	if len(nodeUUID) != 16 || len(sessionID) != 16 || start == 0 || end < start {
		return nil
	}
	gapID := make([]byte, 16)
	if _, err := rand.Read(gapID); err != nil {
		return err
	}
	gap := &pb.SequenceGap{
		GapId: gapID, NodeUuid: nodeUUID, SessionId: sessionID, StartSequence: start, EndSequence: end,
		Reason: pb.GapReason_GAP_REASON_HARD_LIMIT_DATA_LOSS, CreatedAtUnixNano: now.UnixNano(),
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(gap)
	if err != nil {
		return err
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CollectorStoredGap{
		GapID: gapID, NodeUUID: nodeUUID, SessionID: sessionID, StartSequence: start, EndSequence: end,
		Reason: int32(gap.GetReason()), Payload: payload, CreatedAtUnixNano: gap.GetCreatedAtUnixNano(),
	}).Error; err != nil {
		return err
	}
	return enqueue(tx, outboxGap, payload)
}
