package engine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var sqliteMagic = []byte("SQLite format 3\x00")

// Detect determines the engine kind for a path: directories open as a set
// of file views, glob patterns classify by their extension, and files go by
// content (magic bytes) first, extension second.
func Detect(path string) (Kind, error) {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return KindDir, nil
	} else if err != nil && strings.ContainsAny(filepath.Base(path), "*?[") {
		// A glob pattern rather than a real file.
		if kind, ok := tabularKind(path); ok {
			return kind, nil
		}
		return "", fmt.Errorf("glob %s must end in .parquet, .csv, .tsv or .json", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	header := make([]byte, 16)
	n, _ := f.Read(header)
	header = header[:n]

	if kind, ok := detectMagic(header); ok {
		return kind, nil
	}
	if kind, ok := detectExtension(path); ok {
		return kind, nil
	}
	return "", fmt.Errorf("cannot determine file type of %s: no SQLite, DuckDB or Parquet magic bytes, and extension %q is not one of .db, .sqlite, .sqlite3, .duckdb, .ddb, .parquet, .csv, .tsv, .json, .jsonl, .ndjson", path, filepath.Ext(path))
}

// detectMagic checks the first 16 bytes of the file.
// SQLite files start with "SQLite format 3\x00"; DuckDB files carry "DUCK"
// at byte offset 8; Parquet files start with "PAR1".
func detectMagic(header []byte) (Kind, bool) {
	if bytes.HasPrefix(header, sqliteMagic) {
		return KindSQLite, true
	}
	if len(header) >= 12 && bytes.Equal(header[8:12], []byte("DUCK")) {
		return KindDuckDB, true
	}
	if bytes.HasPrefix(header, []byte("PAR1")) {
		return KindParquet, true
	}
	return "", false
}

func detectExtension(path string) (Kind, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3":
		return KindSQLite, true
	case ".duckdb", ".ddb":
		return KindDuckDB, true
	}
	return tabularKind(path)
}

// tabularKind classifies flat data files by extension, ignoring a trailing
// compression suffix (data.csv.gz is still CSV).
func tabularKind(path string) (Kind, bool) {
	n := strings.ToLower(filepath.Base(path))
	n = strings.TrimSuffix(n, ".gz")
	n = strings.TrimSuffix(n, ".zst")
	switch filepath.Ext(n) {
	case ".parquet":
		return KindParquet, true
	case ".csv", ".tsv":
		return KindCSV, true
	case ".json", ".jsonl", ".ndjson":
		return KindJSON, true
	}
	return "", false
}
