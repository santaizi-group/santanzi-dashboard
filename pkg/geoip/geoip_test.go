package geoip

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestQueryIPFromBundlePrefersIPv4(t *testing.T) {
	got := queryIPFromBundle("8.8.8.8/2001:4860:4860::8888")
	if got == nil || got.To4() == nil || got.String() != "8.8.8.8" {
		t.Fatalf("got %v", got)
	}
	only6 := queryIPFromBundle("2001:4860:4860::8888")
	if only6 == nil || only6.To4() != nil {
		t.Fatalf("expected IPv6, got %v", only6)
	}
	if queryIPFromBundle("") != nil || queryIPFromBundle("not-an-ip") != nil {
		t.Fatal("invalid bundle should miss")
	}
}

func TestLookupNilIP(t *testing.T) {
	if _, err := Lookup(nil, &IPInfo{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestLookupCodeFromAddrOnStub(t *testing.T) {
	if net.ParseIP("8.8.8.8") == nil {
		t.Fatal("sanity")
	}
	if db == nil {
		if got := LookupCodeFromAddr("8.8.8.8"); got != "" {
			t.Fatalf("stub db should miss, got %q", got)
		}
		return
	}
	if got := LookupCodeFromAddr("8.8.8.8"); got == "" {
		t.Fatal("embedded GeoIP db should resolve 8.8.8.8")
	}
	if dual := LookupCodeFromAddr("8.8.8.8/2001:4860:4860::8888"); dual != LookupCodeFromAddr("8.8.8.8") {
		t.Fatalf("dual-stack should prefer IPv4, got %q", dual)
	}
}

func TestExternalGeoIPPathDoesNotPanic(t *testing.T) {
	t.Cleanup(loadDatabase)
	t.Setenv(envGeoIPDB, filepath.Join(t.TempDir(), "missing.mmdb"))
	loadDatabase()
	if LookupCodeFromAddr("8.8.8.8") != "" && db == nil {
		t.Fatal("unavailable db should return empty")
	}
	if _, err := Lookup(net.ParseIP("8.8.8.8"), &IPInfo{}); db == nil && err == nil {
		t.Fatal("unavailable db should error")
	}
}

func TestBrokenExternalGeoIPFallsBack(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.mmdb")
	if err := os.WriteFile(bad, []byte("not-a-maxmind-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(loadDatabase)
	t.Setenv(envGeoIPDB, bad)
	loadDatabase()
	if got := LookupCodeFromAddr("203.0.113.10"); got != "" && db == nil {
		t.Fatalf("broken external db should not panic, got %q", got)
	}
}

func TestLookupHopPrivateAndFormat(t *testing.T) {
	private := LookupHop("10.0.0.1")
	if !private.Private {
		t.Fatalf("%+v", private)
	}
	if FormatHopGeo(private) != "" {
		t.Fatal("private geo is rendered by i18n")
	}
	if LookupHop("not-an-ip").CountryCode != "" || LookupHop("").Private {
		t.Fatal("invalid address should miss")
	}
	if got := FormatHopGeo(HopInfo{CountryName: "United States", ASName: "Google LLC"}); got != "United States · Google LLC" {
		t.Fatal(got)
	}
	if got := FormatHopGeo(HopInfo{CountryCode: "us"}); got != "US" {
		t.Fatal(got)
	}
}
