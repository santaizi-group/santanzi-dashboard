package telemetry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const clockSkewLimit = 5 * time.Minute

type Store struct {
	db         *gorm.DB
	bucketSize int64
	states     *StateSampleBuffer
}

func persistTelemetryEvent(eventType pb.TelemetryEventType) bool {
	switch eventType {
	case pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_HEARTBEAT,
		pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE,
		pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE_ROLLUP:
		return false
	default:
		return true
	}
}

func NewStore(db *gorm.DB) *Store {
	return NewStoreWithBucketSize(db, 30*time.Second)
}

func NewStoreWithBucketSize(db *gorm.DB, bucketSize time.Duration) *Store {
	if bucketSize <= 0 {
		bucketSize = 30 * time.Second
	}
	return &Store{db: db, bucketSize: int64(bucketSize), states: liveStateBuffer()}
}

func EventID(nodeUUID, sessionID []byte, sequence uint64) ([]byte, error) {
	if len(nodeUUID) != 16 || len(sessionID) != 16 {
		return nil, errors.New("node UUID and session ID must be 16 bytes")
	}
	var input [40]byte
	copy(input[:16], nodeUUID)
	copy(input[16:32], sessionID)
	binary.BigEndian.PutUint64(input[32:], sequence)
	sum := sha256.Sum256(input[:])
	return append([]byte(nil), sum[:16]...), nil
}

type IngestResult struct {
	Acks        []*pb.SessionAck
	FreshEvents []*pb.TelemetryEvent
}

func (s *Store) Replicate(ctx context.Context, batch *pb.ReplicationBatch, receivedAt time.Time) (uint64, error) {
	if batch == nil || batch.GetCollectorUuid() == "" || len(batch.GetReplicationSession()) != 16 || batch.GetBatchSequence() == 0 {
		return 0, errors.New("invalid replication batch identity")
	}
	committed := batch.GetSpoolThroughId()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.CollectorReplicationReceipt
		err := tx.Where("collector_uuid = ? AND replication_session = ? AND batch_sequence = ?",
			batch.GetCollectorUuid(), batch.GetReplicationSession(), batch.GetBatchSequence()).First(&existing).Error
		if err == nil {
			committed = existing.CommittedSpoolThrough
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		byEventID := make(map[string]hotEventMeta, len(batch.GetEvents()))
		for _, event := range batch.GetEvents() {
			if err := validateEvent(event); err != nil {
				return err
			}
			meta, err := s.acceptEvent(tx, event, receivedAt)
			if err != nil {
				return err
			}
			byEventID[string(event.GetEventId())] = meta
		}
		for _, observation := range batch.GetObservations() {
			if len(observation.GetEventId()) != 16 || observation.GetObserverId() == "" {
				return errors.New("invalid replicated observation")
			}
			meta, ok := byEventID[string(observation.GetEventId())]
			if !ok {
				var row model.TelemetryEvent
				if err := tx.Select("node_uuid", "collected_at", "clock_untrusted", "event_type").First(&row, "event_id = ?", observation.GetEventId()).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						continue
					}
					return fmt.Errorf("replicated observation references unknown event: %w", err)
				}
				meta = hotEventMeta{nodeUUID: row.NodeUUID, collectedAt: row.CollectedAt, clockUntrusted: row.ClockUntrusted, persist: persistTelemetryEvent(pb.TelemetryEventType(row.EventType))}
			}
			if meta.persist {
				row := model.TelemetryObservation{
					EventID: observation.GetEventId(), ObserverID: observation.GetObserverId(), NodeUUID: meta.nodeUUID,
					ReceivedAt: observation.GetReceivedAtUnixNano(), ReplicatedAt: receivedAt.UnixNano(), Metadata: observation.GetMetadata(),
				}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
					return err
				}
			}
			evidenceAt := meta.collectedAt
			if meta.clockUntrusted {
				evidenceAt = observation.GetReceivedAtUnixNano()
			}
			if err := s.recordPathEvidence(tx, meta.nodeUUID, observation.GetObserverId(), evidenceAt, time.Unix(0, observation.GetReceivedAtUnixNano())); err != nil {
				return err
			}
		}
		for _, gap := range batch.GetGaps() {
			if err := validateGap(gap); err != nil {
				return err
			}
			row := model.TelemetryGap{
				GapID: gap.GetGapId(), NodeUUID: gap.GetNodeUuid(), SessionID: gap.GetSessionId(),
				StartSequence: gap.GetStartSequence(), EndSequence: gap.GetEndSequence(), Reason: int32(gap.GetReason()),
				ReplacementEventID: gap.GetReplacementEventId(), CreatedAtUnixNano: gap.GetCreatedAtUnixNano(),
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		for _, health := range batch.GetHealth() {
			bucketSeconds := s.bucketSize
			bucketStart := health.GetSampledAtUnixNano() / bucketSeconds * bucketSeconds
			row := model.ObserverHealthBucket{
				ObserverID: health.GetObserverId(), BucketStart: bucketStart, Healthy: health.GetHealthy(),
				ProcessSession: health.GetProcessSession(), Revision: 1,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "observer_id"}, {Name: "bucket_start"}},
				DoUpdates: clause.AssignmentColumns([]string{"healthy", "process_session", "revision", "updated_at"}),
			}).Create(&row).Error; err != nil {
				return err
			}
			var assignments []model.ObserverAssignment
			if err := tx.Where("observer_id = ? AND valid_from < ? AND (valid_to = 0 OR valid_to > ?)", health.GetObserverId(), bucketStart+bucketSeconds, bucketStart).Find(&assignments).Error; err != nil {
				return err
			}
			for _, assignment := range assignments {
				if err := enqueueAvailability(tx, assignment.NodeUUID, bucketStart, "observer_health"); err != nil {
					return err
				}
			}
		}
		for _, fact := range batch.GetDataLoss() {
			if len(fact.GetFactId()) != 16 || fact.GetCollectorUuid() != batch.GetCollectorUuid() || fact.GetReason() == pb.GapReason_GAP_REASON_UNSPECIFIED {
				return errors.New("invalid collector data loss fact")
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.TelemetryDataLoss{
				FactID: fact.GetFactId(), ComponentID: fact.GetCollectorUuid(), OccurredAt: fact.GetOccurredAtUnixNano(),
				Reason: int32(fact.GetReason()), FirstSpoolID: fact.GetFirstSpoolId(), LastSpoolID: fact.GetLastSpoolId(),
				LostRecords: fact.GetLostRecords(), Detail: fact.GetDetail(),
			}).Error; err != nil {
				return err
			}
		}
		if runtime := batch.GetRuntime(); runtime != nil {
			row := CollectorRuntimeFromProto(batch.GetCollectorUuid(), runtime, receivedAt, true)
			if err := UpsertCollectorRuntime(tx, row, true); err != nil {
				return err
			}
		}
		return tx.Create(&model.CollectorReplicationReceipt{
			CollectorUUID: batch.GetCollectorUuid(), ReplicationSession: batch.GetReplicationSession(),
			BatchSequence: batch.GetBatchSequence(), CommittedSpoolThrough: batch.GetSpoolThroughId(),
		}).Error
	})
	return committed, err
}

