package telemetry

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/geoip"
	"github.com/hi2shark/santaizi-dashboard/pkg/netprobe"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ProbeSampleRetention = DefaultEvidenceRetain

type ProbeSummary struct {
	CollectorsTotal   int64
	CollectorsOnline  int64
	CollectorsOffline int64
	CollectorsUnknown int64
	PathsAssigned     int64
	PathsReachable    int64
	PathsDown         int64
	PathsNoTarget     int64
}

type ProbeTCPView struct {
	Port  uint    `json:"port"`
	OK    bool    `json:"ok"`
	RttMs float64 `json:"rtt_ms,omitempty"`
	Error string  `json:"error,omitempty"`
}

type ProbePath struct {
	ServerID      uint64
	ServerName    string
	DisplayIndex  int
	Tag           string
	CollectorID   string
	CollectorName string
	TargetSource  string
	Hostname      string
	IPv4          string
	IPv6          string
	Reachable     bool
	DisplayRttMs  float64
	SampledAt     int64
	LastError     string
	ICMPOk        bool
	ICMPRttMs     float64
	ICMPLoss      float64
	ICMPSent      uint32
	ICMPRecv      uint32
	TCP           []ProbeTCPView
	HasICMP       bool
	HasTrace      bool
	MTR           ProbeMTRView
}

type ProbeMTRView struct {
	Loss      float64
	HopCount  int
	SampledAt int64
	Protocol  string
	Port      uint
}

type ProbePathFilter struct {
	ServerID    uint64
	CollectorID string
}

type ProbeSampleFilter struct {
	CollectorID string
	ServerID    uint64
	Kind        string
	Port        uint
}

type ProbeSampleRow struct {
	CollectorID  string
	ServerID     uint64
	ServerName   string
	Kind         string
	Port         uint
	BucketStart  int64
	MinMs        float64
	AvgMs        float64
	MaxMs        float64
	Loss         float64
	SuccessCount uint32
	FailCount    uint32
}

type ProbeHopView struct {
	TTL         uint
	Address     string
	Loss        float64
	AvgMs       float64
	Sent        int
	Geo         string
	CountryCode string
	Private     bool
}

type ProbeTraceLegView struct {
	SampledAt   int64
	Destination string
	Port        uint
	Hops        []ProbeHopView
}

type ProbeTraceView struct {
	CollectorID string
	ServerID    uint64
	SampledAt   int64
	Destination string
	Protocol    string
	Port        uint
	Hops        []ProbeHopView
	ICMP        *ProbeTraceLegView
	TCP         *ProbeTraceLegView
}

func CollectorKind(kind string) string {
	return model.NormalizeCollectorKind(kind)
}

func LoadProbeSummary(db *gorm.DB, now time.Time) (ProbeSummary, error) {
	var summary ProbeSummary
	var collectors []model.Collector
	if err := db.Where("deleted = ? AND revoked = ? AND kind = ?", false, false, model.CollectorKindProbe).Find(&collectors).Error; err != nil {
		return summary, err
	}
	summary.CollectorsTotal = int64(len(collectors))
	if len(collectors) > 0 {
		ids := make([]string, 0, len(collectors))
		for _, collector := range collectors {
			ids = append(ids, collector.CollectorUUID)
		}
		var runtimes []model.CollectorRuntime
		if err := db.Where("collector_uuid IN ?", ids).Find(&runtimes).Error; err != nil {
			return summary, err
		}
		byID := map[string]int64{}
		for _, runtime := range runtimes {
			byID[runtime.CollectorUUID] = runtime.LastSeen
		}
		for _, collector := range collectors {
			switch CollectorStatus(byID[collector.CollectorUUID], now) {
			case CollectorStatusOnline:
				summary.CollectorsOnline++
			case CollectorStatusOffline:
				summary.CollectorsOffline++
			default:
				summary.CollectorsUnknown++
			}
		}
	}
	paths, err := loadProbePaths(db, ProbePathFilter{}, now)
	if err != nil {
		return summary, err
	}
	summary.PathsAssigned = int64(len(paths))
	for _, path := range paths {
		if path.TargetSource == "none" {
			summary.PathsNoTarget++
			continue
		}
		if path.SampledAt == 0 {
			continue
		}
		if path.Reachable {
			summary.PathsReachable++
		} else {
			summary.PathsDown++
		}
	}
	return summary, nil
}

