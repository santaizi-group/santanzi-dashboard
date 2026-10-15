// Package sqliteutil opens the panel and collector SQLite pools.
// Busy timeout and the transaction lock mode have to be set on every
// connection: database/sql does not copy PRAGMA from the first one.
package sqliteutil

import (
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	MaxOpenConns  = 8
	MaxIdleConns  = 4
	BusyTimeoutMS = 5000
)

// DSN makes each pooled connection wait for the writer and start transactions
// with BEGIN IMMEDIATE. A deferred transaction can return SQLITE_BUSY at once
// when two connections both try to upgrade a read lock.
func DSN(path string) string {
	if path == "" {
		return ""
	}
	joiner := "?"
	if strings.Contains(path, "?") {
		joiner = "&"
	}
	// auto_vacuum is applied by the driver before journal_mode. A later PRAGMA cannot
	// turn it on once the file header has been written.
	return path + joiner + "_busy_timeout=5000&_txlock=immediate&_journal_mode=WAL&_foreign_keys=on&_synchronous=NORMAL&_auto_vacuum=incremental"
}

// Open opens path and caps the pool. Callers still run migrations.
func Open(path string, debug bool) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(DSN(path)), gormConfig(debug))
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(MaxOpenConns)
	sqlDB.SetMaxIdleConns(MaxIdleConns)
	return db, nil
}

func gormConfig(debug bool) *gorm.Config {
	level := logger.Warn
	if debug {
		level = logger.Info
	}
	return &gorm.Config{
		CreateBatchSize: 200,
		Logger: logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  level,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
	}
}

// Busy reports a SQLite write-lock conflict. The driver waits BusyTimeoutMS
// before returning this; callers should leave the work queued.
func Busy(err error) bool {
	if err == nil {
		return false
	}
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && busyCode(sqliteErr.Code) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
}

func busyCode(code sqlite3.ErrNo) bool {
	base := sqlite3.ErrNo(int(code) & 0xff)
	return base == sqlite3.ErrBusy || base == sqlite3.ErrLocked
}
