package encoder

import (
	"bufio"
	"io"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// WriteBufferBytes is the output write-buffer size (engine-wide, settable at
// startup). A larger buffer means fewer, bigger writes to the destination — which
// matters most for object/network backends (S3) where each write is a round-trip.
var WriteBufferBytes = 64 * 1024

type jsonEncoder struct {
	array   bool
	columns []string
	keys    [][]byte // precomputed `"<column>":` fragments (JSON-escaped, built once)
	buf     []byte   // reused per-record scratch buffer
	w       *bufio.Writer
	n       int
}

func newJSON(s spec.JSONSpec) *jsonEncoder {
	return &jsonEncoder{array: s.Mode == "array"}
}

func (e *jsonEncoder) Begin(w io.Writer, columns []string) error {
	e.columns = columns
	e.w = bufio.NewWriterSize(w, WriteBufferBytes)
	e.n = 0
	// Precompute the quoted key + colon for each output column ONCE, so per-record
	// encoding never re-escapes or re-allocates the keys.
	e.keys = make([][]byte, len(columns))
	for i, c := range columns {
		k := appendJSONString(make([]byte, 0, len(c)+3), c)
		e.keys[i] = append(k, ':')
	}
	e.buf = make([]byte, 0, 8192)
	if e.array {
		return e.w.WriteByte('[')
	}
	return nil
}

// Write serialises one record directly into a reused byte buffer (no reflection,
// no per-field allocation, no map ordering) and flushes it to the buffered writer.
// Values are emitted in output-column order; a column absent from the record
// yields null (matching the previous json.Marshal behaviour).
func (e *jsonEncoder) Write(rec *canonical.Record) error {
	buf := e.buf[:0]
	if e.array && e.n > 0 {
		buf = append(buf, ',')
	}
	buf = append(buf, '{')

	// cursor walks rec.Fields in lockstep with the columns (records are built in
	// column order by decode/transform), so the common case is O(fields) with no
	// lookups; a mismatch falls back to Get for that column only.
	cursor := 0
	for j, kb := range e.keys {
		if j > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, kb...)
		var val canonical.Value
		found := false
		if cursor < len(rec.Fields) && rec.Fields[cursor].Name == e.columns[j] {
			val, found = rec.Fields[cursor].Val, true
			cursor++
		} else if v, ok := rec.Get(e.columns[j]); ok {
			val, found = v, true
		}
		if found {
			buf = appendJSONValue(buf, val)
		} else {
			buf = append(buf, 'n', 'u', 'l', 'l')
		}
	}
	buf = append(buf, '}')
	if !e.array {
		buf = append(buf, '\n')
	}
	e.buf = buf
	_, err := e.w.Write(buf)
	e.n++
	return err
}

func (e *jsonEncoder) End() error {
	if e.array {
		if err := e.w.WriteByte(']'); err != nil {
			return err
		}
	}
	return e.w.Flush()
}

// appendJSONValue appends a canonical value as JSON to dst.
func appendJSONValue(dst []byte, v canonical.Value) []byte {
	switch v.Kind {
	case canonical.KindString:
		return appendJSONString(dst, v.S)
	case canonical.KindInt:
		return strconv.AppendInt(dst, v.I, 10)
	case canonical.KindFloat:
		if math.IsNaN(v.F) || math.IsInf(v.F, 0) { // not representable in JSON
			return append(dst, 'n', 'u', 'l', 'l')
		}
		return strconv.AppendFloat(dst, v.F, 'g', -1, 64)
	case canonical.KindBool:
		if v.B {
			return append(dst, 't', 'r', 'u', 'e')
		}
		return append(dst, 'f', 'a', 'l', 's', 'e')
	default:
		return append(dst, 'n', 'u', 'l', 'l')
	}
}

const hexdigits = "0123456789abcdef"

// appendJSONString appends s as a quoted, escaped JSON string. It escapes the
// mandatory characters (", \, and control bytes < 0x20) per RFC 8259 and, like
// encoding/json, substitutes the Unicode replacement char U+FFFD for any invalid
// UTF-8 byte or lone surrogate — so the output is ALWAYS valid UTF-8 even when the
// input isn't (e.g. a Windows-1252/Latin-1 CSV cell). Valid multi-byte UTF-8 is
// passed through unescaped. (It does not HTML-escape < > & like encoding/json's
// default; the output is still valid JSON.)
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf { // ASCII fast path
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch c {
			case '"':
				dst = append(dst, '\\', '"')
			case '\\':
				dst = append(dst, '\\', '\\')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexdigits[c>>4], hexdigits[c&0xf])
			}
			i++
			start = i
			continue
		}
		// Non-ASCII: validate the rune. An invalid byte (or a surrogate, which
		// DecodeRuneInString reports as RuneError,1) is replaced with U+FFFD.
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 1 { // invalid encoding at s[i]
			dst = append(dst, s[start:i]...)
			dst = append(dst, 0xef, 0xbf, 0xbd) // U+FFFD
			i++
			start = i
			continue
		}
		i += size // valid multi-byte rune — leave as-is
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}
