package telemetry

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/netprobe"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func probeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Collector{}, &model.CollectorScope{}, &model.Server{}, &model.ServerRuntime{}, &model.ProbeSampleBucket{}, &model.ProbeLatest{}, &model.ProbeTrace{}, &model.ProbeAlertState{}, &model.CollectorRuntime{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func probeServer(id uint64, name, secret string) model.Server {
	return model.Server{
		Common: model.Common{ID: id}, Name: name, Secret: secret,
		ProbeEnableICMP: model.BoolPtr(true), ProbeEnableTCP: model.BoolPtr(true), ProbeEnableMTR: model.BoolPtr(true),
	}
}

func TestResolveProbeTargetOverrideAndNone(t *testing.T) {
	db := probeTestDB(t)
	override := ResolveProbeTarget(db, model.Server{Common: model.Common{ID: 1}, ProbeTarget: "origin.example"})
	if override.Source != "override" || override.Hostname != "origin.example" {
		t.Fatalf("%+v", override)
	}
	none := ResolveProbeTarget(db, model.Server{Common: model.Common{ID: 2}})
	if none.Source != "none" {
		t.Fatalf("%+v", none)
	}
}

func TestIngestProbeSampleNoTargetDoesNotAlert(t *testing.T) {
	db := probeTestDB(t)
	previous := singleton.DB
	singleton.DB = db
	t.Cleanup(func() { singleton.DB = previous })
	collector := model.Collector{CollectorUUID: "probe-a", Name: "HK", Kind: model.CollectorKindProbe, TokenHash: bytes.Repeat([]byte{1}, 32), RegistrationToken: "token-a", ProbeNotify: true, FailThreshold: 1, NotificationTag: "default"}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Server{Common: model.Common{ID: 9}, Name: "n9", Secret: "secret-9"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := IngestProbeSamples(db, &collector, &pb.ProbeSampleBatch{Samples: []*pb.ProbeSample{{
		ServerId: 9, SampledAtUnixNano: time.Now().UnixNano(), LastError: "timeout",
		Icmp: &pb.ProbeICMPSample{Ok: false, Error: "timeout"},
	}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var state model.ProbeAlertState
	if err := db.First(&state, "collector_uuid = ? AND server_id = ?", "probe-a", 9).Error; err == nil && state.DownNotified {
		t.Fatal("no-target path should not notify")
	}
}

func TestDisplayRTTPrefersICMPThenTCP(t *testing.T) {
	db := probeTestDB(t)
	collector := model.Collector{CollectorUUID: "probe-b", Name: "JP", Kind: model.CollectorKindProbe, TokenHash: bytes.Repeat([]byte{2}, 32), RegistrationToken: "token-b"}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	if err := IngestProbeSamples(db, &collector, &pb.ProbeSampleBatch{Samples: []*pb.ProbeSample{{
		ServerId: 3, SampledAtUnixNano: time.Now().UnixNano(),
		Icmp: &pb.ProbeICMPSample{Ok: false, Error: "blocked"},
		Tcp:  []*pb.ProbeTCPSample{{Port: 443, Ok: true, RttMs: 42}},
	}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var latest model.ProbeLatest
	if err := db.First(&latest, "collector_uuid = ? AND server_id = ?", "probe-b", 3).Error; err != nil {
		t.Fatal(err)
	}
	if !latest.Reachable || latest.DisplayRttMs != 42 {
		t.Fatalf("%+v", latest)
	}
}

func TestLoadProbePathsFiltersAndEmptyTrace(t *testing.T) {
	db := probeTestDB(t)
	probe := model.Collector{CollectorUUID: "probe-c", Name: "SG", Kind: model.CollectorKindProbe, TokenHash: bytes.Repeat([]byte{3}, 32), RegistrationToken: "token-c"}
	other := model.Collector{CollectorUUID: "probe-d", Name: "US", Kind: model.CollectorKindProbe, TokenHash: bytes.Repeat([]byte{4}, 32), RegistrationToken: "token-d"}
	if err := db.Create(&probe).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "probe-c", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "probe-d", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	alpha := probeServer(4, "alpha", "secret-4")
	alpha.ProbeTarget = "1.1.1.1"
	alpha.DisplayIndex = 42
	alpha.Tag = "edge"
	if err := db.Create(&alpha).Error; err != nil {
		t.Fatal(err)
	}
	beta := probeServer(5, "beta", "secret-5")
	if err := db.Create(&beta).Error; err != nil {
		t.Fatal(err)
	}
	paths, err := LoadProbePaths(db, ProbePathFilter{CollectorID: "probe-c", ServerID: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0].CollectorID != "probe-c" || paths[0].ServerID != 4 || paths[0].TargetSource != "override" || paths[0].DisplayIndex != 42 || paths[0].Tag != "edge" {
		t.Fatalf("%+v", paths)
	}
	none, err := LoadProbePaths(db, ProbePathFilter{CollectorID: "probe-c", ServerID: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 1 || none[0].TargetSource != "none" {
		t.Fatalf("%+v", none)
	}
	trace, err := GetProbeTrace(db, "probe-c", 4)
	if err != nil {
		t.Fatal(err)
	}
	if trace != nil {
		t.Fatalf("expected empty trace, got %+v", trace)
	}
}

func TestLoadProbePathsHidesLatestWhenCollectorOffline(t *testing.T) {
	db := probeTestDB(t)
	now := time.Unix(1_700_000_090, 0)
	probe := model.Collector{CollectorUUID: "probe-e", Name: "FRA", Kind: model.CollectorKindProbe, TokenHash: bytes.Repeat([]byte{5}, 32), RegistrationToken: "token-e"}
	if err := db.Create(&probe).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "probe-e", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	gamma := probeServer(6, "gamma", "secret-6")
	gamma.ProbeTarget = "1.1.1.1"
	if err := db.Create(&gamma).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorRuntime{CollectorUUID: "probe-e", Status: "online", LastSeen: now.Add(-2 * time.Minute).UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeLatest{
		CollectorUUID: "probe-e", ServerID: 6, Reachable: true, DisplayRttMs: 21.5, SampledAt: now.UnixNano(), ICMPOk: true, ICMPRttMs: 21.5,
	}).Error; err != nil {
		t.Fatal(err)
	}
	offline, err := loadProbePaths(db, ProbePathFilter{CollectorID: "probe-e"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(offline) != 1 || offline[0].Reachable || offline[0].DisplayRttMs != 0 || offline[0].SampledAt != 0 {
		t.Fatalf("offline collector should hide latest RTT: %+v", offline)
	}
	if err := db.Model(&model.CollectorRuntime{}).Where("collector_uuid = ?", "probe-e").Update("last_seen", now.Add(-10*time.Second).UnixNano()).Error; err != nil {
		t.Fatal(err)
	}
	online, err := loadProbePaths(db, ProbePathFilter{CollectorID: "probe-e"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 1 || !online[0].Reachable || online[0].DisplayRttMs != 21.5 {
		t.Fatalf("online collector should keep latest RTT: %+v", online)
	}
}

func TestLoadProbePathsIncludesLastHopLoss(t *testing.T) {
	db := probeTestDB(t)
	now := time.Unix(1_700_000_090, 0)
	probe := model.Collector{CollectorUUID: "probe-mtr", Name: "CD", Kind: model.CollectorKindProbe, TokenHash: bytes.Repeat([]byte{8}, 32), RegistrationToken: "token-mtr", EnableICMP: true, EnableTCP: true, EnableMTR: true}
	if err := db.Create(&probe).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "probe-mtr", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	host := probeServer(31, "edge-a", "secret-31")
	host.ProbeTarget = "1.1.1.1"
	if err := db.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorRuntime{CollectorUUID: "probe-mtr", Status: "online", LastSeen: now.Add(-10 * time.Second).UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeLatest{
		CollectorUUID: "probe-mtr", ServerID: 31, Reachable: true, DisplayRttMs: 21.5, SampledAt: now.UnixNano(), ICMPOk: true, ICMPRttMs: 21.5, HasTrace: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	hopsJSON, err := json.Marshal([]netprobe.Hop{
		{TTL: 1, Address: "10.0.0.1", Loss: 0, Avg: 2 * time.Millisecond, Sent: 10},
		{TTL: 8, Address: "1.1.1.1", Loss: 0.12, Avg: 40 * time.Millisecond, Sent: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeTrace{
		CollectorUUID: "probe-mtr", ServerID: 31, SampledAt: now.UnixNano(), Destination: "1.1.1.1", HopsJSON: hopsJSON,
	}).Error; err != nil {
		t.Fatal(err)
	}
	paths, err := loadProbePaths(db, ProbePathFilter{CollectorID: "probe-mtr", ServerID: 31}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || !paths[0].HasTrace || paths[0].MTR.HopCount != 2 || paths[0].MTR.Loss != 0.12 {
		t.Fatalf("last hop loss: %+v", paths)
	}
}

func TestBuildProbeTargetsPortsTypesAndFamilies(t *testing.T) {
	db := probeTestDB(t)
	collector := model.Collector{
		CollectorUUID: "probe-ports", Name: "HK", Kind: model.CollectorKindProbe,
		TokenHash: bytes.Repeat([]byte{6}, 32), RegistrationToken: "token-ports",
		TCPPorts: "22,443", EnableICMP: true, EnableTCP: true, EnableMTR: true, EnableIPv4: model.BoolPtr(true), EnableIPv6: model.BoolPtr(true),
	}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "probe-ports", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	custom := probeServer(11, "custom", "secret-11")
	custom.ProbeTarget = "1.1.1.1"
	custom.ProbeTCPPorts = "2222"
	fallback := probeServer(12, "fallback", "secret-12")
	fallback.ProbeTarget = "8.8.8.8"
	disabledTCP := probeServer(13, "notcp", "secret-13")
	disabledTCP.ProbeTarget = "9.9.9.9"
	disabledTCP.ProbeEnableTCP = model.BoolPtr(false)
	allOff := probeServer(14, "off", "secret-14")
	allOff.ProbeTarget = "4.4.4.4"
	allOff.ProbeEnableICMP, allOff.ProbeEnableTCP, allOff.ProbeEnableMTR = model.BoolPtr(false), model.BoolPtr(false), model.BoolPtr(false)
	for _, server := range []model.Server{custom, fallback, disabledTCP, allOff} {
		if err := db.Create(&server).Error; err != nil {
			t.Fatal(err)
		}
	}
	targets, err := BuildProbeTargets(db, &collector)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uint64]*pb.ProbeTarget{}
	for _, target := range targets {
		byID[target.GetServerId()] = target
	}
	if got := byID[11]; got == nil || len(got.GetTcpPorts()) != 1 || got.GetTcpPorts()[0] != 2222 {
		t.Fatalf("custom ports: %+v", got)
	}
	if got := byID[12]; got == nil || len(got.GetTcpPorts()) != 2 || got.GetTcpPorts()[0] != 22 || got.GetTcpPorts()[1] != 443 {
		t.Fatalf("fallback ports: %+v", got)
	}
	if got := byID[13]; got == nil || got.GetEnableTcp() || !got.GetEnableIcmp() {
		t.Fatalf("host tcp off: %+v", got)
	}
	if _, exists := byID[14]; exists {
		t.Fatal("all-off host should be omitted")
	}

	collector.EnableICMP = false
	if err := db.Save(&collector).Error; err != nil {
		t.Fatal(err)
	}
	targets, err = BuildProbeTargets(db, &collector)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.GetEnableIcmp() {
			t.Fatalf("collector icmp off should win: %+v", target)
		}
	}

	paths, err := LoadProbePaths(db, ProbePathFilter{CollectorID: "probe-ports", ServerID: 14})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0].TargetSource != "none" {
		t.Fatalf("all-off path: %+v", paths)
	}
}

func TestBuildProbeTargetsFiltersIPFamily(t *testing.T) {
	db := probeTestDB(t)
	collector := model.Collector{
		CollectorUUID: "probe-v6", Name: "JP", Kind: model.CollectorKindProbe,
		TokenHash: bytes.Repeat([]byte{7}, 32), RegistrationToken: "token-v6",
		EnableICMP: true, EnableTCP: true, EnableMTR: true, EnableIPv4: model.BoolPtr(false), EnableIPv6: model.BoolPtr(true),
	}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "probe-v6", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	dual := probeServer(21, "dual", "secret-21")
	if err := db.Create(&model.ServerRuntime{ServerID: 21, LastIP: "192.0.2.10/2001:db8::10"}).Error; err != nil {
		t.Fatal(err)
	}
	v4only := probeServer(22, "v4", "secret-22")
	if err := db.Create(&model.ServerRuntime{ServerID: 22, LastIP: "192.0.2.20"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&dual).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&v4only).Error; err != nil {
		t.Fatal(err)
	}
	targets, err := BuildProbeTargets(db, &collector)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].GetServerId() != 21 || targets[0].GetIpv4() != "" || targets[0].GetIpv6() != "2001:db8::10" {
		t.Fatalf("%+v", targets)
	}
	empty := ProbeConfigFromCollector(&model.Collector{Kind: model.CollectorKindProbe, EnableIPv4: model.BoolPtr(true), EnableIPv6: model.BoolPtr(true)})
	if len(empty.GetIpFamilies()) != 0 {
		t.Fatalf("both families should encode as empty: %+v", empty.GetIpFamilies())
	}
	v4, v6 := ProbeConfigIPFamilies(&pb.ProbeConfig{})
	if !v4 || !v6 {
		t.Fatal("empty ip_families means both")
	}
}

func TestLoadProbePathsOmitsUnsampledICMPAndDisabledMTR(t *testing.T) {
	db := probeTestDB(t)
	now := time.Unix(1_700_000_200, 0)
	collector := model.Collector{
		CollectorUUID: "probe-omit", Name: "HK", Kind: model.CollectorKindProbe,
		TokenHash: bytes.Repeat([]byte{9}, 32), RegistrationToken: "token-omit",
		EnableICMP: true, EnableTCP: true, EnableMTR: true,
	}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "probe-omit", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorRuntime{CollectorUUID: "probe-omit", Status: "online", LastSeen: now.Add(-10 * time.Second).UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	tcpOnly := probeServer(41, "tcp-only", "secret-41")
	tcpOnly.ProbeTarget = "1.1.1.1"
	tcpOnly.ProbeEnableICMP = model.BoolPtr(false)
	mtrOff := probeServer(42, "mtr-off", "secret-42")
	mtrOff.ProbeTarget = "1.0.0.1"
	mtrOff.ProbeEnableMTR = model.BoolPtr(false)
	for _, server := range []model.Server{tcpOnly, mtrOff} {
		if err := db.Create(&server).Error; err != nil {
			t.Fatal(err)
		}
	}
	tcpJSON, _ := json.Marshal([]ProbeTCPView{{Port: 443, OK: true, RttMs: 12}})
	if err := db.Create(&model.ProbeLatest{
		CollectorUUID: "probe-omit", ServerID: 41, Reachable: true, DisplayRttMs: 12, SampledAt: now.UnixNano(), TCPJSON: tcpJSON,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeLatest{
		CollectorUUID: "probe-omit", ServerID: 42, Reachable: true, DisplayRttMs: 8, SampledAt: now.UnixNano(), ICMPOk: true, ICMPRttMs: 8, ICMPSent: 5, HasTrace: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	hopsJSON, _ := json.Marshal([]netprobe.Hop{{TTL: 1, Address: "10.0.0.1", Loss: 1, Sent: 3}})
	if err := db.Create(&model.ProbeTrace{CollectorUUID: "probe-omit", ServerID: 42, SampledAt: now.UnixNano(), HopsJSON: hopsJSON}).Error; err != nil {
		t.Fatal(err)
	}
	paths, err := loadProbePaths(db, ProbePathFilter{CollectorID: "probe-omit"}, now)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uint64]ProbePath{}
	for _, path := range paths {
		byID[path.ServerID] = path
	}
	if got := byID[41]; !got.Reachable || got.HasICMP || got.ICMPSent != 0 || len(got.TCP) != 1 {
		t.Fatalf("tcp-only should omit icmp: %+v", got)
	}
	if got := byID[42]; got.HasTrace || got.MTR.HopCount != 0 || !got.HasICMP {
		t.Fatalf("mtr off should drop stale trace: %+v", got)
	}
}

func TestLoadProbePathsPrefersTCPMTRWhenICMPLossFull(t *testing.T) {
	db := probeTestDB(t)
	now := time.Unix(1_700_000_210, 0)
	collector := model.Collector{
		CollectorUUID: "probe-tcp-mtr", Name: "LAX", Kind: model.CollectorKindProbe,
		TokenHash: bytes.Repeat([]byte{10}, 32), RegistrationToken: "token-tcp-mtr",
		EnableICMP: true, EnableTCP: true, EnableMTR: true,
	}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "probe-tcp-mtr", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorRuntime{CollectorUUID: "probe-tcp-mtr", Status: "online", LastSeen: now.Add(-10 * time.Second).UnixNano()}).Error; err != nil {
		t.Fatal(err)
	}
	host := probeServer(51, "blocked-icmp", "secret-51")
	host.ProbeTarget = "9.9.9.9"
	if err := db.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProbeLatest{
		CollectorUUID: "probe-tcp-mtr", ServerID: 51, Reachable: true, SampledAt: now.UnixNano(), HasTrace: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	icmpHops, _ := json.Marshal([]netprobe.Hop{{TTL: 1, Address: "10.0.0.1", Loss: 1, Sent: 3}})
	tcpHops, _ := json.Marshal([]netprobe.Hop{{TTL: 1, Address: "10.0.0.1", Loss: 0, Avg: 4 * time.Millisecond, Sent: 3}, {TTL: 8, Address: "9.9.9.9", Loss: 0, Avg: 40 * time.Millisecond, Sent: 3}})
	if err := db.Create(&model.ProbeTrace{
		CollectorUUID: "probe-tcp-mtr", ServerID: 51, SampledAt: now.UnixNano(), Destination: "9.9.9.9", HopsJSON: icmpHops,
		TCPSampledAt: now.UnixNano(), TCPDestination: "9.9.9.9", TCPHopsJSON: tcpHops, TCPPort: 443,
	}).Error; err != nil {
		t.Fatal(err)
	}
	paths, err := loadProbePaths(db, ProbePathFilter{CollectorID: "probe-tcp-mtr", ServerID: 51}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0].MTR.Protocol != "tcp" || paths[0].MTR.Port != 443 || paths[0].MTR.HopCount != 2 || paths[0].MTR.Loss != 0 {
		t.Fatalf("should prefer tcp mtr: %+v", paths[0].MTR)
	}
	trace, err := GetProbeTrace(db, "probe-tcp-mtr", 51)
	if err != nil || trace == nil || trace.Protocol != "tcp" || trace.TCP == nil || trace.ICMP == nil || len(trace.TCP.Hops) != 2 {
		t.Fatalf("trace both legs: %+v %v", trace, err)
	}
	if !trace.TCP.Hops[0].Private {
		t.Fatal("private hop should be marked")
	}
}

func TestIngestTCPTraceDoesNotWipeICMPHops(t *testing.T) {
	db := probeTestDB(t)
	collector := model.Collector{CollectorUUID: "probe-keep", Name: "SG", Kind: model.CollectorKindProbe, TokenHash: bytes.Repeat([]byte{11}, 32), RegistrationToken: "token-keep"}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := IngestProbeSamples(db, &collector, &pb.ProbeSampleBatch{Samples: []*pb.ProbeSample{{
		ServerId: 61, SampledAtUnixNano: now.UnixNano(),
		Mtr: &pb.ProbeMTRTrace{SampledAtUnixNano: now.UnixNano(), Destination: "1.1.1.1", Protocol: "icmp", Hops: []*pb.ProbeMTRHop{{Ttl: 1, Address: "10.0.0.1", Sent: 3}}},
	}}}, now); err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Minute)
	if err := IngestProbeSamples(db, &collector, &pb.ProbeSampleBatch{Samples: []*pb.ProbeSample{{
		ServerId: 61, SampledAtUnixNano: later.UnixNano(),
		MtrTcp: &pb.ProbeMTRTrace{SampledAtUnixNano: later.UnixNano(), Destination: "1.1.1.1", Protocol: "tcp", Port: 443, Hops: []*pb.ProbeMTRHop{{Ttl: 1, Address: "1.1.1.1", Sent: 3}}},
	}}}, later); err != nil {
		t.Fatal(err)
	}
	var row model.ProbeTrace
	if err := db.First(&row, "collector_uuid = ? AND server_id = ?", "probe-keep", 61).Error; err != nil {
		t.Fatal(err)
	}
	var icmpHops, tcpHops []netprobe.Hop
	_ = json.Unmarshal(row.HopsJSON, &icmpHops)
	_ = json.Unmarshal(row.TCPHopsJSON, &tcpHops)
	if len(icmpHops) != 1 || icmpHops[0].Address != "10.0.0.1" || len(tcpHops) != 1 || row.TCPPort != 443 {
		t.Fatalf("icmp hops should remain: %+v tcp=%+v port=%d", icmpHops, tcpHops, row.TCPPort)
	}
}
