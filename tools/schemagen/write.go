package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// writePretty writes v as serde_json::to_vec_pretty does: two-space
// indent, sorted keys, no HTML escaping.
func writePretty(b *strings.Builder, v any, indent string) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case float:
		s := strconv.FormatFloat(float64(x), 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		b.WriteString(s)
	case string:
		writeString(b, x)
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range x {
			b.WriteString(indent + "  ")
			writePretty(b, e, indent+"  ")
			if i < len(x)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(indent + "]")
	case obj:
		writeObject(b, x, indent)
	case map[string]obj:
		o := obj{}
		for k, e := range x {
			o[k] = e
		}
		writeObject(b, o, indent)
	default:
		panic(fmt.Sprintf("schemagen: cannot write %T", v))
	}
}

// writeObject writes a JSON object with sorted keys.
func writeObject(b *strings.Builder, x obj, indent string) {
	if len(x) == 0 {
		b.WriteString("{}")
		return
	}
	keys := slices.Sorted(maps.Keys(x))
	b.WriteString("{\n")
	for i, k := range keys {
		b.WriteString(indent + "  ")
		writeString(b, k)
		b.WriteString(": ")
		writePretty(b, x[k], indent+"  ")
		if i < len(keys)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(indent + "}")
}

// writeString quotes s as serde_json does.
func writeString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20:
			fmt.Fprintf(b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}
