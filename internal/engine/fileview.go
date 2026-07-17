package engine

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fileViewEngine serves flat data files (Parquet, CSV/TSV, JSON) through an
// in-memory DuckDB with one view per file over the matching read_* reader.
// Nothing is ever written to the files.
type fileViewEngine struct {
	duckdbEngine
	kind Kind
}

func (e *fileViewEngine) Kind() Kind { return e.kind }

// readerFor maps a file kind to its DuckDB table function.
func readerFor(kind Kind) string {
	switch kind {
	case KindCSV:
		return "read_csv_auto"
	case KindJSON:
		return "read_json_auto"
	default:
		return "read_parquet"
	}
}

// openFileView opens a single data file (or glob pattern) as one view.
func openFileView(path string, kind Kind) (Engine, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := createFileView(db, viewBaseName(path), path, kind); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, oneLineError(err))
	}
	return &fileViewEngine{duckdbEngine{db: db}, kind}, nil
}

// openDir opens every recognizable data file directly inside a directory,
// one view per file.
func openDir(dir string) (Engine, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type dataFile struct {
		name string
		path string
		kind Kind
	}
	var files []dataFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if kind, ok := tabularKind(e.Name()); ok {
			files = append(files, dataFile{viewBaseName(e.Name()), filepath.Join(dir, e.Name()), kind})
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no data files (parquet, csv, tsv, json) found in %s", dir)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}
	taken := map[string]int{}
	for _, f := range files {
		name := f.name
		taken[name]++
		if n := taken[name]; n > 1 {
			name = fmt.Sprintf("%s %d", name, n)
		}
		if err := createFileView(db, name, f.path, f.kind); err != nil {
			db.Close()
			return nil, fmt.Errorf("open %s: %w", f.path, oneLineError(err))
		}
	}
	return &fileViewEngine{duckdbEngine{db: db}, KindDir}, nil
}

func createFileView(db *sql.DB, name, path string, kind Kind) error {
	stmt := "CREATE VIEW " + QuoteIdent(name) +
		" AS SELECT * FROM " + readerFor(kind) + "(" + quoteLiteral(path) + ")"
	_, err := db.Exec(stmt)
	return err
}

// oneLineError trims DuckDB's multi-paragraph errors to their first line.
func oneLineError(err error) error {
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return fmt.Errorf("%s", strings.TrimSpace(msg[:i]))
	}
	return err
}

// viewBaseName derives a view name from a file name or glob, e.g.
// "/data/daily sales.csv.gz" -> "daily sales"; glob patterns become "files".
func viewBaseName(path string) string {
	name := filepath.Base(path)
	lower := strings.ToLower(name)
	for _, suffix := range []string{".gz", ".zst"} {
		if strings.HasSuffix(lower, suffix) {
			name = name[:len(name)-len(suffix)]
			lower = strings.ToLower(name)
			break
		}
	}
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if name == "" {
		name = "data"
	}
	if strings.ContainsAny(name, "*?[") {
		name = "files"
	}
	return name
}

// quoteLiteral quotes a string literal single-quote style, doubling
// embedded quotes.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
