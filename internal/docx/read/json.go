package read

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Object is a JSON object. Like docxdriver's serde_json maps, its keys
// are written in sorted (byte) order.
type Object = map[string]any

// Encode writes v as JSON exactly as docxdriver's serde_json does:
// pretty-printed with two-space indents, or compact. v may hold nil, bool,
// string, the integer types, []any, []Object, []string and Object.
// Strings escape only '"', '\\' and control characters (\b, \f, \n, \r,
// \t, else \u00xx with lowercase hex); everything else is written as is.
func Encode(v any, pretty bool) (string, error) {
	var b strings.Builder
	if err := encode(&b, v, pretty, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

// encode writes v as JSON to builder b, reporting unsupported types.
func encode(b *strings.Builder, v any, pretty bool, depth int) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case string:
		writeString(b, x)
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case uint32:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		b.WriteString(strconv.FormatUint(x, 10))
	case json.Number:
		b.WriteString(string(x)) // a number carried as its text (FromOracle)
	case []string:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		return encodeArray(b, items, pretty, depth)
	case []Object:
		items := make([]any, len(x))
		for i, o := range x {
			items[i] = o
		}
		return encodeArray(b, items, pretty, depth)
	case []any:
		return encodeArray(b, x, pretty, depth)
	case Object:
		return encodeObject(b, x, pretty, depth)
	default:
		return fmt.Errorf("read: cannot encode %T as JSON", v)
	}
	return nil
}

// newline writes a newline and depth indent to builder b.
func newline(b *strings.Builder, depth int) {
	b.WriteByte('\n')
	for range depth {
		b.WriteString("  ")
	}
}

// encodeArray writes items as a JSON array, reporting an encoding error.
func encodeArray(b *strings.Builder, items []any, pretty bool, depth int) error {
	if len(items) == 0 {
		b.WriteString("[]")
		return nil
	}
	b.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		if pretty {
			newline(b, depth+1)
		}
		if err := encode(b, it, pretty, depth+1); err != nil {
			return err
		}
	}
	if pretty {
		newline(b, depth)
	}
	b.WriteByte(']')
	return nil
}

// encodeObject writes object members sorted by key as JSON, reporting errors.
func encodeObject(b *strings.Builder, o Object, pretty bool, depth int) error {
	if len(o) == 0 {
		b.WriteString("{}")
		return nil
	}
	keys := slices.Sorted(maps.Keys(o))
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		if pretty {
			newline(b, depth+1)
		}
		writeString(b, k)
		b.WriteByte(':')
		if pretty {
			b.WriteByte(' ')
		}
		if err := encode(b, o[k], pretty, depth+1); err != nil {
			return err
		}
	}
	if pretty {
		newline(b, depth)
	}
	b.WriteByte('}')
	return nil
}

const hexDigits = "0123456789abcdef"

// writeString writes s as a quoted JSON string to builder b.
func writeString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0xF])
		case c < utf8.RuneSelf:
			b.WriteByte(c)
		default:
			// Invalid UTF-8 cannot come from the parsed parts (the
			// tokenizer checks it); write U+FFFD as Rust's lossy
			// conversions would.
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n == 1 {
				b.WriteString("�")
			} else {
				b.WriteString(s[i : i+n])
			}
			i += n
			continue
		}
		i++
	}
	b.WriteByte('"')
}
