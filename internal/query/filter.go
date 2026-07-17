// Package query builds filtered SELECT statements and scans their results
// with the global 100-row display limit.
package query

import (
	"strconv"
	"strings"
)

// Op is a filter operator.
type Op string

const (
	OpContains Op = "~" // case-insensitive substring match (LIKE %v%)
	OpEq       Op = "="
	OpNe       Op = "!="
	OpGt       Op = ">"
	OpGe       Op = ">="
	OpLt       Op = "<"
	OpLe       Op = "<="
	OpNull     Op = "null"
	OpNotNull  Op = "not null"
	OpIn       Op = "in"    // value-set membership (from the value picker)
	OpRaw      Op = "where" // raw SQL fragment, not tied to a column
)

// Filter is one filter. Filters combine with AND.
type Filter struct {
	Column      string
	Op          Op
	Value       string   // value text (or the raw SQL for OpRaw)
	Values      []string // selected values for OpIn
	IncludeNull bool     // OpIn: NULL is one of the selected values
}

// ParseFilter interprets user input for a column filter.
// A leading operator (=, !=, >, >=, <, <=) selects a comparison; the bare
// words "null" / "not null" test for NULL; anything else is a
// case-insensitive substring match.
func ParseFilter(column, input string) Filter {
	trimmed := strings.TrimSpace(input)
	switch strings.ToLower(trimmed) {
	case "null":
		return Filter{Column: column, Op: OpNull}
	case "not null":
		return Filter{Column: column, Op: OpNotNull}
	}
	for _, op := range []Op{OpGe, OpLe, OpNe, OpEq, OpGt, OpLt} { // two-char ops first
		if strings.HasPrefix(trimmed, string(op)) {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, string(op)))
			return Filter{Column: column, Op: op, Value: value}
		}
	}
	return Filter{Column: column, Op: OpContains, Value: trimmed}
}

// Describe renders a filter for status lines, e.g. `name ~ ada`, `age > 30`,
// `email null`, `tag in (a, b, ∅)`, `where age % 2 = 0`.
func (f Filter) Describe() string {
	switch f.Op {
	case OpNull, OpNotNull:
		return f.Column + " " + string(f.Op)
	case OpIn:
		vals := append([]string(nil), f.Values...)
		if f.IncludeNull {
			vals = append(vals, "∅")
		}
		return f.Column + " in (" + strings.Join(vals, ", ") + ")"
	case OpRaw:
		return "where " + f.Value
	default:
		return f.Column + " " + string(f.Op) + " " + f.Value
	}
}

// BuildWhere renders filters as a WHERE clause (without the WHERE keyword)
// plus bound arguments. quote is the engine's identifier quoter. Returns
// ("", nil) when there are no filters.
func BuildWhere(quote func(string) string, filters []Filter) (string, []any) {
	if len(filters) == 0 {
		return "", nil
	}
	var (
		parts []string
		args  []any
	)
	for _, f := range filters {
		col := quote(f.Column)
		switch f.Op {
		case OpNull:
			parts = append(parts, col+" IS NULL")
		case OpNotNull:
			parts = append(parts, col+" IS NOT NULL")
		case OpIn:
			var terms []string
			if len(f.Values) > 0 {
				// Compare the column's VARCHAR rendering against the picked
				// strings: they came from the same CAST in the value picker,
				// and binding by guessed type breaks on mixed-content
				// columns (DuckDB tries to cast the whole column).
				marks := strings.TrimSuffix(strings.Repeat("?, ", len(f.Values)), ", ")
				terms = append(terms, "CAST("+col+" AS VARCHAR) IN ("+marks+")")
				for _, v := range f.Values {
					args = append(args, v)
				}
			}
			if f.IncludeNull {
				terms = append(terms, col+" IS NULL")
			}
			switch len(terms) {
			case 0: // nothing selected; matches nothing rather than everything
				parts = append(parts, "1 = 0")
			case 1:
				parts = append(parts, terms[0])
			default:
				parts = append(parts, "("+strings.Join(terms, " OR ")+")")
			}
		case OpRaw:
			// User-authored SQL fragment, run as written (the connection is
			// read-only, same trust level as the SQL view).
			parts = append(parts, "("+f.Value+")")
		case OpContains:
			// LOWER on both sides keeps it case-insensitive on SQLite
			// (non-ASCII) and DuckDB (which has case-sensitive LIKE).
			parts = append(parts, "LOWER(CAST("+col+" AS VARCHAR)) LIKE LOWER(?)")
			args = append(args, "%"+f.Value+"%")
		default:
			parts = append(parts, col+" "+string(f.Op)+" ?")
			args = append(args, typedValue(f.Value))
		}
	}
	return strings.Join(parts, " AND "), args
}

// typedValue binds numeric-looking comparison values as numbers so that
// numeric columns compare numerically (DuckDB in particular rejects or
// miscompares string params against numeric columns).
func typedValue(v string) any {
	if i, err := strconv.ParseInt(v, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}
	return v
}

// Order is an ORDER BY on one column.
type Order struct {
	Column string
	Desc   bool
}

// Describe renders the order for status lines, e.g. `order age ↓`.
func (o Order) Describe() string {
	arrow := "↑"
	if o.Desc {
		arrow = "↓"
	}
	return "order " + o.Column + " " + arrow
}

// BuildSelect renders the SELECT for a table with only the visible columns
// (all columns when cols is empty), the given filters and optional order,
// limited to limit rows. tableSQL is the already-quoted table reference
// (Engine.QuoteTable).
func BuildSelect(quote func(string) string, tableSQL string, cols []string, filters []Filter, order *Order, limit int) (string, []any) {
	sel := "*"
	if len(cols) > 0 {
		quoted := make([]string, len(cols))
		for i, c := range cols {
			quoted[i] = quote(c)
		}
		sel = strings.Join(quoted, ", ")
	}
	q := "SELECT " + sel + " FROM " + tableSQL
	where, args := BuildWhere(quote, filters)
	if where != "" {
		q += " WHERE " + where
	}
	if order != nil {
		q += " ORDER BY " + quote(order.Column)
		if order.Desc {
			q += " DESC"
		}
	}
	q += " LIMIT " + strconv.Itoa(limit)
	return q, args
}
