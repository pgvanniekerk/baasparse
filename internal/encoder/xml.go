package encoder

import (
	"bufio"
	"encoding/xml"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// xmlEncoder writes flat-record XML: a root element wrapping one record element
// per record, one child element per output column. Hand-serialized like the
// JSON/DSV encoders: per-column open/close tags are precomputed once, records
// render into a reused buffer, and a cursor walks rec.Fields in lockstep with
// the columns. Text escaping matches encoding/xml's EscapeText (including
// substituting U+FFFD for runes that are invalid in XML), so output is always
// well-formed. Column names are sanitized into valid XML element names
// (invalid name runes become '_'); a null value omits the element.
type xmlEncoder struct {
	root    string
	record  string
	columns []string
	open    [][]byte // precomputed "<name>"
	close_  [][]byte // precomputed "</name>"
	buf     []byte
	w       *bufio.Writer
}

func newXMLEnc(s spec.XMLSpec) *xmlEncoder {
	root := s.RootElement
	if root == "" {
		root = "records"
	}
	rec := s.RecordElement
	if rec == "" {
		rec = "record"
	}
	return &xmlEncoder{root: xmlSafeName(root), record: xmlSafeName(rec)}
}

func (e *xmlEncoder) Begin(w io.Writer, columns []string) error {
	e.columns = columns
	e.w = bufio.NewWriterSize(w, WriteBufferBytes)
	e.open = make([][]byte, len(columns))
	e.close_ = make([][]byte, len(columns))
	for i, c := range columns {
		n := xmlSafeName(c)
		e.open[i] = []byte("<" + n + ">")
		e.close_[i] = []byte("</" + n + ">")
	}
	e.buf = make([]byte, 0, 8192)
	if _, err := e.w.WriteString(xml.Header); err != nil {
		return err
	}
	if _, err := e.w.WriteString("<" + e.root + ">\n"); err != nil {
		return err
	}
	return nil
}

func (e *xmlEncoder) Write(rec *canonical.Record) error {
	buf := e.buf[:0]
	buf = append(buf, "  <"...)
	buf = append(buf, e.record...)
	buf = append(buf, '>')
	cursor := 0
	for j := range e.columns {
		var val canonical.Value
		found := false
		if cursor < len(rec.Fields) && rec.Fields[cursor].Name == e.columns[j] {
			val, found = rec.Fields[cursor].Val, true
			cursor++
		} else if v, ok := rec.Get(e.columns[j]); ok {
			val, found = v, true
		}
		if !found || val.Kind == canonical.KindNull {
			continue // null/missing → element omitted
		}
		buf = append(buf, e.open[j]...)
		buf = appendXMLValue(buf, val)
		buf = append(buf, e.close_[j]...)
	}
	buf = append(buf, "</"...)
	buf = append(buf, e.record...)
	buf = append(buf, '>', '\n')
	e.buf = buf
	_, err := e.w.Write(buf)
	return err
}

func (e *xmlEncoder) End() error {
	if _, err := e.w.WriteString("</" + e.root + ">\n"); err != nil {
		return err
	}
	return e.w.Flush()
}

func appendXMLValue(dst []byte, v canonical.Value) []byte {
	switch v.Kind {
	case canonical.KindString:
		return appendXMLText(dst, v.S)
	case canonical.KindInt:
		return strconv.AppendInt(dst, v.I, 10)
	case canonical.KindFloat:
		if math.IsNaN(v.F) || math.IsInf(v.F, 0) {
			return appendXMLText(dst, v.String())
		}
		return strconv.AppendFloat(dst, v.F, 'g', -1, 64)
	case canonical.KindBool:
		return strconv.AppendBool(dst, v.B)
	default:
		return dst
	}
}

// appendXMLText appends s escaped for XML character data, mirroring
// encoding/xml.EscapeText: the five predefined entities plus \t \n \r as
// numeric references, with runes that are not valid XML 1.0 characters (or
// invalid UTF-8 bytes) replaced by U+FFFD.
func appendXMLText(dst []byte, s string) []byte {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '"':
			dst = append(dst, "&#34;"...)
		case '\'':
			dst = append(dst, "&#39;"...)
		case '&':
			dst = append(dst, "&amp;"...)
		case '<':
			dst = append(dst, "&lt;"...)
		case '>':
			dst = append(dst, "&gt;"...)
		case '\t':
			dst = append(dst, "&#x9;"...)
		case '\n':
			dst = append(dst, "&#xA;"...)
		case '\r':
			dst = append(dst, "&#xD;"...)
		default:
			if !isValidXMLChar(r) || (r == utf8.RuneError && size == 1) {
				dst = append(dst, 0xef, 0xbf, 0xbd) // U+FFFD
			} else {
				dst = append(dst, s[i:i+size]...)
			}
		}
		i += size
	}
	return dst
}

