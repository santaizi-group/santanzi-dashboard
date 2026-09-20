package traffic

import (
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"gorm.io/gorm"
)

const MaxUsageLookback = maxDailyLookback

type Bytes struct {
	In    uint64
	Out   uint64
	Total uint64
}

func RangeUsage(db *gorm.DB, node []byte, from, to time.Time, direction string) (uint64, error) {
	if db == nil || len(node) == 0 {
		return 0, nil
	}
	if direction == "" {
		direction = model.TrafficDirectionTotal
	}
	return sumRollupUsage(db, node, direction, from, to)
}

func RangeUsageSplit(db *gorm.DB, node []byte, from, to time.Time) (Bytes, error) {
	var out Bytes
	if db == nil || len(node) == 0 {
		return out, nil
	}
	in, err := sumRollupUsage(db, node, model.TrafficDirectionInbound, from, to)
	if err != nil {
		return out, err
	}
	outbound, err := sumRollupUsage(db, node, model.TrafficDirectionOutbound, from, to)
	if err != nil {
		return out, err
	}
	out.In = in
	out.Out = outbound
	out.Total = addBytes(in, outbound)
	return out, nil
}

type nodeSum struct {
	NodeUUID []byte
	NetIn    uint64 `gorm:"column:net_in"`
	NetOut   uint64 `gorm:"column:net_out"`
}

func RangeUsageMany(db *gorm.DB, nodes [][]byte, from, to time.Time) (map[string]Bytes, error) {
	result := make(map[string]Bytes, len(nodes))
	if db == nil || len(nodes) == 0 || !to.After(from) {
		return result, nil
	}
	for _, node := range nodes {
		if len(node) == 0 {
			continue
		}
		result[string(node)] = Bytes{}
	}
	firstFullHour := from.Truncate(time.Hour)
	if firstFullHour.Before(from) {
		firstFullHour = firstFullHour.Add(time.Hour)
	}
	lastFullHourEnd := to.Truncate(time.Hour)
	if firstFullHour.Before(lastFullHourEnd) {
		var rows []nodeSum
		if err := db.Model(&model.StateRollup{}).
			Select("node_uuid, COALESCE(SUM(net_in_total), 0) as net_in, COALESCE(SUM(net_out_total), 0) as net_out").
			Where("node_uuid IN ? AND resolution = ? AND window_start >= ? AND window_start < ?",
				nodes, "1h", firstFullHour.UnixNano(), lastFullHourEnd.UnixNano()).
			Group("node_uuid").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			cur := result[string(row.NodeUUID)]
			cur.In = addBytes(cur.In, row.NetIn)
			cur.Out = addBytes(cur.Out, row.NetOut)
			result[string(row.NodeUUID)] = cur
		}
		if err := addMinuteSums(db, nodes, from, firstFullHour, result); err != nil {
			return nil, err
		}
		if err := addMinuteSums(db, nodes, lastFullHourEnd, to, result); err != nil {
			return nil, err
		}
	} else if err := addMinuteSums(db, nodes, from, to, result); err != nil {
		return nil, err
	}
	for key, item := range result {
		item.Total = addBytes(item.In, item.Out)
		result[key] = item
	}
	return result, nil
}

func addMinuteSums(db *gorm.DB, nodes [][]byte, start, end time.Time, result map[string]Bytes) error {
	if !end.After(start) {
		return nil
	}
	var rows []nodeSum
	if err := db.Model(&model.StateRollup{}).
		Select("node_uuid, COALESCE(SUM(net_in_total), 0) as net_in, COALESCE(SUM(net_out_total), 0) as net_out").
		Where("node_uuid IN ? AND resolution = ? AND window_start >= ? AND window_start < ?",
			nodes, "1m", start.UnixNano(), end.UnixNano()).
		Group("node_uuid").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		cur := result[string(row.NodeUUID)]
		cur.In = addBytes(cur.In, row.NetIn)
		cur.Out = addBytes(cur.Out, row.NetOut)
		result[string(row.NodeUUID)] = cur
	}
	return nil
}
