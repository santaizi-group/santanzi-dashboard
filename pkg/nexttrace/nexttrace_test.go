package nexttrace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseJSONCLIHops(t *testing.T) {
	raw := []byte(`{
	  "Hops": [
	    [
	      {"Success": true, "Address": "10.0.0.1:0", "Hostname": "gw", "TTL": 1, "RTT": 1500000, "Geo": {"country": "中国", "prov": "广东", "city": "深圳", "asnumber": "4134", "owner": "电信"}},
	      {"Success": true, "Address": "10.0.0.1:0", "TTL": 1, "RTT": 2500000, "Geo": {"country": "中国", "prov": "广东"}}
	    ],
	    [
	      {"Success": false, "TTL": 2},
	      {"Success": true, "Address": "1.1.1.1", "TTL": 2, "RTT": 12000000, "Geo": {"country": "Australia", "asnumber": "13335", "owner": "Cloudflare"}}
	    ]
	  ]
	}`)
	result, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hops) != 2 {
		t.Fatalf("hops=%d", len(result.Hops))
	}
	if result.Hops[0].Address != "10.0.0.1" || result.Hops[0].Province != "广东" || result.Hops[0].City != "深圳" || result.Hops[0].ASN != "4134" {
		t.Fatalf("%+v", result.Hops[0])
	}
	if result.Hops[0].Sent != 2 || result.Hops[0].Loss != 0 || result.Hops[0].RttMs < 1.9 || result.Hops[0].RttMs > 2.1 {
		t.Fatalf("rtt/loss %+v", result.Hops[0])
	}
	if result.Hops[1].Address != "1.1.1.1" || result.Hops[1].Loss != 0.5 {
		t.Fatalf("%+v", result.Hops[1])
	}
}

func TestParseJSONAttempts(t *testing.T) {
	raw := []byte(`{
	  "target": "example.com",
	  "resolved_ip": "93.184.216.34",
	  "hops": [
	    {"ttl": 1, "attempts": [
	      {"success": true, "ip": "192.168.1.1", "hostname": "gateway.local", "rtt_ms": 1.2, "geo": {"country": "Private", "asnumber": "65535"}}
	    ]}
	  ]
	}`)
	result, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Destination != "93.184.216.34" || len(result.Hops) != 1 || result.Hops[0].Address != "192.168.1.1" {
		t.Fatalf("%+v", result)
	}
}

func TestResolveBinaryExplicitNoFallback(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-nexttrace")
	if _, err := ResolveBinary(missing); err == nil {
		t.Fatal("expected error for missing explicit path")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := ResolveBinary(""); err == nil {
		t.Fatal("expected missing PATH lookup")
	}
}

func TestResolveBinaryExplicitOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nexttrace")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveBinary(path)
	if err != nil || got != path {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestArgsTCP(t *testing.T) {
	args := Args(TraceOpts{Target: "1.1.1.1", TCP: true, Port: 443, IPv4: true})
	joined := stringsJoin(args)
	if !containsAll(args, "--json", "-M", "--tcp", "--port", "443", "--ipv4", "1.1.1.1") {
		t.Fatalf("%s", joined)
	}
}

func TestRunUsesRunner(t *testing.T) {
	orig := Runner
	t.Cleanup(func() { Runner = orig })
	Runner = func(ctx context.Context, bin string, args []string) ([]byte, error) {
		if bin != "/opt/nexttrace/nexttrace" {
			t.Fatalf("bin %s", bin)
		}
		return []byte(`{"Hops":[[{"Success":true,"Address":"9.9.9.9","TTL":1,"RTT":1000000}]]}`), nil
	}
	result, err := Run(context.Background(), "/opt/nexttrace/nexttrace", TraceOpts{Target: "9.9.9.9"})
	if err != nil || len(result.Hops) != 1 || result.Hops[0].Address != "9.9.9.9" {
		t.Fatalf("%+v %v", result, err)
	}
}

func stringsJoin(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += " "
		}
		out += value
	}
	return out
}

func containsAll(args []string, want ...string) bool {
	set := map[string]bool{}
	for _, arg := range args {
		set[arg] = true
	}
	for _, item := range want {
		if !set[item] {
			return false
		}
	}
	return true
}
