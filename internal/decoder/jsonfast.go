package decoder

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// parseJSONObject parses ONE flat JSON object held in line into rec (which is
// reset), preserving document key order. It exists because encoding/json's
// map[string]any route costs a map, boxed values and a key sort per record —
// ~240 allocs/record on a 50-field object. This parser slices keys and
// unescaped string values directly out of line (zero-copy substrings; the one
// allocation per record is the line string itself, made by the caller), so a
// record costs ~1 alloc. Semantics match encoding/json with UseNumber as the
// previous implementation used it:
//   - numbers: int64 when integral, else float64, else the raw text as string
//   - nested objects/arrays: preserved as their raw JSON text (substring)
//   - strings: JSON escapes decoded (incl. \uXXXX + surrogate pairs); an
//     unescaped string is a zero-copy substring of line
//
// Two deliberate divergences, both on malformed-input territory:
//   - duplicate keys: the FIRST occurrence wins (the old map kept the last);
//     RFC 8259 leaves duplicate handling undefined
//   - trailing non-whitespace after the object is an ERROR (the old code
//     silently ignored it) — garbage after a record now quarantines the file
//     with a reason instead of being dropped
//
// The returned record aliases line; callers must fully consume the record
// before reusing/replacing line (the pipeline consumes records synchronously).
func parseJSONObject(line string, seq int, types map[string]spec.ValueType, rec *canonical.Record) error {
	p := jsonParser{s: line}
	rec.Seq = seq
	rec.Fields = rec.Fields[:0]

	p.skipWS()
	if p.i >= len(p.s) || p.s[p.i] != '{' {
		return fmt.Errorf("expected '{' at offset %d", p.i)
	}
	p.i++
	p.skipWS()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
	} else {
		for {
			p.skipWS()
			key, err := p.parseString()
			if err != nil {
				return fmt.Errorf("object key: %w", err)
			}
			p.skipWS()
			if p.i >= len(p.s) || p.s[p.i] != ':' {
				return fmt.Errorf("expected ':' after key %q at offset %d", key, p.i)
			}
			p.i++
			p.skipWS()
			v, err := p.parseValue()
			if err != nil {
				return fmt.Errorf("value of %q: %w", key, err)
			}
			if types != nil {
				v = coerceValue(v, types[key])
			}
			rec.Fields = append(rec.Fields, canonical.Field{Name: key, Val: v})
			p.skipWS()
			if p.i >= len(p.s) {
				return fmt.Errorf("unterminated object")
			}
			if p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.s[p.i] == '}' {
				p.i++
				break
			}
			return fmt.Errorf("expected ',' or '}' at offset %d", p.i)
		}
	}
	p.skipWS()
	if p.i != len(p.s) {
		return fmt.Errorf("trailing data after object at offset %d", p.i)
	}
	return nil
}

type jsonParser struct {
	s string
	i int
}

func (p *jsonParser) skipWS() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\r', '\n':
			p.i++
		default:
			return
		}
	}
}

// parseValue parses any JSON value at the cursor into a canonical Value.
func (p *jsonParser) parseValue() (canonical.Value, error) {
	if p.i >= len(p.s) {
		return canonical.Value{}, fmt.Errorf("unexpected end of input")
	}
	switch c := p.s[p.i]; {
	case c == '"':
		s, err := p.parseString()
		if err != nil {
			return canonical.Value{}, err
		}
		return canonical.StringVal(s), nil
	case c == '{' || c == '[':
		raw, err := p.parseRaw()
		if err != nil {
			return canonical.Value{}, err
		}
		return canonical.StringVal(raw), nil
	case c == 't':
		if strings.HasPrefix(p.s[p.i:], "true") {
			p.i += 4
			return canonical.BoolVal(true), nil
		}
	case c == 'f':
		if strings.HasPrefix(p.s[p.i:], "false") {
			p.i += 5
			return canonical.BoolVal(false), nil
		}
	case c == 'n':
		if strings.HasPrefix(p.s[p.i:], "null") {
			p.i += 4
			return canonical.NullVal(), nil
		}
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	}
	return canonical.Value{}, fmt.Errorf("invalid value at offset %d", p.i)
}

