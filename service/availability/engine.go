package availability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Engine struct {
	db         *gorm.DB
	bucketSize int64
	minimum    uint32
}

func NewEngine(db *gorm.DB, bucketDuration time.Duration, minimumObservers uint32) *Engine {
	if bucketDuration <= 0 {
		bucketDuration = 30 * time.Second
	}
	if minimumObservers == 0 {
		minimumObservers = 1
	}
	return &Engine{db: db, bucketSize: int64(bucketDuration), minimum: minimumObservers}
}

func (e *Engine) Run(ctx context.Context) {
	processTicker := time.NewTicker(2 * time.Second)
	healthTicker := time.NewTicker(time.Duration(e.bucketSize))
	defer processTicker.Stop()
	defer healthTicker.Stop()
	_ = e.RecordPrimaryHealth(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-processTicker.C:
			_ = e.ProcessQueue(ctx, 200)
		case sampledAt := <-healthTicker.C:
			_ = e.RecordPrimaryHealth(ctx, sampledAt)
		}
	}
}

func (e *Engine) RecordPrimaryHealth(ctx context.Context, sampledAt time.Time) error {
	bucketStart := sampledAt.UnixNano() / e.bucketSize * e.bucketSize
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := model.ObserverHealthBucket{ObserverID: "primary", BucketStart: bucketStart, Healthy: true, ProcessSession: "primary", Revision: 1}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "observer_id"}, {Name: "bucket_start"}},
			DoUpdates: clause.Assignments(map[string]any{"healthy": true, "updated_at": sampledAt}),
		}).Create(&row).Error; err != nil {
			return err
		}
		var assignments []model.ObserverAssignment
		if err := tx.Where("observer_id = ? AND valid_from < ? AND (valid_to = 0 OR valid_to > ?)", "primary", bucketStart+e.bucketSize, bucketStart).Find(&assignments).Error; err != nil {
			return err
		}
		for _, assignment := range assignments {
			// 当前桶随健康行即时重算；上一桶此刻已封窗，这是它获得终结重算的机会，
			// 否则证据只落在上一桶的节点会一直停在 unknown。
			for _, bucket := range []int64{bucketStart, bucketStart - e.bucketSize} {
				if bucket <= 0 {
					continue
				}
				queue := model.AvailabilityRecomputeQueue{NodeUUID: assignment.NodeUUID, BucketStart: bucket, Reason: "primary_health"}
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "node_uuid"}, {Name: "bucket_start"}},
					DoUpdates: clause.Assignments(map[string]any{"reason": queue.Reason, "updated_at": sampledAt}),
				}).Create(&queue).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (e *Engine) ProcessQueue(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 200
	}
	var queued []model.AvailabilityRecomputeQueue
	if err := e.db.WithContext(ctx).Order("bucket_start ASC").Limit(limit).Find(&queued).Error; err != nil {
		return err
	}
	for _, item := range queued {
		if err := e.Recompute(ctx, item.NodeUUID, item.BucketStart, time.Now()); err != nil {
			return err
		}
		if err := e.db.WithContext(ctx).Delete(&model.AvailabilityRecomputeQueue{}, "node_uuid = ? AND bucket_start = ?", item.NodeUUID, item.BucketStart).Error; err != nil {
			return err
		}
	}
	return nil
}

type observerEvidence struct {
	ObserverID string `json:"observer_id"`
	Healthy    bool   `json:"healthy"`
	Seen       bool   `json:"seen"`
}

