package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

func publicServerIDVisible(c *gin.Context, id uint64) bool {
	for _, server := range publicServerSnapshot(c) {
		if server["id"] == id {
			return true
		}
	}
	return false
}

func publicNetworkHistoryItems(resp *singleton.MonitorInfoResponse) []any {
	if resp == nil || len(resp.Result) == 0 {
		return []any{}
	}
	converted := snakeValue(resp.Result)
	if items, ok := converted.([]any); ok && items != nil {
		return items
	}
	return []any{}
}

func publicRemainingBytes(used, quota uint64) uint64 {
	if quota <= used {
		return 0
	}
	return quota - used
}

func publicWarningBytes(quota uint64, warningPercent float64) uint64 {
	if quota == 0 || warningPercent <= 0 {
		return 0
	}
	return uint64(float64(quota) * warningPercent / 100)
}

func publicAvailabilityAllowed(c *gin.Context) bool {
	if singleton.Conf == nil || singleton.Conf.ShowAvailabilityToGuest {
		return true
	}
	if _, ok := c.Get(model.CtxKeyAuthorizedUser); ok {
		return true
	}
	return false
}

func v2PublicServerAvailability(c *gin.Context) {
	if !requireV2PublicAccess(c) {
		return
	}
	if !publicAvailabilityAllowed(c) {
		writeV2Problem(c, http.StatusForbidden, "availability_hidden", "前台可用性展示已关闭")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", "server_id 无效")
		return
	}
	if !publicServerIDVisible(c, id) {
		writeV2Problem(c, http.StatusNotFound, "server_not_found", "服务器不存在")
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days < 1 {
		days = 30
	}
	if days > maxOfflineSummaryDays {
		days = maxOfflineSummaryDays
	}
	summaries, _, err := singleton.GetServerAvailabilitySummaries([]uint64{id}, days)
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	item := summaries[id]
	if item == nil {
		item = &singleton.ServerAvailability{ServerID: id, Days: days}
	}
	writeV2Data(c, http.StatusOK, item)
}

type publicMetricQueryProblem struct {
	status int
	code   string
	detail string
}

func normalizePublicMetricResolution(resolution string) string {
	if resolution == "1h" {
		return "1h"
	}
	return "1m"
}

func publicMetricMaxHours(resolution string) float64 {
	resolution = normalizePublicMetricResolution(resolution)
	maxDays := uint64(30)
	if singleton.Conf != nil {
		if resolution == "1h" {
			maxDays = singleton.Conf.Retention.StateOneHourDays
			if maxDays == 0 {
				maxDays = 365
			}
		} else {
			maxDays = singleton.Conf.Retention.StateOneMinuteDays
			if maxDays == 0 {
				maxDays = 30
			}
		}
	} else if resolution == "1h" {
		maxDays = 365
	}
	return float64(int(maxDays) * 24)
}

func clampPublicMetricWindow(resolution string, hours float64) (string, float64) {
	resolution = normalizePublicMetricResolution(resolution)
	if hours <= 0 {
		hours = 24
	}
	maxHours := publicMetricMaxHours(resolution)
	if hours > maxHours {
		hours = maxHours
	}
	return resolution, hours
}

func parsePublicMetricTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if unix, err := strconv.ParseInt(raw, 10, 64); err == nil && len(raw) >= 10 {
		return time.Unix(unix, 0).UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

func resolvePublicMetricRange(resolution, hoursRaw, startRaw, endRaw string, now time.Time) (string, time.Time, time.Time, *publicMetricQueryProblem) {
	startRaw = strings.TrimSpace(startRaw)
	endRaw = strings.TrimSpace(endRaw)
	if startRaw != "" || endRaw != "" {
		if startRaw == "" || endRaw == "" {
			return "", time.Time{}, time.Time{}, &publicMetricQueryProblem{http.StatusBadRequest, "invalid_range", "start 与 end 必须成对提供"}
		}
		start, ok := parsePublicMetricTime(startRaw)
		if !ok {
			return "", time.Time{}, time.Time{}, &publicMetricQueryProblem{http.StatusBadRequest, "invalid_start", "start 必须是 RFC3339 或 Unix 秒"}
		}
		end, ok := parsePublicMetricTime(endRaw)
		if !ok {
			return "", time.Time{}, time.Time{}, &publicMetricQueryProblem{http.StatusBadRequest, "invalid_end", "end 必须是 RFC3339 或 Unix 秒"}
		}
		if !end.After(start) {
			return "", time.Time{}, time.Time{}, &publicMetricQueryProblem{http.StatusBadRequest, "invalid_range", "end 必须晚于 start"}
		}
		resolution = normalizePublicMetricResolution(resolution)
		maxSpan := time.Duration(publicMetricMaxHours(resolution) * float64(time.Hour))
		if end.Sub(start) > maxSpan {
			return "", time.Time{}, time.Time{}, &publicMetricQueryProblem{http.StatusBadRequest, "range_too_large", "查询跨度超过保留期"}
		}
		return resolution, start, end, nil
	}
	hours := 24.0
	if hoursRaw != "" {
		parsed, err := strconv.ParseFloat(hoursRaw, 64)
		if err != nil || parsed < 0.5 {
			return "", time.Time{}, time.Time{}, &publicMetricQueryProblem{http.StatusBadRequest, "invalid_hours", "hours 无效"}
		}
		hours = parsed
	}
	resolution, hours = clampPublicMetricWindow(resolution, hours)
	return resolution, now.Add(-time.Duration(hours * float64(time.Hour))), now, nil
}

func publicMetricWindowSeconds(row model.StateRollup) int64 {
	if row.WindowEnd > row.WindowStart {
		sec := (row.WindowEnd - row.WindowStart) / int64(time.Second)
		if sec > 0 {
			return sec
		}
	}
	if row.Resolution == "1h" {
		return 3600
	}
	return 60
}

func publicMetricSpeed(average, total uint64, windowSec int64) uint64 {
	if average > 0 || total == 0 || windowSec <= 0 {
		return average
	}
	return total / uint64(windowSec)
}

func decodePublicMetricPoints(rows []model.StateRollup) []gin.H {
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		var payload pb.StateRollupPayload
		if len(row.Payload) > 0 {
			if err := proto.Unmarshal(row.Payload, &payload); err != nil {
				continue
			}
		}
		avg := payload.GetAverage()
		if avg == nil {
			avg = &pb.State{}
		}
		windowSec := publicMetricWindowSeconds(row)
		items = append(items, gin.H{
			"window_start":   time.Unix(0, row.WindowStart).UTC().Format(time.RFC3339),
			"cpu":            avg.GetCpu(),
			"mem_used":       avg.GetMemUsed(),
			"disk_used":      avg.GetDiskUsed(),
			"net_in_speed":   publicMetricSpeed(avg.GetNetInSpeed(), payload.GetNetInTotal(), windowSec),
			"net_out_speed":  publicMetricSpeed(avg.GetNetOutSpeed(), payload.GetNetOutTotal(), windowSec),
			"net_in_total":   payload.GetNetInTotal(),
			"net_out_total":  payload.GetNetOutTotal(),
			"process_count":  avg.GetProcessCount(),
			"tcp_conn_count": avg.GetTcpConnCount(),
			"udp_conn_count": avg.GetUdpConnCount(),
		})
	}
	return items
}

func v2PublicMetrics(c *gin.Context) {
	if !requireV2PublicAccess(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server_id", "server_id 无效")
		return
	}
	if !publicServerIDVisible(c, id) {
		writeV2Problem(c, http.StatusNotFound, "server_not_found", "服务器不存在")
		return
	}
	resolution, from, to, problem := resolvePublicMetricRange(
		c.DefaultQuery("resolution", "1m"),
		c.Query("hours"),
		c.Query("start"),
		c.Query("end"),
		time.Now(),
	)
	if problem != nil {
		writeV2Problem(c, problem.status, problem.code, problem.detail)
		return
	}
	items := []gin.H{}
	var binding model.ServerNodeBinding
	if err := singleton.DB.Where("server_id = ? AND current = ?", id, true).Order("valid_from DESC").First(&binding).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			writeV2List(c, items, v2Meta{Page: 1, PageSize: 0, Total: 0})
			return
		}
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	var rows []model.StateRollup
	if err := singleton.DB.Where("node_uuid = ? AND resolution = ? AND window_start >= ? AND window_start < ?", binding.NodeUUID, resolution, from.UnixNano(), to.UnixNano()).
		Order("window_start ASC").Find(&rows).Error; err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	items = decodePublicMetricPoints(rows)
	writeV2List(c, items, v2Meta{Page: 1, PageSize: len(items), Total: int64(len(items))})
}
