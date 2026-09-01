package controller

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

func TestProbeCapabilityPresetsCloudPhysical(t *testing.T) {
	cloud := monitoringOptionsDTO{CPU: true, Memory: true, Disk: true, Network: true, Connections: true, Processes: true, HostInfo: true, IPReport: true, HTTPProbe: true, ICMPProbe: true, TCPProbe: true, NAT: false}
	physical := cloud
	physical.Temperature = true
	physical.GPU = true
	if cloud.Temperature || cloud.GPU || cloud.NAT {
		t.Fatalf("cloud should disable temperature/gpu/nat: %#v", cloud)
	}
	if !physical.Temperature || !physical.GPU || physical.NAT {
		t.Fatalf("physical should enable temperature/gpu and disable nat: %#v", physical)
	}
	posix, err := buildInstallCommand("linux", "https://example.invalid/install.sh", "h", 5555, "s", false, false, physical, ipReportConfigDTO{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(posix, "--temperature") || !strings.Contains(posix, "--gpu") || !strings.Contains(posix, "--disable-nat") {
		t.Fatalf("physical flags=%s", posix)
	}
}

func TestBuildInstallCommandMatchesInstallerArguments(t *testing.T) {
	options := monitoringOptionsDTO{CPU: true, Memory: true, Disk: true, Network: true, HostInfo: true, IPReport: true, HTTPProbe: true, ICMPProbe: true, TCPProbe: true}
	ipCfg := ipReportConfigDTO{Interface: "eth0", CountryCode: "CN", PreferIPv6: true}
	posix, err := buildInstallCommand("linux", "https://example.invalid/install.sh", "grpc.example.invalid", 5555, "secret", true, false, options, ipCfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(posix, "'grpc.example.invalid' 5555 'secret' --clean-install --confirm-clean-install") || strings.Contains(posix, "--server") || strings.Contains(posix, "--secret") {
		t.Fatalf("posix=%s", posix)
	}
	if !strings.Contains(posix, "--disable-nat") || !strings.Contains(posix, "--ip-report-interface 'eth0'") || !strings.Contains(posix, "--country-code 'CN'") || !strings.Contains(posix, "--use-ipv6-countrycode") {
		t.Fatalf("posix flags=%s", posix)
	}
	windows, err := buildInstallCommand("windows", "https://example.invalid/install.ps1", "grpc.example.invalid", 5555, "secret", true, false, options, ipCfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(windows, "-Server 'grpc.example.invalid' -Port 5555 -Key 'secret' -CleanInstall -ConfirmCleanInstall") || strings.Contains(windows, "-Secret") {
		t.Fatalf("windows=%s", windows)
	}
	if !strings.Contains(windows, "-DisableNAT") || !strings.Contains(windows, "-IpReportInterface 'eth0'") || !strings.Contains(windows, "-CountryCode 'CN'") || !strings.Contains(windows, "-UseIPv6CountryCode") {
		t.Fatalf("windows flags=%s", windows)
	}
}

func TestBuildUpgradeCommandOmitsSecret(t *testing.T) {
	posix, err := buildUpgradeCommand("linux", "https://example.invalid/upgrade_agent.sh")
	if err != nil {
		t.Fatal(err)
	}
	if posix != "curl -fsSL 'https://example.invalid/upgrade_agent.sh' | bash" {
		t.Fatalf("linux=%s", posix)
	}
	if strings.Contains(posix, "secret") || strings.Contains(posix, "-p") {
		t.Fatalf("upgrade command must not include secrets: %s", posix)
	}
	macos, err := buildUpgradeCommand("macos", "https://example.invalid/upgrade.command")
	if err != nil {
		t.Fatal(err)
	}
	if macos != "curl -fsSL 'https://example.invalid/upgrade.command' | sudo bash" {
		t.Fatalf("macos=%s", macos)
	}
	windows, err := buildUpgradeCommand("windows", "https://example.invalid/upgrade.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if windows != "& ([scriptblock]::Create((irm 'https://example.invalid/upgrade.ps1' )))" {
		t.Fatalf("windows=%s", windows)
	}
}

func TestBuildInstallCommandUsesTLSAndPublicPort(t *testing.T) {
	options := monitoringOptionsDTO{CPU: true, Memory: true, Disk: true, Network: true, HostInfo: true, IPReport: true, HTTPProbe: true, ICMPProbe: true, TCPProbe: true}
	posix, err := buildInstallCommand("linux", "https://example.invalid/install.sh", "main.example.invalid", 443, "secret", false, true, options, ipReportConfigDTO{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(posix, "'main.example.invalid' 443 'secret' --tls") {
		t.Fatalf("posix=%s", posix)
	}
	windows, err := buildInstallCommand("windows", "https://example.invalid/install.ps1", "main.example.invalid", 443, "secret", false, true, options, ipReportConfigDTO{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(windows, "-Server 'main.example.invalid' -Port 443 -Key 'secret' -Tls") {
		t.Fatalf("windows=%s", windows)
	}
}

func TestBuildInstallCommandAppendsServerIPHints(t *testing.T) {
	options := monitoringOptionsDTO{CPU: true, Memory: true, Disk: true, Network: true, HostInfo: true, IPReport: true, HTTPProbe: true, ICMPProbe: true, TCPProbe: true}
	posix, err := buildInstallCommand("linux", "https://example.invalid/install.sh", "grpc.example.invalid", 5555, "secret", false, true, options, ipReportConfigDTO{}, []string{"192.0.2.10", "2001:db8::10"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(posix, "'grpc.example.invalid' 5555 'secret' --tls --server-ip '192.0.2.10' --server-ip '2001:db8::10'") {
		t.Fatalf("posix=%s", posix)
	}
	windows, err := buildInstallCommand("windows", "https://example.invalid/install.ps1", "grpc.example.invalid", 5555, "secret", false, true, options, ipReportConfigDTO{}, []string{"192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(windows, "-Server 'grpc.example.invalid' -Port 5555 -Key 'secret' -Tls -ServerIP '192.0.2.10'") {
		t.Fatalf("windows=%s", windows)
	}
}

func TestBuildInstallCommandRustOmitsGoFlags(t *testing.T) {
	options := monitoringOptionsDTO{CPU: true, Memory: true, Disk: true, Network: true, HostInfo: true, IPReport: true, HTTPProbe: true, ICMPProbe: true, TCPProbe: true, NAT: false}
	ipCfg := ipReportConfigDTO{Interface: "eth0", CountryCode: "CN", PreferIPv6: true}
	posix, err := buildInstallCommandWithImpl("linux", "https://example.invalid/install_agent_rs.sh", "grpc.example.invalid", 5555, "secret", true, true, options, ipCfg, []string{"192.0.2.10"}, "rust")
	if err != nil {
		t.Fatal(err)
	}
	want := "curl -fsSL 'https://example.invalid/install_agent_rs.sh' | bash -s -- 'grpc.example.invalid' 5555 'secret' --clean-install --confirm-clean-install --tls"
	if posix != want {
		t.Fatalf("posix=%s", posix)
	}
	if strings.Contains(posix, "--disable-") || strings.Contains(posix, "--server-ip") || strings.Contains(posix, "--temperature") {
		t.Fatalf("rust command must not include go agent flags: %s", posix)
	}
	if _, err := buildInstallCommandWithImpl("macos", "https://example.invalid/install_agent_rs.sh", "h", 5555, "s", false, false, options, ipReportConfigDTO{}, nil, "rust"); err == nil {
		t.Fatal("expected rust macos to fail")
	}
}

func TestResolveGRPCHintIPsSkipsLiteralAndLookupFailure(t *testing.T) {
	if got := resolveGRPCHintIPs("192.0.2.10"); len(got) != 0 {
		t.Fatalf("literal v4 = %v", got)
	}
	if got := resolveGRPCHintIPs("2001:db8::10"); len(got) != 0 {
		t.Fatalf("literal v6 = %v", got)
	}
	original := lookupGRPCHost
	t.Cleanup(func() { lookupGRPCHost = original })
	lookupGRPCHost = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("192.0.2.10")}, {IP: net.ParseIP("192.0.2.10")}, {IP: net.ParseIP("2001:db8::10")}}, nil
	}
	got := resolveGRPCHintIPs("grpc.example.invalid")
	if len(got) != 2 || got[0] != "192.0.2.10" || got[1] != "2001:db8::10" {
		t.Fatalf("resolved = %v", got)
	}
	lookupGRPCHost = func(context.Context, string) ([]net.IPAddr, error) {
		return nil, context.DeadlineExceeded
	}
	if got := resolveGRPCHintIPs("grpc.example.invalid"); len(got) != 0 {
		t.Fatalf("lookup failure = %v", got)
	}
}

func TestPublicGRPCPortUsesProxyWhenSet(t *testing.T) {
	original := singleton.Conf
	t.Cleanup(func() { singleton.Conf = original })
	singleton.Conf = &model.Config{GRPCPort: 5555, ProxyGRPCPort: 443}
	if got := publicGRPCPort(); got != 443 {
		t.Fatalf("proxy port = %d, want 443", got)
	}
	singleton.Conf.ProxyGRPCPort = 0
	if got := publicGRPCPort(); got != 5555 {
		t.Fatalf("listen port = %d, want 5555", got)
	}
}

func TestBuildCollectorInstallCommand(t *testing.T) {
	command := buildCollectorInstallCommand(
		"https://example.invalid/install_collector.sh",
		"primary.example.invalid:5555",
		"tok'en",
		5556,
		true,
		true,
	)
	if !strings.Contains(command, "curl -fsSL 'https://example.invalid/install_collector.sh' | bash -s --") {
		t.Fatalf("command=%s", command)
	}
	if !strings.Contains(command, "--primary-endpoint 'primary.example.invalid:5555'") {
		t.Fatalf("missing endpoint: %s", command)
	}
	if !strings.Contains(command, "--token 'tok'\\''en'") {
		t.Fatalf("token quoting: %s", command)
	}
	if !strings.Contains(command, "--grpc-port 5556") || !strings.Contains(command, "--primary-tls true") || !strings.Contains(command, "--primary-insecure-tls true") {
		t.Fatalf("flags: %s", command)
	}
	off := buildCollectorInstallCommand("https://example.invalid/install_collector.sh", "primary.example.invalid:5555", "token", 5556, true, false)
	if !strings.Contains(off, "--primary-tls true") || !strings.Contains(off, "--primary-insecure-tls false") {
		t.Fatalf("default tls flags: %s", off)
	}
}

func TestBuildScriptCommands(t *testing.T) {
	conf := &model.Config{
		InstallScript: model.InstallScriptConfig{
			Dashboard:        "https://example.invalid/install_dashboard.sh",
			UpgradeCollector: "https://example.invalid/upgrade_collector.sh",
			UpgradeLinux:     "https://example.invalid/upgrade_agent.sh",
			UpgradeMacOS:     "https://example.invalid/upgrade.command",
			UpgradeWindows:   "https://example.invalid/upgrade.ps1",
		},
	}
	commands := buildScriptCommands(conf)
	if len(commands) != 9 {
		t.Fatalf("len=%d", len(commands))
	}
	byID := map[string]scriptCommandDTO{}
	for _, cmd := range commands {
		if _, exists := byID[cmd.ID]; exists {
			t.Fatalf("duplicate id %s", cmd.ID)
		}
		byID[cmd.ID] = cmd
		lower := strings.ToLower(cmd.Command)
		if strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "-p ") {
			t.Fatalf("command must not include secrets: %s=%s", cmd.ID, cmd.Command)
		}
	}
	wantIDs := []string{
		"dashboard_install", "dashboard_upgrade",
		"collector_upgrade", "collector_remove",
		"agent_upgrade_linux", "agent_upgrade_macos", "agent_upgrade_windows",
		"agent_uninstall_posix", "agent_uninstall_windows",
	}
	for _, id := range wantIDs {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing id %s", id)
		}
	}
	if got := byID["dashboard_install"].Command; got != `sh -c "$(curl -fsSL 'https://example.invalid/install_dashboard.sh')"` {
		t.Fatalf("dashboard_install=%s", got)
	}
	if got := byID["collector_upgrade"].Command; got != "curl -fsSL 'https://example.invalid/upgrade_collector.sh' | bash" {
		t.Fatalf("collector_upgrade=%s", got)
	}
	if got := byID["agent_upgrade_linux"].Command; got != "curl -fsSL 'https://example.invalid/upgrade_agent.sh' | bash" {
		t.Fatalf("agent_upgrade_linux=%s", got)
	}
	if !byID["collector_remove"].Destructive || !byID["agent_uninstall_posix"].Destructive || !byID["agent_uninstall_windows"].Destructive {
		t.Fatal("destructive flags missing")
	}
	if byID["dashboard_upgrade"].Destructive || byID["collector_upgrade"].Destructive {
		t.Fatal("upgrade commands must not be destructive")
	}

	skipped := buildScriptCommands(&model.Config{})
	if len(skipped) != 4 {
		t.Fatalf("empty urls should skip install/upgrade scripts, got %d", len(skipped))
	}
	for _, cmd := range skipped {
		switch cmd.ID {
		case "dashboard_upgrade", "collector_remove", "agent_uninstall_posix", "agent_uninstall_windows":
		default:
			t.Fatalf("unexpected remaining command %s", cmd.ID)
		}
	}
}

func TestParseCollectorListenPort(t *testing.T) {
	port, err := parseCollectorListenPort("hk.example.com:6666")
	if err != nil || port != 6666 {
		t.Fatalf("got %d %v", port, err)
	}
	port, err = parseCollectorListenPort("hk.example.com")
	if err != nil || port != 5556 {
		t.Fatalf("default got %d %v", port, err)
	}
	port, err = parseCollectorListenPort("")
	if err != nil || port != 5556 {
		t.Fatalf("empty got %d %v", port, err)
	}
}

func TestNormalizeCollectorListenPort(t *testing.T) {
	port, err := normalizeCollectorListenPort(5556, "edge.example.com:443")
	if err != nil || port != 5556 {
		t.Fatalf("explicit got %d %v", port, err)
	}
	port, err = normalizeCollectorListenPort(0, "edge.example.com:443")
	if err != nil || port != 443 {
		t.Fatalf("from address got %d %v", port, err)
	}
}

func TestResolveCollectorInstallPort(t *testing.T) {
	port, err := resolveCollectorInstallPort(5556, "edge.example.com:443", 0)
	if err != nil || port != 5556 {
		t.Fatalf("listen_port got %d %v", port, err)
	}
	port, err = resolveCollectorInstallPort(0, "edge.example.com:443", 0)
	if err != nil || port != 443 {
		t.Fatalf("address fallback got %d %v", port, err)
	}
	port, err = resolveCollectorInstallPort(5556, "edge.example.com:443", 8443)
	if err != nil || port != 8443 {
		t.Fatalf("requested override got %d %v", port, err)
	}
}

func TestApplyAlertRuleWriteAllowsOfflineWithoutThreshold(t *testing.T) {
	request := alertRuleWriteDTO{
		Name:            "Host offline",
		Enabled:         true,
		TriggerMode:     "always",
		NotificationTag: "ops",
		Conditions: []alertConditionDTO{{
			Type:            "offline",
			DurationSeconds: 30,
			Scope:           monitorScopeDTO{Mode: "all", ServerIDs: []uint64{}},
		}},
	}
	var row model.AlertRule
	if err := applyAlertRuleWrite(&row, request); err != nil {
		t.Fatal(err)
	}
	if len(row.Rules) != 1 || row.Rules[0].Type != "offline" || row.Rules[0].Min != 0 || row.Rules[0].Max != 0 {
		t.Fatalf("unexpected rules: %#v", row.Rules)
	}
}

func TestNormalizeCollectorScopesRejectsIncompleteOrAmbiguousScopes(t *testing.T) {
	valid, err := normalizeCollectorScopes([]collectorScopeRequest{{Type: " SERVER ", Value: " 7 "}, {Type: "tag", Value: " edge "}})
	if err != nil {
		t.Fatal(err)
	}
	if valid[0].Type != "server" || valid[0].Value != "7" || valid[1].Value != "edge" {
		t.Fatalf("unexpected normalized scopes: %#v", valid)
	}
	for _, scopes := range [][]collectorScopeRequest{
		nil,
		{{Type: "all"}, {Type: "server", Value: "7"}},
		{{Type: "server", Value: ""}},
		{{Type: "tag", Value: ""}},
		{{Type: "group", Value: "edge"}, {Type: "group", Value: "edge"}},
	} {
		if _, err := normalizeCollectorScopes(scopes); err == nil {
			t.Fatalf("expected rejection for %#v", scopes)
		}
	}
}
