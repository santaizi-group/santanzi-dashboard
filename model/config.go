package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	kyaml "github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"
)

var Languages = map[string]string{
	"zh-CN": "简体中文",
	"zh-TW": "繁體中文",
	"en-US": "English",
	"es-ES": "Español",
}

const (
	PublicThemeServerStatus = "server-status"
	PublicThemeNazhua       = "nazhua"
)

// NormalizePublicTheme 将公开站主题收敛到内置白名单。
func NormalizePublicTheme(theme string) string {
	switch strings.TrimSpace(theme) {
	case PublicThemeNazhua:
		return PublicThemeNazhua
	default:
		return PublicThemeServerStatus
	}
}

const (
	ConfigTypeGitHub     = "github"
	ConfigTypeGitee      = "gitee"
	ConfigTypeGitlab     = "gitlab"
	ConfigTypeJihulab    = "jihulab"
	ConfigTypeGitea      = "gitea"
	ConfigTypeCloudflare = "cloudflare"
	ConfigTypeOidc       = "oidc"
	ConfigTypeMock       = "mock" // 本地开发模拟登录，切勿用于生产环境
)

const (
	ConfigCoverAll = iota
	ConfigCoverIgnoreAll
)

// SiteConfig 站点前端配置
type SiteConfig struct {
	Brand               string // 站点名称
	CookieName          string // 浏览器 Cookie 名称
	Theme               string
	DashboardTheme      string
	CustomCode          string
	CustomCodeDashboard string
	ViewPassword        string // 前台查看密码
	PrimaryColor        string
	FooterText          string
	LogoURL             string
	SafeCustomCSS       string
	PrimaryLocation     string // 主面板在地球上的位置：ISO2 或 "lat,lon"
}

// PublicSiteConfig 仅包含未登录页面所需的站点配置字段（去除敏感信息）
type PublicSiteConfig struct {
	Brand               string // 站点名称
	Theme               string
	DashboardTheme      string
	CustomCode          string
	CustomCodeDashboard string
}

// PublicConfig 仅包含未登录页面所需的公开配置字段
type PublicConfig struct {
	Site                            PublicSiteConfig
	Language                        string
	MaxTCPPingValue                 int32
	DisableSwitchTemplateInFrontend bool
	ShowAvailabilityToGuest         bool
}

// InstallScriptConfig 一键安装脚本源配置
type InstallScriptConfig struct {
	Linux            string // Linux 中文安装脚本 URL（探针）
	LinuxEn          string // Linux 英文安装脚本 URL（探针）
	Windows          string // Windows 安装脚本 URL（探针）
	MacOS            string // macOS 安装脚本 URL（探针）
	Collector        string // Linux 从端（Collector）安装脚本 URL
	Dashboard        string `koanf:"dashboard" yaml:"dashboard"`                 // 主面板安装脚本 URL
	UpgradeCollector string `koanf:"upgrade_collector" yaml:"upgrade_collector"` // 从端升级脚本 URL
	UpgradeLinux     string `koanf:"upgrade_linux" yaml:"upgrade_linux"`         // Linux 中文升级脚本 URL
	UpgradeLinuxEn   string `koanf:"upgrade_linuxen" yaml:"upgrade_linuxen"`     // Linux 英文升级脚本 URL
	UpgradeWindows   string `koanf:"upgrade_windows" yaml:"upgrade_windows"`
	UpgradeMacOS     string `koanf:"upgrade_macos" yaml:"upgrade_macos"`
}

