package collector

import (
	"context"
	"strings"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/geoip"
	"github.com/hi2shark/santaizi-dashboard/pkg/netprobe"
	"github.com/hi2shark/santaizi-dashboard/pkg/nexttrace"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
)

type routeWork struct {
	Target   model.CollectorCachedProbeTarget
	Protocol string
	Port     uint
	JobID    uint64
}

func (r *Runtime) routeWorker() {
	defer r.wg.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case work := <-r.routeCh:
			sample := r.execRoute(r.ctx, work)
			if sample != nil {
				r.queueProbeSamples([]*pb.ProbeSample{sample})
			}
			r.markRouteDone(work.Target.ServerID, work.Protocol)
		}
	}
}

func (r *Runtime) enqueueRoute(work routeWork) bool {
	if r.routeCh == nil {
		return false
	}
	key := routeKey(work.Target.ServerID, work.Protocol)
	r.probeMu.Lock()
	if r.routeInflight == nil {
		r.routeInflight = map[string]struct{}{}
	}
	if _, busy := r.routeInflight[key]; busy {
		r.probeMu.Unlock()
		return false
	}
	r.routeInflight[key] = struct{}{}
	r.probeMu.Unlock()
	select {
	case r.routeCh <- work:
		return true
	default:
		r.probeMu.Lock()
		delete(r.routeInflight, key)
		r.probeMu.Unlock()
		return false
	}
}

func (r *Runtime) markRouteDone(serverID uint64, protocol string) {
	r.probeMu.Lock()
	delete(r.routeInflight, routeKey(serverID, protocol))
	r.probeMu.Unlock()
}

func routeKey(serverID uint64, protocol string) string {
	return strings.ToLower(protocol) + "/" + itoaUint(serverID)
}

func itoaUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	digits := [20]byte{}
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}

func (r *Runtime) scheduleRoutes(now time.Time, targets []model.CollectorCachedProbeTarget, states map[uint64]*probeState) {
	for _, target := range targets {
		state := states[target.ServerID]
		if state == nil {
			state = &probeState{}
			states[target.ServerID] = state
		}
		interval := model.NormalizeRouteIntervalSec(target.RouteIntervalSec)
		if target.EnableICMP && routeDue(state.lastRouteICMP, now, interval) {
			if r.enqueueRoute(routeWork{Target: target, Protocol: "icmp"}) {
				state.lastRouteICMP = now
			}
		}
		if target.EnableTCP && routeDue(state.lastRouteTCP, now, interval) {
			port := netprobe.TCPMTRPort(nil, netprobe.ParsePorts(target.TCPPorts))
			if port > 0 && r.enqueueRoute(routeWork{Target: target, Protocol: "tcp", Port: port}) {
				state.lastRouteTCP = now
			}
		}
	}
}

func routeDue(last time.Time, now time.Time, intervalSec uint) bool {
	every := time.Duration(model.NormalizeRouteIntervalSec(intervalSec)) * time.Second
	return last.IsZero() || now.Sub(last) >= every
}

func (r *Runtime) enqueueRouteJobs(batch *pb.ProbeRouteJobBatch) {
	if batch == nil {
		return
	}
	targets, err := r.store.ProbeTargets(r.ctx)
	if err != nil {
		return
	}
	byID := map[uint64]model.CollectorCachedProbeTarget{}
	for _, target := range targets {
		byID[target.ServerID] = target
	}
	for _, job := range batch.GetJobs() {
		target, ok := byID[job.GetServerId()]
		if !ok {
			continue
		}
		protocol := strings.ToLower(job.GetProtocol())
		if !acceptRouteJob(target, protocol) {
			continue
		}
		port := uint(job.GetPort())
		if protocol == "tcp" && port == 0 {
			port = netprobe.TCPMTRPort(nil, netprobe.ParsePorts(target.TCPPorts))
		}
		r.enqueueRoute(routeWork{Target: target, Protocol: protocol, Port: port, JobID: job.GetJobId()})
	}
}

func acceptRouteJob(target model.CollectorCachedProbeTarget, protocol string) bool {
	switch protocol {
	case "icmp":
		return target.EnableICMP
	case "tcp":
		return target.EnableTCP
	default:
		return false
	}
}

func (r *Runtime) execRoute(ctx context.Context, work routeWork) *pb.ProbeSample {
	now := time.Now()
	sample := &pb.ProbeSample{ServerId: work.Target.ServerID, SampledAtUnixNano: now.UnixNano()}
	trace := &pb.ProbeRouteTrace{
		SampledAtUnixNano: now.UnixNano(), Protocol: work.Protocol, Port: uint32(work.Port), JobId: work.JobID,
	}
	dests := netprobe.Destinations(work.Target.IPv4, work.Target.IPv6, work.Target.Hostname, work.Target.EnableIPv4, work.Target.EnableIPv6)
	if len(dests) == 0 {
		trace.Error = "无探测目标"
		assignRoute(sample, work.Protocol, trace)
		return sample
	}
	host, family := dests[0].Host, dests[0].ICMPNet
	for _, dest := range dests {
		if dest.ICMPNet == "ip4" {
			host, family = dest.Host, dest.ICMPNet
			break
		}
	}
	trace.Destination = host
	bin, err := nexttrace.ResolveBinary(r.config.NexttracePath)
	if err != nil {
		trace.Error = err.Error()
		assignRoute(sample, work.Protocol, trace)
		return sample
	}
	opts := nexttrace.TraceOpts{Target: host, TCP: work.Protocol == "tcp", Port: work.Port, IPv4: family == "ip4", IPv6: family == "ip6"}
	result, err := nexttrace.Run(ctx, bin, opts)
	if err != nil {
		trace.Error = err.Error()
		assignRoute(sample, work.Protocol, trace)
		return sample
	}
	if result.Destination != "" {
		trace.Destination = result.Destination
	}
	for _, hop := range result.Hops {
		fillHopGeo(&hop)
		trace.Hops = append(trace.Hops, &pb.ProbeRouteHop{
			Ttl: uint32(hop.TTL), Address: hop.Address, Hostname: hop.Hostname,
			RttMs: hop.RttMs, Loss: hop.Loss, Sent: uint32(hop.Sent),
			Asn: hop.ASN, Country: hop.Country, Province: hop.Province, City: hop.City, Owner: hop.Owner,
		})
	}
	assignRoute(sample, work.Protocol, trace)
	return sample
}

func assignRoute(sample *pb.ProbeSample, protocol string, trace *pb.ProbeRouteTrace) {
	if protocol == "tcp" {
		sample.RouteTcp = trace
		return
	}
	sample.RouteIcmp = trace
}

func fillHopGeo(hop *nexttrace.Hop) {
	if hop == nil || hop.Address == "" {
		return
	}
	info := geoip.LookupHop(hop.Address)
	if hop.Country == "" && info.CountryName != "" {
		hop.Country = info.CountryName
	}
	if hop.ASN == "" && info.ASName != "" {
		hop.ASN = info.ASName
	}
}
