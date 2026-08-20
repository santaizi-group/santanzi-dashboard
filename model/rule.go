package model

import (
	"slices"
	"time"

	"gorm.io/gorm"
)

const (
	RuleCoverAll = iota
	RuleCoverIgnoreAll
)

type Rule struct {
	Type     string          `json:"type,omitempty"`
	Min      float64         `json:"min,omitempty"`
	Max      float64         `json:"max,omitempty"`
	Duration uint64          `json:"duration,omitempty"`
	Cover    uint64          `json:"cover,omitempty"`
	Ignore   map[uint64]bool `json:"ignore,omitempty"`
}

func percentage(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) * 100 / float64(total)
}

func (u *Rule) Snapshot(server *Server, db *gorm.DB) interface{} {
	if server == nil || server.State == nil || server.Host == nil {
		return nil
	}
	if u.Cover == RuleCoverAll && u.Ignore[server.ID] {
		return nil
	}
	if u.Cover == RuleCoverIgnoreAll && !u.Ignore[server.ID] {
		return nil
	}
	var value float64
	switch u.Type {
	case "cpu":
		value = server.State.CPU
	case "gpu":
		value = server.State.GPU
	case "memory":
		value = percentage(server.State.MemUsed, server.Host.MemTotal)
	case "swap":
		value = percentage(server.State.SwapUsed, server.Host.SwapTotal)
	case "disk":
		value = percentage(server.State.DiskUsed, server.Host.DiskTotal)
	case "net_in_speed":
		value = float64(server.State.NetInSpeed)
	case "net_out_speed":
		value = float64(server.State.NetOutSpeed)
	case "net_all_speed":
		value = float64(server.State.NetInSpeed + server.State.NetOutSpeed)
	case "offline":
		if offline, ok, err := ServerConsensusOffline(db, server.ID); err == nil && ok {
			if offline {
				return struct{}{}
			}
			return nil
		}
		if server.LastActive.IsZero() {
			value = 0
		} else {
			value = float64(server.LastActive.Unix())
		}
	case "load1":
		value = server.State.Load1
	case "load5":
		value = server.State.Load5
	case "load15":
		value = server.State.Load15
	case "tcp_conn_count":
		value = float64(server.State.TcpConnCount)
	case "udp_conn_count":
		value = float64(server.State.UdpConnCount)
	case "process_count":
		value = float64(server.State.ProcessCount)
	case "temperature_max":
		temperatures := make([]float64, 0, len(server.State.Temperatures))
		for _, sensor := range server.State.Temperatures {
			if sensor.Temperature != 0 {
				temperatures = append(temperatures, sensor.Temperature)
			}
		}
		if len(temperatures) > 0 {
			value = slices.Max(temperatures)
		}
	default:
		return nil
	}
	if u.Type == "offline" && float64(time.Now().Unix())-value > 6 {
		return struct{}{}
	}
	if (u.Max > 0 && value > u.Max) || (u.Min > 0 && value < u.Min) {
		return struct{}{}
	}
	return nil
}