func LoadProbePaths(db *gorm.DB, filter ProbePathFilter) ([]ProbePath, error) {
	return loadProbePaths(db, filter, time.Now())
}

func loadProbePaths(db *gorm.DB, filter ProbePathFilter, now time.Time) ([]ProbePath, error) {
	var collectors []model.Collector
	query := db.Where("deleted = ? AND revoked = ? AND kind = ?", false, false, model.CollectorKindProbe)
	if filter.CollectorID != "" {
		query = query.Where("collector_uuid = ?", filter.CollectorID)
	}
	if err := query.Find(&collectors).Error; err != nil {
		return nil, err
	}
	if len(collectors) == 0 {
		return []ProbePath{}, nil
	}
	var servers []model.Server
	if err := db.Find(&servers).Error; err != nil {
		return nil, err
	}
	serverByID := map[uint64]model.Server{}
	for _, server := range servers {
		serverByID[server.ID] = server
	}
	ids := make([]string, 0, len(collectors))
	for _, collector := range collectors {
		ids = append(ids, collector.CollectorUUID)
	}
	var latests []model.ProbeLatest
	if err := db.Where("collector_uuid IN ?", ids).Find(&latests).Error; err != nil {
		return nil, err
	}
	latestByKey := map[string]model.ProbeLatest{}
	for _, row := range latests {
		latestByKey[row.CollectorUUID+"/"+fmt.Sprintf("%d", row.ServerID)] = row
	}
	var traces []model.ProbeTrace
	if err := db.Where("collector_uuid IN ?", ids).Find(&traces).Error; err != nil {
		return nil, err
	}
	traceByKey := map[string]model.ProbeTrace{}
	for _, row := range traces {
		traceByKey[row.CollectorUUID+"/"+fmt.Sprintf("%d", row.ServerID)] = row
	}
	var scopes []model.CollectorScope
	if err := db.Where("collector_uuid IN ?", ids).Find(&scopes).Error; err != nil {
		return nil, err
	}
	scopesByCollector := map[string][]model.CollectorScope{}
	for _, scope := range scopes {
		scopesByCollector[scope.CollectorUUID] = append(scopesByCollector[scope.CollectorUUID], scope)
	}
	lastSeen, err := collectorLastSeenByID(db, ids)
	if err != nil {
		return nil, err
	}
	var paths []ProbePath
	for _, collector := range collectors {
		online := CollectorStatus(lastSeen[collector.CollectorUUID], now) == CollectorStatusOnline
		for _, server := range servers {
			if filter.ServerID != 0 && server.ID != filter.ServerID {
				continue
			}
			if !singleton.CollectorScopesIncludeServer(scopesByCollector[collector.CollectorUUID], server) {
				continue
			}
			target := ResolveProbeTarget(db, server)
			path := ProbePath{
				ServerID: server.ID, ServerName: server.Name, DisplayIndex: server.DisplayIndex, Tag: server.Tag,
				CollectorID: collector.CollectorUUID, CollectorName: collector.Name,
				TargetSource: target.Source, Hostname: target.Hostname, IPv4: target.IPv4, IPv6: target.IPv6,
			}
			v4, v6 := CollectorIPFamilies(&collector)
			target = ApplyProbeIPFamilies(target, v4, v6)
			path.TargetSource = target.Source
			path.Hostname = target.Hostname
			path.IPv4 = target.IPv4
			path.IPv6 = target.IPv6
			if !HasActiveProbe(server, &collector, target) {
				path.TargetSource = "none"
				path.Hostname, path.IPv4, path.IPv6 = "", "", ""
				paths = append(paths, path)
				continue
			}
			if latest, ok := latestByKey[collector.CollectorUUID+"/"+fmt.Sprintf("%d", server.ID)]; ok && online {
				enableICMP, enableTCP, enableMTR := EffectiveProbeTypes(server, &collector)
				path.Reachable = latest.Reachable
				path.DisplayRttMs = latest.DisplayRttMs
				path.SampledAt = latest.SampledAt
				path.LastError = latest.LastError
				if enableICMP && (latest.ICMPOk || latest.ICMPSent > 0) {
					path.HasICMP = true
					path.ICMPOk = latest.ICMPOk
					path.ICMPRttMs = latest.ICMPRttMs
					path.ICMPLoss = latest.ICMPLoss
					path.ICMPSent = latest.ICMPSent
					path.ICMPRecv = latest.ICMPRecv
				}
				if enableTCP && len(latest.TCPJSON) > 0 {
					_ = json.Unmarshal(latest.TCPJSON, &path.TCP)
				}
				if enableMTR {
					if trace, ok := traceByKey[collector.CollectorUUID+"/"+fmt.Sprintf("%d", server.ID)]; ok {
						var icmpMTR, tcpMTR ProbeMTRView
						icmpOK, tcpOK := false, false
						if enableICMP {
							icmpMTR, icmpOK = probeMTRFromHops(trace.HopsJSON, trace.SampledAt, "icmp", 0)
						}
						if enableTCP {
							tcpMTR, tcpOK = probeMTRFromHops(trace.TCPHopsJSON, trace.TCPSampledAt, "tcp", trace.TCPPort)
						}
						if mtr, ok := selectProbeMTR(icmpMTR, tcpMTR, icmpOK, tcpOK); ok {
							path.MTR = mtr
							path.HasTrace = true
						}
					}
				}
			}
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func probeMTRFromHops(raw []byte, sampledAt int64, protocol string, port uint) (ProbeMTRView, bool) {
	var hops []netprobe.Hop
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &hops)
	}
	if len(hops) == 0 {
		return ProbeMTRView{}, false
	}
	last := hops[len(hops)-1]
	return ProbeMTRView{Loss: last.Loss, HopCount: len(hops), SampledAt: sampledAt, Protocol: protocol, Port: port}, true
}

func selectProbeMTR(icmp, tcp ProbeMTRView, icmpOK, tcpOK bool) (ProbeMTRView, bool) {
	if icmpOK && tcpOK && icmp.Loss >= 1 {
		return tcp, true
	}
	if icmpOK {
		return icmp, true
	}
	if tcpOK {
		return tcp, true
	}
	return ProbeMTRView{}, false
}

func ListProbeSamples(db *gorm.DB, filter ProbeSampleFilter, offset, limit int) ([]ProbeSampleRow, int64, error) {
	query := db.Model(&model.ProbeSampleBucket{})
	if filter.CollectorID != "" {
		query = query.Where("collector_uuid = ?", filter.CollectorID)
	}
	if filter.ServerID != 0 {
		query = query.Where("server_id = ?", filter.ServerID)
	}
	if filter.Kind != "" {
		query = query.Where("kind = ?", filter.Kind)
	}
	if filter.Port != 0 {
		query = query.Where("port = ?", filter.Port)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.ProbeSampleBucket
	if err := query.Order("bucket_start DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	names := map[uint64]string{}
	out := make([]ProbeSampleRow, 0, len(rows))
	for _, row := range rows {
		name := names[row.ServerID]
		if name == "" {
			var server model.Server
			if err := db.Select("id", "name").First(&server, row.ServerID).Error; err == nil {
				name = server.Name
				names[row.ServerID] = name
			}
		}
		avg := 0.0
		if row.Count > 0 {
			avg = row.SumMs / float64(row.Count)
		}
		loss := 0.0
		if row.Count > 0 {
			loss = row.LossSum / float64(row.Count)
		}
		out = append(out, ProbeSampleRow{
			CollectorID: row.CollectorUUID, ServerID: row.ServerID, ServerName: name, Kind: row.Kind, Port: row.Port,
			BucketStart: row.BucketStart, MinMs: row.MinMs, AvgMs: avg, MaxMs: row.MaxMs, Loss: loss,
			SuccessCount: row.SuccessCount, FailCount: row.FailCount,
		})
	}
	return out, total, nil
}

func GetProbeTrace(db *gorm.DB, collectorID string, serverID uint64) (*ProbeTraceView, error) {
	var row model.ProbeTrace
	if err := db.Where("collector_uuid = ? AND server_id = ?", collectorID, serverID).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	var collector model.Collector
	var collectorPtr *model.Collector
	if err := db.Where("collector_uuid = ?", collectorID).First(&collector).Error; err == nil {
		collectorPtr = &collector
	}
	var server model.Server
	_ = db.First(&server, serverID).Error
	enableICMP, enableTCP, enableMTR := EffectiveProbeTypes(server, collectorPtr)
	if !enableMTR {
		return nil, nil
	}
	view := &ProbeTraceView{CollectorID: row.CollectorUUID, ServerID: row.ServerID}
	if enableICMP {
		if hops := decodeAnnotatedHops(row.HopsJSON); len(hops) > 0 {
			view.ICMP = &ProbeTraceLegView{SampledAt: row.SampledAt, Destination: row.Destination, Hops: hops}
		}
	}
	if enableTCP {
		if hops := decodeAnnotatedHops(row.TCPHopsJSON); len(hops) > 0 {
			view.TCP = &ProbeTraceLegView{SampledAt: row.TCPSampledAt, Destination: row.TCPDestination, Port: row.TCPPort, Hops: hops}
		}
	}
	icmpOK := view.ICMP != nil
	tcpOK := view.TCP != nil
	if !icmpOK && !tcpOK {
		return nil, nil
	}
	icmpView, tcpView := ProbeMTRView{}, ProbeMTRView{}
	if icmpOK {
		icmpView, _ = probeMTRFromHops(row.HopsJSON, row.SampledAt, "icmp", 0)
	}
	if tcpOK {
		tcpView, _ = probeMTRFromHops(row.TCPHopsJSON, row.TCPSampledAt, "tcp", row.TCPPort)
	}
	selected, _ := selectProbeMTR(icmpView, tcpView, icmpOK, tcpOK)
	view.Protocol = selected.Protocol
	view.Port = selected.Port
	view.SampledAt = selected.SampledAt
	if selected.Protocol == "tcp" && view.TCP != nil {
		view.Destination = view.TCP.Destination
		view.Hops = view.TCP.Hops
	} else if view.ICMP != nil {
		view.Destination = view.ICMP.Destination
		view.Hops = view.ICMP.Hops
		view.Protocol = "icmp"
	}
	return view, nil
}

func decodeAnnotatedHops(raw []byte) []ProbeHopView {
	var hops []netprobe.Hop
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &hops)
	}
	out := make([]ProbeHopView, 0, len(hops))
	for _, hop := range hops {
		info := geoip.LookupHop(hop.Address)
		out = append(out, ProbeHopView{
			TTL: hop.TTL, Address: hop.Address, Loss: hop.Loss,
			AvgMs: float64(hop.Avg) / float64(time.Millisecond), Sent: hop.Sent,
			Geo: geoip.FormatHopGeo(info), CountryCode: info.CountryCode, Private: info.Private,
		})
	}
	return out
}

func IngestProbeSamples(db *gorm.DB, collector *model.Collector, batch *pb.ProbeSampleBatch, now time.Time) error {
	if collector == nil || batch == nil {
		return nil
	}
	for _, sample := range batch.GetSamples() {
		if err := ingestProbeSample(db, collector, sample, now); err != nil {
			return err
		}
	}
	return nil
}

func ingestProbeSample(db *gorm.DB, collector *model.Collector, sample *pb.ProbeSample, now time.Time) error {
	if sample == nil || sample.GetServerId() == 0 {
		return nil
	}
	sampledAt := sample.GetSampledAtUnixNano()
	if sampledAt == 0 {
		sampledAt = now.UnixNano()
	}
	icmp := sample.GetIcmp()
	tcpViews := make([]ProbeTCPView, 0, len(sample.GetTcp()))
	anyTCP := false
	for _, item := range sample.GetTcp() {
		view := ProbeTCPView{Port: uint(item.GetPort()), OK: item.GetOk(), RttMs: item.GetRttMs(), Error: item.GetError()}
		tcpViews = append(tcpViews, view)
		if view.OK {
			anyTCP = true
		}
		if err := upsertProbeBucket(db, collector.CollectorUUID, sample.GetServerId(), "tcp", view.Port, sampledAt, view.OK, view.RttMs, 0); err != nil {
			return err
		}
	}
	icmpOK := icmp != nil && icmp.GetOk()
	icmpRtt := 0.0
	icmpLoss := 0.0
	if icmp != nil {
		icmpRtt = icmp.GetRttMs()
		icmpLoss = icmp.GetLoss()
		if err := upsertProbeBucket(db, collector.CollectorUUID, sample.GetServerId(), "icmp", 0, sampledAt, icmpOK, icmpRtt, icmpLoss); err != nil {
			return err
		}
	}
	hasConnectivity := icmp != nil || len(tcpViews) > 0
	reachable := icmpOK || anyTCP
	if !hasConnectivity {
		reachable = true
	}
	display := 0.0
	if icmpOK {
		display = icmpRtt
	} else {
		for _, item := range tcpViews {
			if item.OK {
				display = item.RttMs
				break
			}
		}
	}
	tcpJSON, _ := json.Marshal(tcpViews)
	icmpTrace := sample.GetMtr()
	tcpTrace := sample.GetMtrTcp()
	hasICMPTrace := icmpTrace != nil && len(icmpTrace.GetHops()) > 0
	hasTCPTrace := tcpTrace != nil && len(tcpTrace.GetHops()) > 0
	latest := model.ProbeLatest{
		CollectorUUID: collector.CollectorUUID, ServerID: sample.GetServerId(), SampledAt: sampledAt,
		Reachable: reachable, DisplayRttMs: display, LastError: sample.GetLastError(),
		ICMPOk: icmpOK, ICMPRttMs: icmpRtt, ICMPLoss: icmpLoss,
		TCPJSON: tcpJSON, HasTrace: hasICMPTrace || hasTCPTrace, UpdatedAt: now,
	}
	if icmp != nil {
		latest.ICMPSent = icmp.GetPacketsSent()
		latest.ICMPRecv = icmp.GetPacketsReceived()
	}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&latest).Error; err != nil {
		return err
	}
	if hasICMPTrace || hasTCPTrace {
		if err := upsertProbeTrace(db, collector.CollectorUUID, sample.GetServerId(), icmpTrace, tcpTrace, now); err != nil {
			return err
		}
	}
	return evaluateProbeAlert(db, collector, sample.GetServerId(), reachable, hasConnectivity, display, sample.GetLastError(), now)
}

