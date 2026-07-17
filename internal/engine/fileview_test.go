package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dbx/internal/query"
)

func writeData(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// openAndFetch opens a path, asserts one view with the given name, and
// returns its rows.
func openAndFetch(t *testing.T, path string, wantKind Kind, wantView string) *query.Result {
	t.Helper()
	kind, err := Detect(path)
	if err != nil {
		t.Fatal(err)
	}
	if kind != wantKind {
		t.Fatalf("Detect(%s) = %q, want %q", path, kind, wantKind)
	}
	eng, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })

	ctx := context.Background()
	tables, err := eng.ListTables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].Name != wantView {
		t.Fatalf("expected one view %q, got %+v", wantView, tables)
	}
	q, args := query.BuildSelect(eng.QuoteIdent, eng.QuoteTable(tables[0]), nil, nil, nil, query.MaxRows)
	res, err := query.Run(ctx, eng.DB(), q, args...)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestCSVEngine(t *testing.T) {
	path := writeData(t, t.TempDir(), "people.csv",
		"id,name,age\n1,Ada,36\n2,Alan,41\n3,,17\n")
	res := openAndFetch(t, path, KindCSV, "people")
	if len(res.Columns) != 3 || res.Columns[1] != "name" {
		t.Fatalf("columns = %v", res.Columns)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(res.Rows))
	}
	if res.Rows[0][1].Text != "Ada" {
		t.Fatalf("first name = %+v", res.Rows[0][1])
	}
	if !res.Rows[2][1].Null {
		t.Fatalf("empty csv field should scan as NULL: %+v", res.Rows[2])
	}
}

func TestTSVEngine(t *testing.T) {
	path := writeData(t, t.TempDir(), "people.tsv",
		"id\tname\n1\tAda\n2\tAlan\n")
	res := openAndFetch(t, path, KindCSV, "people")
	if len(res.Rows) != 2 || res.Rows[1][1].Text != "Alan" {
		t.Fatalf("tsv rows = %+v", res.Rows)
	}
}

func TestJSONEngine(t *testing.T) {
	path := writeData(t, t.TempDir(), "events.json",
		`[{"id": 1, "kind": "login"}, {"id": 2, "kind": null}]`)
	res := openAndFetch(t, path, KindJSON, "events")
	if len(res.Columns) != 2 || len(res.Rows) != 2 {
		t.Fatalf("json result = %v %v", res.Columns, res.Rows)
	}
	if !res.Rows[1][1].Null {
		t.Fatalf("null json field should scan as NULL: %+v", res.Rows[1])
	}
}

func TestNDJSONEngine(t *testing.T) {
	path := writeData(t, t.TempDir(), "log.ndjson",
		"{\"id\": 1, \"msg\": \"a\"}\n{\"id\": 2, \"msg\": \"b\"}\n")
	res := openAndFetch(t, path, KindJSON, "log")
	if len(res.Rows) != 2 || res.Rows[0][1].Text != "a" {
		t.Fatalf("ndjson rows = %+v", res.Rows)
	}
}

func TestDirectoryEngine(t *testing.T) {
	dir := t.TempDir()
	writeData(t, dir, "people.csv", "id,name\n1,Ada\n")
	writeData(t, dir, "events.jsonl", "{\"id\": 1}\n")
	writeData(t, dir, "notes.txt", "not a data file")
	writeData(t, dir, "people.json", "[{\"id\": 9}]") // same base name as people.csv

	kind, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindDir {
		t.Fatalf("Detect(dir) = %q", kind)
	}
	eng, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if eng.Kind() != KindDir {
		t.Fatalf("kind = %q", eng.Kind())
	}

	tables, err := eng.ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tb := range tables {
		names[tb.Name] = true
	}
	if len(tables) != 3 {
		t.Fatalf("got %d views, want 3 (txt skipped): %+v", len(tables), tables)
	}
	// Base-name collision gets a numbered view; the txt file is skipped.
	if !names["events"] || !names["people"] || !names["people 2"] {
		t.Fatalf("unexpected view names: %+v", tables)
	}
}

func TestEmptyDirectoryFails(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("empty directory should fail to open")
	}
}

func TestGlobEngine(t *testing.T) {
	dir := t.TempDir()
	writeData(t, dir, "a.csv", "id,src\n1,a\n")
	writeData(t, dir, "b.csv", "id,src\n2,b\n")
	pattern := filepath.Join(dir, "*.csv")

	res := openAndFetch(t, pattern, KindCSV, "files")
	if len(res.Rows) != 2 {
		t.Fatalf("glob should union both files, got %+v", res.Rows)
	}
}

func TestBadCSVError(t *testing.T) {
	// A parquet-named file with garbage: open must fail with a short error.
	path := writeData(t, t.TempDir(), "junk.parquet", "PAR1 this is not real parquet")
	_, err := Open(path)
	if err == nil {
		t.Fatal("garbage parquet should fail to open")
	}
	if len(err.Error()) > 300 {
		t.Fatalf("error should be trimmed to one line, got %d chars: %s", len(err.Error()), err)
	}
}
