package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOptimizeSkipsVacuumBelowThreshold(t *testing.T) {
	db := newRetentionDB(t)
	maintainer := NewDatabaseMaintainer(db, "", func() RetentionPolicy {
		return RetentionPolicy{CompactMinBytes: 1 << 30, MaxRuntime: time.Minute, BatchSize: 100}
	})
	result := maintainer.RunOnce(context.Background(), true)
	if result.Compacted {
		t.Fatalf("compacted below threshold: %#v", result)
	}
}

func TestOptimizeCompactsWhenReclaimable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "santaizi.db")
	db, err := gorm.Open(sqlite.Open(path+"?_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_, _ = sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.TelemetryObservation{}); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 32768)
	old := time.Now().Add(-40 * 24 * time.Hour).UnixNano()
	rows := make([]model.TelemetryObservation, 80)
	for i := range rows {
		id := make([]byte, 16)
		id[0] = byte(i)
		rows[i] = model.TelemetryObservation{
			EventID: id, ObserverID: "primary", NodeUUID: make([]byte, 16), ReceivedAt: old, Metadata: payload,
		}
	}
	if err := db.CreateInBatches(rows, 80).Error; err != nil {
		t.Fatal(err)
	}
	before, err := pageCount(db)
	if err != nil {
		t.Fatal(err)
	}
	maintainer := NewDatabaseMaintainer(db, path, func() RetentionPolicy {
		return RetentionPolicy{CompactMinBytes: 1, MaxRuntime: time.Minute, BatchSize: 200}
	})
	result := maintainer.RunOnce(context.Background(), true)
	if result.Error != "" {
		t.Fatalf("optimize: %#v", result)
	}
	if !result.Compacted {
		reclaimable, _ := reclaimableBytes(db)
		var size int64
		if info, statErr := os.Stat(path); statErr == nil {
			size = info.Size()
		}
		t.Fatalf("expected compact: %#v reclaimable=%d file=%d", result, reclaimable, size)
	}
	after, err := pageCount(db)
	if err != nil {
		t.Fatal(err)
	}
	if after >= before {
		t.Fatalf("page_count before=%d after=%d", before, after)
	}
	if info, statErr := os.Stat(path + "-wal"); statErr == nil && info.Size() > 0 {
		t.Fatalf("wal not truncated after compact: %d", info.Size())
	}
}

func TestStartOptimizeRejectsConcurrentRun(t *testing.T) {
	db := newRetentionDB(t)
	maintainer := NewDatabaseMaintainer(db, "", nil)
	maintainer.mu.Lock()
	maintainer.running = true
	maintainer.mu.Unlock()
	if maintainer.Start(true) {
		t.Fatal("second start should fail")
	}
	status := maintainer.Status()
	if !status.Running {
		t.Fatal("status should report running")
	}
}

func TestPeriodicOptimizeCompactsLegacyWhenReclaimable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "santaizi.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_, _ = sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.TelemetryObservation{}); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 32768)
	old := time.Now().Add(-40 * 24 * time.Hour).UnixNano()
	rows := make([]model.TelemetryObservation, 80)
	for i := range rows {
		id := make([]byte, 16)
		id[0] = byte(i)
		rows[i] = model.TelemetryObservation{
			EventID: id, ObserverID: "primary", NodeUUID: make([]byte, 16), ReceivedAt: old, Metadata: payload,
		}
	}
	if err := db.CreateInBatches(rows, 80).Error; err != nil {
		t.Fatal(err)
	}
	maintainer := NewDatabaseMaintainer(db, path, func() RetentionPolicy {
		return RetentionPolicy{CompactMinBytes: 1, MaxRuntime: time.Minute, BatchSize: 200, AutoCompact: true}
	})
	result := maintainer.RunOnce(context.Background(), false)
	if result.Error != "" {
		t.Fatalf("optimize: %#v", result)
	}
	if !result.Compacted {
		reclaimable, _ := reclaimableBytes(db)
		t.Fatalf("expected auto compact on NONE vacuum: %#v reclaimable=%d", result, reclaimable)
	}
}

func TestDataLossTableNameMatchesDrain(t *testing.T) {
	db := newRetentionDB(t)
	if !sqliteTableExists(db, "telemetry_data_losses") {
		t.Fatal("expected telemetry_data_losses")
	}
	if !sqliteTableExists(db, "collector_replication_receipts") {
		t.Fatal("expected collector_replication_receipts")
	}
}

func TestProbeSnapshotTableNamesMatchDrain(t *testing.T) {
	db := newRetentionDB(t)
	if err := db.AutoMigrate(&model.ProbeLatest{}, &model.ProbeTrace{}); err != nil {
		t.Fatal(err)
	}
	if !sqliteTableExists(db, "probe_latests") {
		t.Fatal("expected probe_latests")
	}
	if !sqliteTableExists(db, "probe_traces") {
		t.Fatal("expected probe_traces")
	}
}

func TestMaintenanceTruncatesOversizedWAL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "santaizi.db")
	db, err := gorm.Open(sqlite.Open(path+"?_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_, _ = sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.TelemetryObservation{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelemetryObservation{
		EventID: make([]byte, 16), ObserverID: "primary", NodeUUID: make([]byte, 16), ReceivedAt: time.Now().UnixNano(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, walBytes := databaseFileSizes(path); walBytes == 0 {
		t.Fatal("expected non-empty WAL before maintenance")
	}
	maintainer := NewDatabaseMaintainer(db, path, nil)
	maintainer.walTruncateBytes = 1
	maintainer.truncateOversizedWAL()
	if _, walBytes := databaseFileSizes(path); walBytes != 0 {
		t.Fatalf("walBytes=%d", walBytes)
	}
}