// parseString parses a JSON string at the cursor. When the string contains no
// escapes it is returned as a zero-copy substring of the input.
func (p *jsonParser) parseString() (string, error) {
	if p.i >= len(p.s) || p.s[p.i] != '"' {
		return "", fmt.Errorf("expected '\"' at offset %d", p.i)
	}
	p.i++
	start := p.i
	// Fast path: scan to the closing quote; bail to the unescape path on '\'.
	// Raw control bytes (< 0x20) inside a string are invalid JSON (RFC 8259 —
	// they must be escaped), matching encoding/json's rejection.
	for j := p.i; j < len(p.s); j++ {
		switch c := p.s[j]; {
		case c == '"':
			s := p.s[start:j]
			p.i = j + 1
			return s, nil
		case c == '\\':
			return p.parseStringEscaped(start, j)
		case c < 0x20:
			return "", fmt.Errorf("invalid control character in string at offset %d", j)
		}
	}
	return "", fmt.Errorf("unterminated string at offset %d", start-1)
}

// parseStringEscaped finishes parsing a string that contains at least one
// escape, decoding into a fresh buffer. start is the first content byte, esc
// the offset of the first backslash.
func (p *jsonParser) parseStringEscaped(start, esc int) (string, error) {
	buf := make([]byte, 0, len(p.s)-start)
	buf = append(buf, p.s[start:esc]...)
	j := esc
	for j < len(p.s) {
		c := p.s[j]
		if c == '"' {
			p.i = j + 1
			return string(buf), nil
		}
		if c != '\\' {
			if c < 0x20 {
				return "", fmt.Errorf("invalid control character in string at offset %d", j)
			}
			buf = append(buf, c)
			j++
			continue
		}
		j++
		if j >= len(p.s) {
			return "", fmt.Errorf("unterminated escape at offset %d", j-1)
		}
		switch p.s[j] {
		case '"':
			buf = append(buf, '"')
		case '\\':
			buf = append(buf, '\\')
		case '/':
			buf = append(buf, '/')
		case 'b':
			buf = append(buf, '\b')
		case 'f':
			buf = append(buf, '\f')
		case 'n':
			buf = append(buf, '\n')
		case 'r':
			buf = append(buf, '\r')
		case 't':
			buf = append(buf, '\t')
		case 'u':
			r, n, err := decodeUnicodeEscape(p.s, j-1)
			if err != nil {
				return "", err
			}
			buf = utf8.AppendRune(buf, r)
			j = j - 1 + n // absolute: the escape spans [backslash, backslash+n)
			continue
		default:
			return "", fmt.Errorf("invalid escape '\\%c' at offset %d", p.s[j], j-1)
		}
		j++
	}
	return "", fmt.Errorf("unterminated string")
}

// decodeUnicodeEscape decodes a \uXXXX escape (with surrogate-pair handling)
// starting at the backslash offset i. Returns the rune and the total escape
// length in bytes (6 or 12). Unpaired surrogates decode to U+FFFD, matching
// encoding/json.
func decodeUnicodeEscape(s string, i int) (rune, int, error) {
	if i+6 > len(s) {
		return 0, 0, fmt.Errorf("truncated \\u escape at offset %d", i)
	}
	v, err := strconv.ParseUint(s[i+2:i+6], 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid \\u escape at offset %d", i)
	}
	r := rune(v)
	if !utf16.IsSurrogate(r) {
		return r, 6, nil
	}
	// High surrogate: try to combine with a following \uXXXX low surrogate.
	if i+12 <= len(s) && s[i+6] == '\\' && s[i+7] == 'u' {
		v2, err2 := strconv.ParseUint(s[i+8:i+12], 16, 32)
		if err2 == nil {
			if dec := utf16.DecodeRune(r, rune(v2)); dec != utf8.RuneError {
				return dec, 12, nil
			}
			// An invalid pair: emit U+FFFD for the first escape only, matching
			// encoding/json (the second escape is decoded on its own next).
			return utf8.RuneError, 6, nil
		}
	}
	return utf8.RuneError, 6, nil
}