func (s *Store) recordPathEvidence(tx *gorm.DB, nodeUUID []byte, observerID string, evidenceAt int64, receivedAt time.Time) error {
	bucketStart := evidenceAt / s.bucketSize * s.bucketSize
	health := model.ObserverHealthBucket{ObserverID: observerID, BucketStart: bucketStart, Healthy: true, Revision: 1}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "observer_id"}, {Name: "bucket_start"}},
		DoUpdates: clause.Assignments(map[string]any{"healthy": true, "updated_at": receivedAt}),
	}).Create(&health).Error; err != nil {
		return err
	}
	path := model.ObserverPathBucket{
		NodeUUID: nodeUUID, ObserverID: observerID, BucketStart: bucketStart,
		Seen: true, LastSeenAt: evidenceAt, Revision: 1,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_uuid"}, {Name: "observer_id"}, {Name: "bucket_start"}},
		DoUpdates: clause.Assignments(map[string]any{
			"seen": true, "last_seen_at": evidenceAt, "updated_at": receivedAt,
		}),
	}).Create(&path).Error; err != nil {
		return err
	}
	return enqueueAvailability(tx, nodeUUID, bucketStart, "observation")
}

func enqueueAvailability(tx *gorm.DB, nodeUUID []byte, bucketStart int64, reason string) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_uuid"}, {Name: "bucket_start"}},
		DoUpdates: clause.Assignments(map[string]any{
			"reason": reason, "updated_at": time.Now(),
		}),
	}).Create(&model.AvailabilityRecomputeQueue{NodeUUID: nodeUUID, BucketStart: bucketStart, Reason: reason}).Error
}

