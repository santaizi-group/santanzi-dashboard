package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"github.com/hi2shark/santaizi-dashboard/service/telemetry"
	trafficservice "github.com/hi2shark/santaizi-dashboard/service/traffic"
)

const defaultUptimeDays = 7

type Options struct {
	Period     string
	Cover      uint8
	Ignore     map[uint64]bool
	Sections   map[string]bool
	UptimeDays int
	Now        time.Time
}

type HostRow struct {
	ID           uint64
	Name         string
	Tag          string
	Note         string
	PublicNote   string
	Online       bool
	CPU          float64
	MemPct       float64
	DiskPct      float64
	Load1        float64
	NetIn        uint64
	NetOut       uint64
	NetInSpeed   uint64
	NetOutSpeed  uint64
	Uptime       uint64
	Platform     string
	PlatformVer  string
	Arch         string
	AgentVersion string
	MemUsed      uint64
	MemTotal     uint64
	DiskUsed     uint64
	DiskTotal    uint64
	NodeUUID     []byte
}

type TrafficRow struct {
	PolicyID uint64
	ServerID uint64
	Name     string
	Used     uint64
	Quota    uint64
	Percent  float64
	Status   string
}

type UptimeRow struct {
	ServerID   uint64
	Name       string
	Percent    float64
	OfflineSec uint64
	LongestSec uint64
}

type AlertRow struct {
	Kind    string
	Title   string
	Detail  string
	Started time.Time
}

type Snapshot struct {
	GeneratedAt      time.Time
	Period           string
	WindowStart      time.Time
	WindowEnd        time.Time
	TotalServers     int
	OnlineServers    int
	OfflineServers   int
	CollectorsOnline int64
	CollectorsTotal  int64
	ProbesOnline     int64
	ProbesTotal      int64
	ProbePathsUp     int64
	ProbePathsDown   int64
	Incidents        int64
	Hosts            []HostRow
	Traffic          []TrafficRow
	Uptime           []UptimeRow
	Alerts           []AlertRow
}

func Collect(opts Options) (Snapshot, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
		if singleton.Loc != nil {
			now = now.In(singleton.Loc)
		}
	}
	if opts.Ignore == nil {
		opts.Ignore = map[uint64]bool{}
	}
	if opts.UptimeDays <= 0 {
		opts.UptimeDays = defaultUptimeDays
	}
	windowEnd := now
	windowStart := now.AddDate(0, 0, -opts.UptimeDays)
	if opts.Period != "" {
		windowStart = windowStartFor(opts.Period, now)
	}
	snap := Snapshot{GeneratedAt: now, Period: opts.Period, WindowStart: windowStart, WindowEnd: windowEnd}

	hosts := selectedHosts(opts.Cover, opts.Ignore)
	snap.TotalServers = len(hosts)
	for _, host := range hosts {
		if host.Online {
			snap.OnlineServers++
		} else {
			snap.OfflineServers++
		}
	}
	snap.Hosts = hosts

	if singleton.DB != nil {
		conn, err := telemetry.LoadConnectionSummary(singleton.DB, now)
		if err != nil {
			return snap, err
		}
		snap.CollectorsOnline = conn.CollectorsOnline
		snap.CollectorsTotal = conn.CollectorsTotal
		probes, err := telemetry.LoadProbeSummary(singleton.DB, now)
		if err != nil {
			return snap, err
		}
		snap.ProbesOnline = probes.CollectorsOnline
		snap.ProbesTotal = probes.CollectorsTotal
		snap.ProbePathsUp = probes.PathsReachable
		snap.ProbePathsDown = probes.PathsDown
		singleton.DB.Model(&model.AvailabilityIncident{}).Where("ended_at = 0").Count(&snap.Incidents)
		if want(opts.Sections, "traffic") {
			snap.Traffic = collectTraffic(hosts, now)
		}
		if want(opts.Sections, "uptime") {
			snap.Uptime = CollectUptime(hosts, windowStart, windowEnd)
		}
		if want(opts.Sections, "alerts") {
			snap.Alerts = collectAlerts(now)
		}
	}
	return snap, nil
}

