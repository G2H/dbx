package query

import (
	"fmt"
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
		return Cell{Text: strconv.FormatFloat(v, 'g', -1, 64)}
	case float32:
		return Cell{Text: strconv.FormatFloat(float64(v), 'g', -1, 32)}
	case bool:
		return Cell{Text: strconv.FormatBool(v)}
	default:
		return Cell{Text: sanitize(fmt.Sprint(v))}
	}
}

// sanitize keeps cell text on one line; grids never wrap.
func sanitize(s string) string {
	if !strings.ContainsAny(s, "\n\r\t") {
		return s
	}
	r := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")
	return r.Replace(s)
}
