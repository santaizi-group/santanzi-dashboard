package sqliteutil

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func TestDSNSetsWaitOnEveryConnection(t *testing.T) {
	got := DSN("/var/lib/santaizi-dashboard/sqlite.db")
	for _, want := range []string{
		"_busy_timeout=5000",
		"_txlock=immediate",
		"_journal_mode=WAL",
		"_foreign_keys=on",
		"_synchronous=NORMAL",
		"_auto_vacuum=incremental",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DSN %q missing %s", got, want)
		}
	}
	joined := DSN("file:name?mode=memory&cache=shared")
	if !strings.Contains(joined, "&_busy_timeout=5000") {
		t.Fatalf("existing query was replaced: %s", joined)
	}
}

func TestBusy(t *testing.T) {
	if !Busy(sqlite3.Error{Code: sqlite3.ErrBusy}) {
		t.Fatal("ErrBusy")
	}
	if !Busy(sqlite3.Error{Code: sqlite3.ErrLocked}) {
		t.Fatal("ErrLocked")
	}
	if !Busy(fmt.Errorf("commit: %w", sqlite3.Error{Code: sqlite3.ErrBusy})) {
		t.Fatal("wrapped ErrBusy")
	}
	if !Busy(errors.New("database is locked")) {
		t.Fatal("message")
	}
	if Busy(errors.New("constraint failed")) {
		t.Fatal("unrelated error")
	}
	if Busy(nil) {
		t.Fatal("nil")
	}
}
