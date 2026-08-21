package telemetry

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/geoip"
	"github.com/hi2shark/santaizi-dashboard/pkg/nexttrace"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"gorm.io/gorm"
)

const probeRouteJobTTL = 10 * time.Minute

type ProbeRouteHopView struct {
	TTL      uint    `json:"ttl"`
	Address  string  `json:"address,omitempty"`
	Hostname string  `json:"hostname,omitempty"`
	RttMs    float64 `json:"rtt_ms,omitempty"`
	Loss     float64 `json:"loss,omitempty"`
	Sent     uint    `json:"sent,omitempty"`
	ASN      string  `json:"asn,omitempty"`
	Country  string  `json:"country,omitempty"`
	Province string  `json:"province,omitempty"`
	City     string  `json:"city,omitempty"`
	Owner    string  `json:"owner,omitempty"`
	Geo      string  `json:"geo,omitempty"`
	Private  bool    `json:"private,omitempty"`
}

type ProbeRouteRecordView struct {
	ID          uint64              `json:"id"`
	SampledAt   int64               `json:"sampled_at"`
	Destination string              `json:"destination,omitempty"`
	Port        uint                `json:"port,omitempty"`
	Error       string              `json:"error,omitempty"`
	JobID       uint64              `json:"job_id,omitempty"`
	Hops        []ProbeRouteHopView `json:"hops"`
}

type ProbeRouteJobView struct {
	ID          uint64 `json:"id"`
	Protocol    string `json:"protocol"`
	Port        uint   `json:"port,omitempty"`
	Status      string `json:"status"`
	RequestedAt int64  `json:"requested_at"`
	Error       string `json:"error,omitempty"`
}

type ProbeRouteHistoryView struct {
	CollectorID string                 `json:"collector_id"`
	ServerID    uint64                 `json:"server_id"`
	EnableICMP  bool                   `json:"enable_icmp"`
	EnableTCP   bool                   `json:"enable_tcp"`
	ICMP        []ProbeRouteRecordView `json:"icmp"`
	TCP         []ProbeRouteRecordView `json:"tcp"`
	Job         *ProbeRouteJobView     `json:"job,omitempty"`
}

