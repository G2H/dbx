package query

import (
	"context"
	"database/sql"
	"time"
)

// MaxRows is the global display row limit.
const MaxRows = 100

// Cell is one rendered value. Null cells render as a dim ∅ in the UI.
type Cell struct {
	Text string
	Null bool
}

// Result is a fully scanned, display-ready query result.
type Result struct {
	Columns   []string
	Rows      [][]Cell
	Truncated bool // true when more than MaxRows rows existed
	Elapsed   time.Duration
}

// Run executes q and scans at most MaxRows rows. The query runs as written
// (no rewriting); scanning just stops once the limit is reached, and
// Truncated reports whether at least one more row existed.
func Run(ctx context.Context, db *sql.DB, q string, args ...any) (*Result, error) {
	start := time.Now()
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res, err := scanLimited(rows, MaxRows)
	if err != nil {
		return nil, err
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

func scanLimited(rows *sql.Rows, limit int) (*Result, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	res := &Result{Columns: cols}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	for rows.Next() {
		if len(res.Rows) >= limit {
			res.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]Cell, len(cols))
		for i, v := range values {
			row[i] = renderCell(v)
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}