func selectedHosts(cover uint8, ignore map[uint64]bool) []HostRow {
	singleton.SortedServerLock.RLock()
	defer singleton.SortedServerLock.RUnlock()
	out := make([]HostRow, 0, len(singleton.SortedServerList))
	for _, server := range singleton.SortedServerList {
		if server == nil || !model.ServerInBotScope(server.ID, cover, ignore) {
			continue
		}
		row := HostRow{
			ID: server.ID, Name: server.Name, Tag: server.Tag, Note: server.Note,
			PublicNote: server.PublicNote, Online: hostOnline(server),
		}
		if server.State != nil {
			row.CPU = server.State.CPU
			row.Load1 = server.State.Load1
			row.NetIn = server.State.NetInTransfer
			row.NetOut = server.State.NetOutTransfer
			row.NetInSpeed = server.State.NetInSpeed
			row.NetOutSpeed = server.State.NetOutSpeed
			row.Uptime = server.State.Uptime
			row.MemUsed = server.State.MemUsed
			row.DiskUsed = server.State.DiskUsed
			if server.Host != nil {
				row.MemPct = pct(server.State.MemUsed, server.Host.MemTotal)
				row.DiskPct = pct(server.State.DiskUsed, server.Host.DiskTotal)
			}
		}
		if server.Host != nil {
			row.Platform = server.Host.Platform
			row.PlatformVer = server.Host.PlatformVersion
			row.Arch = server.Host.Arch
			row.AgentVersion = server.Host.Version
			row.MemTotal = server.Host.MemTotal
			row.DiskTotal = server.Host.DiskTotal
		}
		out = append(out, row)
	}
	return out
}

func hostOnline(server *model.Server) bool {
	if server == nil {
		return false
	}
	if offline, ok, err := model.ServerConsensusOffline(singleton.DB, server.ID); err == nil && ok {
		return !offline
	}
	if server.LastActive.IsZero() || singleton.Conf == nil {
		return false
	}
	return time.Since(server.LastActive) < time.Duration(singleton.Conf.Telemetry.OfflineThresholdSeconds)*time.Second
}

func collectTraffic(hosts []HostRow, now time.Time) []TrafficRow {
	ids := make([]uint64, 0, len(hosts))
	names := map[uint64]string{}
	for _, host := range hosts {
		ids = append(ids, host.ID)
		names[host.ID] = host.Name
	}
	summaries, err := trafficservice.Summaries(singleton.DB, ids, now)
	if err != nil {
		return nil
	}
	out := []TrafficRow{}
	for serverID, items := range summaries {
		for _, item := range items {
			out = append(out, TrafficRow{
				PolicyID: item.PolicyID, ServerID: serverID, Name: names[serverID] + " / " + item.Name,
				Used: item.UsedBytes, Quota: item.QuotaBytes, Percent: item.UsagePercent, Status: item.Status,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Percent > out[j].Percent })
	return out
}

func CollectUptime(hosts []HostRow, start, end time.Time) []UptimeRow {
	out := make([]UptimeRow, 0, len(hosts))
	period := end.Sub(start).Seconds()
	if period <= 0 {
		return out
	}
	for _, host := range hosts {
		var histories []model.ServerOfflineHistory
		_ = singleton.DB.Where("server_id = ? AND started_at < ? AND (ended_at IS NULL OR ended_at > ?)", host.ID, end, start).Find(&histories).Error
		offline, longest := singleton.SummarizeOfflineIntervals(histories, start, end)
		pct := singleton.FormatAvailabilityPercent((period - float64(offline)) * 100 / period)
		out = append(out, UptimeRow{ServerID: host.ID, Name: host.Name, Percent: pct, OfflineSec: offline, LongestSec: longest})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Percent < out[j].Percent })
	return out
}

func collectAlerts(now time.Time) []AlertRow {
	out := []AlertRow{}
	var offlines []model.ServerOfflineHistory
	if err := singleton.DB.Where("ended_at IS NULL").Order("started_at DESC").Limit(20).Find(&offlines).Error; err == nil {
		names := hostNames()
		for _, item := range offlines {
			out = append(out, AlertRow{Kind: "offline", Title: names[item.ServerID], Detail: "连通异常", Started: item.StartedAt})
		}
	}
	var incidents []model.AvailabilityIncident
	if err := singleton.DB.Where("ended_at = 0").Order("started_at DESC").Limit(20).Find(&incidents).Error; err == nil {
		for _, item := range incidents {
			started := time.Unix(0, item.StartedAt).In(now.Location())
			out = append(out, AlertRow{Kind: "incident", Title: item.CurrentClassification, Detail: item.Reason, Started: started})
		}
	}
	return out
}

func hostNames() map[uint64]string {
	singleton.SortedServerLock.RLock()
	defer singleton.SortedServerLock.RUnlock()
	out := map[uint64]string{}
	for _, server := range singleton.SortedServerList {
		if server != nil {
			out[server.ID] = server.Name
		}
	}
	return out
}

func windowStartFor(period string, now time.Time) time.Time {
	switch period {
	case model.BotPeriodWeekly:
		return now.AddDate(0, 0, -7)
	case model.BotPeriodMonthly:
		return now.AddDate(0, -1, 0)
	default:
		return now.AddDate(0, 0, -1)
	}
}

func want(sections map[string]bool, name string) bool {
	if len(sections) == 0 {
		return true
	}
	return sections[name]
}

func pct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) * 100 / float64(total)
}

