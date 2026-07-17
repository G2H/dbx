# Project: `dbx`, a single-binary TUI explorer for SQLite and DuckDB files

A Go TUI application invoked as `dbx <file>` for quickly exploring the contents of `.db` (SQLite) and `.duckdb` files. Read-only by default.

## Tech choices

- Language: Go (latest stable).
- TUI framework: `github.com/charmbracelet/bubbletea` with `github.com/charmbracelet/bubbles` (table, textinput, textarea, list) and `lipgloss` for styling.
- SQLite driver: `modernc.org/sqlite` (pure Go, no cgo).
- DuckDB driver: `github.com/marcboeker/go-duckdb/v2` (cgo; bundles static libduckdb, output is a single self-contained binary).
- Both accessed via `database/sql`.

## File type detection

Detect the engine by file content, not just extension:
- SQLite files start with the 16-byte magic `SQLite format 3\x00`.
- DuckDB files contain the magic `DUCK` at byte offset 8.
- Fall back to extension (`.db`, `.sqlite`, `.sqlite3` -> SQLite; `.duckdb`, `.ddb` -> DuckDB) if magic detection is ambiguous; exit with a clear error otherwise.

Open connections read-only (SQLite: `?mode=ro`; DuckDB: `ACCESS_MODE=READ_ONLY`).

## Engine abstraction

```go
type Engine interface {
    ListTables(ctx context.Context) ([]string, error) // include views, marked as such
    DescribeTable(ctx context.Context, table string) ([]ColumnInfo, error)
    QuoteIdent(name string) string
    DB() *sql.DB
}
```

- SQLite: tables from `sqlite_master`, schema from `PRAGMA table_info(...)`.
- DuckDB: `information_schema.tables` / `information_schema.columns`.
- `ColumnInfo`: name, declared type, nullability, PK flag, default (blank when unavailable).
- All identifier interpolation through `QuoteIdent` (double-quote style, doubling embedded quotes). All filter values are bound parameters, never string-interpolated.

## Row limit

Fetch at most 100 rows everywhere. For user-written SQL, run the query as-is but stop scanning after 100 rows, and indicate "first 100 rows shown" when truncated. Render NULL distinctly (dim `∅`), truncate long cells with ellipsis.

## Views / screens

1. Tables view (default): left pane list of tables/views with fuzzy filter (`/`); right pane split: schema (top) + 100-row preview (bottom). Enter opens details view.
2. Table details view: full-screen scrollable grid; `c` toggles a columns show/hide panel (space toggles one, `a` all; SELECT-level hiding); `f` filters the highlighted column (plain text -> case-insensitive LIKE %v%; leading operators `=`, `!=`, `>`, `>=`, `<`, `<=`, `null`, `not null`); filters AND together, shown in status bar, cleared individually (`F` filter list) or all (`ctrl+r`). Esc back keeping state.
3. SQL view: textarea on top, results below; ctrl+enter / F5 executes; <=100 rows, truncation note, exec time, inline errors; in-session history ctrl+p/ctrl+n.

## Global UX

- `1`/`2`/`3` switch views; `tab` cycles focus inside a view.
- Arrows move, PgUp/PgDn jump 10, Enter opens, esc back / leave input / cancel query, `q`/ctrl+c quit.
- Graceful resize. All chrome per the visual design language (no stock bubbles help styling).

## Visual design language (mandatory, from "mef")

Palette (exact, six colors): accent `#86D5FD`, dim `#7F7F7F`, text `#DADADA`, selection bg `#015F88`, ok `#4ADE80`, fail `#D95757`. Brand = accent+bold. Titles = text+bold. Keys in hints = accent+bold. Borders dim. Respect NO_COLOR. Never paint full-screen backgrounds.

Global frame: alt-screen; one-column left margin; inner width = max(24, term_width - 2); banner top, body clipped/padded to fill exactly, footer (status line + key hints) pinned to bottom.

Banner: blank line, `▌ dbx. SQLite & DuckDB explorer.` (bold accent), dim context line indented 3 (file path `· engine`, or `file · table` on details view, middle-truncated), blank line.

Boxes: dim single-line borders, title embedded in top border (`┌─ title ───┐`). Title dim normally, accent when focused (focus shown ONLY by title color). Body lines `│ ` + content + padding + ` │`. Data grids carry COLUMN HEADERS in the top border, each header aligned exactly above its column, one dash-space around each word; header laid out with same column offsets as content rows, overlaid onto border dashes (row offset j -> border index j+1).

Data grids: two spaces between columns; cells truncate, never wrap; widths from header + sampled cells with caps, widest flexible column flexes; horizontal scroll shifts whole columns. NULL = dim `∅`. Error text in fail red. Schema grid: name in text color, type/nullable/default dim, PK marked with accent `▌` prefix.

Glyphs: `✓` ok green, `✗` failed red, `●` running accent, `⚠` needs attention red, blank = no data. Query state: `● running 3s` (ticking), `✓ 87 rows · 12ms`, `✗ <one-line error>`, truncation notice dim.

Selection: `▌ ` accent-on-#015F88 + row body text-on-#015F88, monochrome (per-column colors flattened). Unselected rows prefixed with two spaces.

Footer: line 1 status/confirmation (query state, active-filter summary dim; confirmations `▌ <prompt> (y/n)` red bar + bold, captures all keys: y/enter confirm, n/esc/q cancel). Line 2 key hints: `<key> <label>` pairs, three spaces apart, keys accent bold, labels dim, context-sensitive; on narrow terminals drop labels.

Confirmations only for: quitting while a query is running, clearing ALL filters.

Truncation: middle `…` for names/paths, tail `…` for cells. Cell-aware width math (go-runewidth). No wrapping in tables ever. No em dashes; middle dots `·` join composed labels.

CLI help: blank line, brand line, one-line description, `version:` line; sections: accent name, 72-char dim `─` rule, two-column entries; ends with two blank lines.

Per-view layout: Tables view: left `tables (N)` box (filter input under title on `/`), right stacked `schema` + `preview`. Details view: full-width grid; `c` right-side `columns` panel; `F` `filters (N)` box (Enter edits, `x` removes). SQL view: `sql` editor box (focused by default) + `results` grid.

## Project layout

```
cmd/dbx/main.go        // arg parsing, detection, engine construction, tea.Program start
internal/engine/       // Engine interface + sqlite.go + duckdb.go + detect.go
internal/ui/           // app.go, tablesview.go, detailsview.go, sqlview.go, styles.go
internal/query/        // filter -> WHERE builder, limited row scanner, value rendering
```

## Build

`go build ./cmd/dbx` produces one binary. cgo required (DuckDB). Makefile with `build`, `test`, `lint` targets. README documents the cross-compile caveat (matching C toolchain, e.g. zig cc) rather than solving it.

## Testing & acceptance

Unit tests: detection (magic bytes), identifier quoting, filter->WHERE builder (operators, NULL), limited row scanning. Integration test: temp SQLite + DuckDB dbs, assert ListTables / DescribeTable / row fetch. Acceptance: both formats open to Tables view; selection updates schema+preview; details view hide/show + filters AND; SQL view runs SELECTs <=100 rows, errors don't crash; binary needs nothing beyond libc.

## Milestones (in order)

1. Engine layer + detection + tests (no UI).
2. Tables view.
3. Table details view with column toggle + filters.
4. SQL view.
5. Polish: banner, footer, glyphs, CLI help per design language; resize; README; Makefile.