type TelemetryConfig struct {
	DataDir                            string `koanf:"data_dir" yaml:"data_dir"`
	SigningKeyPath                     string `koanf:"signing_key_path" yaml:"signing_key_path"`
	SecretKeyPath                      string `koanf:"secret_key_path" yaml:"secret_key_path"`
	PrimaryEndpoint                    string `koanf:"primary_endpoint" yaml:"primary_endpoint"`
	StateIntervalSeconds               uint64 `koanf:"state_interval_seconds" yaml:"state_interval_seconds"`
	HeartbeatIntervalSeconds           uint64 `koanf:"heartbeat_interval_seconds" yaml:"heartbeat_interval_seconds"`
	OfflineThresholdSeconds            uint64 `koanf:"offline_threshold_seconds" yaml:"offline_threshold_seconds"`
	IngestBatchSize                    int    `koanf:"ingest_batch_size" yaml:"ingest_batch_size"`
	IngestQueueSize                    int    `koanf:"ingest_queue_size" yaml:"ingest_queue_size"`
	CredentialValidityDays             uint64 `koanf:"credential_validity_days" yaml:"credential_validity_days"`
	CredentialRefreshDays              uint64 `koanf:"credential_refresh_days" yaml:"credential_refresh_days"`
	CredentialGraceDays                uint64 `koanf:"credential_grace_days" yaml:"credential_grace_days"`
	AvailabilityBucketSeconds          uint64 `koanf:"availability_bucket_seconds" yaml:"availability_bucket_seconds"`
	MinObservers                       uint32 `koanf:"min_observers" yaml:"min_observers"`
	EnableConnectivityNotification     bool   `koanf:"enable_connectivity_notification" yaml:"enable_connectivity_notification"`
	EnableCorrectionNotification       bool   `koanf:"enable_correction_notification" yaml:"enable_correction_notification"`
	EnableCollectorOfflineNotification bool   `koanf:"enable_collector_offline_notification" yaml:"enable_collector_offline_notification"`
	EnableCollectorOnlineNotification  bool   `koanf:"enable_collector_online_notification" yaml:"enable_collector_online_notification"`
	EnableDataLossNotification         bool   `koanf:"enable_data_loss_notification" yaml:"enable_data_loss_notification"`
}

type CollectorModeConfig struct {
	PrimaryEndpoint     string `koanf:"primary_endpoint" yaml:"primary_endpoint"`
	PrimaryTLS          bool   `koanf:"primary_tls" yaml:"primary_tls"`
	PrimaryInsecureTLS  bool   `koanf:"primary_insecure_tls" yaml:"primary_insecure_tls"`
	RegistrationToken   string `koanf:"registration_token" yaml:"registration_token"`
	DatabasePath        string `koanf:"database_path" yaml:"database_path"`
	SpoolMaxBytes       uint64 `koanf:"spool_max_bytes" yaml:"spool_max_bytes"`
	SpoolMaxAgeDays     uint64 `koanf:"spool_max_age_days" yaml:"spool_max_age_days"`
	StatusAuthorization string `koanf:"status_authorization" yaml:"status_authorization"`
	NexttracePath       string `koanf:"nexttrace_path" yaml:"nexttrace_path"`
}

type GRPCTLSConfig struct {
	Enabled              bool   `koanf:"enabled" yaml:"enabled"`
	CertFile             string `koanf:"cert_file" yaml:"cert_file"`
	KeyFile              string `koanf:"key_file" yaml:"key_file"`
	ClientCAFile         string `koanf:"client_ca_file" yaml:"client_ca_file"`
	RequireAgentMTLS     bool   `koanf:"require_agent_mtls" yaml:"require_agent_mtls"`
	RequireCollectorMTLS bool   `koanf:"require_collector_mtls" yaml:"require_collector_mtls"`
}

type RollupConfig struct {
	Enabled   bool `koanf:"enabled" yaml:"enabled"`
	BatchSize int  `koanf:"batch_size" yaml:"batch_size"`
}

