//go:build windows

package netprobe

func setSocketTTL(fd uintptr, v6 bool, ttl int) error {
	_ = fd
	_ = v6
	_ = ttl
	return nil
}