func upsertProbeTrace(db *gorm.DB, collectorUUID string, serverID uint64, icmpTrace, tcpTrace *pb.ProbeMTRTrace, now time.Time) error {
	var row model.ProbeTrace
	err := db.Where("collector_uuid = ? AND server_id = ?", collectorUUID, serverID).First(&row).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if err == gorm.ErrRecordNotFound {
		row = model.ProbeTrace{CollectorUUID: collectorUUID, ServerID: serverID}
	}
	if hopsJSON, ok := hopsJSONFromProto(icmpTrace); ok {
		row.SampledAt = icmpTrace.GetSampledAtUnixNano()
		row.Destination = icmpTrace.GetDestination()
		row.HopsJSON = hopsJSON
	}
	if hopsJSON, ok := hopsJSONFromProto(tcpTrace); ok {
		row.TCPSampledAt = tcpTrace.GetSampledAtUnixNano()
		row.TCPDestination = tcpTrace.GetDestination()
		row.TCPHopsJSON = hopsJSON
		row.TCPPort = uint(tcpTrace.GetPort())
	}
	row.UpdatedAt = now
	return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
}

func hopsJSONFromProto(trace *pb.ProbeMTRTrace) ([]byte, bool) {
	if trace == nil || len(trace.GetHops()) == 0 {
		return nil, false
	}
	hops := make([]netprobe.Hop, 0, len(trace.GetHops()))
	for _, hop := range trace.GetHops() {
		hops = append(hops, netprobe.Hop{
			TTL: uint(hop.GetTtl()), Address: hop.GetAddress(), Loss: hop.GetLoss(),
			Avg: time.Duration(hop.GetAvgMs() * float64(time.Millisecond)), Sent: int(hop.GetSent()),
		})
	}
	raw, _ := json.Marshal(hops)
	return raw, true
}

