package netprobe

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	ping "github.com/prometheus-community/pro-bing"
)

type ICMPResult struct {
	OK         bool
	RTT        time.Duration
	Loss       float64
	Sent       int
	Received   int
	Error      string
	ResolvedIP string
}

type TCPResult struct {
	Port  uint
	OK    bool
	RTT   time.Duration
	Error string
}

type Hop struct {
	TTL     uint
	Address string
	Loss    float64
	Avg     time.Duration
	Sent    int
}

type TraceResult struct {
	Destination string
	Hops        []Hop
}

func ICMP(ctx context.Context, host string, count int, timeout time.Duration) ICMPResult {
	return ICMPOn(ctx, host, "", count, timeout)
}

func ICMPOn(ctx context.Context, host, network string, count int, timeout time.Duration) ICMPResult {
	if count <= 0 {
		count = 5
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	result := ICMPResult{}
	if strings.TrimSpace(host) == "" {
		result.Error = "empty host"
		return result
	}
	pinger, err := ping.NewPinger(host)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if network != "" {
		pinger.SetNetwork(network)
	}
	pinger.SetPrivileged(true)
	pinger.Count = count
	pinger.Timeout = timeout * time.Duration(count)
	pinger.Interval = 200 * time.Millisecond
	done := make(chan struct{})
	go func() {
		defer close(done)
		err = pinger.Run()
	}()
	select {
	case <-ctx.Done():
		pinger.Stop()
		<-done
		result.Error = ctx.Err().Error()
		return result
	case <-done:
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	stats := pinger.Statistics()
	if stats == nil {
		result.Error = "no icmp statistics"
		return result
	}
	result.Sent = stats.PacketsSent
	result.Received = stats.PacketsRecv
	result.Loss = stats.PacketLoss
	result.RTT = stats.AvgRtt
	result.OK = stats.PacketsRecv > 0
	if pinger.IPAddr() != nil {
		result.ResolvedIP = pinger.IPAddr().String()
	}
	if !result.OK && result.Error == "" {
		result.Error = "icmp timeout"
	}
	return result
}

func TCP(ctx context.Context, host string, port uint, timeout time.Duration) TCPResult {
	return TCPOn(ctx, host, "tcp", port, timeout)
}

func TCPOn(ctx context.Context, host, network string, port uint, timeout time.Duration) TCPResult {
	result := TCPResult{Port: port}
	if strings.TrimSpace(host) == "" || port == 0 || port > 65535 {
		result.Error = "invalid tcp target"
		return result
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if network == "" {
		network = "tcp"
	}
	dialer := net.Dialer{Timeout: timeout}
	started := time.Now()
	conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(host, strconv.FormatUint(uint64(port), 10)))
	result.RTT = time.Since(started)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	_ = conn.Close()
	result.OK = true
	return result
}

func MTR(ctx context.Context, host string, maxTTL, probesPerHop int, timeout time.Duration) TraceResult {
	return MTROn(ctx, host, "", maxTTL, probesPerHop, timeout)
}

func MTROn(ctx context.Context, host, family string, maxTTL, probesPerHop int, timeout time.Duration) TraceResult {
	if maxTTL <= 0 {
		maxTTL = 30
	}
	if probesPerHop <= 0 {
		probesPerHop = 10
	}
	if timeout <= 0 {
		timeout = time.Second
	}
	trace := TraceResult{Destination: host}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return trace
	}
	dest := pickIP(ips, family)
	if dest == nil {
		return trace
	}
	trace.Destination = dest.String()
	network := "ip4:icmp"
	if dest.To4() == nil {
		network = "ip6:ipv6-icmp"
	}
	for ttl := 1; ttl <= maxTTL; ttl++ {
		if ctx.Err() != nil {
			break
		}
		hop := probeHop(ctx, network, dest, ttl, probesPerHop, timeout)
		trace.Hops = append(trace.Hops, hop)
		if hop.Address != "" && hop.Address == dest.String() {
			break
		}
	}
	return trace
}

func probeHop(ctx context.Context, network string, dest net.IP, ttl, probes int, timeout time.Duration) Hop {
	hop := Hop{TTL: uint(ttl), Sent: probes}
	var sum time.Duration
	received := 0
	addrCounts := map[string]int{}
	for i := 0; i < probes; i++ {
		if ctx.Err() != nil {
			break
		}
		addr, rtt, ok := sendTTLProbe(ctx, network, dest, ttl, timeout)
		if !ok {
			continue
		}
		received++
		sum += rtt
		if addr != "" {
			addrCounts[addr]++
		}
	}
	if hop.Sent > 0 {
		hop.Loss = float64(hop.Sent-received) / float64(hop.Sent)
	}
	if received > 0 {
		hop.Avg = sum / time.Duration(received)
	}
	best, bestN := "", 0
	for addr, n := range addrCounts {
		if n > bestN {
			best, bestN = addr, n
		}
	}
	hop.Address = best
	return hop
}

func sendTTLProbe(ctx context.Context, network string, dest net.IP, ttl int, timeout time.Duration) (string, time.Duration, bool) {
	dialNetwork := "ip4:icmp"
	listenNetwork := "ip4:icmp"
	if dest.To4() == nil {
		dialNetwork = "ip6:ipv6-icmp"
		listenNetwork = "ip6:ipv6-icmp"
	}
	_ = network
	conn, err := net.ListenPacket(listenNetwork, "")
	if err != nil {
		return "", 0, false
	}
	defer conn.Close()
	if err := setTTL(conn, dest.To4() == nil, ttl); err != nil {
		return "", 0, false
	}
	payload := icmpEcho(dest.To4() == nil)
	started := time.Now()
	if _, err := conn.WriteTo(payload, &net.IPAddr{IP: dest}); err != nil {
		return "", 0, false
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 512)
	n, addr, err := conn.ReadFrom(buf)
	if err != nil || n == 0 {
		return "", 0, false
	}
	_ = dialNetwork
	_ = ctx
	ip := ""
	if ipAddr, ok := addr.(*net.IPAddr); ok && ipAddr.IP != nil {
		ip = ipAddr.IP.String()
	} else {
		ip = addr.String()
	}
	return ip, time.Since(started), true
}

func icmpEcho(v6 bool) []byte {
	// ICMP Echo Request: type(8)/code(0)/checksum/id/seq + payload
	buf := make([]byte, 16)
	if v6 {
		buf[0] = 128
	} else {
		buf[0] = 8
	}
	buf[4], buf[5] = 0x12, 0x34
	buf[6], buf[7] = 0x00, 0x01
	copy(buf[8:], []byte("santaizi-mtr"))
	sum := checksum(buf)
	buf[2] = byte(sum >> 8)
	buf[3] = byte(sum)
	return buf
}

func checksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	return ^uint16(sum)
}