func ingestProbeRoutes(db *gorm.DB, collector *model.Collector, sample *pb.ProbeSample, now time.Time) error {
	if collector == nil || sample == nil {
		return nil
	}
	keep := model.NormalizeRouteKeep(collector.RouteKeep)
	for _, trace := range []*pb.ProbeRouteTrace{sample.GetRouteIcmp(), sample.GetRouteTcp()} {
		if trace == nil {
			continue
		}
		if err := insertProbeRoute(db, collector.CollectorUUID, sample.GetServerId(), keep, trace, now); err != nil {
			return err
		}
		if jobID := trace.GetJobId(); jobID > 0 {
			if err := finishProbeRouteJob(db, jobID, trace.GetError(), now); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertProbeRoute(db *gorm.DB, collectorUUID string, serverID uint64, keep uint, trace *pb.ProbeRouteTrace, now time.Time) error {
	protocol := strings.ToLower(strings.TrimSpace(trace.GetProtocol()))
	if protocol != "icmp" && protocol != "tcp" {
		return nil
	}
	hops := nexttrace.ProtoToHops(trace.GetHops())
	hopsJSON, err := json.Marshal(hops)
	if err != nil {
		return err
	}
	sampledAt := trace.GetSampledAtUnixNano()
	if sampledAt == 0 {
		sampledAt = now.UnixNano()
	}
	row := model.ProbeRoute{
		CollectorUUID: collectorUUID, ServerID: serverID, Protocol: protocol,
		SampledAt: sampledAt, Destination: trace.GetDestination(), Port: uint(trace.GetPort()),
		HopsJSON: hopsJSON, Error: strings.TrimSpace(trace.GetError()), JobID: trace.GetJobId(), UpdatedAt: now,
	}
	if err := db.Create(&row).Error; err != nil {
		return err
	}
	return pruneProbeRoutes(db, collectorUUID, serverID, protocol, keep)
}

func pruneProbeRoutes(db *gorm.DB, collectorUUID string, serverID uint64, protocol string, keep uint) error {
	keep = model.NormalizeRouteKeep(keep)
	var ids []uint64
	if err := db.Model(&model.ProbeRoute{}).
		Where("collector_uuid = ? AND server_id = ? AND protocol = ?", collectorUUID, serverID, protocol).
		Order("sampled_at DESC, id DESC").
		Offset(int(keep)).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return db.Where("id IN ?", ids).Delete(&model.ProbeRoute{}).Error
}

func PruneAllProbeRoutes(db *gorm.DB) error {
	var collectors []model.Collector
	if err := db.Where("kind = ? AND deleted = ?", model.CollectorKindProbe, false).Find(&collectors).Error; err != nil {
		return err
	}
	keepByUUID := map[string]uint{}
	for _, collector := range collectors {
		keepByUUID[collector.CollectorUUID] = model.NormalizeRouteKeep(collector.RouteKeep)
	}
	var keys []struct {
		CollectorUUID string
		ServerID      uint64
		Protocol      string
	}
	if err := db.Model(&model.ProbeRoute{}).Distinct("collector_uuid", "server_id", "protocol").Find(&keys).Error; err != nil {
		return err
	}
	for _, key := range keys {
		keep := keepByUUID[key.CollectorUUID]
		if keep == 0 {
			keep = model.DefaultRouteKeep
		}
		if err := pruneProbeRoutes(db, key.CollectorUUID, key.ServerID, key.Protocol, keep); err != nil {
			return err
		}
	}
	return nil
}

func ListProbeRoutes(db *gorm.DB, collectorID string, serverID uint64) (*ProbeRouteHistoryView, error) {
	if collectorID == "" || serverID == 0 {
		return nil, errors.New("collector_id and server_id are required")
	}
	var collector model.Collector
	if err := db.Where("collector_uuid = ? AND deleted = ?", collectorID, false).First(&collector).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return errView(err)
	}
	var server model.Server
	if err := db.First(&server, serverID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return errView(err)
	}
	icmpOn, tcpOn, _ := EffectiveProbeTypes(server, &collector)
	view := &ProbeRouteHistoryView{
		CollectorID: collectorID, ServerID: serverID, EnableICMP: icmpOn, EnableTCP: tcpOn,
	}
	keep := model.NormalizeRouteKeep(collector.RouteKeep)
	icmp, err := loadProbeRouteRecords(db, collectorID, serverID, "icmp", keep)
	if err != nil {
		return nil, err
	}
	tcp, err := loadProbeRouteRecords(db, collectorID, serverID, "tcp", keep)
	if err != nil {
		return nil, err
	}
	view.ICMP, view.TCP = icmp, tcp
	job, err := latestPendingProbeRouteJob(db, collectorID, serverID, time.Now())
	if err != nil {
		return nil, err
	}
	view.Job = job
	return view, nil
}

func errView(err error) (*ProbeRouteHistoryView, error) {
	return nil, err
}

func loadProbeRouteRecords(db *gorm.DB, collectorID string, serverID uint64, protocol string, keep uint) ([]ProbeRouteRecordView, error) {
	var rows []model.ProbeRoute
	if err := db.Where("collector_uuid = ? AND server_id = ? AND protocol = ?", collectorID, serverID, protocol).
		Order("sampled_at DESC, id DESC").Limit(int(keep)).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ProbeRouteRecordView, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProbeRouteRecordView{
			ID: row.ID, SampledAt: row.SampledAt, Destination: row.Destination, Port: row.Port,
			Error: row.Error, JobID: row.JobID, Hops: decodeRouteHops(row.HopsJSON),
		})
	}
	return out, nil
}

func decodeRouteHops(raw []byte) []ProbeRouteHopView {
	if len(raw) == 0 {
		return []ProbeRouteHopView{}
	}
	var hops []nexttrace.Hop
	if err := json.Unmarshal(raw, &hops); err != nil {
		return []ProbeRouteHopView{}
	}
	out := make([]ProbeRouteHopView, 0, len(hops))
	for _, hop := range hops {
		view := ProbeRouteHopView{
			TTL: hop.TTL, Address: hop.Address, Hostname: hop.Hostname, RttMs: hop.RttMs,
			Loss: hop.Loss, Sent: hop.Sent, ASN: hop.ASN, Country: hop.Country,
			Province: hop.Province, City: hop.City, Owner: hop.Owner,
		}
		info := geoip.LookupHop(hop.Address)
		view.Private = info.Private
		if view.Country == "" && info.CountryName != "" {
			view.Country = info.CountryName
		}
		view.Geo = formatRouteHopGeo(view, info)
		out = append(out, view)
	}
	return out
}

func formatRouteHopGeo(hop ProbeRouteHopView, info geoip.HopInfo) string {
	if hop.Private {
		return ""
	}
	parts := make([]string, 0, 5)
	for _, part := range []string{hop.Country, hop.Province, hop.City, hop.Owner} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	if hop.ASN != "" {
		asn := hop.ASN
		if !strings.HasPrefix(strings.ToUpper(asn), "AS") {
			asn = "AS" + asn
		}
		parts = append(parts, asn)
	}
	if len(parts) > 0 {
		return strings.Join(parts, " · ")
	}
	return geoip.FormatHopGeo(info)
}

