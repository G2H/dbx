package engine

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"dbx/internal/query"
)

// seedSQL is engine-neutral DDL/DML used for both engines.
var seedSQL = []string{
	`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT, age INTEGER)`,
	`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, total DOUBLE)`,
	`CREATE VIEW adults AS SELECT * FROM users WHERE age >= 18`,
	`INSERT INTO users VALUES (1, 'Ada Lovelace', 'ada@example.com', 36)`,
	`INSERT INTO users VALUES (2, 'Alan Turing', NULL, 41)`,
	`INSERT INTO users VALUES (3, 'Grace Hopper', 'grace@example.com', 17)`,
	`INSERT INTO orders VALUES (1, 1, 19.99)`,
}

func seedDatabase(t *testing.T, driver, dsn string) {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range seedSQL {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
}

func newSQLiteFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	seedDatabase(t, "sqlite", "file:"+path)
	return path
}

func newDuckDBFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.duckdb")
	seedDatabase(t, "duckdb", path)
	return path
}

func TestEngines(t *testing.T) {
	fixtures := []struct {
		kind Kind
		path func(*testing.T) string
	}{
		{KindSQLite, newSQLiteFixture},
		{KindDuckDB, newDuckDBFixture},
	}
	for _, fx := range fixtures {
		t.Run(string(fx.kind), func(t *testing.T) {
			eng, err := Open(fx.path(t))
			if err != nil {
				t.Fatal(err)
			}
			defer eng.Close()
			if eng.Kind() != fx.kind {
				t.Fatalf("kind = %q, want %q", eng.Kind(), fx.kind)
			}
			ctx := context.Background()

			tables, err := eng.ListTables(ctx)
			if err != nil {
				t.Fatal(err)
			}
			byName := map[string]Table{}
			for _, tb := range tables {
				byName[tb.Name] = tb
			}
			if len(tables) != 3 {
				t.Fatalf("got %d tables, want 3: %+v", len(tables), tables)
			}
			if byName["users"].IsView || byName["orders"].IsView {
				t.Fatal("users/orders should not be views")
			}
			if !byName["adults"].IsView {
				t.Fatal("adults should be a view")
			}

			cols, err := eng.DescribeTable(ctx, byName["users"])
			if err != nil {
				t.Fatal(err)
			}
			if len(cols) != 4 {
				t.Fatalf("got %d columns, want 4: %+v", len(cols), cols)
			}
			if cols[0].Name != "id" || !cols[0].PK {
				t.Fatalf("id should be first and PK: %+v", cols[0])
			}
			if cols[1].Name != "name" || cols[1].Nullable {
				t.Fatalf("name should be NOT NULL: %+v", cols[1])
			}
			if !cols[2].Nullable {
				t.Fatalf("email should be nullable: %+v", cols[2])
			}

			// Row fetch through the query package, with a filter.
			q, args := query.BuildSelect(eng.QuoteIdent, eng.QuoteTable(byName["users"]), nil,
				[]query.Filter{query.ParseFilter("age", "> 18")}, nil, query.MaxRows)
			res, err := query.Run(ctx, eng.DB(), q, args...)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Rows) != 2 {
				t.Fatalf("got %d rows, want 2 (ada, alan): %+v", len(res.Rows), res.Rows)
			}
			if res.Truncated {
				t.Fatal("3-row table must not report truncation")
			}

			// NULL rendering.
			res, err = query.Run(ctx, eng.DB(), `SELECT email FROM users WHERE name LIKE 'Alan%'`)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Rows) != 1 || !res.Rows[0][0].Null {
				t.Fatalf("alan's email should scan as NULL: %+v", res.Rows)
			}

			// Read-only: writes must fail.
			if _, err := eng.DB().Exec(`INSERT INTO orders VALUES (2, 2, 5.0)`); err == nil {
				t.Fatal("write succeeded on a read-only connection")
			}
		})
	}
}

func TestDuckDBNamedSchemas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wh.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE SCHEMA fin`,
		`CREATE TABLE fin.statement (id INTEGER PRIMARY KEY, amount DOUBLE)`,
		`INSERT INTO fin.statement VALUES (1, 12.5), (2, 99.0)`,
		`CREATE TABLE main_table (id INTEGER)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	eng, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	ctx := context.Background()

	tables, err := eng.ListTables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byDisplay := map[string]Table{}
	for _, tb := range tables {
		byDisplay[tb.Display()] = tb
	}
	if len(tables) != 2 {
		t.Fatalf("got %d tables, want 2: %+v", len(tables), tables)
	}
	st, ok := byDisplay["fin.statement"]
	if !ok {
		t.Fatalf("fin.statement not listed: %+v", tables)
	}
	if _, ok := byDisplay["main_table"]; !ok {
		t.Fatalf("main-schema table should display without a prefix: %+v", tables)
	}

	cols, err := eng.DescribeTable(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 || cols[0].Name != "id" || !cols[0].PK {
		t.Fatalf("unexpected columns for fin.statement: %+v", cols)
	}

	q, args := query.BuildSelect(eng.QuoteIdent, eng.QuoteTable(st), nil, nil, nil, query.MaxRows)
	res, err := query.Run(ctx, eng.DB(), q, args...)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(res.Rows))
	}
}

func TestParquetEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daily sales.parquet")
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`COPY (
		SELECT i AS id, 'item ' || i AS item, CASE WHEN i % 3 = 0 THEN NULL ELSE i * 1.5 END AS price
		FROM generate_series(1, 120) t(i)
	) TO '` + strings.ReplaceAll(path, "'", "''") + `' (FORMAT PARQUET)`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	kind, err := Detect(path)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindParquet {
		t.Fatalf("got %q, want parquet", kind)
	}

	eng, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if eng.Kind() != KindParquet {
		t.Fatalf("kind = %q", eng.Kind())
	}
	ctx := context.Background()

	tables, err := eng.ListTables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].Name != "daily sales" {
		t.Fatalf("expected one view named after the file, got %+v", tables)
	}

	cols, err := eng.DescribeTable(ctx, tables[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 || cols[0].Name != "id" || cols[1].Name != "item" {
		t.Fatalf("unexpected columns: %+v", cols)
	}

	res, err := query.Run(ctx, eng.DB(), `SELECT * FROM "daily sales" ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != query.MaxRows || !res.Truncated {
		t.Fatalf("120-row parquet should truncate at %d, got %d (truncated=%v)",
			query.MaxRows, len(res.Rows), res.Truncated)
	}
	if !res.Rows[2][2].Null {
		t.Fatalf("price of id 3 should be NULL: %+v", res.Rows[2])
	}
}

func TestRunTruncatesAt100(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE nums (n INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO nums
		WITH RECURSIVE cnt(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM cnt WHERE x < 250)
		SELECT x FROM cnt`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	eng, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	res, err := query.Run(context.Background(), eng.DB(), `SELECT n FROM nums`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != query.MaxRows {
		t.Fatalf("got %d rows, want %d", len(res.Rows), query.MaxRows)
	}
	if !res.Truncated {
		t.Fatal("250-row scan must report truncation")
	}

	// A query already under the limit is not truncated.
	res, err = query.Run(context.Background(), eng.DB(), `SELECT n FROM nums LIMIT 5`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 5 || res.Truncated {
		t.Fatalf("LIMIT 5 should give 5 rows untruncated, got %d (truncated=%v)", len(res.Rows), res.Truncated)
	}
}
