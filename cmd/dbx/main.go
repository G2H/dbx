// Command dbx is a single-binary TUI explorer for SQLite and DuckDB files.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dbx/internal/engine"
	"dbx/internal/ui"
)

const version = "0.1.0-dev"

var (
	brand   = lipgloss.NewStyle().Foreground(lipgloss.Color("#86D5FD")).Bold(true)
	accent  = lipgloss.NewStyle().Foreground(lipgloss.Color("#86D5FD"))
	dim     = lipgloss.NewStyle().Foreground(lipgloss.Color("#7F7F7F"))
	grey    = lipgloss.NewStyle().Foreground(lipgloss.Color("#DADADA"))
	failure = lipgloss.NewStyle().Foreground(lipgloss.Color("#D95757"))
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printHelp()
		os.Exit(2)
	}
	switch args[0] {
	case "-h", "--help", "help":
		printHelp()
		return
	case "-v", "-V", "--version", "version":
		fmt.Println("dbx " + version)
		return
	}
	path := args[0]

	eng, err := engine.Open(path)
	if err != nil {
		// Render line by line: lipgloss pads multiline strings with
		// trailing spaces, which breaks copy-pasting suggested commands.
		for _, line := range strings.Split("✗ "+err.Error(), "\n") {
			fmt.Fprintln(os.Stderr, failure.Render(line))
		}
		os.Exit(1)
	}
	defer eng.Close()

	p := tea.NewProgram(ui.New(eng, path), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, failure.Render("✗ "+err.Error()))
		os.Exit(1)
	}
}

// printHelp renders --help in the shared design language: brand line,
// description, version line, then accent section names over dim rules with
// two-column entries.
func printHelp() {
	rule := dim.Render(strings.Repeat("─", 72))
	entry := func(cmd, desc string) string {
		return grey.Render(fmt.Sprintf("%-30s", cmd)) + dim.Render(desc)
	}

	fmt.Print("\n" +
		brand.Render("▌ dbx. Data file viewer.") + "\n" +
		"Explore SQLite, DuckDB, Parquet, CSV and JSON data from the terminal.\n" +
		dim.Render("version:") + " " + grey.Render(version) + "\n" +
		"\n" +
		accent.Render("usage") + "\n" +
		rule + "\n" +
		entry("dbx <file>", "open a .db, .duckdb, .parquet, .csv, .tsv,") + "\n" +
		entry("", ".json, .jsonl or .ndjson file (gz/zst ok)") + "\n" +
		entry("dbx <dir>", "open every data file in a directory") + "\n" +
		entry("dbx '<glob>'", "open files matching a pattern as one view") + "\n" +
		entry("dbx --help", "show this help") + "\n" +
		entry("dbx --version", "print the version") + "\n" +
		"\n" +
		accent.Render("keys") + "\n" +
		rule + "\n" +
		entry("s / t", "flip between the sql editor and the tables") + "\n" +
		entry("tab", "cycle focus between boxes") + "\n" +
		entry("↑↓ / j k", "move · PgUp/PgDn jump 10") + "\n" +
		entry("↵", "open the selected table") + "\n" +
		entry("/", "fuzzy-filter the tables list") + "\n" +
		entry("c", "show/hide columns (details view)") + "\n" +
		entry("f", "pick common values to filter a column") + "\n" +
		entry("w", "filter with a raw sql where fragment") + "\n" +
		entry("o", "sort by the highlighted column (asc/desc/off)") + "\n" +
		entry("alt+↵ / F5 / ctrl+r", "run the sql editor contents") + "\n" +
		entry("esc", "back · leave input · cancel a running query") + "\n" +
		entry("q", "quit (asks) · ctrl+c quits immediately") + "\n" +
		"\n" +
		accent.Render("notes") + "\n" +
		rule + "\n" +
		entry("read-only", "files are opened read-only; writes fail") + "\n" +
		entry("100 rows", "every result shows at most the first 100 rows") + "\n" +
		"\n\n")
}