func (s *Store) Ingest(ctx context.Context, batch *pb.TelemetryBatch, observerID string, receivedAt time.Time) (*IngestResult, error) {
	if batch == nil || observerID == "" {
		return nil, errors.New("telemetry batch and observer ID are required")
	}
	result := &IngestResult{}
	maxBySession := make(map[string]uint64)
	sessionIDs := make(map[string][]byte)
	nodeBySession := make(map[string][]byte)
	batchSeq := make(map[string][]uint64)

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, record := range batch.GetRecords() {
			switch body := record.GetRecord().(type) {
			case *pb.TelemetryRecord_Event:
				event := body.Event
				if err := validateEvent(event); err != nil {
					return err
				}
				meta, err := s.acceptEvent(tx, event, receivedAt)
				if err != nil {
					return err
				}
				if meta.persist {
					observation := model.TelemetryObservation{
						EventID:    append([]byte(nil), event.GetEventId()...),
						ObserverID: observerID,
						NodeUUID:   append([]byte(nil), event.GetNodeUuid()...),
						ReceivedAt: receivedAt.UnixNano(),
					}
					if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&observation).Error; err != nil {
						return err
					}
				}
				evidenceAt := event.GetCollectedAtUnixNano()
				if meta.clockUntrusted {
					evidenceAt = receivedAt.UnixNano()
				}
				if err := s.recordPathEvidence(tx, event.GetNodeUuid(), observerID, evidenceAt, receivedAt); err != nil {
					return err
				}
				key := sessionKey(event.GetNodeUuid(), event.GetSessionId())
				if event.GetSequence() > maxBySession[key] {
					maxBySession[key] = event.GetSequence()
				}
				sessionIDs[key] = append([]byte(nil), event.GetSessionId()...)
				nodeBySession[key] = append([]byte(nil), event.GetNodeUuid()...)
				batchSeq[key] = append(batchSeq[key], event.GetSequence())
				if !meta.clockUntrusted && receivedAt.Sub(time.Unix(0, event.GetCollectedAtUnixNano())) <= 30*time.Second {
					result.FreshEvents = append(result.FreshEvents, event)
				}
			case *pb.TelemetryRecord_Gap:
				gap := body.Gap
				if err := validateGap(gap); err != nil {
					return err
				}
				row := model.TelemetryGap{
					GapID:              append([]byte(nil), gap.GetGapId()...),
					NodeUUID:           append([]byte(nil), gap.GetNodeUuid()...),
					SessionID:          append([]byte(nil), gap.GetSessionId()...),
					StartSequence:      gap.GetStartSequence(),
					EndSequence:        gap.GetEndSequence(),
					Reason:             int32(gap.GetReason()),
					ReplacementEventID: append([]byte(nil), gap.GetReplacementEventId()...),
					CreatedAtUnixNano:  gap.GetCreatedAtUnixNano(),
				}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
					return err
				}
				key := sessionKey(gap.GetNodeUuid(), gap.GetSessionId())
				if gap.GetEndSequence() > maxBySession[key] {
					maxBySession[key] = gap.GetEndSequence()
				}
				sessionIDs[key] = append([]byte(nil), gap.GetSessionId()...)
				nodeBySession[key] = append([]byte(nil), gap.GetNodeUuid()...)
				batchSeq[key] = append(batchSeq[key], gap.GetEndSequence())
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
			ack, err := advanceCursor(tx, observerID, nodeBySession[key], sessionIDs[key], maxBySession[key], batchSeq[key], receivedAt)
			if err != nil {
				return err
			}
			result.Acks = append(result.Acks, &pb.SessionAck{NodeUuid: nodeBySession[key], SessionId: sessionIDs[key], AckThrough: ack})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type hotEventMeta struct {
	nodeUUID       []byte
	collectedAt    int64
	clockUntrusted bool
	persist        bool
}

func (s *Store) acceptEvent(tx *gorm.DB, event *pb.TelemetryEvent, receivedAt time.Time) (hotEventMeta, error) {
	clockUntrusted := absDuration(receivedAt.Sub(time.Unix(0, event.GetCollectedAtUnixNano()))) > clockSkewLimit
	meta := hotEventMeta{
		nodeUUID: event.GetNodeUuid(), collectedAt: event.GetCollectedAtUnixNano(),
		clockUntrusted: clockUntrusted, persist: persistTelemetryEvent(event.GetEventType()),
	}
	if event.GetEventType() == pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE && event.GetState() != nil && s.states != nil {
		s.states.Add(event.GetEventId(), event.GetNodeUuid(), event.GetCollectedAtUnixNano(), event.GetState())
	}
	if event.GetEventType() == pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_STATE_ROLLUP && event.GetStateRollup() != nil {
		if err := upsertStateRollup(tx, event.GetNodeUuid(), event.GetStateRollup()); err != nil {
			return meta, err
		}
	}
	if !meta.persist {
		return meta, nil
	}
	encoded, err := proto.Marshal(event)
	if err != nil {
		return meta, err
	}
	row := model.TelemetryEvent{
		EventID: append([]byte(nil), event.GetEventId()...), NodeUUID: append([]byte(nil), event.GetNodeUuid()...),
		SessionID: append([]byte(nil), event.GetSessionId()...), Sequence: event.GetSequence(),
		EventType: int32(event.GetEventType()), Priority: int32(event.GetPriority()),
		CollectedAt: event.GetCollectedAtUnixNano(), AgentUptimeNano: event.GetAgentUptimeNano(),
		SessionElapsedNano: event.GetSessionElapsedNano(), ClockUntrusted: clockUntrusted,
		SourceProtocol: int32(event.GetSourceProtocol()), Reliability: int32(event.GetReliability()),
		Payload: encoded, PayloadRetained: true,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return meta, err
	}
	return meta, nil
}

func upsertStateRollup(tx *gorm.DB, nodeUUID []byte, payload *pb.StateRollupPayload) error {
	if payload == nil || payload.GetWindowStartUnixNano() == 0 {
		return nil
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		return err
	}
	row := model.StateRollup{
		NodeUUID: nodeUUID, Resolution: "1m", WindowStart: payload.GetWindowStartUnixNano(), WindowEnd: payload.GetWindowEndUnixNano(),
		SampleCount: payload.GetSampleCount(), Payload: encoded, NetInTotal: payload.GetNetInTotal(), NetOutTotal: payload.GetNetOutTotal(),
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_uuid"}, {Name: "resolution"}, {Name: "window_start"}},
		DoUpdates: clause.Assignments(map[string]any{
			"window_end": row.WindowEnd, "sample_count": row.SampleCount, "payload": row.Payload,
			"net_in_total": row.NetInTotal, "net_out_total": row.NetOutTotal,
		}),
	}).Create(&row).Error
}