func upsertProbeBucket(db *gorm.DB, collectorUUID string, serverID uint64, kind string, port uint, sampledAt int64, ok bool, rttMs, loss float64) error {
	bucket := sampledAt - sampledAt%int64(time.Minute)
	var row model.ProbeSampleBucket
	err := db.Where("collector_uuid = ? AND server_id = ? AND kind = ? AND port = ? AND bucket_start = ?",
		collectorUUID, serverID, kind, port, bucket).First(&row).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if err == gorm.ErrRecordNotFound {
		row = model.ProbeSampleBucket{CollectorUUID: collectorUUID, ServerID: serverID, Kind: kind, Port: port, BucketStart: bucket}
	}
	row.Count++
	row.LossSum += loss
	if ok {
		row.SuccessCount++
		if row.Count == 1 || rttMs < row.MinMs || row.MinMs == 0 {
			row.MinMs = rttMs
		}
		if rttMs > row.MaxMs {
			row.MaxMs = rttMs
		}
		row.SumMs += rttMs
	} else {
		row.FailCount++
	}
	return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
}

func evaluateProbeAlert(db *gorm.DB, collector *model.Collector, serverID uint64, reachable, hasConnectivity bool, rttMs float64, lastError string, now time.Time) error {
	if !collector.IsProbe() || !collector.ProbeNotify {
		return nil
	}
	var server model.Server
	_ = db.First(&server, serverID).Error
	target := ResolveProbeTarget(db, server)
	v4, v6 := CollectorIPFamilies(collector)
	target = ApplyProbeIPFamilies(target, v4, v6)
	if target.Source == "none" || !hasConnectivity || !HasActiveProbe(server, collector, target) {
		return nil
	}
	var state model.ProbeAlertState
	if err := db.Where("collector_uuid = ? AND server_id = ?", collector.CollectorUUID, serverID).First(&state).Error; err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	threshold := collector.FailThreshold
	if threshold == 0 {
		threshold = model.DefaultProbeFailThreshold
	}
	tag := collector.NotificationTag
	if tag == "" {
		tag = "default"
	}
	if reachable {
		if state.DownNotified {
			singleton.SendNotification(tag, fmt.Sprintf("[ProbeUp] %s → %s", collector.Name, server.Name), singleton.NotificationMuteLabel.ProbeUp(collector.CollectorUUID, serverID), &server)
		}
		state.ConsecutiveFails = 0
		state.DownNotified = false
	} else {
		state.ConsecutiveFails++
		if !state.DownNotified && state.ConsecutiveFails >= threshold {
			errText := lastError
			if errText == "" {
				errText = "unreachable"
			}
			singleton.SendNotification(tag, fmt.Sprintf("[ProbeDown] %s → %s %s", collector.Name, server.Name, errText), singleton.NotificationMuteLabel.ProbeDown(collector.CollectorUUID, serverID), &server)
			state.DownNotified = true
		}
	}
	if collector.LatencyNotify && reachable && rttMs > 0 {
		over := (collector.MaxLatencyMs > 0 && rttMs > collector.MaxLatencyMs) || (collector.MinLatencyMs > 0 && rttMs < collector.MinLatencyMs)
		if over && !state.LatencyAlert {
			singleton.SendNotification(tag, fmt.Sprintf("[ProbeLatency] %s → %s %.0fms", collector.Name, server.Name, rttMs), singleton.NotificationMuteLabel.ProbeLatency(collector.CollectorUUID, serverID), &server)
			state.LatencyAlert = true
		}
		if !over {
			state.LatencyAlert = false
		}
	}
	state.CollectorUUID = collector.CollectorUUID
	state.ServerID = serverID
	state.UpdatedAt = now
	return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&state).Error
}