type RetentionConfig struct {
	StateRawHours      uint64 `koanf:"state_raw_hours" yaml:"state_raw_hours"`
	StateOneMinuteDays uint64 `koanf:"state_one_minute_days" yaml:"state_one_minute_days"`
	StateOneHourDays   uint64 `koanf:"state_one_hour_days" yaml:"state_one_hour_days"`
	ObservationDays    uint64 `koanf:"observation_days" yaml:"observation_days"`
	EvidenceHours      uint64 `koanf:"evidence_hours" yaml:"evidence_hours"`
	LifecycleDays      uint64 `koanf:"lifecycle_days" yaml:"lifecycle_days"`
	BatchSize          int    `koanf:"batch_size" yaml:"batch_size"`
	MaxRuntimeMs       uint64 `koanf:"max_runtime_ms" yaml:"max_runtime_ms"`
	ReceiptDays        uint64 `koanf:"receipt_days" yaml:"receipt_days"`
	CompactMinBytes    int64  `koanf:"compact_min_bytes" yaml:"compact_min_bytes"`
	AutoCompact        *bool  `koanf:"auto_compact" yaml:"auto_compact"`
}

type WebConfig struct {
	Delivery  string `koanf:"delivery" yaml:"delivery"`
	StaticDir string `koanf:"static_dir" yaml:"static_dir"`
}

// Config 站点配置
type Config struct {
	Debug         bool   // debug模式开关
	Language      string // 系统语言，默认 zh-CN
	Mode          string // primary 或 collector
	Site          SiteConfig
	InstallScript InstallScriptConfig
	Telemetry     TelemetryConfig     `koanf:"telemetry" yaml:"telemetry"`
	Collector     CollectorModeConfig `koanf:"collector" yaml:"collector"`
	GRPCTLS       GRPCTLSConfig       `koanf:"grpc_tls" yaml:"grpc_tls"`
	Rollup        RollupConfig        `koanf:"rollup" yaml:"rollup"`
	Retention     RetentionConfig     `koanf:"retention" yaml:"retention"`
	Web           WebConfig           `koanf:"web" yaml:"web"`
	Oauth2        struct {
		Type            string
		Admin           string // 管理员用户名列表
		AdminGroups     string // 管理员用户组列表
		ClientID        string
		ClientSecret    string
		Endpoint        string
		OidcDisplayName string // for OIDC Display Name
		OidcIssuer      string // for OIDC Issuer
		OidcLogoutURL   string // for OIDC Logout URL
		OidcRegisterURL string // for OIDC Register URL
		OidcLoginClaim  string // for OIDC Claim
		OidcGroupClaim  string // for OIDC Group Claim
		OidcScopes      string // for OIDC Scopes
		OidcAutoCreate  bool   // for OIDC Auto Create
		OidcAutoLogin   bool   // for OIDC Auto Login
	}
	HTTPPort      uint
	GRPCPort      uint
	GRPCHost      string
	ProxyGRPCPort uint
	TLS           bool

	EnablePlainIPInNotification     bool // 通知信息IP不打码
	DisableSwitchTemplateInFrontend bool // 前台禁用切换模板功能

	// IP变更提醒
	EnableIPChangeNotification bool
	IPChangeNotificationTag    string
	Cover                      uint8  // 覆盖范围（0:提醒未被 IgnoredIPNotification 包含的所有服务器; 1:仅提醒被 IgnoredIPNotification 包含的服务器;）
	IgnoredIPNotification      string // 特定服务器IP（多个服务器用逗号分隔）

	Location string // 时区，默认为 Asia/Shanghai

	IgnoredIPNotificationServerIDs map[uint64]bool // [ServerID] -> bool(值为true代表当前ServerID在特定服务器列表内）
	MaxTCPPingValue                int32
	AvgPingCount                   int

	DNSServers string

	// 服务器离线历史配置
	EnableOfflineHistory        bool
	OfflineThresholdSeconds     uint64
	OfflineCheckIntervalSeconds uint64
	OfflineMergeGapSeconds      uint64
	OfflineHistoryRetentionDays uint64
	EnableOfflineNotification   bool
	EnableRecoveryNotification  bool
	ShowAvailabilityToGuest     bool // 是否向前台访客展示服务器可用性摘要

	k        *koanf.Koanf
	filePath string
}