func validateEvent(event *pb.TelemetryEvent) error {
	if event == nil || len(event.GetEventId()) != 16 || len(event.GetNodeUuid()) != 16 || len(event.GetSessionId()) != 16 || event.GetSequence() == 0 {
		return errors.New("invalid telemetry event identity")
	}
	expected, err := EventID(event.GetNodeUuid(), event.GetSessionId(), event.GetSequence())
	if err != nil || !bytes.Equal(expected, event.GetEventId()) {
		return errors.New("telemetry event ID does not match identity tuple")
	}
	if event.GetEventType() == pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_UNSPECIFIED || event.GetPriority() == pb.TelemetryPriority_TELEMETRY_PRIORITY_UNSPECIFIED || event.GetPayload() == nil {
		return errors.New("telemetry event type, priority and payload are required")
	}
	return nil
}

func validateGap(gap *pb.SequenceGap) error {
	if gap == nil || len(gap.GetGapId()) != 16 || len(gap.GetNodeUuid()) != 16 || len(gap.GetSessionId()) != 16 || gap.GetStartSequence() == 0 || gap.GetEndSequence() < gap.GetStartSequence() || gap.GetReason() == pb.GapReason_GAP_REASON_UNSPECIFIED {
		return errors.New("invalid telemetry sequence gap")
	}
	return nil
}

func inferredGapID(nodeUUID, sessionID []byte, start, end uint64) []byte {
	var input [48]byte
	copy(input[:16], nodeUUID)
	copy(input[16:32], sessionID)
	binary.BigEndian.PutUint64(input[32:40], start)
	binary.BigEndian.PutUint64(input[40:48], end)
	sum := sha256.Sum256(input[:])
	return append([]byte(nil), sum[:16]...)
}