type ResolvedProbeTarget struct {
	Source   string
	Hostname string
	IPv4     string
	IPv6     string
}

func ResolveProbeTarget(db *gorm.DB, server model.Server) ResolvedProbeTarget {
	override := strings.TrimSpace(server.ProbeTarget)
	if override != "" {
		if ip := net.ParseIP(override); ip != nil {
			if ip.To4() != nil {
				return ResolvedProbeTarget{Source: "override", IPv4: ip.String()}
			}
			return ResolvedProbeTarget{Source: "override", IPv6: ip.String()}
		}
		return ResolvedProbeTarget{Source: "override", Hostname: override}
	}
	ip := ""
	singleton.ServerLock.RLock()
	if running := singleton.ServerList[server.ID]; running != nil && running.Host != nil {
		ip = strings.TrimSpace(running.Host.IP)
	}
	singleton.ServerLock.RUnlock()
	if ip == "" && db != nil {
		var runtime model.ServerRuntime
		if err := db.Select("last_ip").First(&runtime, "server_id = ?", server.ID).Error; err == nil {
			ip = strings.TrimSpace(runtime.LastIP)
		}
	}
	if ip == "" {
		return ResolvedProbeTarget{Source: "none"}
	}
	ipv4, ipv6, _ := utils.SplitIPAddr(ip)
	if ipv4 == "" && ipv6 == "" {
		return ResolvedProbeTarget{Source: "none"}
	}
	return ResolvedProbeTarget{Source: "host_ip", IPv4: ipv4, IPv6: ipv6}
}

