package collector

import (
	"context"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/netprobe"
)

func TestRunProbeTargetPassesMTRProbes(t *testing.T) {
	origICMP, origTCP, origMTR, origTCPMTR := icmpOn, tcpOn, mtrOn, mtrTCPOn
	t.Cleanup(func() {
		icmpOn, tcpOn, mtrOn, mtrTCPOn = origICMP, origTCP, origMTR, origTCPMTR
	})

	icmpOn = func(context.Context, string, string, int, time.Duration) netprobe.ICMPResult {
		return netprobe.ICMPResult{OK: true, Sent: 5, Received: 5}
	}
	tcpOn = func(_ context.Context, _ string, _ string, port uint, _ time.Duration) netprobe.TCPResult {
		return netprobe.TCPResult{OK: true, Port: port}
	}
	var mtrCalls, tcpMTRCalls, mtrProbes, tcpProbes int
	mtrOn = func(_ context.Context, _ string, _ string, _ int, probesPerHop int, _ time.Duration) netprobe.TraceResult {
		mtrCalls++
		mtrProbes = probesPerHop
		return netprobe.TraceResult{}
	}
	mtrTCPOn = func(_ context.Context, _ string, _ string, _ uint, _ int, probesPerHop int, _ time.Duration) netprobe.TraceResult {
		tcpMTRCalls++
		tcpProbes = probesPerHop
		return netprobe.TraceResult{}
	}

	runtime := &Runtime{}
	now := time.Unix(1_800_000_000, 0)
	target := model.CollectorCachedProbeTarget{
		ServerID: 1, IPv4: "192.0.2.1", EnableIPv4: true, EnableICMP: true, EnableTCP: true, EnableMTR: true,
		TCPPorts: "443", MTRProbes: 10, MTRIntervalSec: 300,
	}
	if sample := runtime.runProbeTarget(context.Background(), target, &probeState{}, now); sample == nil {
		t.Fatal("expected sample")
	}
	if mtrCalls != 1 || tcpMTRCalls != 1 || mtrProbes != 10 || tcpProbes != 10 {
		t.Fatalf("mtr calls=%d/%d probes=%d/%d", mtrCalls, tcpMTRCalls, mtrProbes, tcpProbes)
	}

	mtrCalls, tcpMTRCalls, mtrProbes, tcpProbes = 0, 0, 0, 0
	target.MTRProbes = 0
	if sample := runtime.runProbeTarget(context.Background(), target, &probeState{}, now); sample == nil {
		t.Fatal("expected sample for default probes")
	}
	if mtrProbes != 10 || tcpProbes != 10 {
		t.Fatalf("zero probes should fall back to 10, got %d/%d", mtrProbes, tcpProbes)
	}

	mtrCalls, tcpMTRCalls = 0, 0
	target.EnableMTR = false
	target.MTRProbes = 10
	runtime.runProbeTarget(context.Background(), target, &probeState{}, now)
	if mtrCalls != 0 || tcpMTRCalls != 0 {
		t.Fatalf("disabled mtr still called: %d/%d", mtrCalls, tcpMTRCalls)
	}
}