// Read 读取配置文件并应用
func (c *Config) Read(path string) error {
	c.k = koanf.New(".")
	c.filePath = path

	// 先读取环境变量，然后读取配置文件；后者可以覆盖前者，因为三太子支持在线修改配置

	err := c.k.Load(env.Provider("SANTAIZI_", ".", configEnvKey), nil)
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		err = c.k.Load(file.Provider(path), kyaml.Parser())
		if err != nil {
			return err
		}
	}

	err = c.k.Unmarshal("", c)
	if err != nil {
		return err
	}

	if c.Mode == "" {
		c.Mode = "primary"
	}
	if c.Mode != "primary" && c.Mode != "collector" {
		return fmt.Errorf("unsupported mode %q", c.Mode)
	}
	if c.Mode == "primary" && (c.Oauth2.Type == "" || c.Oauth2.Admin == "") {
		return errors.New("missing oauth2 config")
	}
	// mock 模式仅用于本地开发，不需要真实的 ClientID/ClientSecret，且必须同时开启 Debug
	if c.Mode == "primary" && c.Oauth2.Type == ConfigTypeMock && !c.Debug {
		return errors.New("mock oauth2 can only be used in debug mode")
	}
	if c.Mode == "primary" && c.Oauth2.Type != ConfigTypeMock && (c.Oauth2.ClientID == "" || c.Oauth2.ClientSecret == "") {
		return errors.New("missing oauth2 config")
	}

	if c.Site.Brand == "" {
		c.Site.Brand = "Santaizi Monitoring"
	}
	if c.Site.CookieName == "" {
		c.Site.CookieName = "santaizi-dashboard"
	}
	if c.Site.Theme == "" || c.Site.Theme == "default" {
		c.Site.Theme = PublicThemeServerStatus
	} else {
		c.Site.Theme = NormalizePublicTheme(c.Site.Theme)
	}
	if c.Site.DashboardTheme == "" || c.Site.DashboardTheme == "default" {
		c.Site.DashboardTheme = "spa"
	}
	if c.Site.PrimaryColor == "" {
		c.Site.PrimaryColor = "#2563eb"
	}
	if c.Site.LogoURL == "" {
		c.Site.LogoURL = "/static/logo.svg"
	}
	if c.Web.Delivery == "" {
		c.Web.Delivery = "embedded"
	}
	if c.Web.Delivery != "embedded" && c.Web.Delivery != "external" {
		return fmt.Errorf("unsupported web delivery %q", c.Web.Delivery)
	}
	if c.Language == "" {
		c.Language = "zh-CN"
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = 80
	}
	if c.GRPCPort == 0 {
		c.GRPCPort = 5555
	}
	if c.Telemetry.DataDir == "" {
		c.Telemetry.DataDir = "/var/lib/santaizi-dashboard"
	}
	if c.Telemetry.SigningKeyPath == "" {
		c.Telemetry.SigningKeyPath = filepath.Join(c.Telemetry.DataDir, "telemetry-signing.key")
	}
	if c.Telemetry.SecretKeyPath == "" {
		c.Telemetry.SecretKeyPath = filepath.Join(c.Telemetry.DataDir, "business-secrets.key")
	}
	if c.Telemetry.PrimaryEndpoint == "" {
		c.Telemetry.PrimaryEndpoint = c.GRPCHost
	}
	if c.Telemetry.StateIntervalSeconds == 0 {
		c.Telemetry.StateIntervalSeconds = 5
	}
	if c.Telemetry.HeartbeatIntervalSeconds == 0 {
		c.Telemetry.HeartbeatIntervalSeconds = 10
	}
	if c.Telemetry.OfflineThresholdSeconds == 0 {
		c.Telemetry.OfflineThresholdSeconds = 30
	}
	if c.Telemetry.IngestBatchSize == 0 {
		c.Telemetry.IngestBatchSize = 256
	}
	if c.Telemetry.IngestQueueSize == 0 {
		c.Telemetry.IngestQueueSize = 4096
	}
	if c.Telemetry.CredentialValidityDays == 0 {
		c.Telemetry.CredentialValidityDays = 30
	}
	if c.Telemetry.CredentialRefreshDays == 0 {
		c.Telemetry.CredentialRefreshDays = 7
	}
	if c.Telemetry.CredentialGraceDays == 0 {
		c.Telemetry.CredentialGraceDays = 7
	}
	if c.Telemetry.AvailabilityBucketSeconds == 0 {
		c.Telemetry.AvailabilityBucketSeconds = 30
	}
	if c.Telemetry.MinObservers == 0 {
		c.Telemetry.MinObservers = 1
	}
	if c.Collector.DatabasePath == "" {
		c.Collector.DatabasePath = filepath.Join(c.Telemetry.DataDir, "collector.db")
	}
	if c.Collector.SpoolMaxBytes == 0 {
		c.Collector.SpoolMaxBytes = 5 << 30
	}
	if c.Collector.SpoolMaxAgeDays == 0 {
		c.Collector.SpoolMaxAgeDays = 30
	}
	if !c.Rollup.Enabled {
		// Rollups are enabled by default. Deployments can suspend the worker by
		// setting a zero batch size explicitly through a future maintenance mode.
		c.Rollup.Enabled = true
	}
	if c.Rollup.BatchSize == 0 {
		c.Rollup.BatchSize = 1000
	}
	if c.Retention.StateRawHours == 0 {
		c.Retention.StateRawHours = 6
	}
	if c.Retention.StateOneMinuteDays == 0 {
		c.Retention.StateOneMinuteDays = 30
	}
	if c.Retention.StateOneHourDays == 0 {
		c.Retention.StateOneHourDays = 365
	}
	if c.Retention.ObservationDays == 0 {
		c.Retention.ObservationDays = 30
	}
	if c.Retention.EvidenceHours == 0 {
		c.Retention.EvidenceHours = 48
	}
	if c.Retention.LifecycleDays == 0 {
		c.Retention.LifecycleDays = 3650
	}
	if c.Retention.BatchSize == 0 {
		c.Retention.BatchSize = 5000
	}
	if c.Retention.MaxRuntimeMs == 0 {
		c.Retention.MaxRuntimeMs = 20000
	}
	if c.Retention.ReceiptDays == 0 {
		c.Retention.ReceiptDays = 7
	}
	if c.Retention.CompactMinBytes == 0 {
		c.Retention.CompactMinBytes = 64 << 20
	}
	if c.Retention.AutoCompact == nil {
		c.Retention.AutoCompact = BoolPtr(true)
	}
	if c.EnableIPChangeNotification && c.IPChangeNotificationTag == "" {
		c.IPChangeNotificationTag = "default"
	}
	if c.Location == "" {
		c.Location = "Asia/Shanghai"
	}
	if c.MaxTCPPingValue == 0 {
		c.MaxTCPPingValue = 1000
	}
	if c.AvgPingCount == 0 {
		c.AvgPingCount = 2
	}
	// 默认使用本仓库 script/ 目录下的 Agent 专用安装脚本
	if c.InstallScript.Linux == "" {
		c.InstallScript.Linux = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent.sh"
	}
	if c.InstallScript.LinuxEn == "" {
		c.InstallScript.LinuxEn = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent_en.sh"
	}
	if c.InstallScript.Windows == "" {
		c.InstallScript.Windows = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install.ps1"
	}
	if c.InstallScript.MacOS == "" {
		c.InstallScript.MacOS = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install.command"
	}
	if c.InstallScript.Collector == "" {
		c.InstallScript.Collector = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_collector.sh"
	}
	if c.InstallScript.Dashboard == "" {
		c.InstallScript.Dashboard = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_dashboard.sh"
	}
	if c.InstallScript.UpgradeCollector == "" {
		c.InstallScript.UpgradeCollector = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_collector.sh"
	}
	if c.InstallScript.UpgradeLinux == "" {
		c.InstallScript.UpgradeLinux = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_agent.sh"
	}
	if c.InstallScript.UpgradeLinuxEn == "" {
		c.InstallScript.UpgradeLinuxEn = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_agent_en.sh"
	}
	if c.InstallScript.UpgradeWindows == "" {
		c.InstallScript.UpgradeWindows = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade.ps1"
	}
	if c.InstallScript.UpgradeMacOS == "" {
		c.InstallScript.UpgradeMacOS = "https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade.command"
	}
	if c.Oauth2.OidcScopes == "" {
		c.Oauth2.OidcScopes = "openid,profile,email"
	}
	if c.Oauth2.OidcLoginClaim == "" {
		c.Oauth2.OidcLoginClaim = "sub"
	}
	if c.Oauth2.OidcDisplayName == "" {
		c.Oauth2.OidcDisplayName = "OIDC"
	}
	if c.Oauth2.OidcGroupClaim == "" {
		c.Oauth2.OidcGroupClaim = "groups"
	}

	if c.GRPCTLS.RequireAgentMTLS && !c.GRPCTLS.Enabled {
		return errors.New("grpc_tls.require_agent_mtls requires grpc_tls.enabled")
	}
	if c.GRPCTLS.RequireCollectorMTLS && !c.GRPCTLS.Enabled {
		return errors.New("grpc_tls.require_collector_mtls requires grpc_tls.enabled")
	}
	if c.GRPCTLS.Enabled && (c.GRPCTLS.CertFile == "" || c.GRPCTLS.KeyFile == "") {
		return errors.New("grpc_tls.enabled requires cert_file and key_file")
	}

	c.NormalizeOfflineConfig()
	c.updateIgnoredIPNotificationID()
	return nil
}