func ResolveProbeTCPPorts(server model.Server, collector *model.Collector) []uint32 {
	ports := netprobe.ParsePorts(strings.TrimSpace(server.ProbeTCPPorts))
	if len(ports) == 0 && collector != nil {
		ports = netprobe.ParsePorts(collector.TCPPorts)
	}
	out := make([]uint32, 0, len(ports))
	for _, port := range ports {
		out = append(out, uint32(port))
	}
	return out
}

func EffectiveProbeTypes(server model.Server, collector *model.Collector) (icmp, tcp, mtr bool) {
	if collector == nil {
		return model.BoolOrTrue(server.ProbeEnableICMP), model.BoolOrTrue(server.ProbeEnableTCP), model.BoolOrTrue(server.ProbeEnableMTR)
	}
	return collector.EnableICMP && model.BoolOrTrue(server.ProbeEnableICMP), collector.EnableTCP && model.BoolOrTrue(server.ProbeEnableTCP), collector.EnableMTR && model.BoolOrTrue(server.ProbeEnableMTR)
}

func CollectorIPFamilies(collector *model.Collector) (v4, v6 bool) {
	if collector == nil {
		return true, true
	}
	collector.ApplyProbeDefaults()
	return model.BoolOrTrue(collector.EnableIPv4), model.BoolOrTrue(collector.EnableIPv6)
}