func FormatBytes(n uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(n)
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d %s", n, units[i])
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func FormatDuration(seconds uint64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	if seconds < 86400 {
		return fmt.Sprintf("%.1fh", float64(seconds)/3600)
	}
	return fmt.Sprintf("%.1fd", float64(seconds)/86400)
}

func FindServer(query string) *model.Server {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	singleton.SortedServerLock.RLock()
	defer singleton.SortedServerLock.RUnlock()
	for _, server := range singleton.SortedServerList {
		if server == nil {
			continue
		}
		if fmt.Sprintf("%d", server.ID) == query || strings.EqualFold(server.Name, query) {
			return server
		}
	}
	return nil
}

func HostByID(id uint64) (HostRow, bool) {
	if id == 0 {
		return HostRow{}, false
	}
	for _, host := range AllHosts() {
		if host.ID == id {
			return host, true
		}
	}
	return HostRow{}, false
}

func FilterHosts(query string) []HostRow {
	query = strings.TrimSpace(query)
	hosts := selectedHosts(model.RuleCoverAll, nil)
	if query == "" {
		return hosts
	}
	lower := strings.ToLower(query)
	var exact []HostRow
	var partial []HostRow
	for _, host := range hosts {
		if fmt.Sprintf("%d", host.ID) == query || strings.EqualFold(host.Name, query) {
			exact = append(exact, host)
			continue
		}
		if hostMatchesPartial(host, lower) {
			partial = append(partial, host)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

func hostMatchesPartial(host HostRow, query string) bool {
	if strings.Contains(strings.ToLower(host.Name), query) || strings.Contains(strings.ToLower(host.Tag), query) {
		return true
	}
	if strings.Contains(strings.ToLower(host.Note), query) || strings.Contains(strings.ToLower(host.PublicNote), query) {
		return true
	}
	return false
}

func AllHosts() []HostRow {
	return selectedHosts(model.RuleCoverAll, nil)
}

func FindServers(query string) []*model.Server {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	lower := strings.ToLower(query)
	singleton.SortedServerLock.RLock()
	defer singleton.SortedServerLock.RUnlock()
	var exact []*model.Server
	var partial []*model.Server
	for _, server := range singleton.SortedServerList {
		if server == nil {
			continue
		}
		if fmt.Sprintf("%d", server.ID) == query || strings.EqualFold(server.Name, query) {
			exact = append(exact, server)
			continue
		}
		if strings.Contains(strings.ToLower(server.Name), lower) || strings.Contains(strings.ToLower(server.Tag), lower) ||
			strings.Contains(strings.ToLower(server.Note), lower) || strings.Contains(strings.ToLower(server.PublicNote), lower) {
			partial = append(partial, server)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}
