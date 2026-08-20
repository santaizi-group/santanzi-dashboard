package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	HostStateUnknown = "unknown"
	HostStateOnline  = "online"
	HostStateOffline = "offline"

	ConnectivityUnknown     = "unknown"
	ConnectivityFull        = "full"
	ConnectivityPartial     = "partial"
	ConnectivityUnavailable = "unavailable"

	AvailabilityResolutionRaw  = "30s"
	AvailabilityResolutionSpan = "span"
)

type SchemaMigration struct {
	Version   uint64    `gorm:"primaryKey"`
	AppliedAt time.Time `gorm:"not null"`
}

type ServerNodeBinding struct {
	ID        uint64 `gorm:"primaryKey"`
	ServerID  uint64 `gorm:"not null;index:idx_server_node_binding"`
	NodeUUID  []byte `gorm:"type:BLOB;size:16;not null;index:idx_server_node_binding;index:idx_node_binding"`
	ValidFrom int64  `gorm:"not null;index"`
	ValidTo   int64  `gorm:"index"`
	Current   bool   `gorm:"not null;index"`
	Reason    string `gorm:"not null"`
	CreatedAt time.Time
}

type TelemetryEvent struct {
	EventID            []byte `gorm:"type:BLOB;size:16;primaryKey"`
	NodeUUID           []byte `gorm:"type:BLOB;size:16;not null;uniqueIndex:idx_node_session_sequence;index:idx_node_collected,priority:1"`
	SessionID          []byte `gorm:"type:BLOB;size:16;not null;uniqueIndex:idx_node_session_sequence"`
	Sequence           uint64 `gorm:"not null;uniqueIndex:idx_node_session_sequence"`
	EventType          int32  `gorm:"not null;index:idx_event_type_collected,priority:1"`
	Priority           int32  `gorm:"not null;index"`
	CollectedAt        int64  `gorm:"not null;index:idx_node_collected,priority:2;index:idx_event_type_collected,priority:2"`
	AgentUptimeNano    uint64
	SessionElapsedNano uint64
	ClockUntrusted     bool      `gorm:"not null;index"`
	SourceProtocol     int32     `gorm:"not null;index"`
	Reliability        int32     `gorm:"not null"`
	Payload            []byte    `gorm:"type:BLOB"`
	PayloadRetained    bool      `gorm:"not null;default:true;index"`
	CreatedAt          time.Time `gorm:"index"`
}

type TelemetryObservation struct {
	EventID      []byte `gorm:"type:BLOB;size:16;primaryKey"`
	ObserverID   string `gorm:"primaryKey;size:64;index:idx_observer_received,priority:1"`
	NodeUUID     []byte `gorm:"type:BLOB;size:16;not null;index:idx_observation_node_received,priority:1"`
	ReceivedAt   int64  `gorm:"not null;index:idx_observer_received,priority:2;index:idx_observation_node_received,priority:2"`
	ReplicatedAt int64  `gorm:"index"`
	Metadata     []byte `gorm:"type:BLOB"`
	CreatedAt    time.Time
}

type TelemetryGap struct {
	GapID              []byte `gorm:"type:BLOB;size:16;primaryKey"`
	NodeUUID           []byte `gorm:"type:BLOB;size:16;not null;index:idx_gap_session,priority:1"`
	SessionID          []byte `gorm:"type:BLOB;size:16;not null;index:idx_gap_session,priority:2"`
	StartSequence      uint64 `gorm:"not null;index:idx_gap_session,priority:3"`
	EndSequence        uint64 `gorm:"not null"`
	Reason             int32  `gorm:"not null;index"`
	ReplacementEventID []byte `gorm:"type:BLOB;size:16"`
	CreatedAtUnixNano  int64  `gorm:"not null;index"`
	CreatedAt          time.Time
}

type TelemetryIngestCursor struct {
	ReceiverID string `gorm:"primaryKey;size:64"`
	NodeUUID   []byte `gorm:"type:BLOB;size:16;primaryKey"`
	SessionID  []byte `gorm:"type:BLOB;size:16;primaryKey"`
	AckThrough uint64 `gorm:"not null"`
	UpdatedAt  time.Time
}

const (
	CollectorKindObserver = "observer"
	CollectorKindProbe    = "probe"

	DefaultProbeIntervalSec   = 30
	DefaultMTRIntervalSec     = 300
	DefaultProbeTCPPorts      = "22,443"
	DefaultProbeFailThreshold = 3
)

