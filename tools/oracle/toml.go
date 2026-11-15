package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// jv is a JSON value that keeps what rendering a plan as TOML needs and
// encoding/json's generic decoding loses: the order of object members and
// the literal text of numbers (an integer stays a TOML integer, 72.0 stays
// a float).
type jv struct {
	kind    byte // 'o' object, 'a' array, 's' string, 'n' number, 'b' bool, 'z' null
	members []jmember
	items   []*jv
	s       string // string value or number literal
	b       bool
}

type jmember struct {
	key string
	val *jv
}

// parseJV decodes one JSON value, keeping member order and number text.
func parseJV(data []byte) (*jv, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeJV(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after JSON value")
	}
	return v, nil
}

// decodeJV decodes one JSON value from dec into a jv.
func decodeJV(dec *json.Decoder) (*jv, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			v := &jv{kind: 'o'}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("object key %v is not a string", kt)
				}
				val, err := decodeJV(dec)
				if err != nil {
					return nil, err
				}
				v.members = append(v.members, jmember{key, val})
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return v, nil
		case '[':
			v := &jv{kind: 'a'}
			for dec.More() {
				item, err := decodeJV(dec)
				if err != nil {
					return nil, err
				}
				v.items = append(v.items, item)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return v, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	case string:
		return &jv{kind: 's', s: t}, nil
	case json.Number:
		return &jv{kind: 'n', s: string(t)}, nil
	case bool:
		return &jv{kind: 'b', b: t}, nil
	case nil:
		return &jv{kind: 'z'}, nil
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

// get returns the member key of an object, or nil.
func (v *jv) get(key string) *jv {
	if v == nil || v.kind != 'o' {
		return nil
	}
	for _, m := range v.members {
		if m.key == key {
			return m.val
		}
	}
	return nil
}

// planTOML renders a plan (a JSON object) as the TOML docxdriver's CLI
// reads, in the layout of docxdriver's own canonical_toml: the top-level
// scalars, then one [[ops]] table per operation whose nested objects and
// arrays are inline. TOML has no null, so a null member is left out
// (docxdriver's optional fields default to none); a plan that is not an
// object with an ops array of objects is rendered as top-level keys only,
// for the negative cases that break that shape.
func planTOML(plan *jv) (string, error) {
	if plan.kind != 'o' {
		return "", errors.New("plan is not a JSON object")
	}
	var b strings.Builder
	var ops *jv
	for _, m := range plan.members {
		if m.key == "ops" && m.val.kind == 'a' && allObjects(m.val.items) && len(m.val.items) > 0 {
			ops = m.val
			continue
		}
		if m.val.kind == 'z' {
			continue
		}
		s, err := tomlValue(m.val)
		if err != nil {
			return "", fmt.Errorf("%s: %w", m.key, err)
		}
		fmt.Fprintf(&b, "%s = %s\n", tomlKey(m.key), s)
	}
	if ops != nil {
		for _, op := range ops.items {
			b.WriteString("\n[[ops]]\n")
			for _, m := range op.members {
				if m.val.kind == 'z' {
					continue
				}
				s, err := tomlValue(m.val)
				if err != nil {
					return "", fmt.Errorf("ops.%s: %w", m.key, err)
				}
				fmt.Fprintf(&b, "%s = %s\n", tomlKey(m.key), s)
			}
		}
	}
	return b.String(), nil
}

// allObjects reports whether every item is a JSON object.
func allObjects(items []*jv) bool {
	for _, it := range items {
		if it.kind != 'o' {
			return false
		}
	}
	return true
}

// tomlValue renders a value inline.
func tomlValue(v *jv) (string, error) {
	switch v.kind {
	case 's':
		return tomlString(v.s), nil
	case 'b':
		return strconv.FormatBool(v.b), nil
	case 'n':
		return tomlNumber(v.s), nil
	case 'a':
		parts := make([]string, 0, len(v.items))
		for _, it := range v.items {
			if it.kind == 'z' {
				return "", errors.New("null array element has no TOML form")
			}
			s, err := tomlValue(it)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case 'o':
		parts := make([]string, 0, len(v.members))
		for _, m := range v.members {
			if m.val.kind == 'z' {
				continue
			}
			s, err := tomlValue(m.val)
			if err != nil {
				return "", err
			}
			parts = append(parts, tomlKey(m.key)+" = "+s)
		}
		if len(parts) == 0 {
			return "{}", nil
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	case 'z':
		return "", errors.New("null has no TOML form")
	}
	return "", fmt.Errorf("unknown value kind %q", v.kind)
}

// tomlNumber renders a JSON number literal. An integer literal that fits
// TOML's signed 64-bit integers stays an integer; any other number is a
// float (TOML floats need a digit before an exponent, which JSON also
// requires, so a JSON float literal is already a TOML float). A
// too-large integer becomes the float docxdriver's serde_json would read
// it as.
func tomlNumber(lit string) string {
	if !strings.ContainsAny(lit, ".eE") {
		if _, err := strconv.ParseInt(lit, 10, 64); err == nil {
			return lit
		}
		return lit + ".0"
	}
	return lit
}

// tomlKey quotes a key unless it is a bare key.
func tomlKey(k string) string {
	if k == "" {
		return `""`
	}
	for _, r := range k {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return tomlString(k)
		}
	}
	return k
}

// tomlString renders a TOML basic string. TOML forbids control
// characters (other than tab) and DEL in basic strings; they are escaped.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// encodeJV writes v as compact JSON in member order, without HTML
// escaping.
func encodeJV(v *jv) string {
	var b strings.Builder
	writeJV(&b, v)
	return b.String()
}

// writeJV writes v to b as compact JSON in member order.
func writeJV(b *strings.Builder, v *jv) {
	switch v.kind {
	case 'o':
		b.WriteByte('{')
		for i, m := range v.members {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(jsonString(m.key))
			b.WriteByte(':')
			writeJV(b, m.val)
		}
		b.WriteByte('}')
	case 'a':
		b.WriteByte('[')
		for i, it := range v.items {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJV(b, it)
		}
		b.WriteByte(']')
	case 's':
		b.WriteString(jsonString(v.s))
	case 'n':
		b.WriteString(v.s)
	case 'b':
		b.WriteString(strconv.FormatBool(v.b))
	default:
		b.WriteString("null")
	}
}

// jsonString encodes s as a JSON string without HTML escaping.
func jsonString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return `""` // unreachable: a Go string always encodes
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