func sequenceHoleGrace() time.Duration {
	if singleton.Conf == nil {
		return 300 * time.Second
	}
	seconds := singleton.Conf.Telemetry.SequenceHoleGraceSeconds
	if seconds == 0 {
		return 300 * time.Second
	}
	if seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func gapCovers(gaps []model.TelemetryGap, sequence uint64) bool {
	for _, gap := range gaps {
		if gap.StartSequence <= sequence && gap.EndSequence >= sequence {
			return true
		}
	}
	return false
}

func clearCursorHole(cursor *model.TelemetryIngestCursor) {
	cursor.HoleSequence = 0
	cursor.HoleSince = 0
}

func inferCompactedHole(tx *gorm.DB, cursor *model.TelemetryIngestCursor, receiverID string, nodeUUID, sessionID []byte, start, end uint64, receivedAt time.Time, gaps *[]model.TelemetryGap) error {
	gapID := inferredGapID(nodeUUID, sessionID, start, end)
	row := model.TelemetryGap{
		GapID: gapID, NodeUUID: append([]byte(nil), nodeUUID...), SessionID: append([]byte(nil), sessionID...),
		StartSequence: start, EndSequence: end, Reason: int32(pb.GapReason_GAP_REASON_COMPACTED),
		CreatedAtUnixNano: receivedAt.UnixNano(),
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.TelemetryDataLoss{
		FactID: gapID, ComponentID: receiverID, OccurredAt: receivedAt.UnixNano(),
		Reason: int32(pb.GapReason_GAP_REASON_COMPACTED), FirstSpoolID: start, LastSpoolID: end,
		LostRecords: end - start + 1,
		Detail:      fmt.Sprintf("receiver inferred compacted hole %d-%d", start, end),
	}).Error; err != nil {
		return err
	}
	*gaps = append(*gaps, row)
	cursor.AckThrough = end
	clearCursorHole(cursor)
	return nil
}

func advanceCursor(tx *gorm.DB, receiverID string, nodeUUID, sessionID []byte, maxSequence uint64, batchSequences []uint64, receivedAt time.Time) (uint64, error) {
	var cursor model.TelemetryIngestCursor
	err := tx.Where("receiver_id = ? AND node_uuid = ? AND session_id = ?", receiverID, nodeUUID, sessionID).First(&cursor).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cursor = model.TelemetryIngestCursor{ReceiverID: receiverID, NodeUUID: append([]byte(nil), nodeUUID...), SessionID: append([]byte(nil), sessionID...)}
	}
	if maxSequence <= cursor.AckThrough {
		return cursor.AckThrough, nil
	}
	var sequences []uint64
	if err := tx.Model(&model.TelemetryEvent{}).
		Where("node_uuid = ? AND session_id = ? AND sequence > ? AND sequence <= ?", nodeUUID, sessionID, cursor.AckThrough, maxSequence).
		Pluck("sequence", &sequences).Error; err != nil {
		return 0, err
	}
	present := make(map[uint64]bool, len(sequences)+len(batchSequences))
	for _, sequence := range sequences {
		present[sequence] = true
	}
	for _, sequence := range batchSequences {
		if sequence > cursor.AckThrough && sequence <= maxSequence {
			present[sequence] = true
		}
	}
	var gaps []model.TelemetryGap
	if err := tx.Where("node_uuid = ? AND session_id = ? AND end_sequence > ? AND start_sequence <= ?", nodeUUID, sessionID, cursor.AckThrough, maxSequence).Find(&gaps).Error; err != nil {
		return 0, err
	}
	grace := sequenceHoleGrace()
	for cursor.AckThrough < maxSequence {
		next := cursor.AckThrough + 1
		if present[next] || gapCovers(gaps, next) {
			if present[next] {
				cursor.AckThrough = next
			} else {
				for _, gap := range gaps {
					if gap.StartSequence <= next && gap.EndSequence >= next {
						cursor.AckThrough = gap.EndSequence
						break
					}
				}
			}
			if cursor.HoleSequence != 0 && cursor.AckThrough >= cursor.HoleSequence {
				clearCursorHole(&cursor)
			}
			continue
		}
		if grace <= 0 {
			break
		}
		if cursor.HoleSequence != next {
			cursor.HoleSequence = next
			cursor.HoleSince = receivedAt.UnixNano()
			break
		}
		if receivedAt.UnixNano()-cursor.HoleSince < grace.Nanoseconds() {
			break
		}
		end := next
		for end < maxSequence && !present[end+1] && !gapCovers(gaps, end+1) {
			end++
		}
		if err := inferCompactedHole(tx, &cursor, receiverID, nodeUUID, sessionID, next, end, receivedAt, &gaps); err != nil {
			return 0, err
		}
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "receiver_id"}, {Name: "node_uuid"}, {Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"ack_through", "hole_sequence", "hole_since", "updated_at"}),
	}).Create(&cursor).Error; err != nil {
		return 0, err
	}
	return cursor.AckThrough, nil
}

func sessionKey(nodeUUID, sessionID []byte) string {
	return fmt.Sprintf("%x/%x", nodeUUID, sessionID)
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
