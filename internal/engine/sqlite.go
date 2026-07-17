package engine

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

type sqliteEngine struct {
	db *sql.DB
}

func openSQLite(path string) (Engine, error) {
	dsn := "file:" + url.PathEscape(path) + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	return &sqliteEngine{db: db}, nil
}

func (e *sqliteEngine) Kind() Kind   { return KindSQLite }
func (e *sqliteEngine) DB() *sql.DB  { return e.db }
func (e *sqliteEngine) Close() error { return e.db.Close() }

func (e *sqliteEngine) QuoteIdent(name string) string { return QuoteIdent(name) }
func (e *sqliteEngine) QuoteTable(table Table) string { return QuoteIdent(table.Name) }

func (e *sqliteEngine) ListTables(ctx context.Context) ([]Table, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT name, type FROM sqlite_master
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []Table
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, err
		}
		tables = append(tables, Table{Name: name, IsView: typ == "view"})
	}
	return tables, rows.Err()
}

func (e *sqliteEngine) DescribeTable(ctx context.Context, table Table) ([]ColumnInfo, error) {
	rows, err := e.db.QueryContext(ctx,
		`SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info(?)`, table.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []ColumnInfo
	for rows.Next() {
		var (
			name, typ string
			notNull   int
			dflt      sql.NullString
			pk        int
		)
		if err := rows.Scan(&name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, ColumnInfo{
			Name:     name,
			Type:     typ,
			Nullable: notNull == 0,
			PK:       pk > 0,
			Default:  dflt.String,
		})
	}
	return cols, rows.Err()
}
