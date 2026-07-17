package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectSQLiteMagic(t *testing.T) {
	header := append([]byte("SQLite format 3\x00"), make([]byte, 84)...)
	path := writeFile(t, "data.weird", header)
	kind, err := Detect(path)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindSQLite {
		t.Fatalf("got %q, want sqlite", kind)
	}
}

func TestDetectDuckDBMagic(t *testing.T) {
	header := make([]byte, 32)
	copy(header[8:], "DUCK")
	path := writeFile(t, "data.weird", header)
	kind, err := Detect(path)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindDuckDB {
		t.Fatalf("got %q, want duckdb", kind)
	}
}

func TestDetectExtensionFallback(t *testing.T) {
	cases := []struct {
		name string
		want Kind
	}{
		{"empty.db", KindSQLite},
		{"empty.sqlite", KindSQLite},
		{"empty.sqlite3", KindSQLite},
		{"EMPTY.DB", KindSQLite},
		{"empty.duckdb", KindDuckDB},
		{"empty.ddb", KindDuckDB},
		{"empty.parquet", KindParquet},
	}
	for _, c := range cases {
		path := writeFile(t, c.name, nil)
		kind, err := Detect(path)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if kind != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, kind, c.want)
		}
	}
}

func TestDetectParquetMagic(t *testing.T) {
	path := writeFile(t, "data.weird", []byte("PAR1xxxxxxxx"))
	kind, err := Detect(path)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindParquet {
		t.Fatalf("got %q, want parquet", kind)
	}
}

func TestDetectMagicBeatsExtension(t *testing.T) {
	// SQLite content in a .duckdb file: content wins.
	header := append([]byte("SQLite format 3\x00"), make([]byte, 84)...)
	path := writeFile(t, "liar.duckdb", header)
	kind, err := Detect(path)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindSQLite {
		t.Fatalf("got %q, want sqlite", kind)
	}
}

func TestDetectAmbiguousFails(t *testing.T) {
	path := writeFile(t, "mystery.dat", []byte("neither format"))
	if _, err := Detect(path); err == nil {
		t.Fatal("expected an error for ambiguous file")
	}
}

func TestDetectMissingFile(t *testing.T) {
	if _, err := Detect(filepath.Join(t.TempDir(), "nope.db")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestQuoteIdent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"users", `"users"`},
		{"weird name", `"weird name"`},
		{`has"quote`, `"has""quote"`},
		{`""`, `""""""`},
	}
	for _, c := range cases {
		if got := QuoteIdent(c.in); got != c.want {
			t.Fatalf("QuoteIdent(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}
