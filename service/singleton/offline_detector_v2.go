package singleton

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
)

const v2Protocol = "v2"

type v2Consensus struct {
	latestOffline bool
	since         time.Time
	lastSeen      time.Time
	summary       []byte
	duration      time.Duration
}

type v2ObserverEvidence struct {
	ObserverID string `json:"observer_id"`
	Healthy    bool   `json:"healthy"`
	Seen       bool   `json:"seen"`
}

func availabilityBucketDuration() time.Duration {
	sec := uint64(30)
	if Conf != nil && Conf.Telemetry.AvailabilityBucketSeconds > 0 {
		sec = Conf.Telemetry.AvailabilityBucketSeconds
	}
	return time.Duration(sec) * time.Second
}

func v2OfflineThreshold(threshold time.Duration) time.Duration {
	bucket := availabilityBucketDuration()
	if threshold < bucket {
		return bucket
	}
	return threshold
}

func detectV2OfflineServers(now time.Time, threshold time.Duration, batch *[]offlineNotice) {
	if DB == nil {
		return
	}
	var runtimes []model.ServerRuntime
	if err := DB.Where("protocol = ?", v2Protocol).Find(&runtimes).Error; err != nil {
		log.Printf("SANTAIZI>> V2 离线检测查询失败: %v", err)
		return
	}
	threshold = v2OfflineThreshold(threshold)
	bucket := availabilityBucketDuration()
	limit := int(threshold/bucket) + 2
	if limit < 3 {
		limit = 3
	}
	for i := range runtimes {
		rt := &runtimes[i]
		if rt.Status == model.ServerRuntimeStatusRecovering || len(rt.CurrentNodeUUID) != 16 {
			continue
		}
		verdict := inspectV2Consensus(rt.CurrentNodeUUID, now, bucket, limit)
		if verdict.latestOffline {
			if rt.CurrentOfflineID == 0 && verdict.duration >= threshold {
				if history := createV2OfflineHistory(rt, now, threshold); history != nil {
					*batch = append(*batch, offlineNotice{serverID: rt.ServerID, history: history})
				}
			}
			continue
		}
		if rt.CurrentOfflineID != 0 {
			CloseOfflineHistory(rt, nil, nil, now)
		}
	}
}

func inspectV2Consensus(nodeUUID []byte, now time.Time, bucket time.Duration, limit int) v2Consensus {
	// 只在已完结桶上做共识：进行中的桶 seen=0 不代表失联。
	var buckets []model.AvailabilityBucket
	if err := DB.Where("node_uuid = ? AND window_end <= ?", nodeUUID, now.UnixNano()).Order("bucket_start DESC").Limit(limit).Find(&buckets).Error; err != nil {
		log.Printf("SANTAIZI>> V2 观测桶读取失败: %v", err)
		return v2Consensus{}
	}
	if len(buckets) == 0 || buckets[0].HostState != model.HostStateOffline {
		return v2Consensus{}
	}
	sinceNano := buckets[0].BucketStart
	summary := buckets[0].ObserverSummary
	for _, row := range buckets {
		if row.HostState != model.HostStateOffline {
			break
		}
		sinceNano = row.BucketStart
	}
	since := time.Unix(0, sinceNano)
	return v2Consensus{
		latestOffline: true,
		since:         since,
		lastSeen:      lastSeenBeforeBucket(nodeUUID, sinceNano, bucket),
		summary:       summary,
		duration:      now.Sub(since),
	}
}

func lastSeenBeforeBucket(nodeUUID []byte, sinceNano int64, bucket time.Duration) time.Time {
	var max int64
	prevStart := sinceNano - int64(bucket)
	if err := DB.Model(&model.ObserverPathBucket{}).
		Where("node_uuid = ? AND bucket_start = ? AND seen = ?", nodeUUID, prevStart, true).
		Select("MAX(last_seen_at)").Scan(&max).Error; err != nil {
		log.Printf("SANTAIZI>> V2 最后上报查询失败: %v", err)
	}
	if max > 0 {
		return time.Unix(0, max)
	}
	if err := DB.Model(&model.ObserverPathBucket{}).
		Where("node_uuid = ? AND bucket_start < ? AND seen = ?", nodeUUID, sinceNano, true).
		Select("MAX(last_seen_at)").Scan(&max).Error; err != nil {
		log.Printf("SANTAIZI>> V2 最后上报回退查询失败: %v", err)
	}
	if max > 0 {
		return time.Unix(0, max)
	}
	return time.Unix(0, sinceNano)
}