type Collector struct {
	CollectorUUID     string `gorm:"primaryKey;size:64"`
	Name              string `gorm:"not null"`
	Address           string `gorm:"not null"`
	ListenPort        uint   `gorm:"not null;default:0"`
	TokenHash         []byte `gorm:"type:BLOB;size:32;not null" json:"-"`
	RegistrationToken string `gorm:"-" json:"-"`
	TokenCiphertext   []byte `gorm:"column:token_ciphertext;type:BLOB;not null" json:"-"`
	Generation        uint64 `gorm:"not null;default:1"`
	ConfigVersion     uint64 `gorm:"not null;default:1"`
	TLS               bool
	InsecureTLS       bool
	Location          string `gorm:"size:64"`
	Kind              string `gorm:"size:16;not null;default:observer;index"`
	ProbeIntervalSec  uint   `gorm:"not null;default:30"`
	MTRIntervalSec    uint   `gorm:"not null;default:300"`
	TCPPorts          string `gorm:"size:64;not null;default:'22,443'"`
	EnableICMP        bool   `gorm:"not null;default:1"`
	EnableTCP         bool   `gorm:"not null;default:1"`
	EnableMTR         bool   `gorm:"not null;default:1"`
	EnableIPv4        *bool  `gorm:"not null;default:1"`
	EnableIPv6        *bool  `gorm:"not null;default:1"`
	ProbeNotify       bool
	NotificationTag   string `gorm:"size:64"`
	LatencyNotify     bool
	MinLatencyMs      float64
	MaxLatencyMs      float64
	FailThreshold     uint `gorm:"not null;default:3"`
	Revoked           bool `gorm:"not null;index"`
	Deleted           bool `gorm:"not null;index"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func NormalizeCollectorKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", CollectorKindObserver:
		return CollectorKindObserver
	case CollectorKindProbe:
		return CollectorKindProbe
	default:
		return ""
	}
}

func (c Collector) IsProbe() bool {
	return NormalizeCollectorKind(c.Kind) == CollectorKindProbe
}

func (c *Collector) ApplyProbeDefaults() {
	if NormalizeCollectorKind(c.Kind) == "" {
		c.Kind = CollectorKindObserver
	} else {
		c.Kind = NormalizeCollectorKind(c.Kind)
	}
	if c.ProbeIntervalSec == 0 {
		c.ProbeIntervalSec = DefaultProbeIntervalSec
	}
	if c.MTRIntervalSec == 0 {
		c.MTRIntervalSec = DefaultMTRIntervalSec
	}
	if c.TCPPorts == "" {
		c.TCPPorts = DefaultProbeTCPPorts
	}
	if c.FailThreshold == 0 {
		c.FailThreshold = DefaultProbeFailThreshold
	}
	if c.NotificationTag == "" {
		c.NotificationTag = "default"
	}
	if !BoolOrTrue(c.EnableIPv4) && !BoolOrTrue(c.EnableIPv6) {
		c.EnableIPv4 = BoolPtr(true)
		c.EnableIPv6 = BoolPtr(true)
	}
}

func BoolOrTrue(value *bool) bool {
	return value == nil || *value
}

func BoolPtr(value bool) *bool {
	return &value
}

func (c *Collector) BeforeSave(_ *gorm.DB) error {
	value, err := encryptSecret(c.RegistrationToken)
	if err != nil {
		return err
	}
	c.TokenCiphertext = value
	return nil
}

func (c *Collector) AfterFind(_ *gorm.DB) error {
	value, err := decryptSecret(c.TokenCiphertext)
	if err != nil {
		return err
	}
	c.RegistrationToken = value
	return nil
}

type CollectorScope struct {
	ID            uint64 `gorm:"primaryKey"`
	CollectorUUID string `gorm:"size:64;not null;uniqueIndex:idx_collector_scope"`
	ScopeType     string `gorm:"size:16;not null;uniqueIndex:idx_collector_scope"`
	ScopeValue    string `gorm:"size:255;not null;uniqueIndex:idx_collector_scope"`
	CreatedAt     time.Time
}

type CollectorRuntime struct {
	CollectorUUID           string `gorm:"primaryKey;size:64"`
	Status                  string `gorm:"size:32;not null;index"`
	LastSeen                int64  `gorm:"index"`
	LastSync                int64  `gorm:"index"`
	LastPrimarySeen         int64  `gorm:"index"`
	SpoolSize               uint64
	PendingRecords          uint64
	OldestPending           int64
	ReplicationCursor       uint64
	ConnectedAgents         uint64
	ProtocolVersion         string
	SoftwareVersion         string
	HeartbeatRttMs          float64
	HeartbeatRttSampledAt   int64
	ReplicationRttMs        float64
	ReplicationRttSampledAt int64
	UpdatedAt               time.Time
}

type ObserverAssignment struct {
	ID            uint64 `gorm:"primaryKey"`
	NodeUUID      []byte `gorm:"type:BLOB;size:16;not null;index:idx_assignment_range,priority:1"`
	ObserverID    string `gorm:"size:64;not null;index:idx_assignment_range,priority:2"`
	ValidFrom     int64  `gorm:"not null;index:idx_assignment_range,priority:3"`
	ValidTo       int64  `gorm:"index:idx_assignment_range,priority:4"`
	ConfigVersion uint64 `gorm:"not null"`
	Generation    uint64 `gorm:"not null"`
	CreatedAt     time.Time
}

type ObserverHealthBucket struct {
	ObserverID     string `gorm:"primaryKey;size:64"`
	BucketStart    int64  `gorm:"primaryKey"`
	Healthy        bool   `gorm:"not null;index"`
	ProcessSession string
	Revision       uint64 `gorm:"not null;default:1"`
	UpdatedAt      time.Time
}

type ObserverPathBucket struct {
	NodeUUID    []byte `gorm:"type:BLOB;size:16;primaryKey"`
	ObserverID  string `gorm:"primaryKey;size:64"`
	BucketStart int64  `gorm:"primaryKey"`
	Seen        bool   `gorm:"not null"`
	LastSeenAt  int64
	Revision    uint64 `gorm:"not null;default:1"`
	UpdatedAt   time.Time
}

type AvailabilityBucket struct {
	NodeUUID          []byte `gorm:"type:BLOB;size:16;primaryKey"`
	BucketStart       int64  `gorm:"primaryKey"`
	WindowEnd         int64  `gorm:"not null;default:0"`
	Resolution        string `gorm:"size:8;not null;default:'30s';index"`
	HostState         string `gorm:"size:16;not null;index"`
	ConnectivityState string `gorm:"size:16;not null;index"`
	ExpectedObservers uint32 `gorm:"not null"`
	HealthyObservers  uint32 `gorm:"not null"`
	SeenObservers     uint32 `gorm:"not null"`
	ObserverSummary   []byte `gorm:"type:BLOB"`
	Revision          uint64 `gorm:"not null;default:1"`
	Finalized         bool   `gorm:"not null;index"`
	RecalculatedAt    int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type AvailabilityIncident struct {
	ID                    uint64 `gorm:"primaryKey"`
	NodeUUID              []byte `gorm:"type:BLOB;size:16;not null;index:idx_incident_node_time,priority:1"`
	InitialClassification string `gorm:"size:64;not null;index"`
	CurrentClassification string `gorm:"size:64;not null;index"`
	Revision              uint64 `gorm:"not null;default:1"`
	StartedAt             int64  `gorm:"not null;index:idx_incident_node_time,priority:2"`
	EndedAt               int64  `gorm:"index"`
	RecalculatedAt        int64
	Reason                string
	ObserverEvidence      []byte `gorm:"type:BLOB"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type IncidentRevision struct {
	ID             uint64 `gorm:"primaryKey"`
	IncidentID     uint64 `gorm:"not null;index;uniqueIndex:idx_incident_revision"`
	Revision       uint64 `gorm:"not null;uniqueIndex:idx_incident_revision"`
	Classification string `gorm:"size:64;not null"`
	Reason         string
	Evidence       []byte `gorm:"type:BLOB"`
	RecalculatedAt int64  `gorm:"not null;index"`
	CreatedAt      time.Time
}

type StateRollup struct {
	NodeUUID    []byte `gorm:"type:BLOB;size:16;primaryKey"`
	Resolution  string `gorm:"size:8;primaryKey"`
	WindowStart int64  `gorm:"primaryKey"`
	WindowEnd   int64  `gorm:"not null;index"`
	SampleCount uint32 `gorm:"not null"`
	Payload     []byte `gorm:"type:BLOB;not null"`
	NetInTotal  uint64
	NetOutTotal uint64
	CreatedAt   time.Time
}

type AgentTelemetryRuntime struct {
	NodeUUID        []byte `gorm:"type:BLOB;size:16;primaryKey"`
	WalPressure     int32  `gorm:"not null"`
	WalBytes        uint64
	PendingEvents   uint64
	OldestPending   int64
	SinkCursors     []byte `gorm:"type:BLOB"`
	ClockUntrusted  bool
	ProtocolVersion string
	UpdatedAt       time.Time
}

type CollectorReplicationReceipt struct {
	CollectorUUID         string `gorm:"size:64;primaryKey"`
	ReplicationSession    []byte `gorm:"type:BLOB;size:16;primaryKey"`
	BatchSequence         uint64 `gorm:"primaryKey"`
	CommittedSpoolThrough uint64 `gorm:"not null"`
	CreatedAt             time.Time
}

type AvailabilityRecomputeQueue struct {
	NodeUUID    []byte `gorm:"type:BLOB;size:16;primaryKey"`
	BucketStart int64  `gorm:"primaryKey"`
	Reason      string `gorm:"size:64;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TelemetryDataLoss struct {
	FactID       []byte `gorm:"type:BLOB;size:16;primaryKey"`
	ComponentID  string `gorm:"size:64;not null;index"`
	OccurredAt   int64  `gorm:"not null;index"`
	Reason       int32  `gorm:"not null;index"`
	FirstSpoolID uint64
	LastSpoolID  uint64
	LostRecords  uint64
	Detail       string
	CreatedAt    time.Time
}

type TelemetryAlert struct {
	ID          uint64 `gorm:"primaryKey"`
	DedupKey    string `gorm:"size:160;not null;uniqueIndex"`
	AlertType   string `gorm:"size:64;not null;index"`
	Severity    string `gorm:"size:16;not null;index"`
	NodeUUID    []byte `gorm:"type:BLOB;size:16;index"`
	ComponentID string `gorm:"size:64;index"`
	OccurredAt  int64  `gorm:"not null;index"`
	Message     string `gorm:"not null"`
	Notified    bool   `gorm:"not null"`
	CreatedAt   time.Time
}

type ConnectionLatencyBucket struct {
	Kind          string `gorm:"primaryKey;size:32"`
	CollectorUUID string `gorm:"primaryKey;size:64"`
	NodeUUID      []byte `gorm:"type:BLOB;size:16;primaryKey"`
	ObserverID    string `gorm:"primaryKey;size:64"`
	BucketStart   int64  `gorm:"primaryKey"`
	MinMs         float64
	MaxMs         float64
	SumMs         float64
	Count         uint32 `gorm:"not null"`
	UpdatedAt     time.Time
}

type ConnectionLatencyCursor struct {
	Kind          string `gorm:"primaryKey;size:32"`
	CollectorUUID string `gorm:"primaryKey;size:64"`
	NodeUUID      []byte `gorm:"type:BLOB;size:16;primaryKey"`
	ObserverID    string `gorm:"primaryKey;size:64"`
	LastSampledAt int64  `gorm:"not null"`
	UpdatedAt     time.Time
}

type ProbeSampleBucket struct {
	CollectorUUID string `gorm:"primaryKey;size:64"`
	ServerID      uint64 `gorm:"primaryKey"`
	Kind          string `gorm:"primaryKey;size:16"`
	Port          uint   `gorm:"primaryKey"`
	BucketStart   int64  `gorm:"primaryKey"`
	MinMs         float64
	MaxMs         float64
	SumMs         float64
	Count         uint32 `gorm:"not null"`
	LossSum       float64
	SuccessCount  uint32
	FailCount     uint32
	UpdatedAt     time.Time
}

type ProbeLatest struct {
	CollectorUUID string `gorm:"primaryKey;size:64"`
	ServerID      uint64 `gorm:"primaryKey"`
	SampledAt     int64
	Reachable     bool
	DisplayRttMs  float64
	LastError     string
	ICMPOk        bool
	ICMPRttMs     float64
	ICMPLoss      float64
	ICMPSent      uint32
	ICMPRecv      uint32
	TCPJSON       []byte `gorm:"type:BLOB"`
	HasTrace      bool
	UpdatedAt     time.Time
}

type ProbeTrace struct {
	CollectorUUID  string `gorm:"primaryKey;size:64"`
	ServerID       uint64 `gorm:"primaryKey"`
	SampledAt      int64
	Destination    string
	HopsJSON       []byte `gorm:"type:BLOB"`
	TCPSampledAt   int64
	TCPDestination string
	TCPHopsJSON    []byte `gorm:"type:BLOB"`
	TCPPort        uint
	UpdatedAt      time.Time
}

type ProbeAlertState struct {
	CollectorUUID    string `gorm:"primaryKey;size:64"`
	ServerID         uint64 `gorm:"primaryKey"`
	ConsecutiveFails uint
	DownNotified     bool
	LatencyAlert     bool
	UpdatedAt        time.Time
}
