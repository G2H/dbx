package query

import (
	"reflect"
	"testing"
)

func TestParseFilter(t *testing.T) {
	cases := []struct {
		input string
		op    Op
		value string
	}{
		{"ada", OpContains, "ada"},
		{"  ada lovelace  ", OpContains, "ada lovelace"},
		{"= 42", OpEq, "42"},
		{"=42", OpEq, "42"},
		{"!= 42", OpNe, "42"},
		{"> 30", OpGt, "30"},
		{">= 30", OpGe, "30"},
		{"< 30", OpLt, "30"},
		{"<= 30", OpLe, "30"},
		{"null", OpNull, ""},
		{"NULL", OpNull, ""},
		{"not null", OpNotNull, ""},
		{"Not Null", OpNotNull, ""},
		{"nullish", OpContains, "nullish"}, // only the exact word is a null test
	}
	for _, c := range cases {
		f := ParseFilter("col", c.input)
		if f.Op != c.op || f.Value != c.value {
			t.Fatalf("ParseFilter(%q) = {%q %q}, want {%q %q}", c.input, f.Op, f.Value, c.op, c.value)
		}
	}
}

func TestBuildWhere(t *testing.T) {
	quote := func(s string) string { return `"` + s + `"` }

	where, args := BuildWhere(quote, nil)
	if where != "" || args != nil {
		t.Fatalf("empty filters should build nothing, got %q %v", where, args)
	}

	where, args = BuildWhere(quote, []Filter{
		{Column: "name", Op: OpContains, Value: "ada"},
		{Column: "age", Op: OpGt, Value: "30"},
		{Column: "email", Op: OpNull},
		{Column: "note", Op: OpNotNull},
		{Column: "score", Op: OpEq, Value: "1.5"},
		{Column: "tag", Op: OpNe, Value: "x"},
	})
	want := `LOWER(CAST("name" AS VARCHAR)) LIKE LOWER(?)` +
		` AND "age" > ?` +
		` AND "email" IS NULL` +
		` AND "note" IS NOT NULL` +
		` AND "score" = ?` +
		` AND "tag" != ?`
	if where != want {
		t.Fatalf("where = %s\nwant   %s", where, want)
	}
	wantArgs := []any{"%ada%", int64(30), 1.5, "x"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestBuildSelect(t *testing.T) {
	quote := func(s string) string { return `"` + s + `"` }

	q, args := BuildSelect(quote, `"users"`, nil, nil, nil, 100)
	if q != `SELECT * FROM "users" LIMIT 100` || len(args) != 0 {
		t.Fatalf("got %q %v", q, args)
	}

	q, args = BuildSelect(quote, `"users"`, []string{"id", "name"},
		[]Filter{{Column: "name", Op: OpContains, Value: "a"}}, nil, 100)
	want := `SELECT "id", "name" FROM "users" WHERE LOWER(CAST("name" AS VARCHAR)) LIKE LOWER(?) LIMIT 100`
	if q != want {
		t.Fatalf("got  %s\nwant %s", q, want)
	}
	if len(args) != 1 || args[0] != "%a%" {
		t.Fatalf("args = %v", args)
	}
}

func TestBuildSelectOrder(t *testing.T) {
	quote := func(s string) string { return `"` + s + `"` }
	q, _ := BuildSelect(quote, `"users"`, nil, nil, &Order{Column: "age"}, 100)
	if q != `SELECT * FROM "users" ORDER BY "age" LIMIT 100` {
		t.Fatalf("got %q", q)
	}
	q, _ = BuildSelect(quote, `"users"`, nil,
		[]Filter{{Column: "age", Op: OpGt, Value: "1"}}, &Order{Column: "age", Desc: true}, 100)
	if q != `SELECT * FROM "users" WHERE "age" > ? ORDER BY "age" DESC LIMIT 100` {
		t.Fatalf("got %q", q)
	}
	o := Order{Column: "age", Desc: true}
	if o.Describe() != "order age ↓" {
		t.Fatalf("Describe() = %q", o.Describe())
	}
}

func TestBuildWhereInAndRaw(t *testing.T) {
	quote := func(s string) string { return `"` + s + `"` }

	where, args := BuildWhere(quote, []Filter{
		{Column: "tag", Op: OpIn, Values: []string{"a", "b"}, IncludeNull: true},
		{Op: OpRaw, Value: "age % 2 = 0"},
	})
	want := `(CAST("tag" AS VARCHAR) IN (?, ?) OR "tag" IS NULL) AND (age % 2 = 0)`
	if where != want {
		t.Fatalf("where = %s\nwant   %s", where, want)
	}
	if !reflect.DeepEqual(args, []any{"a", "b"}) {
		t.Fatalf("args = %#v", args)
	}

	// Only NULL selected.
	where, args = BuildWhere(quote, []Filter{{Column: "tag", Op: OpIn, IncludeNull: true}})
	if where != `"tag" IS NULL` || len(args) != 0 {
		t.Fatalf("got %q %v", where, args)
	}

	// Nothing selected matches nothing, not everything.
	where, _ = BuildWhere(quote, []Filter{{Column: "tag", Op: OpIn}})
	if where != "1 = 0" {
		t.Fatalf("got %q", where)
	}
}

func TestBuildValueCounts(t *testing.T) {
	quote := func(s string) string { return `"` + s + `"` }
	q := buildValueCounts(quote, `"users"`, "name", SampleFirstRows, 10000, 3)
	want := `SELECT CAST("name" AS VARCHAR) AS v, COUNT(*) AS n` +
		` FROM (SELECT "name" FROM "users" LIMIT 10000)` +
		` GROUP BY 1 ORDER BY n DESC, v LIMIT 3`
	if q != want {
		t.Fatalf("got  %s\nwant %s", q, want)
	}
	// SampleNone scans the whole table, no subquery.
	q = buildValueCounts(quote, `"users"`, "name", SampleNone, 0, 3)
	want = `SELECT CAST("name" AS VARCHAR) AS v, COUNT(*) AS n` +
		` FROM "users" GROUP BY 1 ORDER BY n DESC, v LIMIT 3`
	if q != want {
		t.Fatalf("got  %s\nwant %s", q, want)
	}
	// Reservoir mode samples fairly via USING SAMPLE (DuckDB).
	q = buildValueCounts(quote, `"users"`, "name", SampleReservoir, 10000, 3)
	want = `SELECT CAST("name" AS VARCHAR) AS v, COUNT(*) AS n` +
		` FROM (SELECT "name" FROM "users" USING SAMPLE 10000 ROWS)` +
		` GROUP BY 1 ORDER BY n DESC, v LIMIT 3`
	if q != want {
		t.Fatalf("got  %s\nwant %s", q, want)
	}
}

func TestFilterDescribe(t *testing.T) {
	cases := []struct {
		f    Filter
		want string
	}{
		{Filter{Column: "name", Op: OpContains, Value: "ada"}, "name ~ ada"},
		{Filter{Column: "age", Op: OpGt, Value: "30"}, "age > 30"},
		{Filter{Column: "email", Op: OpNull}, "email null"},
		{Filter{Column: "email", Op: OpNotNull}, "email not null"},
		{Filter{Column: "tag", Op: OpIn, Values: []string{"a", "b"}, IncludeNull: true}, "tag in (a, b, ∅)"},
		{Filter{Op: OpRaw, Value: "age % 2 = 0"}, "where age % 2 = 0"},
	}
	for _, c := range cases {
		if got := c.f.Describe(); got != c.want {
			t.Fatalf("Describe() = %q, want %q", got, c.want)
		}
	}
}

func TestRenderCell(t *testing.T) {
	cases := []struct {
		in   any
		want Cell
	}{
		{nil, Cell{Null: true}},
		{"plain", Cell{Text: "plain"}},
		{"two\nlines\there", Cell{Text: "two lines here"}},
		{[]byte("bytes"), Cell{Text: "bytes"}},
		{[]byte{0xff, 0xfe, 0x00}, Cell{Text: "<blob 3 bytes>"}},
		{int64(42), Cell{Text: "42"}},
		{3.14, Cell{Text: "3.14"}},
		{true, Cell{Text: "true"}},
	}
	for _, c := range cases {
		if got := renderCell(c.in); got != c.want {
			t.Fatalf("renderCell(%#v) = %+v, want %+v", c.in, got, c.want)
		}
	}
}