func createV2OfflineHistory(rt *model.ServerRuntime, now time.Time, threshold time.Duration) *model.ServerOfflineHistory {
	serverRuntimeMu.Lock()
	defer serverRuntimeMu.Unlock()
	return createV2OfflineHistoryTx(rt, now, threshold)
}

func createV2OfflineHistoryTx(rt *model.ServerRuntime, now time.Time, threshold time.Duration) *model.ServerOfflineHistory {
	tx := DB.Begin()
	var current model.ServerRuntime
	if err := tx.First(&current, rt.ServerID).Error; err != nil {
		tx.Rollback()
		return nil
	}
	if current.Protocol != v2Protocol || current.CurrentOfflineID != 0 || current.Status == model.ServerRuntimeStatusRecovering || len(current.CurrentNodeUUID) != 16 {
		tx.Rollback()
		return nil
	}
	bucket := availabilityBucketDuration()
	limit := int(threshold/bucket) + 2
	if limit < 3 {
		limit = 3
	}
	fresh := inspectV2Consensus(current.CurrentNodeUUID, now, bucket, limit)
	if !fresh.latestOffline || fresh.duration < threshold {
		tx.Rollback()
		return nil
	}
	lastSeen := fresh.lastSeen
	if lastSeen.IsZero() {
		lastSeen = fresh.since
	}
	history := model.ServerOfflineHistory{
		ServerID:         current.ServerID,
		StartedAt:        fresh.since,
		DetectedAt:       now,
		Status:           model.OfflineHistoryStatusOpen,
		Reason:           model.OfflineReasonUnknown,
		ThresholdSeconds: uint64(threshold.Seconds()),
		LastSeenAt:       lastSeen,
		LastBootTime:     current.LastBootTime,
		LastUptime:       current.LastUptime,
		LastIP:           current.LastIP,
	}
	if err := tx.Create(&history).Error; err != nil {
		tx.Rollback()
		log.Printf("SANTAIZI>> 创建 V2 离线历史失败: %v", err)
		return nil
	}
	current.Status = model.ServerRuntimeStatusOffline
	current.LastOfflineAt = &now
	current.CurrentOfflineID = history.ID
	if err := tx.Save(&current).Error; err != nil {
		tx.Rollback()
		log.Printf("SANTAIZI>> 更新 V2 运行态为离线失败: %v", err)
		return nil
	}
	if err := tx.Commit().Error; err != nil {
		log.Printf("SANTAIZI>> 提交 V2 离线历史事务失败: %v", err)
		return nil
	}
	return &history
}

func v2ObserverLine(serverID uint64) string {
	if DB == nil {
		return ""
	}
	var rt model.ServerRuntime
	if err := DB.First(&rt, serverID).Error; err != nil || rt.Protocol != v2Protocol || len(rt.CurrentNodeUUID) != 16 {
		return ""
	}
	var bucket model.AvailabilityBucket
	if err := DB.Where("node_uuid = ? AND host_state = ? AND window_end <= ?", rt.CurrentNodeUUID, model.HostStateOffline, time.Now().UnixNano()).
		Order("bucket_start DESC").First(&bucket).Error; err != nil {
		return ""
	}
	names := observerDisplayNames(bucket.ObserverSummary)
	if len(names) == 0 {
		return ""
	}
	return "观测点：" + strings.Join(names, "、")
}

func observerDisplayNames(summary []byte) []string {
	if len(summary) == 0 {
		return nil
	}
	var rows []v2ObserverEvidence
	if json.Unmarshal(summary, &rows) != nil || len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ObserverID != "" && row.ObserverID != primaryObserverID {
			ids = append(ids, row.ObserverID)
		}
	}
	namesByID := map[string]string{}
	if len(ids) > 0 {
		var collectors []model.Collector
		if err := DB.Select("collector_uuid", "name").Where("collector_uuid IN ?", ids).Find(&collectors).Error; err == nil {
			for _, collector := range collectors {
				namesByID[collector.CollectorUUID] = collector.Name
			}
		}
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ObserverID == "" {
			continue
		}
		if row.ObserverID == primaryObserverID {
			out = append(out, "主面板")
			continue
		}
		if name := strings.TrimSpace(namesByID[row.ObserverID]); name != "" {
			out = append(out, name)
			continue
		}
		id := row.ObserverID
		if len(id) > 8 {
			id = id[:8]
		}
		out = append(out, id)
	}
	return out
}
