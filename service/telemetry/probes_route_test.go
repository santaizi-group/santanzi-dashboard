package telemetry

import (
	"bytes"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/geoip"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
)

func TestNormalizeRouteIntervalAndKeep(t *testing.T) {
	if model.NormalizeRouteIntervalSec(30) != model.DefaultRouteIntervalSec {
		t.Fatal("illegal interval should fall back to 1 day")
	}
	if model.NormalizeRouteIntervalSec(3600) != 3600 || model.NormalizeRouteIntervalSec(604800) != 604800 {
		t.Fatal("hour/week should stay")
	}
	if model.NormalizeRouteKeep(0) != 10 || model.NormalizeRouteKeep(51) != 50 || model.NormalizeRouteKeep(3) != 3 {
		t.Fatal("keep clamp")
	}
	if model.NormalizeMTRProbes(0) != 10 || model.NormalizeMTRProbes(11) != 10 || model.NormalizeMTRProbes(7) != 7 {
		t.Fatal("mtr probes clamp")
	}
}

func TestIngestProbeRoutePrunesToNAndANDGate(t *testing.T) {
	db := probeTestDB(t)
	collector := model.Collector{
		CollectorUUID: "probe-route", Name: "HK", Kind: model.CollectorKindProbe,
		TokenHash: bytes.Repeat([]byte{21}, 32), RegistrationToken: "tok",
		EnableICMP: true, EnableTCP: true, EnableMTR: true, RouteKeep: 3, RouteIntervalSec: 86400,
	}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	server := probeServer(71, "edge", "secret-71")
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	for i := 0; i < 5; i++ {
		sampled := now.Add(time.Duration(i) * time.Minute)
		if err := IngestProbeSamples(db, &collector, &pb.ProbeSampleBatch{Samples: []*pb.ProbeSample{{
			ServerId: 71, SampledAtUnixNano: sampled.UnixNano(),
			RouteIcmp: &pb.ProbeRouteTrace{
				SampledAtUnixNano: sampled.UnixNano(), Protocol: "icmp", Destination: "1.1.1.1",
				Hops: []*pb.ProbeRouteHop{{Ttl: 1, Address: "10.0.0.1", Country: "中国"}},
			},
		}}}, sampled); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&model.ProbeRoute{}).Where("collector_uuid = ? AND protocol = ?", "probe-route", "icmp").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("keep 3, got %d", count)
	}
	icmpOff := server
	icmpOff.ProbeEnableICMP = model.BoolPtr(false)
	if err := db.Save(&icmpOff).Error; err != nil {
		t.Fatal(err)
	}
	_, err := EnqueueProbeRouteJob(db, "probe-route", 71, "icmp", now)
	if err != ErrRouteProtocolDisabled {
		t.Fatalf("want disabled, got %v", err)
	}
}

func TestEnqueueProbeRouteJobDedup(t *testing.T) {
	db := probeTestDB(t)
	collector := model.Collector{
		CollectorUUID: "probe-job", Name: "HK", Kind: model.CollectorKindProbe,
		TokenHash: bytes.Repeat([]byte{22}, 32), RegistrationToken: "tok",
		EnableICMP: true, EnableTCP: true, RouteKeep: 10,
	}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	server := probeServer(81, "edge", "secret-81")
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_100, 0)
	first, err := EnqueueProbeRouteJob(db, "probe-job", 81, "icmp", now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnqueueProbeRouteJob(db, "probe-job", 81, "icmp", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("pending should reuse job %d vs %d", first.ID, second.ID)
	}
}

func TestIngestRouteOnlyDoesNotClobberLatest(t *testing.T) {
	db := probeTestDB(t)
	collector := model.Collector{
		CollectorUUID: "probe-latest", Name: "HK", Kind: model.CollectorKindProbe,
		TokenHash: bytes.Repeat([]byte{23}, 32), RegistrationToken: "tok", EnableICMP: true, EnableTCP: true, RouteKeep: 10,
	}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_200, 0)
	if err := IngestProbeSamples(db, &collector, &pb.ProbeSampleBatch{Samples: []*pb.ProbeSample{{
		ServerId: 91, SampledAtUnixNano: now.UnixNano(),
		Icmp: &pb.ProbeICMPSample{Ok: true, RttMs: 12, PacketsSent: 5, PacketsReceived: 5},
	}}}, now); err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Minute)
	if err := IngestProbeSamples(db, &collector, &pb.ProbeSampleBatch{Samples: []*pb.ProbeSample{{
		ServerId: 91, SampledAtUnixNano: later.UnixNano(),
		RouteIcmp: &pb.ProbeRouteTrace{SampledAtUnixNano: later.UnixNano(), Protocol: "icmp", Error: "未找到 nexttrace"},
	}}}, later); err != nil {
		t.Fatal(err)
	}
	var latest model.ProbeLatest
	if err := db.First(&latest, "collector_uuid = ? AND server_id = ?", "probe-latest", 91).Error; err != nil {
		t.Fatal(err)
	}
	if !latest.ICMPOk || latest.ICMPRttMs != 12 {
		t.Fatalf("route-only should not clobber ping latest: %+v", latest)
	}
	var routes int64
	if err := db.Model(&model.ProbeRoute{}).Count(&routes).Error; err != nil {
		t.Fatal(err)
	}
	if routes != 1 {
		t.Fatalf("failed route should still insert, got %d", routes)
	}
}

func TestFormatRouteHopGeoFillsMissingASN(t *testing.T) {
	got := formatRouteHopGeo(
		ProbeRouteHopView{Country: "中国", Province: "上海", Owner: "电信"},
		geoip.HopInfo{ASN: "4809", ASName: "CHINANET-BACKBONE"},
	)
	if got != "中国 · 上海 · 电信 · AS4809" {
		t.Fatal(got)
	}
	kept := formatRouteHopGeo(
		ProbeRouteHopView{Country: "中国", ASN: "4134"},
		geoip.HopInfo{ASN: "4809"},
	)
	if kept != "中国 · AS4134" {
		t.Fatal(kept)
	}
}