func (e *Engine) Recompute(ctx context.Context, nodeUUID []byte, bucketStart int64, recalculatedAt time.Time) error {
	if len(nodeUUID) != 16 || bucketStart <= 0 {
		return errors.New("invalid availability bucket identity")
	}
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var assignments []model.ObserverAssignment
		if err := tx.Where("node_uuid = ? AND valid_from < ? AND (valid_to = 0 OR valid_to > ?)", nodeUUID, bucketStart+e.bucketSize, bucketStart).Find(&assignments).Error; err != nil {
			return err
		}
		expectedSet := make(map[string]bool)
		for _, assignment := range assignments {
			expectedSet[assignment.ObserverID] = true
		}
		observers := make([]string, 0, len(expectedSet))
		for observerID := range expectedSet {
			observers = append(observers, observerID)
		}
		sort.Strings(observers)
		var healthRows []model.ObserverHealthBucket
		if len(observers) > 0 {
			if err := tx.Where("bucket_start = ? AND observer_id IN ?", bucketStart, observers).Find(&healthRows).Error; err != nil {
				return err
			}
		}
		healthySet := make(map[string]bool)
		for _, health := range healthRows {
			healthySet[health.ObserverID] = health.Healthy
		}
		var paths []model.ObserverPathBucket
		if len(observers) > 0 {
			if err := tx.Where("node_uuid = ? AND bucket_start = ? AND observer_id IN ?", nodeUUID, bucketStart, observers).Find(&paths).Error; err != nil {
				return err
			}
		}
		seenSet := make(map[string]bool)
		for _, path := range paths {
			seenSet[path.ObserverID] = path.Seen
		}
		evidence := make([]observerEvidence, 0, len(observers))
		var healthy, seen uint32
		for _, observerID := range observers {
			isHealthy := healthySet[observerID]
			isSeen := isHealthy && seenSet[observerID]
			if isHealthy {
				healthy++
			}
			if isSeen {
				seen++
			}
			evidence = append(evidence, observerEvidence{ObserverID: observerID, Healthy: isHealthy, Seen: isSeen})
		}
		// 窗口未结束不得判离线：健康行常先于证据落桶（健康 ticker 每 30s 为全部节点入队重算），
		// 桶刚开头时 seen=0 只是「尚未看到」而不是「确认失联」。
		windowClosed := bucketStart+e.bucketSize <= recalculatedAt.UnixNano()
		hostState, connectivity := classify(healthy, seen, e.minimum, windowClosed)
		summary, _ := json.Marshal(evidence)
		var existing model.AvailabilityBucket
		err := tx.Where("node_uuid = ? AND bucket_start = ?", nodeUUID, bucketStart).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && existing.Resolution == model.AvailabilityResolutionSpan {
			return nil
		}
		var covering model.AvailabilityBucket
		coverErr := tx.Where("node_uuid = ? AND resolution = ? AND bucket_start <= ? AND window_end > ?",
			nodeUUID, model.AvailabilityResolutionSpan, bucketStart, bucketStart).First(&covering).Error
		if coverErr == nil {
			return nil
		}
		if coverErr != nil && !errors.Is(coverErr, gorm.ErrRecordNotFound) {
			return coverErr
		}
		revision := uint64(1)
		changed := true
		if err == nil {
			revision = existing.Revision
			changed = existing.HostState != hostState || existing.ConnectivityState != connectivity || existing.ExpectedObservers != uint32(len(observers)) || existing.HealthyObservers != healthy || existing.SeenObservers != seen || !bytes.Equal(existing.ObserverSummary, summary)
			if changed {
				revision++
			}
		}
		row := model.AvailabilityBucket{
			NodeUUID: nodeUUID, BucketStart: bucketStart, WindowEnd: bucketStart + e.bucketSize,
			Resolution: model.AvailabilityResolutionRaw, HostState: hostState, ConnectivityState: connectivity,
			ExpectedObservers: uint32(len(observers)), HealthyObservers: healthy, SeenObservers: seen,
			ObserverSummary: summary, Revision: revision, Finalized: bucketStart+e.bucketSize < recalculatedAt.Add(-time.Duration(e.bucketSize)).UnixNano(),
			RecalculatedAt: recalculatedAt.UnixNano(),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_uuid"}, {Name: "bucket_start"}}, UpdateAll: true,
		}).Create(&row).Error; err != nil {
			return err
		}
		if changed {
			return reviseIncident(tx, row, e.bucketSize, recalculatedAt)
		}
		return nil
	})
}

func classify(healthy, seen, minimum uint32, windowClosed bool) (string, string) {
	host := model.HostStateUnknown
	connectivity := model.ConnectivityUnknown
	if seen > 0 {
		host = model.HostStateOnline
		if seen == healthy {
			connectivity = model.ConnectivityFull
		} else {
			connectivity = model.ConnectivityPartial
		}
	} else if windowClosed && healthy >= minimum {
		host = model.HostStateOffline
		connectivity = model.ConnectivityUnavailable
	}
	return host, connectivity
}

func incidentClassification(bucket model.AvailabilityBucket) string {
	switch {
	case bucket.HostState == model.HostStateOffline:
		return "HOST_OFFLINE"
	case bucket.ConnectivityState == model.ConnectivityPartial:
		return "CONNECTIVITY_DEGRADED"
	case bucket.HostState == model.HostStateUnknown:
		return "EVIDENCE_UNKNOWN"
	default:
		return "HEALTHY"
	}
}

