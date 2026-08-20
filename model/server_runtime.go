package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

const (
	ServerRuntimeStatusUnknown    = "unknown"
	ServerRuntimeStatusRecovering = "recovering"
	ServerRuntimeStatusOnline     = "online"
	ServerRuntimeStatusOffline    = "offline"
)

// ServerRuntime 用于持久化保存每台服务器的运行时状态，
// 使 Dashboard 重启后仍能恢复对在线/离线状态的判断。
type ServerRuntime struct {
	ServerID uint64 `gorm:"primaryKey"`

	Status string `gorm:"index"`

	FirstSeenAt   *time.Time
	LastSeenAt    *time.Time
	LastOnlineAt  *time.Time
	LastOfflineAt *time.Time

	LastBootTime     uint64
	LastUptime       uint64
	LastIP           string
	LastAgentVersion string

	CurrentOfflineID uint64 `gorm:"index"`

	CurrentNodeUUID   []byte `gorm:"type:BLOB;size:16;index"`
	CurrentSessionID  []byte `gorm:"type:BLOB;size:16"`
	CurrentSequence   uint64
	Protocol          string `gorm:"index"`
	HostState         string `gorm:"index"`
	ConnectivityState string `gorm:"index"`
	LastCollectedAt   int64  `gorm:"index"`
	LastReceivedAt    int64  `gorm:"index"`
	StatePayload      []byte `gorm:"type:BLOB"`
	HostPayload       []byte `gorm:"type:BLOB"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

const v2RuntimeProtocol = "v2"

// ServerConsensusOffline 按 V2 可用性共识判断服务器是否离线。
// 无 V2 绑定或查询失败时 ok=false，调用方应退回自己的旧逻辑（如 V1 的 LastActive）。
// 已绑定但还没有可用性证据时 ok=true 且 offline=false。
func ServerConsensusOffline(db *gorm.DB, serverID uint64) (offline bool, ok bool, err error) {
	if db == nil {
		return false, false, nil
	}
	var rt ServerRuntime
	if err := db.Select("protocol", "current_node_uuid").Where("server_id = ?", serverID).First(&rt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, false, nil
		}
		return false, false, err
	}
	if rt.Protocol != v2RuntimeProtocol || len(rt.CurrentNodeUUID) != 16 {
		return false, false, nil
	}
	var bucket AvailabilityBucket
	err = db.Select("host_state").Where("node_uuid = ?", rt.CurrentNodeUUID).
		Order("bucket_start DESC").Limit(1).First(&bucket).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, true, nil
		}
		return false, false, err
	}
	return bucket.HostState == HostStateOffline, true, nil
}
