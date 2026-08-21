package singleton

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/patrickmn/go-cache"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
)

var Version = "debug"

func init() {
	if Version == "debug" {
		now := time.Now()
		Version = "dev-" + now.Format("060102")
	}
}

var (
	Conf  *model.Config
	Cache *cache.Cache
	DB    *gorm.DB
	Loc   *time.Location
)

func InitTimezoneAndCache() {
	var err error
	Loc, err = time.LoadLocation(Conf.Location)
	if err != nil {
		panic(err)
	}

	Cache = cache.New(5*time.Minute, 10*time.Minute)
}

// LoadSingleton 加载子服务并执行
func LoadSingleton() {
	InitScheduler()
	loadNotifications() // 加载通知服务
	loadServers()       // 加载服务器列表
	loadAPI()
	initNAT()
	initDDNS()
	startTrafficPolicyEvaluator()
}

// InitConfigFromPath 从给出的文件路径中加载配置
func InitConfigFromPath(path string) {
	Conf = &model.Config{}
	err := Conf.Read(path)
	if err != nil {
		panic(err)
	}
}

// InitDBFromPath 从给出的文件路径中加载数据库
func InitDBFromPath(path string) {
	var err error
	if err = initBusinessSecretEncryption(Conf.Telemetry.SecretKeyPath); err != nil {
		panic(err)
	}
	DB, err = OpenDBFromPath(path, Conf.Debug)
	if err != nil {
		panic(err)
	}
}

// OpenDBFromPath opens a Santaizi database and applies explicit, versioned
// migrations. Databases created before schema_migrations are intentionally
// rejected: this architecture starts from a fresh database by design.
func OpenDBFromPath(path string, debug bool) (*gorm.DB, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		CreateBatchSize: 200,
	})
	if err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			_ = CloseDB(db)
		}
	}()
	if debug {
		db = db.Debug()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	var userTableCount int64
	if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'").Scan(&userTableCount).Error; err != nil {
		return nil, err
	}
	if userTableCount == 0 {
		if _, err := sqlDB.Exec("PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
			return nil, fmt.Errorf("apply PRAGMA auto_vacuum=INCREMENTAL: %w", err)
		}
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	if err := migrateDatabase(db); err != nil {
		return nil, err
	}
	ready = true
	return db, nil
}

// CloseDB releases a SQLite handle so Windows can delete the file (WAL checkpoint + sql.DB.Close).
func CloseDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	_, _ = sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return sqlDB.Close()
}

func migrateDatabase(db *gorm.DB) error {
	var migrationTableCount int64
	if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'").Scan(&migrationTableCount).Error; err != nil {
		return err
	}
	if migrationTableCount == 0 {
		var userTableCount int64
		if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'").Scan(&userTableCount).Error; err != nil {
			return err
		}
		if userTableCount != 0 {
			return errors.New("existing database without schema_migrations is unsupported; configure an empty Santaizi database")
		}
		if err := db.AutoMigrate(&model.SchemaMigration{}); err != nil {
			return err
		}
	}

	var current uint64
	if err := db.Model(&model.SchemaMigration{}).Select("COALESCE(MAX(version), 0)").Scan(&current).Error; err != nil {
		return err
	}
	if current < 1 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(model.Server{}, model.User{},
				model.Notification{}, model.AlertRule{}, model.Monitor{},
				model.MonitorHistory{}, model.Transfer{},
				model.ApiToken{}, model.NAT{}, model.DDNSProfile{},
				model.ServerRuntime{}, model.ServerOfflineHistory{},
				model.ServerNodeBinding{}, model.TelemetryEvent{}, model.TelemetryObservation{},
				model.TelemetryGap{}, model.TelemetryIngestCursor{}, model.Collector{},
				model.CollectorScope{}, model.CollectorRuntime{}, model.ObserverAssignment{},
				model.ObserverHealthBucket{}, model.ObserverPathBucket{}, model.AvailabilityBucket{},
				model.AvailabilityIncident{}, model.IncidentRevision{}, model.StateRollup{},
				model.AgentTelemetryRuntime{}, model.CollectorReplicationReceipt{}, model.AvailabilityRecomputeQueue{}, model.TelemetryDataLoss{}, model.TelemetryAlert{},
				model.TrafficPolicy{}, model.TrafficPolicyState{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 1, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 1
	}
	if current < 2 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorReplicationReceipt{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 2, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 2
	}
	if current < 3 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.AvailabilityRecomputeQueue{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 3, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 3
	}
	if current < 4 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.TelemetryDataLoss{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 4, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 4
	}
	if current < 5 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.TelemetryAlert{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 5, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 5
	}
	if current < 6 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.TrafficPolicy{}, &model.TrafficPolicyState{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 6, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 6
	}
	if current < 7 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorRuntime{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 7, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 7
	}
	if current < 8 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorRuntime{}, &model.ConnectionLatencyBucket{}, &model.ConnectionLatencyCursor{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 8, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 8
	}
	if current < 9 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.Collector{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 9, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 9
	}
	if current < 10 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.Collector{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 10, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 10
	}
	if current < 11 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.Collector{}, &model.Server{}, &model.ProbeSampleBucket{}, &model.ProbeLatest{}, &model.ProbeTrace{}, &model.ProbeAlertState{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 11, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 11
	}
	if current < 12 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.CollectorRuntime{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 12, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 12
	}
	if current < 13 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.Server{}, &model.Collector{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 13, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 13
	}
	if current < 14 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.AvailabilityBucket{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 14, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 14
	}
	if current < 15 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.ProbeTrace{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 15, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 15
	}
	if current < 16 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.Collector{}, &model.ProbeRoute{}, &model.ProbeRouteJob{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 16, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
		current = 16
	}
	if current < 17 {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.Collector{}); err != nil {
				return err
			}
			return tx.Create(&model.SchemaMigration{Version: 17, AppliedAt: time.Now().UTC()}).Error
		})
	}
	return nil
}

