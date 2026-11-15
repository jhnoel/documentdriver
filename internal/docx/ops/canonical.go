package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// CanonicalJSON is the plan's canonical JSON, byte for byte what
// docxdriver 0.5.2's canonical_json writes: keys sorted, nulls left out,
// values normalised, Points as an integer when whole, SignedPoints and
// other floats as serde_json writes f64, no HTML or U+2028/U+2029
// escaping. The plan must come from ParsePlan or ParsePlanJSON.
func (p *Plan) CanonicalJSON() string {
	var b strings.Builder
	writeCanonical(&b, encodeStruct(reflectOf(p)))
	return b.String()
}

// PreviewKey is docxdriver's preview key for the plan against its base:
// "p1:sha256:" and the hex SHA-256 of the domain tag, the protocol
// version, the base and the canonical JSON.
func (p *Plan) PreviewKey() string {
	h := sha256.New()
	h.Write([]byte("docxdriver-plan-preview\x00"))
	h.Write([]byte("1"))
	h.Write([]byte{0})
	h.Write([]byte(p.Base))
	h.Write([]byte{0})
	h.Write([]byte(p.CanonicalJSON()))
	return "p1:sha256:" + hex.EncodeToString(h.Sum(nil))
}

// writeCanonical writes v as serde_json::to_string writes a Value whose
// maps are BTreeMaps.
func writeCanonical(b *strings.Builder, v *value) {
	switch v.kind {
	case kNull:
		b.WriteString("null")
	case kBool:
		b.WriteString(strconv.FormatBool(v.b))
	case kU64:
		b.WriteString(strconv.FormatUint(v.u, 10))
	case kI64:
		b.WriteString(strconv.FormatInt(v.i, 10))
	case kF64:
		b.WriteString(formatFloat(v.f))
	case kString:
		writeString(b, v.s)
	case kArray:
		b.WriteByte('[')
		for i, e := range v.a {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonical(b, e)
		}
		b.WriteByte(']')
	case kObject:
		ms := slices.Clone(v.m)
		slices.SortStableFunc(ms, func(x, y member) int { return strings.Compare(x.key, y.key) })
		b.WriteByte('{')
		first := true
		for _, m := range ms {
			if m.val.kind == kNull {
				continue
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			writeString(b, m.key)
			b.WriteByte(':')
			writeCanonical(b, m.val)
		}
		b.WriteByte('}')
	}
}

// writeString quotes s as serde_json does: only '"', '\\' and control
// characters are escaped.
func writeString(b *strings.Builder, s string) {
	const hexDigits = "0123456789abcdef"
	b.WriteByte('"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		b.WriteString(s[start:i])
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteString(`\u00`)
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0xf])
		}
		start = i + 1
	}
	b.WriteString(s[start:])
	b.WriteByte('"')
}

// encoder is implemented by plan types with their own wire form.
type encoder interface {
	encodeJSON() *value
}

// fieldInfo describes one struct field's wire form.
type fieldInfo struct {
	index    int
	name     string
	optional bool // pointer, or a default tag
	omitzero bool // skip_serializing_if: left out of the output when zero
}

// fieldsOf returns the JSON wire fields of struct type t.
func fieldsOf(t reflect.Type) []fieldInfo {
	var out []fieldInfo
	for i := range t.NumField() {
		f := t.Field(i)
		tag, ok := f.Tag.Lookup("json")
		if !ok || tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		_, hasDefault := f.Tag.Lookup("default")
		out = append(out, fieldInfo{
			index:    i,
			name:     name,
			optional: f.Type.Kind() == reflect.Pointer || hasDefault,
			omitzero: opts == "omitzero",
		})
	}
	return out
}

// encodeValue builds the wire form of rv: struct fields in any order
// (the canonical writer sorts them), nil pointers left out.
func encodeValue(rv reflect.Value) *value {
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return &value{kind: kNull}
		}
		return encodeValue(rv.Elem())
	}
	if rv.CanAddr() {
		if e, ok := rv.Addr().Interface().(encoder); ok {
			return e.encodeJSON()
		}
	}
	if e, ok := rv.Interface().(encoder); ok {
		return e.encodeJSON()
	}
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return &value{kind: kNull}
		}
		return encodeValue(rv.Elem())
	case reflect.String:
		return &value{kind: kString, s: rv.String()}
	case reflect.Bool:
		return &value{kind: kBool, b: rv.Bool()}
	case reflect.Uint32, reflect.Uint8:
		return &value{kind: kU64, u: rv.Uint()}
	case reflect.Float64:
		return &value{kind: kF64, f: rv.Float()}
	case reflect.Slice:
		out := &value{kind: kArray, a: []*value{}}
		for i := range rv.Len() {
			out.a = append(out.a, encodeValue(rv.Index(i)))
		}
		return out
	case reflect.Struct:
		return encodeStruct(rv)
	}
	return &value{kind: kNull}
}

// encodeStruct returns the wire object for struct value rv.
func encodeStruct(rv reflect.Value) *value {
	out := &value{kind: kObject}
	for _, f := range fieldsOf(rv.Type()) {
		fv := rv.Field(f.index)
		if f.omitzero && fv.IsZero() {
			continue
		}
		e := encodeValue(fv)
		if e.kind == kNull {
			continue
		}
		out.m = append(out.m, member{key: f.name, val: e})
	}
	return out
}

// formatFloat formats a finite f64 as serde_json 1.0.150 writes it (the
// zmij crate): the shortest round-trip digits, positional when the
// decimal exponent is in -5..=15 (whole values end in ".0"), otherwise
// scientific with an explicit exponent sign ("1e+16", "1.5e-7").
func formatFloat(f float64) string {
	if f == 0 {
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	var b strings.Builder
	if f < 0 {
		b.WriteByte('-')
		f = -f
	}
	sci := strconv.FormatFloat(f, 'e', -1, 64) // d.ddde±XX
	mant, expText, _ := strings.Cut(sci, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, err := strconv.Atoi(expText)
	if err != nil {
		return sci // unreachable: FormatFloat always writes a decimal exponent
	}
	n := len(digits)
	switch {
	case exp >= -5 && exp <= 15 && n-1 <= exp:
		b.WriteString(digits)
		b.WriteString(strings.Repeat("0", exp+1-n))
		b.WriteString(".0")
	case exp >= 0 && exp <= 15:
		b.WriteString(digits[:exp+1])
		b.WriteByte('.')
		b.WriteString(digits[exp+1:])
	case exp >= -5 && exp < 0:
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", -exp-1))
		b.WriteString(digits)
	default:
		b.WriteByte(digits[0])
		if n > 1 {
			b.WriteByte('.')
			b.WriteString(digits[1:])
		}
		b.WriteByte('e')
		if exp >= 0 {
			b.WriteByte('+')
		} else {
			b.WriteByte('-')
			exp = -exp
		}
		b.WriteString(strconv.Itoa(exp))
	}
	return b.String()
}
