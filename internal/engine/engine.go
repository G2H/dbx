// Package engine abstracts the two supported database engines (SQLite and
// DuckDB) behind a small interface used by the UI.
package engine

import (
	"context"
	"database/sql"
	"strings"
)

// Kind identifies which engine backs a file.
type Kind string

const (
	KindSQLite  Kind = "sqlite"
	KindDuckDB  Kind = "duckdb"
	KindParquet Kind = "parquet"
	KindCSV     Kind = "csv"
	KindJSON    Kind = "json"
	KindDir     Kind = "directory"
)

// Table is one table or view in the database. Schema is empty for engines
// without schemas (SQLite) and for DuckDB's default schema.
type Table struct {
	Schema string
	Name   string
	IsView bool
}

// Display is the human-facing name: `name`, or `schema.name` for tables
// outside the default schema.
func (t Table) Display() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// ColumnInfo describes one column of a table. Fields an engine cannot
// provide are left blank.
type ColumnInfo struct {
	Name     string
	Type     string
	Nullable bool
	PK       bool
	Default  string // empty when no default
}

// Engine is the minimal surface both databases implement.
type Engine interface {
	ListTables(ctx context.Context) ([]Table, error)
	DescribeTable(ctx context.Context, table Table) ([]ColumnInfo, error)
	QuoteIdent(name string) string
	// QuoteTable renders a table reference for SQL, schema-qualified when
	// the table lives outside the default schema.
	QuoteTable(table Table) string
	DB() *sql.DB
	Kind() Kind
	Close() error
}

// QuoteIdent quotes an identifier double-quote style, doubling embedded
// quotes. Both engines accept this form.
func QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Open detects the engine for path and opens a read-only connection.
func Open(path string) (Engine, error) {
	kind, err := Detect(path)
	if err != nil {
		return nil, err
	}
	switch kind {
	case KindDuckDB:
		return openDuckDB(path)
	case KindParquet, KindCSV, KindJSON:
		return openFileView(path, kind)
	case KindDir:
		return openDir(path)
	default:
		return openSQLite(path)
	}
}
