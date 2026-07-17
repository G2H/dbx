package engine

import (
	"errors"
	"strings"
	"testing"
)

func TestFriendlyLockError(t *testing.T) {
	// The real error DuckDB produces when another process holds the file
	// read-write.
	raw := errors.New(`database/sql/driver: could not connect to database: IO Error: Could not set lock on file "/Users/gmini/dev/polishaccounting/data/warehouse.duckdb": Conflicting lock is held in /Users/gmini/Library/Caches/mef/runtimes/0.0.4/python/bin/python3.12 (PID 26894) by user gmini. See also https://duckdb.org/docs/stable/connect/concurrency`)

	err := friendlyLockError("data/warehouse.duckdb", raw)
	if err == nil {
		t.Fatal("lock conflict should be translated")
	}
	for _, want := range []string{
		"data/warehouse.duckdb is open read-write in python3.12 (PID 26894)",
		"one writer or many readers",
		"cp 'data/warehouse.duckdb' /tmp/snapshot.duckdb && dbx /tmp/snapshot.duckdb",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message missing %q:\n%s", want, err)
		}
	}

	// Without the holder detail, the generic phrasing is used.
	err = friendlyLockError("x.duckdb", errors.New("IO Error: Conflicting lock on file"))
	if err == nil || !strings.Contains(err.Error(), "open read-write in another process") {
		t.Fatalf("generic lock message expected, got: %v", err)
	}

	// Unrelated errors pass through untranslated.
	if err := friendlyLockError("x.duckdb", errors.New("out of memory")); err != nil {
		t.Fatalf("non-lock error should return nil, got: %v", err)
	}
}
