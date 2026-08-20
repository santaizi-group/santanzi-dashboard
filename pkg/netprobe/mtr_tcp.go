package netprobe

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func MTRTCP(ctx context.Context, host string, port uint, maxTTL, probesPerHop int, timeout time.Duration) TraceResult {
	return MTRTCPOn(ctx, host, "", port, maxTTL, probesPerHop, timeout)
}

func MTRTCPOn(ctx context.Context, host, family string, port uint, maxTTL, probesPerHop int, timeout time.Duration) TraceResult {
	trace := TraceResult{Destination: host}
	if strings.TrimSpace(host) == "" || port == 0 || port > 65535 {
		return trace
	}
	if maxTTL <= 0 {
		maxTTL = 30
	}
	if probesPerHop <= 0 {
		probesPerHop = 3
	}
	if timeout <= 0 {
		timeout = time.Second
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return trace
	}
	dest := pickIP(ips, family)
	if dest == nil {
		return trace
	}
	trace.Destination = dest.String()
	for ttl := 1; ttl <= maxTTL; ttl++ {
		if ctx.Err() != nil {
			break
		}
		hop := probeTCPHop(ctx, dest, port, ttl, probesPerHop, timeout)
		trace.Hops = append(trace.Hops, hop)
		if hop.Address != "" && hop.Address == dest.String() {
			break
		}
	}
	return trace
}

func TCPMTRPort(results []TCPResult, ports []uint) uint {
	for _, item := range results {
		if item.OK && item.Port > 0 {
			return item.Port
		}
	}
	for _, item := range results {
		if item.Port > 0 {
			return item.Port
		}
	}
	if len(ports) > 0 && ports[0] > 0 {
		return ports[0]
	}
	return 0
}

func probeTCPHop(ctx context.Context, dest net.IP, port uint, ttl, probes int, timeout time.Duration) Hop {
	hop := Hop{TTL: uint(ttl), Sent: probes}
	var sum time.Duration
	received := 0
	addrCounts := map[string]int{}
	for i := 0; i < probes; i++ {
		if ctx.Err() != nil {
			break
		}
		addr, rtt, ok := sendTCPTTLProbe(ctx, dest, port, ttl, timeout)
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

func sendTCPTTLProbe(ctx context.Context, dest net.IP, port uint, ttl int, timeout time.Duration) (string, time.Duration, bool) {
	v6 := dest.To4() == nil
	listenNetwork := "ip4:icmp"
	tcpNetwork := "tcp4"
	if v6 {
		listenNetwork = "ip6:ipv6-icmp"
		tcpNetwork = "tcp6"
	}
	icmpConn, err := net.ListenPacket(listenNetwork, "")
	if err == nil {
		defer icmpConn.Close()
		_ = icmpConn.SetReadDeadline(time.Now().Add(timeout))
	}
	hopCh := make(chan string, 1)
	if icmpConn != nil {
		go func() {
			buf := make([]byte, 512)
			n, addr, err := icmpConn.ReadFrom(buf)
			if err != nil || n == 0 || addr == nil {
				return
			}
			ip := packetIP(addr)
			if ip == "" {
				return
			}
			select {
			case hopCh <- ip:
			default:
			}
		}()
	}
	dialer := net.Dialer{
		Timeout: timeout,
		Control: func(_, _ string, c syscall.RawConn) error {
			var sockErr error
			if ctrlErr := c.Control(func(fd uintptr) {
				sockErr = setSocketTTL(fd, v6, ttl)
			}); ctrlErr != nil {
				return ctrlErr
			}
			return sockErr
		},
	}
	started := time.Now()
	conn, err := dialer.DialContext(ctx, tcpNetwork, net.JoinHostPort(dest.String(), strconv.FormatUint(uint64(port), 10)))
	rtt := time.Since(started)
	if conn != nil {
		_ = conn.Close()
	}
	if tcpReached(err) {
		return dest.String(), rtt, true
	}
	remain := timeout - rtt
	if remain < 0 {
		remain = 0
	}
	timer := time.NewTimer(remain)
	defer timer.Stop()
	select {
	case ip := <-hopCh:
		return ip, time.Since(started), true
	case <-ctx.Done():
		return "", 0, false
	case <-timer.C:
		return "", 0, false
	}
}

func packetIP(addr net.Addr) string {
	if ipAddr, ok := addr.(*net.IPAddr); ok && ipAddr.IP != nil {
		return ipAddr.IP.String()
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		return host
	}
	return addr.String()
}

func tcpReached(err error) bool {
	if err == nil {
		return true
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		err = op.Err
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "actively refused")
}
