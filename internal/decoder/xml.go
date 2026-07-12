package decoder

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// xmlDecoder streams FLAT records out of an XML document (BR-DEC-002): every
// occurrence of the record element (configured, or auto-detected as the first
// element under the document root) yields one record. A record's fields are the
// record element's attributes followed by each direct child element's text
// content (all CharData in the child's subtree, concatenated and
// whitespace-trimmed — nested structure is flattened to its text in the alpha).
// Uses encoding/xml's tokenizer, so entities, CDATA, comments and processing
// instructions are handled by the stdlib; one record is reused across emits
// (the pipeline consumes records synchronously, same contract as DSV/JSON).
type xmlDecoder struct {
	record string // record element local name; "" = auto-detect
	types  map[string]spec.ValueType
}

func newXML(s spec.XMLSpec, fields []spec.FieldSpec) *xmlDecoder {
	return &xmlDecoder{record: s.RecordElement, types: typeMap(fields)}
}

func (d *xmlDecoder) Decode(ctx context.Context, r io.Reader, emit EmitFunc) error {
	// RawToken (vs Token) skips per-token namespace translation and start/end
	// matching, saving several allocations per element; we re-impose the
	// well-formedness check ourselves with a name stack (elems). Entity and CDATA
	// decoding still happen inside the stdlib tokenizer.
	dec := xml.NewDecoder(r)
	recName := d.record
	rec := &canonical.Record{}
	var text strings.Builder
	var elems []string // open-element stack (well-formedness + depth)
	seq := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		tok, err := dec.RawToken()
		if errors.Is(err, io.EOF) {
			if len(elems) != 0 {
				return fmt.Errorf("xml: unexpected EOF: <%s> not closed", elems[len(elems)-1])
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// Auto-detect: the first element under the root is the record element.
			if recName == "" && len(elems) == 1 {
				recName = t.Name.Local
			}
			if t.Name.Local == recName && len(elems) >= 1 {
				if err := d.decodeRecord(dec, t, rec, seq+1, &text); err != nil {
					return fmt.Errorf("xml: record %d: %w", seq+1, err)
				}
				if err := emit(rec); err != nil {
					return err
				}
				seq++
			} else {
				elems = append(elems, t.Name.Local)
			}
		case xml.EndElement:
			if len(elems) == 0 || elems[len(elems)-1] != t.Name.Local {
				return fmt.Errorf("xml: unexpected </%s>", t.Name.Local)
			}
			elems = elems[:len(elems)-1]
		}
	}
}

// decodeRecord consumes one record element's subtree (start already read) and
// fills rec: attributes first, then one field per direct child element.
func (d *xmlDecoder) decodeRecord(dec *xml.Decoder, start xml.StartElement, rec *canonical.Record, seq int, text *strings.Builder) error {
	rec.Seq = seq
	rec.Fields = rec.Fields[:0]
	for _, a := range start.Attr {
		// Namespace declarations (xmlns="..." / xmlns:p="...") are markup
		// plumbing, not record data — with RawToken the latter arrives as
		// Space="xmlns", Local="p", which would otherwise fabricate a field named
		// after the arbitrary prefix and shadow a real field.
		if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
			continue
		}
		rec.Fields = append(rec.Fields, canonical.Field{Name: a.Name.Local, Val: d.value(a.Name.Local, a.Value)})
	}
	for {
		tok, err := dec.RawToken()
		if err != nil {
			return fmt.Errorf("inside <%s>: %w", start.Name.Local, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// A direct child element = one field: its subtree's text content.
			name := t.Name.Local
			text.Reset()
			if err := collectText(dec, name, text); err != nil {
				return fmt.Errorf("field <%s>: %w", name, err)
			}
			val := strings.TrimSpace(text.String())
			rec.Fields = append(rec.Fields, canonical.Field{Name: name, Val: d.value(name, val)})
		case xml.EndElement:
			if t.Name.Local != start.Name.Local {
				return fmt.Errorf("unexpected </%s> inside <%s>", t.Name.Local, start.Name.Local)
			}
			return nil // the record element closed
		}
		// CharData/comments/PIs at record level are ignored (indentation etc.)
	}
}

// collectText consumes the current element's subtree (opened as <name>),
// appending all CharData and verifying tag nesting matches.
func collectText(dec *xml.Decoder, name string, out *strings.Builder) error {
	stack := []string{name}
	for len(stack) > 0 {
		tok, err := dec.RawToken()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
		case xml.EndElement:
			if stack[len(stack)-1] != t.Name.Local {
				return fmt.Errorf("unexpected </%s> inside <%s>", t.Name.Local, stack[len(stack)-1])
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			out.Write(t)
		}
	}
	return nil
}

// value applies the declared-type coercion to a decoded text value (XML values
// are text; declared fields give them types, exactly like DSV cells).
func (d *xmlDecoder) value(name, s string) canonical.Value {
	v := canonical.StringVal(s)
	if d.types != nil {
		v = coerceValue(v, d.types[name])
	}
	return v
}
