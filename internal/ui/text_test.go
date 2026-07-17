package ui

import "testing"

func TestTruncTail(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hell…"},
		{"hello", 0, ""},
	}
	for _, c := range cases {
		if got := truncTail(c.in, c.w); got != c.want {
			t.Fatalf("truncTail(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

func TestTruncMiddle(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"short", 10, "short"},
		{"/home/user/data/sales.duckdb", 15, "/home/u….duckdb"},
		{"abcdefgh", 5, "ab…gh"},
	}
	for _, c := range cases {
		got := truncMiddle(c.in, c.w)
		if got != c.want {
			t.Fatalf("truncMiddle(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
		if dw(got) > c.w {
			t.Fatalf("truncMiddle(%q, %d) = %q is wider than %d", c.in, c.w, got, c.w)
		}
	}
}

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"", "anything", true},
		{"usr", "users", true},
		{"USR", "users", true},
		{"sru", "users", false},
		{"ord", "orders", true},
		{"xyz", "orders", false},
	}
	for _, c := range cases {
		if got := fuzzyMatch(c.pattern, c.s); got != c.want {
			t.Fatalf("fuzzyMatch(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}
