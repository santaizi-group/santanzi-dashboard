package report

import (
	"encoding/hex"
	"errors"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	trafficservice "github.com/hi2shark/santaizi-dashboard/service/traffic"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

const (
	MaxStatsLookback   = 30 * 24 * time.Hour
	MaxTrafficLookback = 90 * 24 * time.Hour
)

var (
	ErrRangeTooLarge = errors.New("查询跨度超过上限")
	ErrRangeInvalid  = errors.New("时间范围无效")
)

type NodeStats struct {
	CPUMin, CPUAvg, CPUMax                float64
	MemUsedMin, MemUsedAvg, MemUsedMax    uint64
	DiskUsedMin, DiskUsedAvg, DiskUsedMax uint64
	LoadMin, LoadAvg, LoadMax             float64
	NetInSpeedAvg, NetOutSpeedAvg         uint64
	In, Out, Total                        uint64
	Samples                               uint32
}

type SeriesPoint struct {
	Start       time.Time
	CPU         float64
	Mem         float64
	Disk        float64
	Load        float64
	NetInSpeed  float64
	NetOutSpeed float64
	In          uint64
	Out         uint64
}

func NodeKey(node []byte) string {
	return hex.EncodeToString(node)
}

func CurrentBindings(db *gorm.DB) (map[uint64][]byte, error) {
	out := map[uint64][]byte{}
	if db == nil {
		return out, nil
	}
	var rows []model.ServerNodeBinding
	if err := db.Where("current = ?", true).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row.NodeUUID) == 0 {
			continue
		}
		out[row.ServerID] = append([]byte(nil), row.NodeUUID...)
	}
	return out, nil
}

func RangeStats(db *gorm.DB, nodes [][]byte, from, to time.Time) (map[string]NodeStats, error) {
	out := map[string]NodeStats{}
	if !to.After(from) {
		return out, ErrRangeInvalid
	}
	if to.Sub(from) > MaxStatsLookback {
		return nil, ErrRangeTooLarge
	}
	if db == nil || len(nodes) == 0 {
		return out, nil
	}
	for _, node := range nodes {
		if len(node) == 0 {
			continue
		}
		out[string(node)] = NodeStats{}
	}
	var rows []model.StateRollup
	if err := db.Where("node_uuid IN ? AND resolution = ? AND window_start >= ? AND window_start < ?",
		nodes, "1h", from.UnixNano(), to.UnixNano()).Find(&rows).Error; err != nil {
		return nil, err
	}
	currentHour := to.Truncate(time.Hour)
	if currentHour.Before(from) {
		currentHour = from
	}
	var minutes []model.StateRollup
	if err := db.Where("node_uuid IN ? AND resolution = ? AND window_start >= ? AND window_start < ?",
		nodes, "1m", currentHour.UnixNano(), to.UnixNano()).Find(&minutes).Error; err != nil {
		return nil, err
	}
	acc := map[string]*statsAcc{}
	for key := range out {
		acc[key] = &statsAcc{}
	}
	for _, row := range rows {
		if row.WindowStart >= currentHour.UnixNano() {
			continue
		}
		applyRollup(acc[string(row.NodeUUID)], row)
	}
	for _, row := range minutes {
		applyRollup(acc[string(row.NodeUUID)], row)
	}
	bytes, err := trafficservice.RangeUsageMany(db, nodes, from, to)
	if err != nil {
		return nil, err
	}
	for key, item := range acc {
		stats := item.finish()
		if used, ok := bytes[key]; ok {
			stats.In = used.In
			stats.Out = used.Out
			stats.Total = used.Total
		}
		out[key] = stats
	}
	return out, nil
}

func RangeSeries(db *gorm.DB, nodes [][]byte, from, to time.Time, resolution string) (map[string][]SeriesPoint, error) {
	out := map[string][]SeriesPoint{}
	if db == nil || len(nodes) == 0 {
		return out, nil
	}
	if !to.After(from) {
		return out, ErrRangeInvalid
	}
	if to.Sub(from) > MaxStatsLookback {
		return nil, ErrRangeTooLarge
	}
	if resolution != "1m" {
		resolution = "1h"
	}
	var rows []model.StateRollup
	if err := db.Where("node_uuid IN ? AND resolution = ? AND window_start >= ? AND window_start < ?",
		nodes, resolution, from.UnixNano(), to.UnixNano()).Order("window_start ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		point := seriesFromRollup(row)
		out[string(row.NodeUUID)] = append(out[string(row.NodeUUID)], point)
	}
	return out, nil
}

func RangeDailyTraffic(db *gorm.DB, nodes [][]byte, from, to time.Time, loc *time.Location) ([]trafficservice.Point, error) {
	if loc == nil {
		loc = time.UTC
	}
	if db == nil || len(nodes) == 0 || !to.After(from) {
		return nil, nil
	}
	if to.Sub(from) > MaxTrafficLookback {
		return nil, ErrRangeTooLarge
	}
	bytes, err := trafficservice.RangeUsageMany(db, nodes, from, to)
	if err != nil {
		return nil, err
	}
	_ = bytes
	var rows []model.StateRollup
	if err := db.Where("node_uuid IN ? AND resolution = ? AND window_start >= ? AND window_start < ?",
		nodes, "1h", from.UnixNano(), to.UnixNano()).Find(&rows).Error; err != nil {
		return nil, err
	}
	sums := map[string]uint64{}
	for _, row := range rows {
		key := time.Unix(0, row.WindowStart).In(loc).Format("2006-01-02")
		sums[key] = addU64(sums[key], addU64(row.NetInTotal, row.NetOutTotal))
	}
	currentHour := to.Truncate(time.Hour)
	var minutes []model.StateRollup
	if err := db.Where("node_uuid IN ? AND resolution = ? AND window_start >= ? AND window_start < ?",
		nodes, "1m", currentHour.UnixNano(), to.UnixNano()).Find(&minutes).Error; err != nil {
		return nil, err
	}
	for _, row := range minutes {
		key := time.Unix(0, row.WindowStart).In(loc).Format("2006-01-02")
		sums[key] = addU64(sums[key], addU64(row.NetInTotal, row.NetOutTotal))
	}
	localFrom := time.Date(from.In(loc).Year(), from.In(loc).Month(), from.In(loc).Day(), 0, 0, 0, 0, loc)
	localTo := time.Date(to.In(loc).Year(), to.In(loc).Month(), to.In(loc).Day(), 0, 0, 0, 0, loc)
	var points []trafficservice.Point
	for day := localFrom; !day.After(localTo); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		points = append(points, trafficservice.Point{Start: day, End: day.AddDate(0, 0, 1), Bytes: sums[key]})
	}
	return points, nil
}

