// Package ui implements the dbx TUI: three views (tables, details, sql)
// rendered in the shared visual design language.
package ui

import "github.com/charmbracelet/lipgloss"

// The palette. Six colors total, per the design language.
var (
	colAccent = lipgloss.Color("#86D5FD")
	colDim    = lipgloss.Color("#7F7F7F")
	colText   = lipgloss.Color("#DADADA")
	colSelBg  = lipgloss.Color("#015F88")
	colOK     = lipgloss.Color("#4ADE80")
	colFail   = lipgloss.Color("#D95757")
)

// S holds every style used by the UI. lipgloss (via termenv) honors
// NO_COLOR by degrading these to plain text.
var S = struct {
	Brand   lipgloss.Style // accent + bold
	Title   lipgloss.Style // text + bold
	Text    lipgloss.Style
	Dim     lipgloss.Style
	Accent  lipgloss.Style
	Key     lipgloss.Style // accent + bold, keys in hints
	OK      lipgloss.Style
	Fail    lipgloss.Style
	FailBar lipgloss.Style // fail + bold, confirmation prompts
	SelBar  lipgloss.Style // accent on selection bg
	SelText lipgloss.Style // text on selection bg
}{
	Brand:   lipgloss.NewStyle().Foreground(colAccent).Bold(true),
	Title:   lipgloss.NewStyle().Foreground(colText).Bold(true),
	Text:    lipgloss.NewStyle().Foreground(colText),
	Dim:     lipgloss.NewStyle().Foreground(colDim),
	Accent:  lipgloss.NewStyle().Foreground(colAccent),
	Key:     lipgloss.NewStyle().Foreground(colAccent).Bold(true),
	OK:      lipgloss.NewStyle().Foreground(colOK),
	Fail:    lipgloss.NewStyle().Foreground(colFail),
	FailBar: lipgloss.NewStyle().Foreground(colFail).Bold(true),
	SelBar:  lipgloss.NewStyle().Foreground(colAccent).Background(colSelBg),
	SelText: lipgloss.NewStyle().Foreground(colText).Background(colSelBg),
}

// Status glyphs, one vocabulary everywhere.
const (
	glyphOK      = "✓"
	glyphFail    = "✗"
	glyphRunning = "●"
	glyphWarn    = "⚠"
	glyphNull    = "∅"
	glyphBar     = "▌"
)