func configEnvKey(name string) string {
	key := strings.ToLower(strings.TrimPrefix(name, "SANTAIZI_"))
	for _, section := range []string{"grpc_tls", "telemetry", "collector", "rollup", "retention", "web", "site", "oauth2", "installscript"} {
		prefix := section + "_"
		if strings.HasPrefix(key, prefix) {
			return section + "." + strings.TrimPrefix(key, prefix)
		}
	}
	return strings.ReplaceAll(key, "_", ".")
}

// NormalizeOfflineConfig 设置离线历史配置的默认值并校验边界。
func (c *Config) NormalizeOfflineConfig() {
	if c.OfflineThresholdSeconds == 0 {
		c.OfflineThresholdSeconds = 30
	}
	if c.OfflineThresholdSeconds < 10 {
		c.OfflineThresholdSeconds = 10
	}
	if c.OfflineCheckIntervalSeconds == 0 {
		c.OfflineCheckIntervalSeconds = 10
	}
	if c.OfflineCheckIntervalSeconds < 5 {
		c.OfflineCheckIntervalSeconds = 5
	}
	if c.OfflineCheckIntervalSeconds > c.OfflineThresholdSeconds {
		c.OfflineCheckIntervalSeconds = c.OfflineThresholdSeconds
	}
	if c.OfflineMergeGapSeconds == 0 {
		c.OfflineMergeGapSeconds = 10
	}
	if c.OfflineMergeGapSeconds > 3600 {
		c.OfflineMergeGapSeconds = 3600
	}
	if c.OfflineHistoryRetentionDays == 0 {
		c.OfflineHistoryRetentionDays = 365
	}
	if c.OfflineHistoryRetentionDays < 1 {
		c.OfflineHistoryRetentionDays = 1
	}
}

// updateIgnoredIPNotificationID 更新用于判断服务器ID是否属于特定服务器的map
func (c *Config) updateIgnoredIPNotificationID() {
	c.IgnoredIPNotificationServerIDs = make(map[uint64]bool)
	splitedIDs := strings.Split(c.IgnoredIPNotification, ",")
	for i := 0; i < len(splitedIDs); i++ {
		id, _ := strconv.ParseUint(splitedIDs[i], 10, 64)
		if id > 0 {
			c.IgnoredIPNotificationServerIDs[id] = true
		}
	}
}

// Save 保存配置文件
func (c *Config) Save() error {
	c.updateIgnoredIPNotificationID()
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.filePath, data, 0600)
}
