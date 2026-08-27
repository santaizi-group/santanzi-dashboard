package collector

import (
	"context"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/netprobe"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
)

var (
	icmpOn   = netprobe.ICMPOn
	tcpOn    = netprobe.TCPOn
	mtrOn    = netprobe.MTROn
	mtrTCPOn = netprobe.MTRTCPOn
)

type probeState struct {
	lastCycle     time.Time
	lastMTR       time.Time
	lastRouteICMP time.Time
	lastRouteTCP  time.Time
	lastReachable *bool
}

func (r *Runtime) isProbe() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.kind == model.CollectorKindProbe
}

func (r *Runtime) setKind(kind string) {
	r.mu.Lock()
	r.kind = model.NormalizeCollectorKind(kind)
	r.mu.Unlock()
}

func (r *Runtime) probeLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	states := map[uint64]*probeState{}
	for {
		select {
		case <-r.ctx.Done():
			return
		case now := <-ticker.C:
			if !r.isProbe() {
				continue
			}
			targets, err := r.store.ProbeTargets(r.ctx)
			if err != nil {
				continue
			}
			live := make(map[uint64]struct{}, len(targets))
			for _, target := range targets {
				live[target.ServerID] = struct{}{}
			}
			for id := range states {
				if _, ok := live[id]; !ok {
					delete(states, id)
				}
			}
			if len(targets) == 0 {
				continue
			}
			var batch []*pb.ProbeSample
			for _, target := range targets {
				state := states[target.ServerID]
				if state == nil {
					state = &probeState{}
					states[target.ServerID] = state
				}
				interval := time.Duration(target.IntervalSec) * time.Second
				if interval <= 0 {
					interval = 30 * time.Second
				}
				if !state.lastCycle.IsZero() && now.Sub(state.lastCycle) < interval {
					continue
				}
				sample := r.runProbeTarget(r.ctx, target, state, now)
				state.lastCycle = now
				if sample != nil {
					batch = append(batch, sample)
				}
			}
			if len(batch) > 0 {
				r.queueProbeSamples(batch)
			}
			r.scheduleRoutes(now, targets, states)
		}
	}
}

func (r *Runtime) runProbeTarget(ctx context.Context, target model.CollectorCachedProbeTarget, state *probeState, now time.Time) *pb.ProbeSample {
	dests := netprobe.Destinations(target.IPv4, target.IPv6, target.Hostname, target.EnableIPv4, target.EnableIPv6)
	if len(dests) == 0 {
		return nil
	}
	sample := &pb.ProbeSample{ServerId: target.ServerID, SampledAtUnixNano: now.UnixNano()}
	var icmp netprobe.ICMPResult
	if target.EnableICMP {
		var results []netprobe.ICMPResult
		for _, dest := range dests {
			results = append(results, icmpOn(ctx, dest.Host, dest.ICMPNet, 5, 2*time.Second))
		}
		icmp = netprobe.MergeICMP(results)
		sample.Icmp = &pb.ProbeICMPSample{
			Ok: icmp.OK, RttMs: durationMilliseconds(icmp.RTT), Loss: icmp.Loss,
			PacketsSent: uint32(icmp.Sent), PacketsReceived: uint32(icmp.Received), Error: icmp.Error,
		}
	}
	var tcpResults []netprobe.TCPResult
	if target.EnableTCP {
		for _, dest := range dests {
			for _, port := range netprobe.ParsePorts(target.TCPPorts) {
				tcpResults = append(tcpResults, tcpOn(ctx, dest.Host, dest.TCPNet, port, 3*time.Second))
			}
		}
		tcpResults = netprobe.MergeTCPByPort(tcpResults)
		for _, result := range tcpResults {
			sample.Tcp = append(sample.Tcp, &pb.ProbeTCPSample{
				Port: uint32(result.Port), Ok: result.OK, RttMs: durationMilliseconds(result.RTT), Error: result.Error,
			})
		}
	}
	hasConnectivity := target.EnableICMP || target.EnableTCP
	reachable := false
	if target.EnableICMP {
		reachable = icmp.OK
	}
	for _, item := range tcpResults {
		if item.OK {
			reachable = true
			break
		}
	}
	if !hasConnectivity {
		reachable = true
	} else {
		sample.LastError = netprobe.DisplayError(icmp, tcpResults)
	}
	flipped := state.lastReachable != nil && *state.lastReachable != reachable
	state.lastReachable = &reachable
	mtrEvery := time.Duration(target.MTRIntervalSec) * time.Second
	if mtrEvery <= 0 {
		mtrEvery = 5 * time.Minute
	}
	if target.EnableMTR && (flipped || state.lastMTR.IsZero() || now.Sub(state.lastMTR) >= mtrEvery) {
		mtrHost, mtrFamily := dests[0].Host, dests[0].ICMPNet
		for _, dest := range dests {
			if dest.ICMPNet == "ip4" {
				mtrHost, mtrFamily = dest.Host, dest.ICMPNet
				break
			}
		}
		sampledAt := now.UnixNano()
		probes := int(model.NormalizeMTRProbes(target.MTRProbes))
		if target.EnableICMP {
			sample.Mtr = probeTraceToProto(mtrOn(ctx, mtrHost, mtrFamily, 30, probes, time.Second), "icmp", 0, sampledAt)
		}
		if target.EnableTCP {
			port := netprobe.TCPMTRPort(tcpResults, netprobe.ParsePorts(target.TCPPorts))
			if port > 0 {
				sample.MtrTcp = probeTraceToProto(mtrTCPOn(ctx, mtrHost, mtrFamily, port, 30, probes, time.Second), "tcp", port, sampledAt)
			}
		}
		state.lastMTR = now
	}
	return sample
}

func probeTraceToProto(trace netprobe.TraceResult, protocol string, port uint, sampledAt int64) *pb.ProbeMTRTrace {
	pbTrace := &pb.ProbeMTRTrace{
		SampledAtUnixNano: sampledAt, Destination: trace.Destination, Protocol: protocol, Port: uint32(port),
	}
	for _, hop := range trace.Hops {
		pbTrace.Hops = append(pbTrace.Hops, &pb.ProbeMTRHop{
			Ttl: uint32(hop.TTL), Address: hop.Address, Loss: hop.Loss,
			AvgMs: durationMilliseconds(hop.Avg), Sent: uint32(hop.Sent),
		})
	}
	return pbTrace
}

func (r *Runtime) queueProbeSamples(samples []*pb.ProbeSample) {
	r.probeMu.Lock()
	r.pendingSamples = append(r.pendingSamples, samples...)
	if len(r.pendingSamples) > 256 {
		r.pendingSamples = r.pendingSamples[len(r.pendingSamples)-256:]
	}
	r.probeMu.Unlock()
}

func (r *Runtime) takeProbeSamples() *pb.ProbeSampleBatch {
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	if len(r.pendingSamples) == 0 {
		return nil
	}
	batch := &pb.ProbeSampleBatch{Samples: r.pendingSamples}
	r.pendingSamples = nil
	return batch
}
