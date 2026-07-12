package encoder

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// dsvEncoder writes DSV output without encoding/csv: cells are appended into a
// reused per-record buffer with strconv.Append* (no per-cell string allocation,
// no per-record row slice) and quoted exactly as encoding/csv would (delimiter,
// quote, \r, \n, leading space, and the literal `\.`), so the output is
// byte-compatible with the previous csv.Writer implementation (UseCRLF=false).
type dsvEncoder struct {
	delim   string // single-rune delimiter, utf8-encoded
	header  bool
	columns []string
	// numSafe: the delimiter can never appear in a rendered number/bool, so
	// those cells skip the quoting scan (true for , ; | \t — every practical
	// delimiter; false for pathological ones like '.' or a digit).
	numSafe bool
	buf     []byte
	w       *bufio.Writer
}

func newDSV(s spec.DSVSpec) (*dsvEncoder, error) {
	delim := ","
	if s.Delimiter != "" {
		r := []rune(s.Delimiter)
		if len(r) != 1 {
			return nil, fmt.Errorf("encoder: dsv delimiter must be a single character, got %q", s.Delimiter)
		}
		// encoding/csv's validDelim: these can never be a delimiter (they collide
		// with quoting/line structure) — erroring here matches csv.Writer, which
		// would otherwise silently emit structurally corrupt output.
		if r[0] == '"' || r[0] == '\r' || r[0] == '\n' || r[0] == utf8.RuneError || r[0] == 0 {
			return nil, fmt.Errorf("encoder: invalid dsv delimiter %q", s.Delimiter)
		}
		delim = string(r[0])
	}
	// Default to writing a header row; for output a header is the friendly default.
	e := &dsvEncoder{delim: delim, header: true, columns: s.Columns}
	// The alphabet covers every byte a rendered int/float/bool can contain:
	// digits, sign/exponent chars, "true"/"false", and "NaN"/"+Inf"/"-Inf".
	e.numSafe = !strings.ContainsAny("0123456789+-.eEtruefalsNIn", delim) && delim != "\""
	return e, nil
}

func (e *dsvEncoder) Begin(w io.Writer, columns []string) error {
	if len(e.columns) == 0 {
		e.columns = columns
	}
	e.w = bufio.NewWriterSize(w, WriteBufferBytes)
	e.buf = make([]byte, 0, 4096)
	if e.header {
		buf := e.buf[:0]
		for i, c := range e.columns {
			if i > 0 {
				buf = append(buf, e.delim...)
			}
			buf = e.appendString(buf, c)
		}
		buf = append(buf, '\n')
		e.buf = buf
		if _, err := e.w.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

// Write renders one record as a DSV row into the reused buffer. Cells are
// emitted in column order; a cursor walks rec.Fields in lockstep (records are
// built in column order by decode/transform) with a Get fallback, so the common
// case is O(fields). A column absent from the record is an empty cell.
// Defined behaviour for the pathological config of DUPLICATE column names: the
// cursor may pair the Nth duplicate column with the Nth duplicate field (the
// old Get-based path always repeated the first match); with unique column
// names — the only meaningful config — output is byte-identical to the old path.
func (e *dsvEncoder) Write(rec *canonical.Record) error {
	buf := e.buf[:0]
	cursor := 0
	for j, col := range e.columns {
		if j > 0 {
			buf = append(buf, e.delim...)
		}
		var val canonical.Value
		found := false
		if cursor < len(rec.Fields) && rec.Fields[cursor].Name == col {
			val, found = rec.Fields[cursor].Val, true
			cursor++
		} else if v, ok := rec.Get(col); ok {
			val, found = v, true
		}
		if found {
			buf = e.appendCell(buf, val)
		}
	}
	buf = append(buf, '\n')
	e.buf = buf
	_, err := e.w.Write(buf)
	return err
}

func (e *dsvEncoder) End() error { return e.w.Flush() }

// appendCell appends one value as a DSV cell. Null renders as an empty cell
// (matching the previous behaviour, where a missing/null value was "").
func (e *dsvEncoder) appendCell(buf []byte, v canonical.Value) []byte {
	switch v.Kind {
	case canonical.KindString:
		return e.appendString(buf, v.S)
	case canonical.KindInt:
		if e.numSafe {
			return strconv.AppendInt(buf, v.I, 10)
		}
	case canonical.KindFloat:
		if e.numSafe {
			return strconv.AppendFloat(buf, v.F, 'g', -1, 64)
		}
	case canonical.KindBool:
		if e.numSafe {
			return strconv.AppendBool(buf, v.B)
		}
	default:
		return buf // null → empty cell
	}
	// Pathological delimiter that can collide with rendered numbers/bools: go
	// through the string path so quoting still applies.
	return e.appendString(buf, v.String())
}

// appendString appends s as a cell, quoting exactly when encoding/csv would:
// the cell contains the delimiter, a quote, \r or \n, begins with a space rune,
// or is the literal `\.` (the PostgreSQL end-of-dump marker csv guards).
func (e *dsvEncoder) appendString(buf []byte, s string) []byte {
	if !e.needsQuotes(s) {
		return append(buf, s...)
	}
	buf = append(buf, '"')
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			buf = append(buf, '"', '"')
		} else {
			buf = append(buf, s[i])
		}
	}
	return append(buf, '"')
}

func (e *dsvEncoder) needsQuotes(s string) bool {
	if s == "" {
		return false
	}
	if s == `\.` {
		return true
	}
	if strings.Contains(s, e.delim) || strings.ContainsAny(s, "\"\r\n") {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}