// RecordTransferHourlyUsage 对流量记录进行打点
func RecordTransferHourlyUsage() {
	ServerLock.Lock()
	defer ServerLock.Unlock()
	now := time.Now()
	nowTrimSeconds := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	var txs []model.Transfer
	for id, server := range ServerList {
		tx := model.Transfer{
			ServerID: id,
			In:       utils.Uint64SubInt64(server.State.NetInTransfer, server.PrevTransferInSnapshot),
			Out:      utils.Uint64SubInt64(server.State.NetOutTransfer, server.PrevTransferOutSnapshot),
		}
		if tx.In == 0 && tx.Out == 0 {
			continue
		}
		server.PrevTransferInSnapshot = int64(server.State.NetInTransfer)   // #nosec G115 -- network transfer fits in int64
		server.PrevTransferOutSnapshot = int64(server.State.NetOutTransfer) // #nosec G115 -- network transfer fits in int64
		tx.CreatedAt = nowTrimSeconds
		txs = append(txs, tx)
	}
	if len(txs) == 0 {
		return
	}
	log.Println("SANTAIZI>> Cron 流量统计入库", len(txs), DB.Create(txs).Error)
}

// CleanMonitorHistory 清理无效或过时的 监控记录 和 流量记录
func CleanMonitorHistory() {
	// 清理已被删除的服务器的监控记录与流量记录
	DB.Unscoped().Delete(&model.MonitorHistory{}, "created_at < ? OR monitor_id NOT IN (SELECT `id` FROM monitors)", time.Now().AddDate(0, 0, -30))
	// 由于网络监控记录的数据较多，并且前端仅使用了 1 天的数据
	// 考虑到 sqlite 数据量问题，仅保留一天数据，
	// server_id = 0 的数据会用于/service页面的可用性展示
	DB.Unscoped().Delete(&model.MonitorHistory{}, "(created_at < ? AND server_id != 0) OR monitor_id NOT IN (SELECT `id` FROM monitors)", time.Now().AddDate(0, 0, -1))
	DB.Unscoped().Delete(&model.Transfer{}, "server_id NOT IN (SELECT `id` FROM servers)")
	// 新流量策略使用 State Rollup 中的安全计数器增量；旧 Transfer
	// 表仅作为当前版本运行期缓存，按 48 小时统一清理。
	DB.Unscoped().Delete(&model.Transfer{}, "created_at < ?", time.Now().Add(-48*time.Hour))
}

// IPDesensitize 根据设置选择是否对IP进行打码处理 返回处理后的IP(关闭打码则返回原IP)
func IPDesensitize(ip string) string {
	if Conf.EnablePlainIPInNotification {
		return ip
	}
	return utils.IPDesensitize(ip)
}