func reviseIncident(tx *gorm.DB, bucket model.AvailabilityBucket, bucketSize int64, recalculatedAt time.Time) error {
	classification := incidentClassification(bucket)
	bucketEnd := bucket.WindowEnd
	if bucketEnd <= bucket.BucketStart {
		bucketEnd = bucket.BucketStart + bucketSize
	}
	incident, found, err := lookupIncident(tx, bucket.NodeUUID, bucket.BucketStart, bucketEnd)
	if err != nil {
		return err
	}
	// HEALTHY 与 EVIDENCE_UNKNOWN 都不是事件：unknown（含窗口未完结的空证据桶）
	// 只关闭仍开着的事件，绝不新开，否则每个新桶都会制造一张无人消费的事件行。
	if classification == "HEALTHY" || classification == "EVIDENCE_UNKNOWN" {
		if !found {
			return nil
		}
		if incident.StartedAt == bucket.BucketStart {
			return applyIncidentUpdate(tx, incident, classification, bucket, bucketEnd, recalculatedAt)
		}
		if incident.EndedAt == 0 || incident.EndedAt > bucket.BucketStart {
			incident.EndedAt = bucket.BucketStart
			incident.RecalculatedAt = recalculatedAt.UnixNano()
			return tx.Save(&incident).Error
		}
		return nil
	}
	if !found {
		incident, found, err = lookupExtendableIncident(tx, bucket.NodeUUID, bucket.BucketStart)
		if err != nil {
			return err
		}
	}
	if !found {
		return createIncident(tx, bucket, classification, recalculatedAt)
	}
	return applyIncidentUpdate(tx, incident, classification, bucket, 0, recalculatedAt)
}

func lookupIncident(tx *gorm.DB, nodeUUID []byte, bucketStart, bucketEnd int64) (model.AvailabilityIncident, bool, error) {
	incident, err := findIncident(tx, "node_uuid = ? AND started_at = ?", nodeUUID, bucketStart)
	if err != nil || incident.ID != 0 {
		return incident, incident.ID != 0, err
	}
	incident, err = findIncident(tx, "node_uuid = ? AND started_at < ? AND (ended_at = 0 OR ended_at >= ?)", nodeUUID, bucketStart, bucketEnd)
	return incident, incident.ID != 0, err
}

func lookupExtendableIncident(tx *gorm.DB, nodeUUID []byte, bucketStart int64) (model.AvailabilityIncident, bool, error) {
	incident, err := findIncident(tx, "node_uuid = ? AND ended_at = 0", nodeUUID)
	if err != nil || incident.ID != 0 {
		return incident, incident.ID != 0, err
	}
	incident, err = findIncident(tx, "node_uuid = ? AND ended_at = ? AND current_classification != ?", nodeUUID, bucketStart, "HEALTHY")
	return incident, incident.ID != 0, err
}

func findIncident(tx *gorm.DB, query string, args ...any) (model.AvailabilityIncident, error) {
	var incident model.AvailabilityIncident
	err := tx.Where(query, args...).Order("started_at DESC").Limit(1).Find(&incident).Error
	return incident, err
}

func createIncident(tx *gorm.DB, bucket model.AvailabilityBucket, classification string, recalculatedAt time.Time) error {
	incident := model.AvailabilityIncident{
		NodeUUID: bucket.NodeUUID, InitialClassification: classification, CurrentClassification: classification,
		Revision: 1, StartedAt: bucket.BucketStart, EndedAt: 0,
		RecalculatedAt: recalculatedAt.UnixNano(), Reason: "availability evidence", ObserverEvidence: bucket.ObserverSummary,
	}
	if err := tx.Create(&incident).Error; err != nil {
		return err
	}
	return tx.Create(&model.IncidentRevision{
		IncidentID: incident.ID, Revision: 1, Classification: classification, Reason: incident.Reason,
		Evidence: bucket.ObserverSummary, RecalculatedAt: recalculatedAt.UnixNano(),
	}).Error
}

func applyIncidentUpdate(tx *gorm.DB, incident model.AvailabilityIncident, classification string, bucket model.AvailabilityBucket, endedAt int64, recalculatedAt time.Time) error {
	sameClass := incident.CurrentClassification == classification
	sameEvidence := bytes.Equal(incident.ObserverEvidence, bucket.ObserverSummary)
	if sameClass && sameEvidence && incident.EndedAt == endedAt {
		return nil
	}
	if sameClass && sameEvidence {
		incident.EndedAt = endedAt
		incident.RecalculatedAt = recalculatedAt.UnixNano()
		return tx.Save(&incident).Error
	}
	incident.Revision++
	incident.CurrentClassification = classification
	incident.EndedAt = endedAt
	incident.RecalculatedAt = recalculatedAt.UnixNano()
	incident.ObserverEvidence = bucket.ObserverSummary
	if err := tx.Save(&incident).Error; err != nil {
		return err
	}
	reason := "late evidence correction"
	if sameClass {
		reason = "availability evidence"
	}
	return tx.Create(&model.IncidentRevision{
		IncidentID: incident.ID, Revision: incident.Revision, Classification: classification,
		Reason: reason, Evidence: bucket.ObserverSummary, RecalculatedAt: recalculatedAt.UnixNano(),
	}).Error
}
