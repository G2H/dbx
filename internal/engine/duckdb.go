package engine

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
)

type duckdbEngine struct {
	db *sql.DB
}

func openDuckDB(path string) (Engine, error) {
	// go-duckdb opens the database eagerly inside sql.Open, so both it and
	// Ping can report connection failures (like lock conflicts).
	db, err := sql.Open("duckdb", path+"?access_mode=read_only")
	if err != nil {
		return nil, translateOpenError(path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, translateOpenError(path, err)
	}
	return &duckdbEngine{db: db}, nil
}

func translateOpenError(path string, err error) error {
	if lockErr := friendlyLockError(path, err); lockErr != nil {
		return lockErr
	}
	if corruptErr := friendlyCorruptError(path, err); corruptErr != nil {
		return corruptErr
	}
	return fmt.Errorf("open duckdb %s: %w", path, err)
}

// friendlyCorruptError translates DuckDB internal errors while loading the
// database (torn or truncated files, typically a copy taken while another
// process was writing). Returns nil when err is not that case.
func friendlyCorruptError(path string, err error) error {
	msg := err.Error()
	if !strings.Contains(msg, "INTERNAL Error") &&
		!strings.Contains(msg, "Corruption") &&
		!strings.Contains(msg, "not a valid DuckDB database file") {
		return nil
	}
	reason := oneLineHead(msg)
	return fmt.Errorf(`%s appears damaged: %s
  If this file is a copy of a live database, it was probably copied while another process was writing (a torn snapshot).
  Re-copy it from the original while the writer is idle, and copy the original's .wal file alongside if one exists.
  It may also have been written by a newer DuckDB than dbx bundles; if so, updating dbx fixes it.`,
		path, reason)
}

// oneLineHead keeps the first line of a multi-line engine error (DuckDB
// appends explanation paragraphs and a full stack trace).
func oneLineHead(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	// Drop the driver prefix; the interesting part starts at the error type.
	if i := strings.LastIndex(msg, "could not connect to database: "); i >= 0 {
		msg = msg[i+len("could not connect to database: "):]
	}
	return strings.TrimSpace(msg)
}

var lockHolderRe = regexp.MustCompile(`Conflicting lock is held in (.+?) \(PID (\d+)\)`)

// friendlyLockError translates DuckDB's "Conflicting lock" failure into a
// message that says what is going on and what to do about it. Returns nil
// when err is not a lock conflict.
func friendlyLockError(path string, err error) error {
	msg := err.Error()
	if !strings.Contains(msg, "Conflicting lock") {
		return nil
	}
	holder := "another process"
	if m := lockHolderRe.FindStringSubmatch(msg); m != nil {
		holder = fmt.Sprintf("%s (PID %s)", filepath.Base(m[1]), m[2])
	}
	return fmt.Errorf(`%s is open read-write in %s.
  DuckDB allows one writer or many readers, never both, so the file cannot be explored right now.
  Wait for that process to finish (or close it), or explore a snapshot:
    cp '%s' /tmp/snapshot.duckdb && dbx /tmp/snapshot.duckdb`, path, holder, path)
}

func (e *duckdbEngine) Kind() Kind   { return KindDuckDB }
func (e *duckdbEngine) DB() *sql.DB  { return e.db }
func (e *duckdbEngine) Close() error { return e.db.Close() }

func (e *duckdbEngine) QuoteIdent(name string) string { return QuoteIdent(name) }

func (e *duckdbEngine) QuoteTable(table Table) string {
	if table.Schema == "" {
		return QuoteIdent(table.Name)
	}
	return QuoteIdent(table.Schema) + "." + QuoteIdent(table.Name)
}

func (e *duckdbEngine) ListTables(ctx context.Context) ([]Table, error) {
	// All user schemas, not just main (warehouses often use named schemas).
	rows, err := e.db.QueryContext(ctx, `
		SELECT table_schema, table_name, table_type FROM information_schema.tables
		WHERE table_schema NOT IN ('information_schema', 'pg_catalog')
		ORDER BY table_schema, table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []Table
	for rows.Next() {
		var schema, name, typ string
		if err := rows.Scan(&schema, &name, &typ); err != nil {
			return nil, err
		}
		if schema == "main" {
			schema = ""
		}
		tables = append(tables, Table{Schema: schema, Name: name, IsView: typ == "VIEW"})
	}
	return tables, rows.Err()
}

func (e *duckdbEngine) DescribeTable(ctx context.Context, table Table) ([]ColumnInfo, error) {
	schema := table.Schema
	if schema == "" {
		schema = "main"
	}
	rows, err := e.db.QueryContext(ctx, `
		SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`, schema, table.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []ColumnInfo
	for rows.Next() {
		var (
			name, typ, nullable string
			dflt                sql.NullString
		)
		if err := rows.Scan(&name, &typ, &nullable, &dflt); err != nil {
			return nil, err
		}
		cols = append(cols, ColumnInfo{
			Name:     name,
			Type:     typ,
			Nullable: nullable == "YES",
			Default:  dflt.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// PK flags come from duckdb_constraints(); information_schema has no
	// simple per-column flag. A failure here degrades to no PK marks.
	pks, err := e.primaryKeyColumns(ctx, schema, table.Name)
	if err == nil {
		for i := range cols {
			if pks[cols[i].Name] {
				cols[i].PK = true
			}
		}
	}
	return cols, nil
}

func (e *duckdbEngine) primaryKeyColumns(ctx context.Context, schema, table string) (map[string]bool, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT unnest(constraint_column_names)
		FROM duckdb_constraints()
		WHERE schema_name = ? AND table_name = ? AND constraint_type = 'PRIMARY KEY'`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pks := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		pks[name] = true
	}
	return pks, rows.Err()
}
