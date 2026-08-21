package collector

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/nexttrace"
)

func TestRouteDueUsesHourDayWeek(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	if !routeDue(time.Time{}, now, 30) {
		t.Fatal("first run should be due")
	}
	if routeDue(now, now.Add(30*time.Minute), 86400) {
		t.Fatal("half hour should not fire daily interval")
	}
	if !routeDue(now, now.Add(2*time.Hour), 3600) {
		t.Fatal("hour interval should fire")
	}
}

func TestScheduleRoutesRespectsICMPAndTCPGates(t *testing.T) {
	runtime := &Runtime{routeCh: make(chan routeWork, 8)}
	now := time.Now()
	states := map[uint64]*probeState{}
	runtime.scheduleRoutes(now, []model.CollectorCachedProbeTarget{
		{ServerID: 1, EnableICMP: false, EnableTCP: false, IPv4: "1.1.1.1", EnableIPv4: true, RouteIntervalSec: 3600, TCPPorts: "443"},
	}, states)
	if len(runtime.routeCh) != 0 {
		t.Fatal("disabled protocols must not enqueue")
	}
	runtime.scheduleRoutes(now, []model.CollectorCachedProbeTarget{
		{ServerID: 2, EnableICMP: true, EnableTCP: false, IPv4: "1.1.1.1", EnableIPv4: true, RouteIntervalSec: 3600},
	}, states)
	if len(runtime.routeCh) != 1 {
		t.Fatalf("icmp on should enqueue once, got %d", len(runtime.routeCh))
	}
	work := <-runtime.routeCh
	if work.Protocol != "icmp" {
		t.Fatalf("got %s", work.Protocol)
	}
}

func fakeNexttrace(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nexttrace")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExecRouteTCPUsesTCPFlagAndExplicitPath(t *testing.T) {
	orig := nexttrace.Runner
	t.Cleanup(func() { nexttrace.Runner = orig })
	bin := fakeNexttrace(t)
	var saw []string
	nexttrace.Runner = func(ctx context.Context, path string, args []string) ([]byte, error) {
		saw = append([]string{path}, args...)
		return []byte(`{"Hops":[[{"Success":true,"Address":"9.9.9.9","TTL":1,"RTT":1000000}]]}`), nil
	}
	runtime := &Runtime{config: model.CollectorModeConfig{NexttracePath: bin}}
	sample := runtime.execRoute(context.Background(), routeWork{
		Target:   model.CollectorCachedProbeTarget{ServerID: 3, IPv4: "1.1.1.1", EnableIPv4: true, TCPPorts: "443"},
		Protocol: "tcp",
		Port:     443,
	})
	if sample == nil || sample.GetRouteTcp() == nil || len(sample.GetRouteTcp().GetHops()) != 1 {
		t.Fatalf("%+v", sample)
	}
	joined := strings.Join(saw, " ")
	if !strings.Contains(joined, "--tcp") || !strings.Contains(joined, "--port 443") || strings.Contains(joined, " --udp") {
		t.Fatalf("args %s", joined)
	}
	if saw[0] != bin {
		t.Fatalf("bin %s", saw[0])
	}
}

func TestExecRouteICMPDoesNotPassTCP(t *testing.T) {
	orig := nexttrace.Runner
	t.Cleanup(func() { nexttrace.Runner = orig })
	var args []string
	nexttrace.Runner = func(ctx context.Context, bin string, got []string) ([]byte, error) {
		args = got
		return []byte(`{"Hops":[]}`), nil
	}
	runtime := &Runtime{config: model.CollectorModeConfig{NexttracePath: fakeNexttrace(t)}}
	sample := runtime.execRoute(context.Background(), routeWork{
		Target:   model.CollectorCachedProbeTarget{ServerID: 4, IPv4: "1.1.1.1", EnableIPv4: true},
		Protocol: "icmp",
	})
	if sample.GetRouteIcmp() == nil || sample.GetRouteTcp() != nil {
		t.Fatal("icmp sample should only set route_icmp")
	}
	for _, arg := range args {
		if arg == "--tcp" {
			t.Fatal("icmp must not pass --tcp")
		}
	}
}

func TestExecRouteMissingExplicitPathDoesNotFallback(t *testing.T) {
	runtime := &Runtime{config: model.CollectorModeConfig{NexttracePath: "/tmp/does-not-exist-nexttrace"}}
	t.Setenv("PATH", t.TempDir())
	sample := runtime.execRoute(context.Background(), routeWork{
		Target:   model.CollectorCachedProbeTarget{ServerID: 5, IPv4: "1.1.1.1", EnableIPv4: true},
		Protocol: "icmp",
	})
	if sample.GetRouteIcmp() == nil || sample.GetRouteIcmp().GetError() == "" {
		t.Fatal("missing explicit binary should error")
	}
	if !strings.Contains(sample.GetRouteIcmp().GetError(), "未找到 nexttrace") {
		t.Fatalf("%s", sample.GetRouteIcmp().GetError())
	}
}

func TestAcceptRouteJobANDGate(t *testing.T) {
	target := model.CollectorCachedProbeTarget{EnableICMP: true, EnableTCP: false}
	if !acceptRouteJob(target, "icmp") || acceptRouteJob(target, "tcp") || acceptRouteJob(target, "udp") {
		t.Fatal("AND gate")
	}
}
