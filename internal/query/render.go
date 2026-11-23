package query

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// renderCell converts a raw driver value into display text.
func renderCell(v any) Cell {
	switch v := v.(type) {
	case nil:
		return Cell{Null: true}
	case []byte:
		if utf8.Valid(v) {
			return Cell{Text: sanitize(string(v))}
		}
		return Cell{Text: fmt.Sprintf("<blob %d bytes>", len(v))}
	case string:
		return Cell{Text: sanitize(v)}
	case time.Time:
		return Cell{Text: v.Format("2006-01-02 15:04:05")}
	case float64:
		return Cell{Text: formatFloat(v, 64)}
	case float32:
		return Cell{Text: formatFloat(float64(v), 32)}
	case bool:
		return Cell{Text: strconv.FormatBool(v)}
	default:
		return Cell{Text: sanitize(fmt.Sprint(v))}
	}
}

// formatFloat prints plain decimals (2438385.69, not 2.43838569e+06) and
// keeps scientific notation for extremes only: magnitude at least 1e21 or
// below 1e-6, the same thresholds JavaScript uses. bits is 32 or 64 so the
// shortest round-tripping digits match the source type.
func formatFloat(v float64, bits int) string {
	abs := math.Abs(v)
	if abs >= 1e21 || (abs < 1e-6 && abs != 0) || math.IsInf(v, 0) || math.IsNaN(v) {
		return strconv.FormatFloat(v, 'g', -1, bits)
	}
	return strconv.FormatFloat(v, 'f', -1, bits)
}

// sanitize keeps cell text on one line; grids never wrap.
func sanitize(s string) string {
	if !strings.ContainsAny(s, "\n\r\t") {
		return s
	}
	r := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")
	return r.Replace(s)
}