// isValidXMLChar reports whether r is a legal XML 1.0 character
// (https://www.w3.org/TR/xml/#charsets — same set encoding/xml enforces).
func isValidXMLChar(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D ||
		(r >= 0x20 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}

// xmlSafeName produces an element name that Go's own encoding/xml parser
// accepts. sanitizeXMLName enforces the XML 1.0 5th-edition grammar, but the
// stdlib implements the (stricter, per-character) 4TH-edition tables and
// rejects some 5th-ed names (e.g. Ĳ U+0132) — and our decoder IS the stdlib.
// So verify the sanitized name against the stdlib once (this runs per column
// at Begin, not per record) and fall back to conservative ASCII sanitization
// when it disagrees. Deterministic, never fails.
func xmlSafeName(s string) string {
	n := sanitizeXMLName(s)
	if stdlibAcceptsXMLName(n) {
		return n
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ { // byte-wise: every non-ASCII byte becomes '_'
		c := s[i]
		ok := c == '_' || c == '-' || c == '.' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if i == 0 {
			ok = c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		}
		if ok {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// stdlibAcceptsXMLName reports whether encoding/xml parses <name/>.
func stdlibAcceptsXMLName(n string) bool {
	dec := xml.NewDecoder(strings.NewReader("<" + n + "/>"))
	_, err := dec.Token()
	return err == nil
}

// sanitizeXMLName maps an arbitrary column/element name onto a valid XML
// element name: name runes outside the accepted set become '_', and a leading
// rune that cannot start a name is prefixed with '_'. Deterministic, never
// fails — a config problem yields an ugly-but-well-formed element instead of a
// permanently failing pipeline. Validity is the XML 1.0 (5th ed.) NameStartChar
// / NameChar production — NOT unicode.IsLetter, which admits runes the XML
// grammar (and hence our own decoder) rejects. ':' is excluded so a sanitized
// name can never fabricate a namespace prefix.
func sanitizeXMLName(s string) string {
	if s == "" {
		return "_"
	}
	var b []byte
	for i, r := range s {
		ok := isXMLNameChar(r)
		if i == 0 {
			ok = isXMLNameStartChar(r)
		}
		if ok {
			if b != nil {
				b = utf8.AppendRune(b, r)
			}
			continue
		}
		if b == nil { // first invalid rune: copy the prefix, then substitute
			b = make([]byte, 0, len(s)+1)
			b = append(b, s[:i]...)
			if i == 0 {
				b = append(b, '_')
				// a rune that could continue a name but not start one keeps its value
				if isXMLNameChar(r) {
					b = utf8.AppendRune(b, r)
				}
				continue
			}
		}
		b = append(b, '_')
	}
	if b == nil {
		return s
	}
	return string(b)
}

// isXMLNameStartChar implements XML 1.0 5th-ed NameStartChar minus ':'.
func isXMLNameStartChar(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		return true
	case r >= 0xC0 && r <= 0xD6, r >= 0xD8 && r <= 0xF6, r >= 0xF8 && r <= 0x2FF,
		r >= 0x370 && r <= 0x37D, r >= 0x37F && r <= 0x1FFF, r >= 0x200C && r <= 0x200D,
		r >= 0x2070 && r <= 0x218F, r >= 0x2C00 && r <= 0x2FEF, r >= 0x3001 && r <= 0xD7FF,
		r >= 0xF900 && r <= 0xFDCF, r >= 0xFDF0 && r <= 0xFFFD, r >= 0x10000 && r <= 0xEFFFF:
		return true
	}
	return false
}

// isXMLNameChar implements XML 1.0 5th-ed NameChar minus ':'.
func isXMLNameChar(r rune) bool {
	if isXMLNameStartChar(r) {
		return true
	}
	switch {
	case r == '-', r == '.', r >= '0' && r <= '9', r == 0xB7,
		r >= 0x300 && r <= 0x36F, r >= 0x203F && r <= 0x2040:
		return true
	}
	return false
}
