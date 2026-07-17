package query

import (
	"context"
	"database/sql"
	"strconv"
)

// SchemaSample bounds how many rows the schema pane's common-values column
// scans, so browsing stays fast on huge tables. The value picker uses
// SampleNone (whole table): its list drives filters, so completeness
// matters more than latency there.
const SchemaSample = 10000

// SampleMode selects how value counting bounds its scan.
type SampleMode int

const (
	SampleNone      SampleMode = iota // scan the whole table
	SampleFirstRows                   // LIMIT n: engines without sampling (SQLite)
	SampleReservoir                   // USING SAMPLE n ROWS: DuckDB-backed engines
)

// ValueCount is one distinct value of a column and how often it occurs in
// the sampled rows.
type ValueCount struct {
	Value string
	Null  bool
	Count int64
}

// buildValueCounts renders the top-k most frequent values query for one
// column, bounding the scan per mode.
func buildValueCounts(quote func(string) string, tableSQL, col string, mode SampleMode, n, k int) string {
	c := quote(col)
	from := tableSQL
	switch mode {
	case SampleFirstRows:
		from = "(SELECT " + c + " FROM " + tableSQL + " LIMIT " + strconv.Itoa(n) + ")"
	case SampleReservoir:
		from = "(SELECT " + c + " FROM " + tableSQL + " USING SAMPLE " + strconv.Itoa(n) + " ROWS)"
	}
	return "SELECT CAST(" + c + " AS VARCHAR) AS v, COUNT(*) AS n" +
		" FROM " + from +
		" GROUP BY 1 ORDER BY n DESC, v LIMIT " + strconv.Itoa(k)
}

// ValueCounts returns the k most frequent values of a column, most
// frequent first, scanning per the sample mode (n rows for the bounded
// modes).
func ValueCounts(ctx context.Context, db *sql.DB, quote func(string) string, tableSQL, col string, mode SampleMode, n, k int) ([]ValueCount, error) {
	rows, err := db.QueryContext(ctx, buildValueCounts(quote, tableSQL, col, mode, n, k))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ValueCount
	for rows.Next() {
		var (
			v sql.NullString
			n int64
		)
		if err := rows.Scan(&v, &n); err != nil {
			return nil, err
		}
		out = append(out, ValueCount{Value: sanitize(v.String), Null: !v.Valid, Count: n})
	}
	return out, rows.Err()
}
