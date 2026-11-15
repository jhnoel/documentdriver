package html

import "strings"

// The note helpers of docxdriver's html/surface.rs the dialect parser uses.

// CanonicalizeNoteInner returns the canonical body of a footnote or endnote:
// "" when blank, the bare text of a single plain one-line paragraph, and
// otherwise the block markup with paragraph id/ord attributes removed.
func CanonicalizeNoteInner(rendered string) string {
	trimmed := strings.TrimSpace(rendered)
	if trimmed == "" {
		return ""
	}
	stripped := StripParaAddressAttrs(trimmed)
	if rest, ok := strings.CutPrefix(stripped, "<p>"); ok {
		if inner, ok := strings.CutSuffix(rest, "</p>"); ok &&
			!strings.Contains(inner, "<") && !strings.Contains(stripped, "\n") {
			return inner
		}
	}
	return stripped
}

// isASCIIAlnumByte reports whether c is an ASCII letter or digit.
func isASCIIAlnumByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// StripParaAddressAttrs removes ` id="…"` and ` ord="…"` from every
// paragraph (p, h1..h6) opening tag in markup, keeping everything else.
func StripParaAddressAttrs(markup string) string {
	var out strings.Builder
	out.Grow(len(markup))
	rest := markup
	for {
		lt := strings.IndexByte(rest, '<')
		if lt < 0 {
			break
		}
		out.WriteString(rest[:lt])
		after := rest[lt+1:]
		nameEnd := len(after)
		for i := 0; i < len(after); i++ {
			if !isASCIIAlnumByte(after[i]) {
				nameEnd = i
				break
			}
		}
		name := after[:nameEnd]
		isPara := name == "p" || (len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6')
		if isPara && nameEnd < len(after) && after[nameEnd] == ' ' {
			tagEnd := len(after)
			if k := strings.IndexByte(after, '>'); k >= 0 {
				tagEnd = k + 1
			}
			out.WriteByte('<')
			out.WriteString(stripOneAttr(stripOneAttr(after[:tagEnd], "id"), "ord"))
			rest = after[tagEnd:]
		} else {
			out.WriteByte('<')
			rest = after
		}
	}
	out.WriteString(rest)
	return out.String()
}

// stripOneAttr removes attribute attr occurrences from tag markup.
func stripOneAttr(tag, attr string) string {
	needle := " " + attr + `="`
	var out strings.Builder
	rest := tag
	for {
		pos := strings.Index(rest, needle)
		if pos < 0 {
			break
		}
		out.WriteString(rest[:pos])
		after := rest[pos+len(needle):]
		_, after0, ok := strings.Cut(after, "\"")
		if !ok {
			out.WriteString(rest[pos:])
			rest = ""
			continue
		}
		rest = after0
	}
	out.WriteString(rest)
	return out.String()
}
