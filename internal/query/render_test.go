package query

import (
	"math"
	"testing"
)

// Floats print as plain decimals; scientific notation is kept for extremes
// only (magnitude >= 1e21 or below 1e-6), the same rule JavaScript uses.
func TestRenderCellFloat(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{2438385.69, "2438385.69"},
		{1e6, "1000000"},
		{-12345678.5, "-12345678.5"},
		{123456789012345680000.0, "123456789012345680000"},
		{1e21, "1e+21"},
		{0.5, "0.5"},
		{0.000001, "0.000001"},
		{0.0000001, "1e-07"},
		{0.0, "0"},
		{-2.5, "-2.5"},
		{math.Inf(1), "+Inf"},
		{math.NaN(), "NaN"},
		{float32(2438385.5), "2438385.5"},
		{float32(0.1), "0.1"},
	}
	for _, c := range cases {
		if got := renderCell(c.in).Text; got != c.want {
			t.Errorf("renderCell(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