// parseNumber parses a JSON number, validating the RFC 8259 grammar
// (-? (0|[1-9][0-9]*) ('.'[0-9]+)? ([eE][+-]?[0-9]+)?) exactly as encoding/json
// does — malformed numbers like "01", "1.", "1e" or "1-2" are ERRORS, never
// silently coerced. Semantics for valid numbers match json.Number: integral →
// int64, else float64; a grammar-valid but unrepresentable magnitude (overflow,
// strconv.ErrRange, e.g. 1e999) keeps the raw text as a string, as the previous
// json.Number-based code did.
func (p *jsonParser) parseNumber() (canonical.Value, error) {
	s, start := p.s, p.i
	j := p.i
	if j < len(s) && s[j] == '-' {
		j++
	}
	switch {
	case j < len(s) && s[j] == '0':
		j++
	case j < len(s) && s[j] >= '1' && s[j] <= '9':
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
	default:
		return canonical.Value{}, fmt.Errorf("invalid number at offset %d", start)
	}
	isInt := true
	if j < len(s) && s[j] == '.' {
		isInt = false
		j++
		if j >= len(s) || s[j] < '0' || s[j] > '9' {
			return canonical.Value{}, fmt.Errorf("invalid number at offset %d", start)
		}
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
	}
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		isInt = false
		j++
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j >= len(s) || s[j] < '0' || s[j] > '9' {
			return canonical.Value{}, fmt.Errorf("invalid number at offset %d", start)
		}
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
	}
	text := s[start:j]
	p.i = j
	if isInt {
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return canonical.IntVal(n), nil
		}
		// int64 overflow: fall through to float, as json.Number.Int64→Float64 did
	}
	f, err := strconv.ParseFloat(text, 64)
	if err == nil {
		return canonical.FloatVal(f), nil
	}
	var ne *strconv.NumError
	if errors.As(err, &ne) && ne.Err == strconv.ErrRange {
		return canonical.StringVal(text), nil // grammar-valid, unrepresentable
	}
	return canonical.Value{}, fmt.Errorf("invalid number %q at offset %d", text, start)
}

// maxNestingDepth bounds recursive validation of nested values (encoding/json
// bounds nesting at 10000 the same way).
const maxNestingDepth = 10000

// parseRaw captures a nested object/array as its raw JSON text (a zero-copy
// substring), FULLY VALIDATING it with a recursive skip-parse — mismatched
// closers ({"a":[1}]) or malformed contents are errors, matching the previous
// implementation's behaviour (which decoded nested values through the stdlib).
// The raw text preserves the source form (the old code re-marshalled nested
// values compacted with sorted keys).
func (p *jsonParser) parseRaw() (string, error) {
	start := p.i
	if err := p.skipValue(0); err != nil {
		return "", err
	}
	return p.s[start:p.i], nil
}

// skipValue validates and consumes one JSON value of any type at the cursor.
func (p *jsonParser) skipValue(depth int) error {
	if depth > maxNestingDepth {
		return fmt.Errorf("nesting too deep at offset %d", p.i)
	}
	p.skipWS()
	if p.i >= len(p.s) {
		return fmt.Errorf("unexpected end of input")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		p.i++
		p.skipWS()
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return nil
		}
		for {
			p.skipWS()
			if _, err := p.parseString(); err != nil {
				return err
			}
			p.skipWS()
			if p.i >= len(p.s) || p.s[p.i] != ':' {
				return fmt.Errorf("expected ':' at offset %d", p.i)
			}
			p.i++
			if err := p.skipValue(depth + 1); err != nil {
				return err
			}
			p.skipWS()
			if p.i >= len(p.s) {
				return fmt.Errorf("unterminated object")
			}
			if p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.s[p.i] == '}' {
				p.i++
				return nil
			}
			return fmt.Errorf("expected ',' or '}' at offset %d", p.i)
		}
	case c == '[':
		p.i++
		p.skipWS()
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return nil
		}
		for {
			if err := p.skipValue(depth + 1); err != nil {
				return err
			}
			p.skipWS()
			if p.i >= len(p.s) {
				return fmt.Errorf("unterminated array")
			}
			if p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.s[p.i] == ']' {
				p.i++
				return nil
			}
			return fmt.Errorf("expected ',' or ']' at offset %d", p.i)
		}
	case c == '"':
		_, err := p.parseString()
		return err
	case c == 't':
		if strings.HasPrefix(p.s[p.i:], "true") {
			p.i += 4
			return nil
		}
		return fmt.Errorf("invalid value at offset %d", p.i)
	case c == 'f':
		if strings.HasPrefix(p.s[p.i:], "false") {
			p.i += 5
			return nil
		}
		return fmt.Errorf("invalid value at offset %d", p.i)
	case c == 'n':
		if strings.HasPrefix(p.s[p.i:], "null") {
			p.i += 4
			return nil
		}
		return fmt.Errorf("invalid value at offset %d", p.i)
	default:
		_, err := p.parseNumber()
		return err
	}
}
