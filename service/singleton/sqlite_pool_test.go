package singleton

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/hi2shark/santaizi-dashboard/service/sqliteutil"
)

func TestOpenDBFromPathBusyTimeoutOnEveryConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "santaizi.db")
	db, err := OpenDBFromPath(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := CloseDB(db); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if stats := sqlDB.Stats(); stats.MaxOpenConnections != sqliteutil.MaxOpenConns {
		t.Fatalf("max open connections = %d, want %d", stats.MaxOpenConnections, sqliteutil.MaxOpenConns)
	}
	ctx := context.Background()
	first, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = first.Close()
		_ = second.Close()
	})
	for i, conn := range []*sql.Conn{first, second} {
		var timeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != sqliteutil.BusyTimeoutMS {
			t.Fatalf("conn %d busy_timeout=%d, want %d", i, timeout, sqliteutil.BusyTimeoutMS)
		}
	}
}