type statsAcc struct {
	cpuMin, cpuSum, cpuMax    float64
	memMin, memSum, memMax    uint64
	diskMin, diskSum, diskMax uint64
	loadMin, loadSum, loadMax float64
	netInSum, netOutSum       float64
	samples                   uint32
	init                      bool
}

func applyRollup(acc *statsAcc, row model.StateRollup) {
	if acc == nil {
		return
	}
	payload := new(pb.StateRollupPayload)
	if len(row.Payload) == 0 || proto.Unmarshal(row.Payload, payload) != nil {
		return
	}
	minS, avgS, maxS := payload.GetMinimum(), payload.GetAverage(), payload.GetMaximum()
	if avgS == nil {
		return
	}
	if minS == nil {
		minS = avgS
	}
	if maxS == nil {
		maxS = avgS
	}
	count := payload.GetSampleCount()
	if count == 0 {
		count = 1
	}
	if !acc.init {
		acc.cpuMin, acc.cpuMax = minS.GetCpu(), maxS.GetCpu()
		acc.memMin, acc.memMax = minS.GetMemUsed(), maxS.GetMemUsed()
		acc.diskMin, acc.diskMax = minS.GetDiskUsed(), maxS.GetDiskUsed()
		acc.loadMin, acc.loadMax = minS.GetLoad1(), maxS.GetLoad1()
		acc.init = true
	} else {
		if minS.GetCpu() < acc.cpuMin {
			acc.cpuMin = minS.GetCpu()
		}
		if maxS.GetCpu() > acc.cpuMax {
			acc.cpuMax = maxS.GetCpu()
		}
		if minS.GetMemUsed() < acc.memMin {
			acc.memMin = minS.GetMemUsed()
		}
		if maxS.GetMemUsed() > acc.memMax {
			acc.memMax = maxS.GetMemUsed()
		}
		if minS.GetDiskUsed() < acc.diskMin {
			acc.diskMin = minS.GetDiskUsed()
		}
		if maxS.GetDiskUsed() > acc.diskMax {
			acc.diskMax = maxS.GetDiskUsed()
		}
		if minS.GetLoad1() < acc.loadMin {
			acc.loadMin = minS.GetLoad1()
		}
		if maxS.GetLoad1() > acc.loadMax {
			acc.loadMax = maxS.GetLoad1()
		}
	}
	acc.cpuSum += avgS.GetCpu() * float64(count)
	acc.memSum += avgS.GetMemUsed() * uint64(count)
	acc.diskSum += avgS.GetDiskUsed() * uint64(count)
	acc.loadSum += avgS.GetLoad1() * float64(count)
	acc.netInSum += float64(avgS.GetNetInSpeed()) * float64(count)
	acc.netOutSum += float64(avgS.GetNetOutSpeed()) * float64(count)
	acc.samples += count
}

func (a *statsAcc) finish() NodeStats {
	if a == nil || a.samples == 0 {
		return NodeStats{}
	}
	n := float64(a.samples)
	return NodeStats{
		CPUMin: a.cpuMin, CPUAvg: a.cpuSum / n, CPUMax: a.cpuMax,
		MemUsedMin: a.memMin, MemUsedAvg: a.memSum / uint64(a.samples), MemUsedMax: a.memMax,
		DiskUsedMin: a.diskMin, DiskUsedAvg: a.diskSum / uint64(a.samples), DiskUsedMax: a.diskMax,
		LoadMin: a.loadMin, LoadAvg: a.loadSum / n, LoadMax: a.loadMax,
		NetInSpeedAvg: uint64(a.netInSum / n), NetOutSpeedAvg: uint64(a.netOutSum / n),
		Samples: a.samples,
	}
}

func seriesFromRollup(row model.StateRollup) SeriesPoint {
	point := SeriesPoint{Start: time.Unix(0, row.WindowStart).UTC(), In: row.NetInTotal, Out: row.NetOutTotal}
	payload := new(pb.StateRollupPayload)
	if len(row.Payload) == 0 || proto.Unmarshal(row.Payload, payload) != nil {
		return point
	}
	avg := payload.GetAverage()
	if avg == nil {
		return point
	}
	point.CPU = avg.GetCpu()
	point.Mem = float64(avg.GetMemUsed())
	point.Disk = float64(avg.GetDiskUsed())
	point.Load = avg.GetLoad1()
	point.NetInSpeed = float64(avg.GetNetInSpeed())
	point.NetOutSpeed = float64(avg.GetNetOutSpeed())
	return point
}

func addU64(left, right uint64) uint64 {
	if left > ^uint64(0)-right {
		return ^uint64(0)
	}
	return left + right
}
