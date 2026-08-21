package nexttrace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	pb "github.com/hi2shark/santaizi-dashboard/proto"
)

const (
	ErrNotFound = "未找到 nexttrace"
	execTimeout = 45 * time.Second
)

// Runner is overridable in tests.
var Runner = runCommand

type Hop struct {
	TTL      uint    `json:"ttl"`
	Address  string  `json:"address"`
	Hostname string  `json:"hostname,omitempty"`
	RttMs    float64 `json:"rtt_ms"`
	Loss     float64 `json:"loss"`
	Sent     uint    `json:"sent"`
	ASN      string  `json:"asn,omitempty"`
	Country  string  `json:"country,omitempty"`
	Province string  `json:"province,omitempty"`
	City     string  `json:"city,omitempty"`
	Owner    string  `json:"owner,omitempty"`
}

type Result struct {
	Destination string
	Hops        []Hop
}

func ResolveBinary(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if !filepath.IsAbs(configured) {
			return "", fmt.Errorf("%s: 必须是绝对路径", ErrNotFound)
		}
		if !isExecutable(configured) {
			return "", fmt.Errorf("%s", ErrNotFound)
		}
		return configured, nil
	}
	for _, name := range []string{"nexttrace", "nexttrace-tiny"} {
		path, err := exec.LookPath(name)
		if err == nil && isExecutable(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s", ErrNotFound)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

type TraceOpts struct {
	Target   string
	TCP      bool
	Port     uint
	IPv6     bool
	IPv4     bool
	Language string
}

func Args(opts TraceOpts) []string {
	args := []string{"--json", "-M", "--queries", "3", "--max-hops", "30"}
	lang := strings.TrimSpace(opts.Language)
	if lang == "" {
		lang = "cn"
	}
	args = append(args, "--language", lang)
	if opts.TCP {
		args = append(args, "--tcp")
		if opts.Port > 0 {
			args = append(args, "--port", strconv.FormatUint(uint64(opts.Port), 10))
		}
	}
	if opts.IPv6 && !opts.IPv4 {
		args = append(args, "--ipv6")
	} else if opts.IPv4 && !opts.IPv6 {
		args = append(args, "--ipv4")
	}
	return append(args, opts.Target)
}

func Run(ctx context.Context, bin string, opts TraceOpts) (Result, error) {
	if strings.TrimSpace(opts.Target) == "" {
		return Result{}, errors.New("empty target")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, execTimeout)
		defer cancel()
	}
	out, err := Runner(ctx, bin, Args(opts))
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		if len(out) > 0 {
			msg = strings.TrimSpace(string(out)) + " " + msg
		}
		return Result{}, errors.New(compactError(msg))
	}
	parsed, err := ParseJSON(out)
	if err != nil {
		return Result{}, err
	}
	if parsed.Destination == "" {
		parsed.Destination = opts.Target
	}
	return parsed, nil
}

func compactError(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 480 {
		return msg[:480]
	}
	if msg == "" {
		return "nexttrace failed"
	}
	return msg
}

func runCommand(ctx context.Context, bin string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return stdout.Bytes(), fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}

func ParseJSON(raw []byte) (Result, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return Result{}, errors.New("empty nexttrace json")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Result{}, fmt.Errorf("nexttrace json: %w", err)
	}
	if hopsRaw, ok := envelope["Hops"]; ok {
		return parseCLIHops(hopsRaw, envelope)
	}
	if hopsRaw, ok := envelope["hops"]; ok {
		if result, err := parseAttemptHops(hopsRaw); err == nil && len(result.Hops) > 0 {
			if dest, ok := envelope["resolved_ip"]; ok {
				result.Destination = jsonString(dest)
			}
			if result.Destination == "" {
				result.Destination = jsonString(envelope["target"])
			}
			return result, nil
		}
		return parseCLIHops(hopsRaw, envelope)
	}
	return Result{}, errors.New("nexttrace json missing hops")
}

func parseCLIHops(hopsRaw json.RawMessage, envelope map[string]json.RawMessage) (Result, error) {
	var groups [][]cliHop
	if err := json.Unmarshal(hopsRaw, &groups); err != nil {
		return Result{}, fmt.Errorf("nexttrace hops: %w", err)
	}
	result := Result{Destination: jsonString(envelope["TraceMapUrl"])}
	_ = result.Destination
	result.Destination = ""
	for index, group := range groups {
		hop := mergeCLIGroup(index+1, group)
		if hop.TTL == 0 {
			continue
		}
		result.Hops = append(result.Hops, hop)
	}
	return result, nil
}

type cliHop struct {
	Success  bool            `json:"Success"`
	Address  json.RawMessage `json:"Address"`
	Hostname string          `json:"Hostname"`
	TTL      int             `json:"TTL"`
	RTT      json.RawMessage `json:"RTT"`
	Geo      *cliGeo         `json:"Geo"`
	IP       string          `json:"ip"`
}

