package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	_ "modernc.org/sqlite"
)

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// awkward column names that have broken SQL builders before: reserved
// words, the value-counts aliases, spaces, quotes.
var awkwardCols = []string{"rank", "order", "group", "select", "v", "n", "count", "with space", `qu"ote`}

func stressDB(t *testing.T, driver, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := "CREATE TABLE t ("
	for i, c := range awkwardCols {
		if i > 0 {
			ddl += ", "
		}
		ddl += quoteIdent(c) + " INTEGER"
	}
	ddl += ")"
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		vals := ""
		for j := range awkwardCols {
			if j > 0 {
				vals += ", "
			}
			vals += "1"
		}
		if _, err := db.Exec("INSERT INTO t VALUES (" + vals + ")"); err != nil {
			t.Fatal(err)
		}
		_ = i
	}
	return db
}

// A value picked from a mixed-content text column must filter without a
// conversion error (DuckDB used to try casting the whole column to INT64
// when the picked value looked numeric).
func TestInFilterOnMixedContentColumn(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (rank VARCHAR)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES ('42'), ('abc'), ('7th'), ('42')`); err != nil {
		t.Fatal(err)
	}
	q, args := BuildSelect(quoteIdent, `"t"`, nil,
		[]Filter{{Column: "rank", Op: OpIn, Values: []string{"42"}}}, nil, MaxRows)
	res, err := Run(context.Background(), db, q, args...)
	if err != nil {
		t.Fatalf("mixed-content IN filter failed: %v\n  sql: %s", err, q)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(res.Rows))
	}
}

// The value picker (sample 0) must see values that only occur beyond the
// first 10k rows.
func TestValueCountsSeesRareLateValues(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// 17k rows: the first 16.5k false, the last 500 true.
	if _, err := db.Exec(`CREATE TABLE flags AS
		SELECT i > 16500 AS active FROM generate_series(1, 17000) t(i)`); err != nil {
		t.Fatal(err)
	}
	values, err := ValueCounts(context.Background(), db, quoteIdent, `"flags"`, "active", SampleNone, 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("full scan should see both values, got %+v", values)
	}
	byVal := map[string]int64{}
	for _, v := range values {
		byVal[v.Value] = v.Count
	}
	if byVal["true"] != 500 || byVal["false"] != 16500 {
		t.Fatalf("unexpected counts: %+v", values)
	}

	// The reservoir sample is fair: 10k of 17k rows misses the 500 trues
	// with probability ~1e-200, so both values must appear.
	sampled, err := ValueCounts(context.Background(), db, quoteIdent, `"flags"`, "active", SampleReservoir, SchemaSample, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(sampled) != 2 {
		t.Fatalf("reservoir sample should see both values, got %+v", sampled)
	}
}

func TestBuildersWithAwkwardColumnNames(t *testing.T) {
	engines := []struct {
		name string
		db   func(t *testing.T) *sql.DB
	}{
		{"sqlite", func(t *testing.T) *sql.DB {
			return stressDB(t, "sqlite", "file:"+filepath.Join(t.TempDir(), "s.db"))
		}},
		{"duckdb", func(t *testing.T) *sql.DB {
			return stressDB(t, "duckdb", "")
		}},
	}
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			db := e.db(t)
			ctx := context.Background()
			for _, col := range awkwardCols {
				// Sort ascending and descending.
				for _, desc := range []bool{false, true} {
					q, args := BuildSelect(quoteIdent, `"t"`, nil, nil, &Order{Column: col, Desc: desc}, MaxRows)
					if _, err := Run(ctx, db, q, args...); err != nil {
						t.Errorf("sort %q desc=%v: %v\n  sql: %s", col, desc, err, q)
					}
				}
				// Contains filter plus sort together.
				q, args := BuildSelect(quoteIdent, `"t"`, nil,
					[]Filter{{Column: col, Op: OpContains, Value: "1"}}, &Order{Column: col}, MaxRows)
				if _, err := Run(ctx, db, q, args...); err != nil {
					t.Errorf("filter+sort %q: %v\n  sql: %s", col, err, q)
				}
				// IN filter.
				q, args = BuildSelect(quoteIdent, `"t"`, nil,
					[]Filter{{Column: col, Op: OpIn, Values: []string{"1", "2"}, IncludeNull: true}}, nil, MaxRows)
				if _, err := Run(ctx, db, q, args...); err != nil {
					t.Errorf("in filter %q: %v\n  sql: %s", col, err, q)
				}
				// Value counts (the f picker and the schema common column).
				if _, err := ValueCounts(ctx, db, quoteIdent, `"t"`, col, SampleNone, 0, 30); err != nil {
					t.Errorf("value counts %q: %v", col, err)
				}
			}
		})
	}
}