func ProbeConfigIPFamilies(cfg *pb.ProbeConfig) (v4, v6 bool) {
	if cfg == nil || len(cfg.GetIpFamilies()) == 0 {
		return true, true
	}
	for _, family := range cfg.GetIpFamilies() {
		switch family {
		case pb.ProbeIPFamily_PROBE_IP_FAMILY_IPV4:
			v4 = true
		case pb.ProbeIPFamily_PROBE_IP_FAMILY_IPV6:
			v6 = true
		}
	}
	if !v4 && !v6 {
		return true, true
	}
	return v4, v6
}

func IPFamiliesProto(collector *model.Collector) []pb.ProbeIPFamily {
	v4, v6 := CollectorIPFamilies(collector)
	if v4 && v6 {
		return nil
	}
	var families []pb.ProbeIPFamily
	if v4 {
		families = append(families, pb.ProbeIPFamily_PROBE_IP_FAMILY_IPV4)
	}
	if v6 {
		families = append(families, pb.ProbeIPFamily_PROBE_IP_FAMILY_IPV6)
	}
	return families
}

func ApplyProbeIPFamilies(resolved ResolvedProbeTarget, v4, v6 bool) ResolvedProbeTarget {
	if !v4 {
		resolved.IPv4 = ""
	}
	if !v6 {
		resolved.IPv6 = ""
	}
	if resolved.IPv4 == "" && resolved.IPv6 == "" && resolved.Hostname == "" {
		resolved.Source = "none"
	}
	return resolved
}