type cliGeo struct {
	Asnumber string  `json:"asnumber"`
	Country  string  `json:"country"`
	Prov     string  `json:"prov"`
	City     string  `json:"city"`
	Owner    string  `json:"owner"`
	Isp      string  `json:"isp"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

func mergeCLIGroup(ttl int, group []cliHop) Hop {
	hop := Hop{TTL: uint(ttl)}
	var rtts []float64
	sent := 0
	ok := 0
	for _, item := range group {
		sent++
		addr := hopAddress(item)
		if item.Success || addr != "" {
			ok++
			if hop.Address == "" {
				hop.Address = addr
			}
			if hop.Hostname == "" {
				hop.Hostname = item.Hostname
			}
			applyGeo(&hop, item.Geo)
			if ms := rttMilliseconds(item.RTT); ms > 0 {
				rtts = append(rtts, ms)
			}
		}
		if item.TTL > 0 {
			hop.TTL = uint(item.TTL)
		}
	}
	hop.Sent = uint(sent)
	if sent > 0 {
		hop.Loss = float64(sent-ok) / float64(sent)
	}
	if len(rtts) > 0 {
		sum := 0.0
		for _, ms := range rtts {
			sum += ms
		}
		hop.RttMs = sum / float64(len(rtts))
	}
	return hop
}

type attemptHop struct {
	TTL      int             `json:"ttl"`
	Attempts []attemptSample `json:"attempts"`
	Address  string          `json:"address"`
	Hostname string          `json:"hostname"`
	RttMs    float64         `json:"rtt_ms"`
	Geo      *cliGeo         `json:"geo"`
	Success  bool            `json:"success"`
	IP       string          `json:"ip"`
}

type attemptSample struct {
	Success  bool    `json:"success"`
	IP       string  `json:"ip"`
	Hostname string  `json:"hostname"`
	RttMs    float64 `json:"rtt_ms"`
	Geo      *cliGeo `json:"geo"`
}

func parseAttemptHops(hopsRaw json.RawMessage) (Result, error) {
	var hops []attemptHop
	if err := json.Unmarshal(hopsRaw, &hops); err != nil {
		return Result{}, err
	}
	var result Result
	for index, item := range hops {
		ttl := item.TTL
		if ttl == 0 {
			ttl = index + 1
		}
		hop := Hop{TTL: uint(ttl), Address: firstNonEmpty(item.Address, item.IP), Hostname: item.Hostname, RttMs: item.RttMs}
		applyGeo(&hop, item.Geo)
		sent := len(item.Attempts)
		okCount := 0
		var rtts []float64
		for _, sample := range item.Attempts {
			sentOK := sample.Success || sample.IP != ""
			if sentOK {
				okCount++
			}
			if hop.Address == "" {
				hop.Address = sample.IP
			}
			if hop.Hostname == "" {
				hop.Hostname = sample.Hostname
			}
			applyGeo(&hop, sample.Geo)
			if sample.RttMs > 0 {
				rtts = append(rtts, sample.RttMs)
			}
		}
		if sent == 0 {
			sent = 1
			if hop.Address != "" || item.Success {
				okCount = 1
			}
		}
		hop.Sent = uint(sent)
		if sent > 0 {
			hop.Loss = float64(sent-okCount) / float64(sent)
		}
		if hop.RttMs == 0 && len(rtts) > 0 {
			sum := 0.0
			for _, ms := range rtts {
				sum += ms
			}
			hop.RttMs = sum / float64(len(rtts))
		}
		result.Hops = append(result.Hops, hop)
	}
	return result, nil
}

func applyGeo(hop *Hop, geo *cliGeo) {
	if hop == nil || geo == nil {
		return
	}
	if hop.ASN == "" {
		hop.ASN = strings.TrimSpace(geo.Asnumber)
	}
	if hop.Country == "" {
		hop.Country = strings.TrimSpace(geo.Country)
	}
	if hop.Province == "" {
		hop.Province = strings.TrimSpace(geo.Prov)
	}
	if hop.City == "" {
		hop.City = strings.TrimSpace(geo.City)
	}
	if hop.Owner == "" {
		hop.Owner = strings.TrimSpace(firstNonEmpty(geo.Owner, geo.Isp))
	}
}

func hopAddress(item cliHop) string {
	if ip := strings.TrimSpace(item.IP); ip != "" {
		return stripPort(ip)
	}
	if len(item.Address) == 0 || string(item.Address) == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(item.Address, &asString); err == nil {
		return stripPort(asString)
	}
	var obj map[string]any
	if err := json.Unmarshal(item.Address, &obj); err == nil {
		for _, key := range []string{"IP", "ip", "IPAddress"} {
			if value, ok := obj[key]; ok {
				if text, ok := value.(string); ok {
					return stripPort(text)
				}
			}
		}
	}
	return stripPort(strings.Trim(string(item.Address), `"`))
}

func stripPort(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return value
}

func rttMilliseconds(raw json.RawMessage) float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		if asFloat > 1e6 {
			return time.Duration(asFloat).Seconds() * 1000
		}
		return asFloat
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if ms, err := strconv.ParseFloat(strings.TrimSuffix(asString, "ms"), 64); err == nil {
			return ms
		}
	}
	return 0
}

func jsonString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func HopsToProto(hops []Hop) []*pb.ProbeRouteHop {
	out := make([]*pb.ProbeRouteHop, 0, len(hops))
	for _, hop := range hops {
		out = append(out, &pb.ProbeRouteHop{
			Ttl: uint32(hop.TTL), Address: hop.Address, Hostname: hop.Hostname,
			RttMs: hop.RttMs, Loss: hop.Loss, Sent: uint32(hop.Sent),
			Asn: hop.ASN, Country: hop.Country, Province: hop.Province, City: hop.City, Owner: hop.Owner,
		})
	}
	return out
}

func ProtoToHops(hops []*pb.ProbeRouteHop) []Hop {
	out := make([]Hop, 0, len(hops))
	for _, hop := range hops {
		if hop == nil {
			continue
		}
		out = append(out, Hop{
			TTL: uint(hop.GetTtl()), Address: hop.GetAddress(), Hostname: hop.GetHostname(),
			RttMs: hop.GetRttMs(), Loss: hop.GetLoss(), Sent: uint(hop.GetSent()),
			ASN: hop.GetAsn(), Country: hop.GetCountry(), Province: hop.GetProvince(), City: hop.GetCity(), Owner: hop.GetOwner(),
		})
	}
	return out
}
