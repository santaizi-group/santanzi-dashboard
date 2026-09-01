package telemetry

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	maintenanceInterval       = 5 * time.Minute
	incrementalPages          = 4096
	incrementalMaxRounds      = 32
	vacuumBusyTimeoutMS       = 60000
	defaultBusyTimeout        = 5000
	compactCooldown           = 6 * time.Hour
	autoCompactHugeFactor     = int64(8)
	walTruncateThresholdBytes = int64(256 << 20)
)

type DatabaseStatus struct {
	FileBytes        int64                `json:"file_bytes"`
	WALBytes         int64                `json:"wal_bytes"`
	ReclaimableBytes int64                `json:"reclaimable_bytes"`
	Running          bool                 `json:"running"`
	LastRun          *DatabaseOptimizeRun `json:"last_run,omitempty"`
}

type DatabaseOptimizeRun struct {
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Deleted   int64  `json:"deleted"`
	Compacted bool   `json:"compacted"`
	Skipped   string `json:"skipped,omitempty"`
	Error     string `json:"error,omitempty"`
}

type DatabaseMaintainer struct {
	db       *gorm.DB
	path     string
	policyFn func() RetentionPolicy
	rollup   *RollupWorker

	walTruncateBytes int64

	mu            sync.Mutex
	running       bool
	last          *DatabaseOptimizeRun
	lastCompactAt time.Time
}

func NewDatabaseMaintainer(db *gorm.DB, path string, policyFn func() RetentionPolicy) *DatabaseMaintainer {
	if policyFn == nil {
		policyFn = func() RetentionPolicy { return NormalizeRetentionPolicy(RetentionPolicy{}) }
	}
	return &DatabaseMaintainer{
		db: db, path: path, policyFn: policyFn,
		rollup:           NewRollupWorker(db, RetentionPolicy{}),
		walTruncateBytes: walTruncateThresholdBytes,
	}
}

func (m *DatabaseMaintainer) Run(ctx context.Context) {
	_ = m.Start(false)
	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.Start(false)
		}
	}
}

func (m *DatabaseMaintainer) Start(compact bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return false
	}
	m.running = true
	go m.execute(compact)
	return true
}

func (m *DatabaseMaintainer) RunOnce(ctx context.Context, compact bool) DatabaseOptimizeRun {
	return m.run(ctx, compact)
}

func (m *DatabaseMaintainer) Status() DatabaseStatus {
	fileBytes, walBytes := databaseFileSizes(m.path)
	reclaimable, _ := reclaimableBytes(m.db)
	m.mu.Lock()
	defer m.mu.Unlock()
	status := DatabaseStatus{
		FileBytes: fileBytes, WALBytes: walBytes, ReclaimableBytes: reclaimable,
		Running: m.running,
	}
	if m.last != nil {
		copyRun := *m.last
		status.LastRun = &copyRun
	}
	return status
}

func (m *DatabaseMaintainer) execute(compact bool) {
	result := m.run(context.Background(), compact)
	m.mu.Lock()
	m.last = &result
	m.running = false
	m.mu.Unlock()
}

func (m *DatabaseMaintainer) run(ctx context.Context, compact bool) DatabaseOptimizeRun {
	started := time.Now().UTC()
	result := DatabaseOptimizeRun{StartedAt: started.Format(time.RFC3339)}
	policy := m.policyFn()
	if m.rollup != nil && sqliteTableExists(m.db, "telemetry_events") {
		m.rollup.retention = NormalizeRetentionPolicy(policy)
		_ = m.rollup.RollupPending(ctx, started)
	}
	deleted, err := DrainRetention(ctx, m.db, policy, started)
	result.Deleted = deleted
	if err != nil && !errors.Is(err, context.Canceled) {
		result.Error = err.Error()
		result.EndedAt = time.Now().UTC().Format(time.RFC3339)
		return result
	}
	reclaimable, _ := reclaimableBytes(m.db)
	mode, _ := autoVacuumMode(m.db)
	wantCompact := compact
	if !wantCompact && policy.AutoCompact && reclaimable >= policy.CompactMinBytes {
		legacy := mode != 2
		huge := reclaimable >= policy.CompactMinBytes*autoCompactHugeFactor
		if (legacy || huge) && (m.lastCompactAt.IsZero() || time.Since(m.lastCompactAt) >= compactCooldown) {
			wantCompact = true
		}
	}
	if wantCompact && reclaimable >= policy.CompactMinBytes {
		compacted, skip, compactErr := m.compactIfPossible()
		result.Compacted = compacted
		result.Skipped = skip
		if compactErr != nil {
			result.Error = compactErr.Error()
		}
		if compacted {
			m.lastCompactAt = time.Now()
		}
	} else {
		_ = incrementalVacuumBudget(m.db, incrementalPages, incrementalMaxRounds)
	}
	m.truncateOversizedWAL()
	result.EndedAt = time.Now().UTC().Format(time.RFC3339)
	return result
}

