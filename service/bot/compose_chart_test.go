package bot

import (
	"testing"
	"time"

	trafficservice "github.com/hi2shark/santaizi-dashboard/service/traffic"
)

func TestDailyTrafficBarsWeeklyBucketing(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// 20 天 → 超过 16 天阈值，应按周分桶
	var points []trafficservice.Point
	for i := 0; i < 20; i++ {
		points = append(points, trafficservice.Point{Start: base.AddDate(0, 0, i), Bytes: 100})
	}
	bars := dailyTrafficBars(points)
	if len(bars) == 0 || len(bars) >= 20 {
		t.Fatalf("weekly buckets = %d", len(bars))
	}
	var total float64
	for _, bar := range bars {
		total += bar.Value
	}
	if total != 2000 {
		t.Fatalf("bucket sum = %v, want 2000", total)
	}

	// 10 天 → 保持按天
	var daily []trafficservice.Point
	for i := 0; i < 10; i++ {
		daily = append(daily, trafficservice.Point{Start: base.AddDate(0, 0, i), Bytes: 10})
	}
	if bars := dailyTrafficBars(daily); len(bars) != 10 {
		t.Fatalf("daily bars = %d, want 10", len(bars))
	}
}
