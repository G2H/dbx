package ui

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	_ "modernc.org/sqlite"

	"dbx/internal/engine"
)

func init() {
	// Don't sleep through the 1s elapsed ticker in tests.
	tickCmd = func() tea.Cmd { return nil }
}

// newTestApp opens a seeded temp SQLite database and returns a sized app.
func newTestApp(t *testing.T) *App {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT, age INTEGER)`,
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, total REAL)`,
		`CREATE VIEW adults AS SELECT * FROM users WHERE age >= 18`,
		`INSERT INTO users VALUES (1, 'Ada Lovelace', 'ada@example.com', 36)`,
		`INSERT INTO users VALUES (2, 'Alan Turing', NULL, 41)`,
		`INSERT INTO users VALUES (3, 'Grace Hopper', 'grace@example.com', 17)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	eng, err := engine.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })

	a := New(eng, path)
	runCmd(t, a, a.Init())
	process(t, a, tea.WindowSizeMsg{Width: 100, Height: 32})
	return a
}

// process feeds one message through Update and executes resulting commands
// synchronously.
func process(t *testing.T, a *App, msg tea.Msg) {
	t.Helper()
	_, cmd := a.Update(msg)
	runCmd(t, a, cmd)
}

func runCmd(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			runCmd(t, a, c)
		}
		return
	}
	if _, ok := msg.(tickMsg); ok {
		return // don't loop the elapsed ticker in tests
	}
	process(t, a, msg)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "f5":
		return tea.KeyMsg{Type: tea.KeyF5}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, a *App, keys ...string) {
	t.Helper()
	for _, k := range keys {
		process(t, a, key(k))
	}
}

func typeText(t *testing.T, a *App, text string) {
	t.Helper()
	for _, r := range text {
		press(t, a, string(r))
	}
}

func view(a *App) string { return stripToWidth(a.View()) }

// gridHeaderLine returns the top border line of the (leftmost) data grid.
// When a side panel is open its own top border shares the line; cut it off
// at the second ┌ corner. Titled boxes (`┌─ title`) are skipped.
func gridHeaderLine(v string) string {
	for _, l := range strings.Split(v, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "┌") {
			continue
		}
		if i := strings.Index(l[len("┌"):], "┌"); i >= 0 {
			l = l[:i+len("┌")]
		}
		if strings.HasPrefix(l, "┌─ ") {
			continue
		}
		return l
	}
	return ""
}

func TestTablesViewStartup(t *testing.T) {
	a := newTestApp(t)
	v := view(a)

	for _, want := range []string{
		"▌ dbx. Data file viewer.",
		"· sqlite",
		"tables (3)",
		"adults", "orders", "users",
		"· view", // adults is marked as a view
		"column", "type", "null", "pk", "default",
	} {
		if !strings.Contains(v, want) {
			t.Fatalf("startup view missing %q:\n%s", want, v)
		}
	}
	// Every line fits the terminal.
	for _, line := range strings.Split(v, "\n") {
		if dw(line) > 100 {
			t.Fatalf("line wider than terminal: %q", line)
		}
	}
}

func TestTablesViewSelectionUpdatesPanes(t *testing.T) {
	a := newTestApp(t)
	// adults(view) -> orders -> users; select users.
	press(t, a, "down", "down")
	v := view(a)
	if !strings.Contains(v, "email") {
		t.Fatalf("schema pane should show users columns:\n%s", v)
	}
	if !strings.Contains(v, "ada@example.com") {
		t.Fatalf("preview pane should show user rows:\n%s", v)
	}
	if !strings.Contains(v, "∅") {
		t.Fatalf("NULL email should render as ∅:\n%s", v)
	}
}

func TestTablesFuzzyFilter(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "/")
	typeText(t, a, "usr")
	v := view(a)
	if !strings.Contains(v, "tables (1/3)") {
		t.Fatalf("filter should narrow to 1 table:\n%s", v)
	}
	if strings.Contains(v, "orders") {
		t.Fatalf("orders should be filtered out:\n%s", v)
	}
	press(t, a, "esc") // clear the filter
	if !strings.Contains(view(a), "orders") {
		t.Fatal("esc should clear the filter")
	}
}

func TestDetailsViewFlow(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "down", "down", "enter") // open users
	if a.view != viewDetails {
		t.Fatalf("enter should open details, got view %d", a.view)
	}
	v := view(a)
	if !strings.Contains(v, "· users") {
		t.Fatalf("banner context should show the table:\n%s", v)
	}
	if !strings.Contains(v, "Ada Lovelace") {
		t.Fatalf("details grid should show rows:\n%s", v)
	}

	// Raw where filter: id > 1.
	press(t, a, "w")
	typeText(t, a, "id > 1")
	press(t, a, "enter")
	v = view(a)
	if !strings.Contains(v, "2 rows") {
		t.Fatalf("filter id > 1 should leave 2 rows:\n%s", v)
	}
	if !strings.Contains(v, "1 filter · where id > 1") {
		t.Fatalf("status should describe the filter:\n%s", v)
	}
	if strings.Contains(v, "Ada Lovelace") {
		t.Fatalf("Ada (id 1) should be filtered out:\n%s", v)
	}

	// A second where filter ANDs.
	press(t, a, "w")
	typeText(t, a, "name like '%Alan%'")
	press(t, a, "enter")
	v = view(a)
	if !strings.Contains(v, "1 row") {
		t.Fatalf("both filters should leave 1 row:\n%s", v)
	}
	if !strings.Contains(v, "2 filters · where id > 1 · where name like '%Alan%'") {
		t.Fatalf("status should list both filters:\n%s", v)
	}

	// Clear all filters via ctrl+r confirmation.
	press(t, a, "ctrl+r")
	if !strings.Contains(view(a), "clear all 2 filters? (y/n)") {
		t.Fatalf("ctrl+r should ask for confirmation:\n%s", view(a))
	}
	press(t, a, "y")
	if !strings.Contains(view(a), "3 rows") {
		t.Fatalf("clearing filters should restore 3 rows:\n%s", view(a))
	}

	// Esc returns to the tables view keeping state.
	press(t, a, "esc")
	if a.view != viewTables {
		t.Fatal("esc should return to the tables view")
	}
}

func TestDetailsColumnToggle(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "down", "down", "enter") // open users
	press(t, a, "c")
	v := view(a)
	if !strings.Contains(v, "columns") || !strings.Contains(v, "[x]") {
		t.Fatalf("columns panel should list checkboxes:\n%s", v)
	}
	press(t, a, "space") // hide id
	v = view(a)
	if !strings.Contains(v, "[ ]") {
		t.Fatalf("id should be unchecked:\n%s", v)
	}
	// Check the grid header border, not the whole view (the panel also
	// contains the column name).
	if h := gridHeaderLine(v); strings.Contains(h, " id ") {
		t.Fatalf("hidden id column should leave the grid header: %q", h)
	}
	press(t, a, "space") // show id again
	if h := gridHeaderLine(view(a)); !strings.Contains(h, " id ") {
		t.Fatalf("re-checking should bring the id column back: %q", h)
	}

	// a toggles all off -> "no visible columns" note, no query crash.
	press(t, a, "a")
	if !strings.Contains(view(a), "no visible columns") {
		t.Fatalf("hiding every column should show the note:\n%s", view(a))
	}
	press(t, a, "a", "esc")
	if !strings.Contains(view(a), "Ada Lovelace") {
		t.Fatal("showing all columns should restore rows")
	}
}

func TestSQLView(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "s")
	if a.view != viewSQL {
		t.Fatal("s should open the sql view")
	}
	typeText(t, a, "select name from users order by id")
	press(t, a, "f5")
	v := view(a)
	if !strings.Contains(v, "Ada Lovelace") {
		t.Fatalf("results should show rows:\n%s", v)
	}
	if !strings.Contains(v, "3 rows") {
		t.Fatalf("status should show the row count:\n%s", v)
	}

	// Bad SQL surfaces inline, no crash.
	press(t, a, "ctrl+a") // textarea: start of line; harmless
	for range "select name from users order by id" {
		press(t, a, "backspace")
	}
	a.sv.ta.SetValue("select nope from nowhere")
	press(t, a, "f5")
	v = view(a)
	if !strings.Contains(v, "✗") {
		t.Fatalf("bad sql should show a ✗ error:\n%s", v)
	}

	// History: ctrl+p recalls the last good query.
	process(t, a, tea.KeyMsg{Type: tea.KeyCtrlP})
	if !strings.Contains(a.sv.ta.Value(), "select") {
		t.Fatal("ctrl+p should recall history")
	}

	// Writes fail (read-only) but do not crash.
	a.sv.ta.SetValue("insert into users values (9, 'X', null, 1)")
	press(t, a, "f5")
	if !strings.Contains(view(a), "✗") {
		t.Fatal("write on read-only db should surface an error")
	}
}

func TestQuitAlwaysConfirms(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "q")
	if !strings.Contains(view(a), "quit? (y/n)") {
		t.Fatalf("q should ask for confirmation:\n%s", view(a))
	}
	press(t, a, "n")
	if strings.Contains(view(a), "quit?") {
		t.Fatal("n should dismiss the confirmation")
	}
	press(t, a, "q")
	_, cmd := a.Update(key("y"))
	if cmd == nil {
		t.Fatal("y should confirm the quit")
	}
	if msg := cmd(); msg != nil {
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Fatalf("expected QuitMsg, got %T", msg)
		}
	}
}

func TestViewFlipKeys(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "s")
	if a.view != viewSQL {
		t.Fatal("s should flip to the sql view")
	}
	press(t, a, "esc", "t") // esc leaves the editor, t flips back
	if a.view != viewTables {
		t.Fatal("t should flip back to the tables view")
	}
	// From an open table, flipping remembers the details screen.
	press(t, a, "down", "down", "enter")
	if a.view != viewDetails {
		t.Fatal("enter should open details")
	}
	press(t, a, "s")
	if a.view != viewSQL {
		t.Fatal("s should flip to sql from details")
	}
	press(t, a, "esc", "t")
	if a.view != viewDetails {
		t.Fatal("t should return to the details screen")
	}
}

func TestValuesPanelFilter(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "down", "down", "enter") // open users
	press(t, a, "f")                     // values panel for id
	v := view(a)
	if !strings.Contains(v, "filter id") {
		t.Fatalf("values panel should open for id:\n%s", v)
	}
	if !strings.Contains(v, "1 ×1") || !strings.Contains(v, "[ ]") {
		t.Fatalf("panel should list common values with counts:\n%s", v)
	}
	press(t, a, "space") // select value 1 -> id in (1)
	v = view(a)
	if !strings.Contains(v, "[x]") {
		t.Fatalf("value should be checked:\n%s", v)
	}
	if !strings.Contains(v, "1 row") || !strings.Contains(v, "id in (1)") {
		t.Fatalf("selecting a value should filter rows:\n%s", v)
	}
	press(t, a, "down", "space") // also select value 2 -> id in (1, 2)
	if !strings.Contains(view(a), "2 rows") || !strings.Contains(view(a), "id in (1, 2)") {
		t.Fatalf("multi-select should OR values:\n%s", view(a))
	}
	press(t, a, "space") // deselect 2 again
	if !strings.Contains(view(a), "id in (1)") {
		t.Fatal("deselecting should shrink the filter")
	}
	press(t, a, "esc")
	if !strings.Contains(view(a), "1 row") {
		t.Fatal("filter should survive closing the panel")
	}
}

func TestSortCycle(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "down", "down", "enter")        // open users
	press(t, a, "right", "right", "right", "o") // sort by age ascending
	v := view(a)
	if !strings.Contains(v, "order age ↑") {
		t.Fatalf("status should show ascending sort:\n%s", v)
	}
	if !firstRowContains(v, "Grace") {
		t.Fatalf("youngest (Grace, 17) should be first ascending:\n%s", v)
	}
	press(t, a, "o") // descending
	v = view(a)
	if !strings.Contains(v, "order age ↓") {
		t.Fatalf("status should show descending sort:\n%s", v)
	}
	if !firstRowContains(v, "Alan") {
		t.Fatalf("oldest (Alan, 41) should be first descending:\n%s", v)
	}
	press(t, a, "o") // cleared
	v = view(a)
	if strings.Contains(v, "order age") {
		t.Fatalf("third press should clear the sort:\n%s", v)
	}
	if !firstRowContains(v, "Ada") {
		t.Fatalf("natural order should return (Ada, id 1):\n%s", v)
	}
}

// firstRowContains reports whether the first data row of the leftmost grid
// contains sub.
func firstRowContains(v, sub string) bool {
	lines := strings.Split(v, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "┌") && !strings.HasPrefix(t, "┌─ ") {
			if i+1 < len(lines) {
				return strings.Contains(lines[i+1], sub)
			}
		}
	}
	return false
}

func TestWhereFilter(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "down", "down", "enter") // open users
	press(t, a, "w")
	typeText(t, a, "age > 30 and email is not null")
	press(t, a, "enter")
	v := view(a)
	if !strings.Contains(v, "1 row") {
		t.Fatalf("raw where should filter to ada only:\n%s", v)
	}
	if !strings.Contains(v, "where age > 30 and email is not null") {
		t.Fatalf("status should describe the raw filter:\n%s", v)
	}
	// Bad SQL surfaces inline and can be removed via the filters panel.
	press(t, a, "w")
	typeText(t, a, "nonsense(")
	press(t, a, "enter")
	if !strings.Contains(view(a), "✗") {
		t.Fatalf("bad raw sql should surface an error:\n%s", view(a))
	}
	press(t, a, "F", "down", "x") // remove the bad filter
	if !strings.Contains(view(a), "1 row") {
		t.Fatalf("removing the bad filter should restore rows:\n%s", view(a))
	}
}