// truncateOversizedWAL shrinks the WAL once it exceeds the threshold. The
// passive auto-checkpoint only recycles frames in place, so without an
// explicit TRUNCATE the file sits at its historical high-water mark forever.
func (m *DatabaseMaintainer) truncateOversizedWAL() {
	if m.path == "" || m.walTruncateBytes <= 0 {
		return
	}
	if _, walBytes := databaseFileSizes(m.path); walBytes < m.walTruncateBytes {
		return
	}
	sqlDB, err := m.db.DB()
	if err != nil {
		return
	}
	_, _ = sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
}

func (m *DatabaseMaintainer) compactIfPossible() (bool, string, error) {
	fileBytes, _ := databaseFileSizes(m.path)
	if m.path != "" {
		if free, ok := volumeFreeBytes(m.path); ok && fileBytes > 0 && free < uint64(fileBytes) {
			return false, "insufficient_disk", nil
		}
	}
	if err := compactDatabase(m.db); err != nil {
		return false, "", err
	}
	return true, "", nil
}

func databaseFileSizes(path string) (fileBytes, walBytes int64) {
	if path == "" {
		return 0, 0
	}
	if info, err := os.Stat(path); err == nil {
		fileBytes = info.Size()
	}
	if info, err := os.Stat(path + "-wal"); err == nil {
		walBytes = info.Size()
	}
	return fileBytes, walBytes
}

func reclaimableBytes(db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, errors.New("database is not open")
	}
	var pageSize, freelist int64
	if err := db.Raw("PRAGMA page_size").Scan(&pageSize).Error; err != nil {
		return 0, err
	}
	if err := db.Raw("PRAGMA freelist_count").Scan(&freelist).Error; err != nil {
		return 0, err
	}
	return pageSize * freelist, nil
}

func autoVacuumMode(db *gorm.DB) (int, error) {
	var mode int
	if err := db.Raw("PRAGMA auto_vacuum").Scan(&mode).Error; err != nil {
		return 0, err
	}
	return mode, nil
}

func incrementalVacuum(db *gorm.DB, pages int) error {
	mode, err := autoVacuumMode(db)
	if err != nil || mode != 2 {
		return err
	}
	if pages <= 0 {
		return db.Exec("PRAGMA incremental_vacuum").Error
	}
	return db.Exec("PRAGMA incremental_vacuum(" + strconv.Itoa(pages) + ")").Error
}

func incrementalVacuumBudget(db *gorm.DB, pages, rounds int) error {
	if rounds <= 0 {
		return incrementalVacuum(db, pages)
	}
	for i := 0; i < rounds; i++ {
		reclaimable, err := reclaimableBytes(db)
		if err != nil {
			return err
		}
		if reclaimable == 0 {
			return nil
		}
		if err := incrementalVacuum(db, pages); err != nil {
			return err
		}
	}
	return nil
}

func compactDatabase(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if _, err := sqlDB.Exec("PRAGMA busy_timeout=" + strconv.Itoa(vacuumBusyTimeoutMS)); err != nil {
		return err
	}
	defer func() { _, _ = sqlDB.Exec("PRAGMA busy_timeout=" + strconv.Itoa(defaultBusyTimeout)) }()
	if _, err := sqlDB.Exec("PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
		return err
	}
	if _, err := sqlDB.Exec("VACUUM"); err != nil {
		return err
	}
	// VACUUM flows the whole database through the WAL; truncate right away so
	// the -wal file does not sit at roughly the database size until shutdown.
	_, _ = sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return nil
}

func pageCount(db *gorm.DB) (int64, error) {
	var count int64
	if err := db.Raw("PRAGMA page_count").Scan(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
