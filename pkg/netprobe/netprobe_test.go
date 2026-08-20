package netprobe

import (
	"context"
	"testing"
	"time"
)

func TestParsePorts(t *testing.T) {
	ports := ParsePorts("22, 443,22,0,abc")
	if len(ports) != 2 || ports[0] != 22 || ports[1] != 443 {
		t.Fatalf("%v", ports)
	}
}

func TestFormatHostPrefersOverrideThenIPv4(t *testing.T) {
	if got := FormatHost("1.1.1.1", "::1", "origin.example"); got != "origin.example" {
		t.Fatal(got)
	}
	if got := FormatHost("1.1.1.1", "::1", ""); got != "1.1.1.1" {
		t.Fatal(got)
	}
}

func TestDestinationsAndMerge(t *testing.T) {
	dests := Destinations("1.1.1.1", "2001:db8::1", "", true, true)
	if len(dests) != 2 || dests[0].TCPNet != "tcp4" || dests[1].TCPNet != "tcp6" {
		t.Fatalf("%+v", dests)
	}
	v6 := Destinations("1.1.1.1", "2001:db8::1", "", false, true)
	if len(v6) != 1 || v6[0].Host != "2001:db8::1" {
		t.Fatalf("%+v", v6)
	}
	hostOnly := Destinations("", "", "origin.example", true, false)
	if len(hostOnly) != 1 || hostOnly[0].Host != "origin.example" || hostOnly[0].ICMPNet != "ip4" {
		t.Fatalf("%+v", hostOnly)
	}
	merged := MergeICMP([]ICMPResult{{OK: false, Error: "timeout"}, {OK: true, RTT: 2}})
	if !merged.OK {
		t.Fatalf("%+v", merged)
	}
	tcp := MergeTCPByPort([]TCPResult{{Port: 22, OK: false}, {Port: 22, OK: true, RTT: 3}, {Port: 443, OK: true}})
	if len(tcp) != 2 || !tcp[0].OK || tcp[0].Port != 22 || tcp[1].Port != 443 {
		t.Fatalf("%+v", tcp)
	}
}

func TestDisplayErrorWhenAnyTCPSucceeds(t *testing.T) {
	if err := DisplayError(ICMPResult{OK: false, Error: "timeout"}, []TCPResult{{Port: 443, OK: true}}); err != "" {
		t.Fatal(err)
	}
	if err := DisplayError(ICMPResult{OK: false, Error: "timeout"}, []TCPResult{{Port: 22, Error: "refused"}}); err != "timeout" {
		t.Fatal(err)
	}
}

func TestTCPMTRPortPrefersSuccessfulThenConfigured(t *testing.T) {
	if got := TCPMTRPort([]TCPResult{{Port: 22, OK: false}, {Port: 443, OK: true}}, []uint{80}); got != 443 {
		t.Fatalf("ok port: %d", got)
	}
	if got := TCPMTRPort([]TCPResult{{Port: 22, OK: false}}, []uint{80}); got != 22 {
		t.Fatalf("failed port still reusable: %d", got)
	}
	if got := TCPMTRPort(nil, []uint{58880, 443}); got != 58880 {
		t.Fatalf("configured: %d", got)
	}
	if got := TCPMTRPort(nil, nil); got != 0 {
		t.Fatal("empty should miss")
	}
}

func TestMTRTCPOnRejectsInvalidTarget(t *testing.T) {
	ctx := context.Background()
	if hops := MTRTCPOn(ctx, "", "", 443, 3, 1, time.Millisecond).Hops; len(hops) != 0 {
		t.Fatalf("%+v", hops)
	}
	if hops := MTRTCPOn(ctx, "example.invalid", "", 0, 3, 1, time.Millisecond).Hops; len(hops) != 0 {
		t.Fatalf("%+v", hops)
	}
}
