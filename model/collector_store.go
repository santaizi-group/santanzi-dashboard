package model

import "time"

type CollectorSchemaMigration struct {
	Version   uint64 `gorm:"primaryKey"`
	AppliedAt time.Time
}

type CollectorMeta struct {
	Key       string `gorm:"primaryKey;size:64"`
	Value     []byte `gorm:"type:BLOB;not null"`
	UpdatedAt time.Time
}

type CollectorStoredEvent struct {
	EventID     []byte `gorm:"type:BLOB;size:16;primaryKey"`
	NodeUUID    []byte `gorm:"type:BLOB;size:16;not null;uniqueIndex:idx_collector_node_session_sequence;index:idx_collector_event_collected,priority:1"`
	SessionID   []byte `gorm:"type:BLOB;size:16;not null;uniqueIndex:idx_collector_node_session_sequence"`
	Sequence    uint64 `gorm:"not null;uniqueIndex:idx_collector_node_session_sequence"`
	EventType   int32  `gorm:"not null;index"`
	Priority    int32  `gorm:"not null"`
	CollectedAt int64  `gorm:"not null;index:idx_collector_event_collected,priority:2"`
	Payload     []byte `gorm:"type:BLOB;not null"`
	CreatedAt   time.Time
}

type CollectorStoredObservation struct {
	EventID      []byte `gorm:"type:BLOB;size:16;primaryKey"`
	ObserverID   string `gorm:"size:64;primaryKey;index:idx_collector_observation_received,priority:1"`
	NodeUUID     []byte `gorm:"type:BLOB;size:16;not null;index"`
	ReceivedAt   int64  `gorm:"not null;index:idx_collector_observation_received,priority:2"`
	ReplicatedAt int64
	Metadata     []byte `gorm:"type:BLOB"`
	CreatedAt    time.Time
}

type CollectorStoredGap struct {
	GapID             []byte `gorm:"type:BLOB;size:16;primaryKey"`
	NodeUUID          []byte `gorm:"type:BLOB;size:16;not null;index:idx_collector_gap_session,priority:1"`
	SessionID         []byte `gorm:"type:BLOB;size:16;not null;index:idx_collector_gap_session,priority:2"`
	StartSequence     uint64 `gorm:"not null;index:idx_collector_gap_session,priority:3"`
	EndSequence       uint64 `gorm:"not null"`
	Reason            int32  `gorm:"not null"`
	Payload           []byte `gorm:"type:BLOB;not null"`
	CreatedAtUnixNano int64  `gorm:"not null;index"`
	CreatedAt         time.Time
}

type CollectorAgentCursor struct {
	NodeUUID   []byte `gorm:"type:BLOB;size:16;primaryKey"`
	SessionID  []byte `gorm:"type:BLOB;size:16;primaryKey"`
	AckThrough uint64 `gorm:"not null"`
	UpdatedAt  time.Time
}

type CollectorOutbox struct {
	SpoolID    uint64 `gorm:"primaryKey;autoIncrement"`
	RecordType string `gorm:"size:24;not null;index"`
	Payload    []byte `gorm:"type:BLOB;not null"`
	CreatedAt  time.Time
}

type CollectorReplicationCursor struct {
	ID                      uint64 `gorm:"primaryKey"`
	CommittedSpoolThroughID uint64 `gorm:"not null"`
	NextBatchSequence       uint64 `gorm:"not null"`
	UpdatedAt               time.Time
}

type CollectorAuthorizationCache struct {
	ID                    uint64 `gorm:"primaryKey"`
	CollectorUUID         string `gorm:"size:64;not null"`
	PrimaryPublicKey      []byte `gorm:"type:BLOB;not null"`
	KeyID                 []byte `gorm:"type:BLOB;not null"`
	ConfigVersion         uint64 `gorm:"not null"`
	LastPrimarySeenNano   int64  `gorm:"not null"`
	AgentCACertificatePEM string `gorm:"type:TEXT"`
	Kind                  string `gorm:"size:16"`
	ProbeConfigJSON       []byte `gorm:"type:BLOB"`
	UpdatedAt             time.Time
}

type CollectorCachedProbeTarget struct {
	ServerID         uint64 `gorm:"primaryKey"`
	ServerName       string
	IPv4             string
	IPv6             string
	Hostname         string
	TCPPorts         string
	EnableICMP       bool
	EnableTCP        bool
	EnableMTR        bool
	EnableIPv4       bool
	EnableIPv6       bool
	IntervalSec      uint
	MTRIntervalSec   uint
	MTRProbes        uint
	RouteIntervalSec uint
	UpdatedAt        time.Time
}

type CollectorCachedAssignment struct {
	NodeUUID      []byte `gorm:"type:BLOB;size:16;primaryKey"`
	ObserverID    string `gorm:"size:64;primaryKey"`
	ValidFrom     int64  `gorm:"primaryKey"`
	ValidTo       int64  `gorm:"index"`
	Generation    uint64 `gorm:"not null"`
	ConfigVersion uint64 `gorm:"not null;index"`
	UpdatedAt     time.Time
}

type CollectorCachedRevocation struct {
	NodeUUID      []byte `gorm:"type:BLOB;size:16;primaryKey"`
	ConfigVersion uint64 `gorm:"not null;index"`
	UpdatedAt     time.Time
}

type CollectorHealthEvidence struct {
	ID             uint64 `gorm:"primaryKey;autoIncrement"`
	ObserverID     string `gorm:"size:64;not null;index:idx_collector_health_time,priority:1"`
	SampledAt      int64  `gorm:"not null;index:idx_collector_health_time,priority:2"`
	Healthy        bool   `gorm:"not null"`
	ProcessSession string `gorm:"size:64;not null"`
	Replicated     bool   `gorm:"not null;index"`
	CreatedAt      time.Time
}

type CollectorDataLoss struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	FactID       []byte `gorm:"type:BLOB;size:16;not null;uniqueIndex"`
	OccurredAt   int64  `gorm:"not null;index"`
	Reason       string `gorm:"size:64;not null"`
	FirstSpoolID uint64
	LastSpoolID  uint64
	LostRecords  uint64
	Detail       string
	Replicated   bool `gorm:"not null;index"`
	CreatedAt    time.Time
}
