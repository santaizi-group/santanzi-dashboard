package controller

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func installAgentRSScript(t *testing.T) string {
	t.Helper()
	script := filepath.Join("..", "..", "..", "script", "install_agent_rs.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("install script: %v", err)
	}
	return script
}

func runInstallAgentRSParse(t *testing.T, dir string, args ...string) (yamlText, cacheText string, err error) {
	t.Helper()
	// Windows 的 sh（Git Bash）会按 POSIX 规则重读 CreateProcess 命令行。
	// 密钥里的单引号会把后续参数粘进同一个参数。参数写进脚本文件后由 sh 按脚本解析。
	var script strings.Builder
	script.WriteString("#!/bin/sh\nexec")
	script.WriteString(" " + shellSingleQuote(filepath.ToSlash(installAgentRSScript(t))))
	for _, arg := range args {
		script.WriteString(" " + shellSingleQuote(arg))
	}
	script.WriteString("\n")
	wrapper := filepath.Join(dir, "invoke.sh")
	if err := os.WriteFile(wrapper, []byte(script.String()), 0o700); err != nil {
		t.Fatalf("write invoke script: %v", err)
	}
	cmd := exec.Command("sh", filepath.ToSlash(wrapper))
	cmd.Env = append(os.Environ(),
		"SANTAIZI_AGENT_RS_PARSE_ONLY=1",
		"SANTAIZI_AGENT_YAML="+filepath.Join(dir, "agent.yaml"),
		"SANTAIZI_AGENT_DATA="+filepath.Join(dir, "data"),
	)
	out, err := cmd.CombinedOutput()
	yamlRaw, _ := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	cacheRaw, _ := os.ReadFile(filepath.Join(dir, "data", "endpoint-cache.json"))
	if err != nil {
		return string(yamlRaw), string(cacheRaw), &parseError{err: err, output: string(out)}
	}
	return string(yamlRaw), string(cacheRaw), nil
}

type parseError struct {
	err    error
	output string
}

func (e *parseError) Error() string {
	return e.err.Error() + ": " + e.output
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func TestInstallAgentRSParseOnlyCloudPhysicalAndDialCache(t *testing.T) {
	dir := t.TempDir()
	yamlText, cacheText, err := runInstallAgentRSParse(t, dir,
		"install_agent", "grpc.example.invalid", "5555", "sec'ret",
		"--clean-install", "--confirm-clean-install", "--tls",
		"--server-ip", "192.0.2.10", "--server-ip", "2001:DB8::10", "--server-ip", "192.0.2.10",
		"--disable-connections", "--disable-processes", "--disable-nat",
		"--ip-report-interface", "eth0", "--country-code", "CN", "--use-ipv6-countrycode",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"server: 'grpc.example.invalid:5555'",
		"client_secret: 'sec''ret'",
		"tls: true",
		"ip_report_interface: 'eth0'",
		"country_code: 'CN'",
		"use_ipv6_countrycode: true",
		"connections: false",
		"processes: false",
		"temperature: false",
		"gpu: false",
		"nat: false",
		"cpu: true",
		`data_dir: '` + filepath.Join(dir, "data") + `'`,
	} {
		if !strings.Contains(yamlText, want) {
			t.Fatalf("yaml missing %q\n%s", want, yamlText)
		}
	}
	if strings.Contains(yamlText, "192.0.2.10") || strings.Contains(yamlText, "server-ip") || strings.Contains(yamlText, "2001:db8::10") {
		t.Fatalf("server IP must stay out of yaml:\n%s", yamlText)
	}
	if !strings.Contains(cacheText, `"host":"grpc.example.invalid"`) || !strings.Contains(cacheText, `"port":"5555"`) || !strings.Contains(cacheText, `"192.0.2.10"`) || !strings.Contains(cacheText, `"2001:db8::10"`) {
		t.Fatalf("cache=%s", cacheText)
	}
	if strings.Count(cacheText, "192.0.2.10") != 1 {
		t.Fatalf("duplicate server ip was not collapsed: %s", cacheText)
	}
	info, err := os.Stat(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("yaml mode=%o", info.Mode().Perm())
	}

	kept := cacheText
	yamlText, cacheText, err = runInstallAgentRSParse(t, dir,
		"grpc.example.INVALID", "5555", "secret",
		"--server-ip", "198.51.100.4",
		"--temperature", "--gpu",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(yamlText, "temperature: true") || !strings.Contains(yamlText, "gpu: true") || !strings.Contains(yamlText, "nat: true") {
		t.Fatalf("physical yaml=\n%s", yamlText)
	}
	if cacheText != kept {
		t.Fatalf("same host+port should keep dial cache\nkept=%s\nnow=%s", kept, cacheText)
	}

	other := t.TempDir()
	_, cacheText, err = runInstallAgentRSParse(t, other,
		"other.example.invalid", "5555", "secret",
		"--server-ip", "198.51.100.4", "--server-ip", "0.0.0.0", "--server-ip", "224.0.0.1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cacheText, `"198.51.100.4"`) || strings.Contains(cacheText, "0.0.0.0") || strings.Contains(cacheText, "224.0.0.1") {
		t.Fatalf("unusable IPs leaked into cache: %s", cacheText)
	}

	literal := t.TempDir()
	_, cacheText, err = runInstallAgentRSParse(t, literal, "192.0.2.10", "5555", "secret", "--server-ip", "198.51.100.4")
	if err != nil {
		t.Fatal(err)
	}
	if cacheText != "" {
		t.Fatalf("literal server should skip dial cache: %s", cacheText)
	}

	invalid := t.TempDir()
	if _, _, err = runInstallAgentRSParse(t, invalid, "grpc.example.invalid", "5555", "secret", "--server-ip", "not-an-ip"); err == nil {
		t.Fatal("expected invalid server ip to fail")
	}
	if _, _, err = runInstallAgentRSParse(t, invalid, "grpc.example.invalid", "5555", "secret", "--nope"); err == nil {
		t.Fatal("expected unknown flag to fail")
	}
	if _, _, err = runInstallAgentRSParse(t, invalid, "grpc.example.invalid", "5555", "secret", "--clean-install"); err == nil {
		t.Fatal("expected unconfirmed clean install to fail")
	}
}
