package bot

import (
	"fmt"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

func PeriodKey(period string, now time.Time) string {
	switch period {
	case model.BotPeriodWeekly:
		y, w := now.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	case model.BotPeriodMonthly:
		return now.Format("2006-01")
	default:
		return now.Format("2006-01-02")
	}
}

func ReportDue(row model.BotReport, now time.Time) bool {
	if !row.IsEnabled() {
		return false
	}
	key := PeriodKey(row.Period, now)
	if row.LastPeriodKey == key {
		return false
	}
	scheduled := time.Date(now.Year(), now.Month(), now.Day(), row.HourLocal, row.Minute, 0, 0, now.Location())
	switch row.Period {
	case model.BotPeriodWeekly:
		want := time.Weekday(row.Weekday)
		if now.Weekday() != want {
			return false
		}
	case model.BotPeriodMonthly:
		day := row.DayOfMonth
		if day <= 0 {
			day = 1
		}
		last := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
		if day > last {
			day = last
		}
		if now.Day() != day {
			return false
		}
	}
	return !now.Before(scheduled)
}

func ScanReports() {
	if singleton.DB == nil {
		return
	}
	now := time.Now()
	if singleton.Loc != nil {
		now = now.In(singleton.Loc)
	}
	var rows []model.BotReport
	if err := singleton.DB.Find(&rows).Error; err != nil {
		return
	}
	for i := range rows {
		row := rows[i]
		if !ReportDue(row, now) {
			continue
		}
		if err := Shared().SendReport(&row, false); err != nil {
			row.LastStatus = err.Error()
			_ = singleton.DB.Model(&row).Update("last_status", row.LastStatus).Error
			continue
		}
	}
}