type Destination struct {
	Host    string
	ICMPNet string
	TCPNet  string
}

func Destinations(ipv4, ipv6, hostname string, enable4, enable6 bool) []Destination {
	if !enable4 && !enable6 {
		enable4, enable6 = true, true
	}
	var dests []Destination
	if enable4 {
		host := strings.TrimSpace(ipv4)
		if host == "" {
			host = strings.TrimSpace(hostname)
		}
		if host != "" {
			dests = append(dests, Destination{Host: host, ICMPNet: "ip4", TCPNet: "tcp4"})
		}
	}
	if enable6 {
		host := strings.TrimSpace(ipv6)
		if host == "" {
			host = strings.TrimSpace(hostname)
		}
		if host != "" {
			dests = append(dests, Destination{Host: host, ICMPNet: "ip6", TCPNet: "tcp6"})
		}
	}
	return dests
}

func MergeICMP(results []ICMPResult) ICMPResult {
	var best ICMPResult
	for i, item := range results {
		if i == 0 {
			best = item
			continue
		}
		if item.OK && (!best.OK || (item.RTT > 0 && (best.RTT == 0 || item.RTT < best.RTT))) {
			best = item
		}
	}
	return best
}

func MergeTCPByPort(results []TCPResult) []TCPResult {
	order := make([]uint, 0, len(results))
	byPort := map[uint]TCPResult{}
	for _, item := range results {
		prev, ok := byPort[item.Port]
		if !ok {
			order = append(order, item.Port)
			byPort[item.Port] = item
			continue
		}
		if item.OK && (!prev.OK || (item.RTT > 0 && (prev.RTT == 0 || item.RTT < prev.RTT))) {
			byPort[item.Port] = item
		}
	}
	out := make([]TCPResult, 0, len(order))
	for _, port := range order {
		out = append(out, byPort[port])
	}
	return out
}

func pickIP(ips []net.IPAddr, family string) net.IP {
	for _, item := range ips {
		if item.IP == nil {
			continue
		}
		is4 := item.IP.To4() != nil
		switch family {
		case "ip4":
			if is4 {
				return item.IP
			}
		case "ip6":
			if !is4 {
				return item.IP
			}
		default:
			return item.IP
		}
	}
	return nil
}

func FormatHost(ipv4, ipv6, hostname string) string {
	if hostname != "" {
		return hostname
	}
	if ipv4 != "" {
		return ipv4
	}
	return ipv6
}

func ParsePorts(raw string) []uint {
	seen := map[uint]struct{}{}
	var ports []uint
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := strconv.ParseUint(part, 10, 16)
		if err != nil || value == 0 {
			continue
		}
		port := uint(value)
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	return ports
}

func DisplayError(icmp ICMPResult, tcp []TCPResult) string {
	if icmp.OK {
		return ""
	}
	for _, item := range tcp {
		if item.OK {
			return ""
		}
	}
	if icmp.Error != "" {
		return icmp.Error
	}
	for _, item := range tcp {
		if item.Error != "" {
			return fmt.Sprintf("tcp:%d %s", item.Port, item.Error)
		}
	}
	return "unreachable"
}