func HasActiveProbe(server model.Server, collector *model.Collector, resolved ResolvedProbeTarget) bool {
	icmp, tcp, mtr := EffectiveProbeTypes(server, collector)
	if !icmp && !tcp && !mtr {
		return false
	}
	return resolved.Source != "none"
}

func BuildProbeTargets(db *gorm.DB, collector *model.Collector) ([]*pb.ProbeTarget, error) {
	if collector == nil || !collector.IsProbe() {
		return nil, nil
	}
	collector.ApplyProbeDefaults()
	var scopes []model.CollectorScope
	if err := db.Where("collector_uuid = ?", collector.CollectorUUID).Find(&scopes).Error; err != nil {
		return nil, err
	}
	var servers []model.Server
	if err := db.Find(&servers).Error; err != nil {
		return nil, err
	}
	v4, v6 := CollectorIPFamilies(collector)
	var targets []*pb.ProbeTarget
	for _, server := range servers {
		if !singleton.CollectorScopesIncludeServer(scopes, server) {
			continue
		}
		resolved := ApplyProbeIPFamilies(ResolveProbeTarget(db, server), v4, v6)
		icmp, tcp, mtr := EffectiveProbeTypes(server, collector)
		if !HasActiveProbe(server, collector, resolved) {
			continue
		}
		targets = append(targets, &pb.ProbeTarget{
			ServerId: server.ID, ServerName: server.Name, Ipv4: resolved.IPv4, Ipv6: resolved.IPv6, Hostname: resolved.Hostname,
			TcpPorts: ResolveProbeTCPPorts(server, collector), EnableIcmp: icmp, EnableTcp: tcp, EnableMtr: mtr,
			IntervalSeconds: uint32(collector.ProbeIntervalSec), MtrIntervalSeconds: uint32(collector.MTRIntervalSec),
		})
	}
	return targets, nil
}

func ProbeConfigFromCollector(collector *model.Collector) *pb.ProbeConfig {
	if collector == nil {
		return nil
	}
	collector.ApplyProbeDefaults()
	kind := pb.CollectorKind_COLLECTOR_KIND_OBSERVER
	if collector.IsProbe() {
		kind = pb.CollectorKind_COLLECTOR_KIND_PROBE
	}
	ports := netprobe.ParsePorts(collector.TCPPorts)
	pbPorts := make([]uint32, 0, len(ports))
	for _, port := range ports {
		pbPorts = append(pbPorts, uint32(port))
	}
	return &pb.ProbeConfig{
		Kind: kind, IntervalSeconds: uint32(collector.ProbeIntervalSec), MtrIntervalSeconds: uint32(collector.MTRIntervalSec),
		TcpPorts: pbPorts, EnableIcmp: collector.EnableICMP, EnableTcp: collector.EnableTCP, EnableMtr: collector.EnableMTR,
		Notify: collector.ProbeNotify, NotificationTag: collector.NotificationTag, LatencyNotify: collector.LatencyNotify,
		MinLatencyMs: collector.MinLatencyMs, MaxLatencyMs: collector.MaxLatencyMs, FailThreshold: uint32(collector.FailThreshold),
		IpFamilies: IPFamiliesProto(collector),
	}
}

func ProtoCollectorKind(kind string) pb.CollectorKind {
	if model.NormalizeCollectorKind(kind) == model.CollectorKindProbe {
		return pb.CollectorKind_COLLECTOR_KIND_PROBE
	}
	return pb.CollectorKind_COLLECTOR_KIND_OBSERVER
}
