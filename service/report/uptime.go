package report

import (
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"gorm.io/gorm"
)

const uptimeLookback = time.Hour

// CollectUptime 按已结束的可用性桶计算可用率：
// (full + partial) / (full + partial + unavailable)。unknown 与空隙不进分母。
// 没有绑定或分母为 0 时 HasData 为 false，不把缺数据当成 100%。
func CollectUptime(hosts []HostRow, start, end time.Time) []UptimeRow {
	now := time.Now()
	if singleton.Loc != nil {
		now = now.In(singleton.Loc)
	}
	return collectUptime(singleton.DB, hosts, start, end, now)
}

// FillUptimeLongest 只给当前要展示的行补最长不可用段。
func FillUptimeLongest(rows []UptimeRow, start, end time.Time) {
	now := time.Now()
	if singleton.Loc != nil {
		now = now.In(singleton.Loc)
	}
	fillUptimeLongest(singleton.DB, rows, start, end, now)
}

func collectUptime(db *gorm.DB, hosts []HostRow, start, end time.Time, now time.Time) []UptimeRow {
	if !end.After(start) {
		return nil
	}
	out := make([]UptimeRow, 0, len(hosts))
	if db == nil {
		for _, host := range hosts {
			out = append(out, UptimeRow{ServerID: host.ID, Name: host.Name})
		}
		sortUptime(out)
		return out
	}
	if end.After(now) {
		end = now
	}
	bindings, err := CurrentBindings(db)
	if err != nil {
		bindings = map[uint64][]byte{}
	}
	type nodeHost struct {
		index int
		node  []byte
	}
	byNode := map[string]nodeHost{}
	nodes := make([][]byte, 0, len(hosts))
	for _, host := range hosts {
		row := UptimeRow{ServerID: host.ID, Name: host.Name}
		node := host.NodeUUID
		if len(node) == 0 {
			node = bindings[host.ID]
		}
		out = append(out, row)
		if len(node) == 0 {
			continue
		}
		key := string(node)
		if _, ok := byNode[key]; ok {
			continue
		}
		byNode[key] = nodeHost{index: len(out) - 1, node: append([]byte(nil), node...)}
		nodes = append(nodes, byNode[key].node)
	}
	sums := sumAvailability(db, nodes, start, end, now)
	for key, item := range byNode {
		agg := sums[key]
		up := agg[model.ConnectivityFull] + agg[model.ConnectivityPartial]
		down := agg[model.ConnectivityUnavailable]
		if up+down <= 0 {
			continue
		}
		row := &out[item.index]
		row.HasData = true
		row.Percent = singleton.FormatAvailabilityPercent(float64(up) / float64(up+down) * 100)
		row.OfflineSec = uint64(down / int64(time.Second))
	}
	sortUptime(out)
	return out
}

func sortUptime(out []UptimeRow) {
	// 有数据的按可用率从低到高；没数据的排在后面，避免 0 被当成最差。
	for i := 1; i < len(out); i++ {
		row := out[i]
		j := i
		for j > 0 && uptimeLess(row, out[j-1]) {
			out[j] = out[j-1]
			j--
		}
		out[j] = row
	}
}

func uptimeLess(a, b UptimeRow) bool {
	if a.HasData != b.HasData {
		return a.HasData
	}
	if a.Percent != b.Percent {
		return a.Percent < b.Percent
	}
	return a.Name < b.Name
}

type availabilitySum map[string]int64

func sumAvailability(db *gorm.DB, nodes [][]byte, start, end, now time.Time) map[string]availabilitySum {
	out := map[string]availabilitySum{}
	if db == nil || len(nodes) == 0 || !end.After(start) {
		return out
	}
	clipStart := start.UnixNano()
	clipEnd := end.UnixNano()
	nowNs := now.UnixNano()
	lookback := start.Add(-uptimeLookback).UnixNano()
	fallback := int64(30 * time.Second)
	const chunk = 400
	for from := 0; from < len(nodes); from += chunk {
		to := from + chunk
		if to > len(nodes) {
			to = len(nodes)
		}
		var rows []struct {
			NodeUUID          []byte `gorm:"column:node_uuid"`
			ConnectivityState string `gorm:"column:connectivity_state"`
			SpanNs            int64  `gorm:"column:span_ns"`
		}
		err := db.Raw(`
SELECT node_uuid, connectivity_state,
  SUM(MAX(0, MIN(eff_end, ?) - MAX(bucket_start, ?))) AS span_ns
FROM (
  SELECT node_uuid, connectivity_state, bucket_start,
    CASE WHEN window_end > bucket_start THEN window_end ELSE bucket_start + ? END AS eff_end
  FROM availability_buckets
  WHERE node_uuid IN ?
    AND bucket_start < ?
    AND bucket_start >= ?
    AND (window_end = 0 OR window_end <= ?)
) AS spans
WHERE eff_end > ?
GROUP BY node_uuid, connectivity_state
`, clipEnd, clipStart, fallback, nodes[from:to], clipEnd, lookback, nowNs, clipStart).Scan(&rows).Error
		if err != nil {
			continue
		}
		for _, row := range rows {
			if row.SpanNs <= 0 {
				continue
			}
			key := string(row.NodeUUID)
			if out[key] == nil {
				out[key] = availabilitySum{}
			}
			out[key][row.ConnectivityState] += row.SpanNs
		}
	}
	return out
}