func EnqueueProbeRouteJob(db *gorm.DB, collectorID string, serverID uint64, protocol string, now time.Time) (*ProbeRouteJobView, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol != "icmp" && protocol != "tcp" {
		return nil, ErrInvalidRouteProtocol
	}
	var collector model.Collector
	if err := db.Where("collector_uuid = ? AND deleted = ?", collectorID, false).First(&collector).Error; err != nil {
		return nil, err
	}
	if !collector.IsProbe() {
		return nil, ErrRouteCollectorKind
	}
	var server model.Server
	if err := db.First(&server, serverID).Error; err != nil {
		return nil, err
	}
	icmpOn, tcpOn, _ := EffectiveProbeTypes(server, &collector)
	if protocol == "icmp" && !icmpOn {
		return nil, ErrRouteProtocolDisabled
	}
	if protocol == "tcp" && !tcpOn {
		return nil, ErrRouteProtocolDisabled
	}
	port := uint(0)
	if protocol == "tcp" {
		ports := ResolveProbeTCPPorts(server, &collector)
		if len(ports) == 0 {
			return nil, ErrRouteProtocolDisabled
		}
		port = uint(ports[0])
	}
	if err := expireStaleProbeRouteJobs(db, now); err != nil {
		return nil, err
	}
	var existing model.ProbeRouteJob
	err := db.Where("collector_uuid = ? AND server_id = ? AND protocol = ? AND status = ?", collectorID, serverID, protocol, model.ProbeRouteJobPending).
		Order("id DESC").First(&existing).Error
	if err == nil {
		return jobView(&existing), nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	job := model.ProbeRouteJob{
		CollectorUUID: collectorID, ServerID: serverID, Protocol: protocol, Port: port,
		Status: model.ProbeRouteJobPending, RequestedAt: now.UnixNano(), UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		return nil, err
	}
	return jobView(&job), nil
}

var (
	ErrInvalidRouteProtocol  = errors.New("protocol must be icmp or tcp")
	ErrRouteCollectorKind    = errors.New("collector is not a probe")
	ErrRouteProtocolDisabled = errors.New("protocol is disabled for this host")
)

func jobView(job *model.ProbeRouteJob) *ProbeRouteJobView {
	if job == nil {
		return nil
	}
	return &ProbeRouteJobView{
		ID: job.ID, Protocol: job.Protocol, Port: job.Port, Status: job.Status,
		RequestedAt: job.RequestedAt, Error: job.Error,
	}
}

func latestPendingProbeRouteJob(db *gorm.DB, collectorID string, serverID uint64, now time.Time) (*ProbeRouteJobView, error) {
	if err := expireStaleProbeRouteJobs(db, now); err != nil {
		return nil, err
	}
	var job model.ProbeRouteJob
	err := db.Where("collector_uuid = ? AND server_id = ? AND status = ?", collectorID, serverID, model.ProbeRouteJobPending).
		Order("requested_at DESC, id DESC").First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return jobView(&job), nil
}

func ListPendingProbeRouteJobs(db *gorm.DB, collectorUUID string, now time.Time) ([]model.ProbeRouteJob, error) {
	if err := expireStaleProbeRouteJobs(db, now); err != nil {
		return nil, err
	}
	var jobs []model.ProbeRouteJob
	if err := db.Where("collector_uuid = ? AND status = ?", collectorUUID, model.ProbeRouteJobPending).
		Order("id ASC").Find(&jobs).Error; err != nil {
		return nil, err
	}
	return jobs, nil
}

func expireStaleProbeRouteJobs(db *gorm.DB, now time.Time) error {
	cutoff := now.Add(-probeRouteJobTTL).UnixNano()
	return db.Model(&model.ProbeRouteJob{}).
		Where("status = ? AND requested_at < ?", model.ProbeRouteJobPending, cutoff).
		Updates(map[string]any{"status": model.ProbeRouteJobFailed, "error": "从端未领取", "finished_at": now.UnixNano(), "updated_at": now}).Error
}

func finishProbeRouteJob(db *gorm.DB, jobID uint64, lastError string, now time.Time) error {
	if jobID == 0 {
		return nil
	}
	status := model.ProbeRouteJobDone
	if strings.TrimSpace(lastError) != "" {
		status = model.ProbeRouteJobFailed
	}
	return db.Model(&model.ProbeRouteJob{}).Where("id = ? AND status = ?", jobID, model.ProbeRouteJobPending).
		Updates(map[string]any{
			"status": status, "error": strings.TrimSpace(lastError),
			"finished_at": now.UnixNano(), "updated_at": now,
		}).Error
}

func PendingJobsToProto(jobs []model.ProbeRouteJob) *pb.ProbeRouteJobBatch {
	if len(jobs) == 0 {
		return nil
	}
	batch := &pb.ProbeRouteJobBatch{}
	for _, job := range jobs {
		batch.Jobs = append(batch.Jobs, &pb.ProbeRouteJob{
			JobId: job.ID, ServerId: job.ServerID, Protocol: job.Protocol, Port: uint32(job.Port),
		})
	}
	return batch
}
