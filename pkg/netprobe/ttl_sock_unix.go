//go:build unix

package netprobe

import "golang.org/x/sys/unix"

func setSocketTTL(fd uintptr, v6 bool, ttl int) error {
	if v6 {
		return unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, ttl)
	}
	return unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TTL, ttl)
}