func fillUptimeLongest(db *gorm.DB, rows []UptimeRow, start, end, now time.Time) {
	if db == nil || len(rows) == 0 || !end.After(start) {
		return
	}
	if end.After(now) {
		end = now
	}
	bindings, err := CurrentBindings(db)
	if err != nil {
		return
	}
	ids := map[string][]int{}
	nodes := make([][]byte, 0, len(rows))
	for i := range rows {
		if !rows[i].HasData {
			continue
		}
		node := bindings[rows[i].ServerID]
		if len(node) == 0 {
			continue
		}
		key := string(node)
		if _, ok := ids[key]; !ok {
			nodes = append(nodes, append([]byte(nil), node...))
		}
		ids[key] = append(ids[key], i)
	}
	if len(nodes) == 0 {
		return
	}
	spans := loadAvailabilitySpans(db, nodes, start, end, now)
	clipStart := start.UnixNano()
	clipEnd := end.UnixNano()
	for key, indexes := range ids {
		longest := longestUnavailable(spans[key], clipStart, clipEnd)
		sec := uint64(longest / int64(time.Second))
		for _, idx := range indexes {
			rows[idx].LongestSec = sec
		}
	}
}

type availabilitySpan struct {
	start, end   int64
	connectivity string
}

func loadAvailabilitySpans(db *gorm.DB, nodes [][]byte, start, end, now time.Time) map[string][]availabilitySpan {
	out := map[string][]availabilitySpan{}
	clipEnd := end.UnixNano()
	nowNs := now.UnixNano()
	lookback := start.Add(-uptimeLookback).UnixNano()
	fallback := int64(30 * time.Second)
	var rows []struct {
		NodeUUID          []byte `gorm:"column:node_uuid"`
		BucketStart       int64  `gorm:"column:bucket_start"`
		WindowEnd         int64  `gorm:"column:window_end"`
		ConnectivityState string `gorm:"column:connectivity_state"`
	}
	err := db.Raw(`
SELECT node_uuid, bucket_start, window_end, connectivity_state
FROM availability_buckets
WHERE node_uuid IN ?
  AND bucket_start < ?
  AND bucket_start >= ?
  AND (window_end = 0 OR window_end <= ?)
ORDER BY bucket_start ASC
`, nodes, clipEnd, lookback, nowNs).Scan(&rows).Error
	if err != nil {
		return out
	}
	for _, row := range rows {
		eff := row.WindowEnd
		if eff <= row.BucketStart {
			eff = row.BucketStart + fallback
		}
		key := string(row.NodeUUID)
		out[key] = append(out[key], availabilitySpan{start: row.BucketStart, end: eff, connectivity: row.ConnectivityState})
	}
	return out
}

func longestUnavailable(spans []availabilitySpan, clipStart, clipEnd int64) int64 {
	var longest int64
	inRun := false
	var runStart, runEnd int64
	closeRun := func() {
		if !inRun {
			return
		}
		if d := runEnd - runStart; d > longest {
			longest = d
		}
		inRun = false
	}
	for _, span := range spans {
		cs := span.start
		if cs < clipStart {
			cs = clipStart
		}
		ce := span.end
		if ce > clipEnd {
			ce = clipEnd
		}
		if ce <= cs || span.connectivity != model.ConnectivityUnavailable {
			closeRun()
			continue
		}
		if !inRun {
			runStart, runEnd, inRun = cs, ce, true
			continue
		}
		if cs <= runEnd {
			if ce > runEnd {
				runEnd = ce
			}
			continue
		}
		closeRun()
		runStart, runEnd, inRun = cs, ce, true
	}
	closeRun()
	return longest
}
