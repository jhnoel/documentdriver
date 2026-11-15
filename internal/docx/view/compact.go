package view

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// isASCIISpace reports whether c is an ASCII space character.
func isASCIISpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

// isASCIIAlpha reports whether c is an ASCII letter.
func isASCIIAlpha(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

// attr is one attribute of a generated start tag: its byte range (from the
// whitespace before it), name and raw value.
type attr struct {
	start, end  int
	name, value string
}

// startTag is a generated start tag: its name, byte range and attributes.
type startTag struct {
	name       string
	start, end int
	attrs      []attr
}

// scanTags finds the start tags of generated HTML, respecting quoted
// values (html/compact.rs tags). Text is never parsed as markup.
func scanTags(html string) []startTag {
	b := html
	var out []startTag
	for p := 0; p < len(b); {
		if b[p] != '<' || p+1 >= len(b) || !isASCIIAlpha(b[p+1]) {
			p++
			continue
		}
		t := startTag{start: p}
		p++
		for p < len(b) && !isASCIISpace(b[p]) && b[p] != '>' {
			p++
		}
		t.name = strings.TrimRight(b[t.start+1:p], "/")
		for p < len(b) && b[p] != '>' {
			start := p
			for p < len(b) && isASCIISpace(b[p]) {
				p++
			}
			ns := p
			for p < len(b) && !isASCIISpace(b[p]) && b[p] != '=' && b[p] != '>' && b[p] != '/' {
				p++
			}
			name := b[ns:p]
			if name == "" {
				if p < len(b) && b[p] != '>' {
					p++
				}
				continue
			}
			for p < len(b) && isASCIISpace(b[p]) {
				p++
			}
			value := ""
			if p < len(b) && b[p] == '=' {
				p++
				for p < len(b) && isASCIISpace(b[p]) {
					p++
				}
				if p < len(b) && (b[p] == '\'' || b[p] == '"') {
					q := b[p]
					p++
					vs := p
					for p < len(b) && b[p] != q {
						p++
					}
					value = b[vs:p]
					if p < len(b) {
						p++
					}
				} else {
					vs := p
					for p < len(b) && !isASCIISpace(b[p]) && b[p] != '>' {
						p++
					}
					value = b[vs:p]
				}
			}
			t.attrs = append(t.attrs, attr{start, p, name, value})
		}
		if p < len(b) {
			p++
		}
		t.end = p
		out = append(out, t)
	}
	return out
}

// modelHTML drops the presentation-only attributes (data-ord and
// contenteditable) from every start tag, keeping
// text and attribute values as they are (html/compact.rs model_html).
func modelHTML(html string) string {
	var out strings.Builder
	out.Grow(len(html))
	from := 0
	for _, t := range scanTags(html) {
		out.WriteString(html[from:t.start])
		nameEnd := t.start + 1 + len(t.name)
		for nameEnd < len(html) && !isASCIISpace(html[nameEnd]) && html[nameEnd] != '>' {
			nameEnd++
		}
		out.WriteString(html[t.start:nameEnd])
		pos := nameEnd
		for _, a := range t.attrs {
			if a.start > pos {
				out.WriteString(html[pos:a.start]) // stray bytes the scanner skipped
			}
			raw := html[a.start:a.end]
			if a.name != "data-ord" && a.name != "contenteditable" {
				out.WriteString(raw)
			}
			pos = a.end
		}
		if pos < t.end {
			out.WriteString(html[pos:t.end])
		}
		from = t.end
	}
	out.WriteString(html[from:])
	return out.String()
}

type edit struct {
	start, end int
	with       string
}

// rewrite returns html with each edit's replacement spliced into its byte range.
func rewrite(html string, edits []edit) string {
	var out strings.Builder
	from := 0
	for _, e := range edits {
		out.WriteString(html[from:e.start])
		out.WriteString(e.with)
		from = e.end
	}
	out.WriteString(html[from:])
	return out.String()
}

// AssetAliases gives each asset a short alias URL "assets/i<n>.<ext>"
// (its canonical URL moves to asset_url) and rewrites the src attributes
// that name a canonical URL (html/compact.rs asset_aliases).
func AssetAliases(html string, assets []any) string {
	return AliasSources(html, AliasAssets(assets))
}

// AliasAssets gives each asset its alias URL (AssetAliases) and returns
// the aliases by canonical URL.
func AliasAssets(assets []any) map[string]string {
	aliases := map[string]string{}
	for i, a := range assets {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		url, ok := m["url"].(string)
		if !ok {
			continue
		}
		ext := url[strings.LastIndexByte(url, '.')+1:]
		alias := "assets/i" + strconv.Itoa(i+1) + "." + ext
		m["url"] = alias
		m["asset_url"] = url
		aliases[url] = alias
	}
	return aliases
}

// AliasSources rewrites the src attributes of html that name a canonical
// URL to its alias.
func AliasSources(html string, aliases map[string]string) string {
	var edits []edit
	for _, t := range scanTags(html) {
		for _, a := range t.attrs {
			if a.name != "src" {
				continue
			}
			if alias, ok := aliases[a.value]; ok {
				edits = append(edits, edit{a.start, a.end, ` src="` + alias + `"`})
			}
		}
	}
	return rewrite(html, edits)
}

var paragraphTags = map[string]bool{"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true}

// AgentHTML is the markup agents read (html/compact.rs agent_html): a
// paragraph's style lists only what differs from its named style, the
// default style's id is dropped, a list number repeating its item's marker
// is dropped, note links and bodies lose their page-scoped DOM ids, and
// the presentation-only attributes go.
func AgentHTML(html string, styles map[string]string, defaultStyle string, hasDefault bool) string {
	defaultAttr := escapeAttr(defaultStyle)
	var edits []edit
	var prev *startTag
	tags := scanTags(html)
	for i := range tags {
		t := &tags[i]
		get := func(name string) (string, bool) {
			for _, a := range t.attrs {
				if a.name == name {
					return a.value, true
				}
			}
			return "", false
		}
		paragraph := paragraphTags[t.name]
		var base string
		hasBase := false
		if paragraph {
			if id, ok := get("data-style"); ok {
				if dec, ok := decodeAttr(id); ok {
					base, hasBase = styles[dec]
				}
			} else if hasDefault {
				base, hasBase = styles[defaultStyle]
			}
		}
		adjacent := func(tag, name string) (string, bool) {
			if prev == nil || prev.end != t.start || prev.name != tag {
				return "", false
			}
			for _, a := range prev.attrs {
				if a.name == name {
					return a.value, true
				}
			}
			return "", false
		}
		for _, a := range t.attrs {
			drop := false
			switch a.name {
			case "style":
				dec, ok := decodeAttr(a.value)
				if !hasBase || !ok {
					continue
				}
				if delta := cssDelta(dec, base); delta != "" {
					edits = append(edits, edit{a.start, a.end, ` style="` + escapeAttr(delta) + `"`})
					continue
				}
				drop = true
			case "data-style":
				drop = paragraph && hasDefault && defaultAttr == a.value
			case "data-number":
				m, ok := adjacent("li", "data-marker")
				drop = t.name == "p" && ok && m == a.value
			case "href":
				_, ok := adjacent("sup", "data-note")
				drop = t.name == "a" && strings.HasPrefix(a.value, "#docx-") && ok
			case "id":
				_, ok := get("data-note-body")
				drop = t.name == "div" && strings.HasPrefix(a.value, "docx-") && ok
			}
			if drop {
				edits = append(edits, edit{a.start, a.end, ""})
			}
		}
		prev = t
	}
	return modelHTML(rewrite(html, edits))
}

// StyleIDs returns the decoded data-style ids html names.
func StyleIDs(html string) map[string]bool {
	out := map[string]bool{}
	for _, t := range scanTags(html) {
		for _, a := range t.attrs {
			if a.name == "data-style" {
				if v, ok := decodeAttr(a.value); ok {
					out[v] = true
				}
			}
		}
	}
	return out
}

// decodeAttr resolves an attribute value's character references the way
// an XML parser reads it in a double-quoted attribute, and reports false
// when the value would not parse.
func decodeAttr(v string) (string, bool) {
	d, err := tree.Parse([]byte(`<x v="` + v + `"/>`))
	if err != nil {
		return "", false
	}
	r := d.Root()
	if r == nil {
		return "", false
	}
	return r.Attr("v")
}

// cssPairs splits "prop:value;" pairs, only on separators outside quotes.
func cssPairs(css string) map[string]string {
	out := map[string]string{}
	var quote rune
	from := 0
	split := func(i int) {
		if k, v, ok := strings.Cut(css[from:i], ":"); ok {
			out[k] = v
		}
		from = i + 1
	}
	for i, c := range css {
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && c == ';':
			split(i)
		}
	}
	if quote == 0 {
		split(len(css))
	}
	return out
}

// cssDelta is the declarations of effective that base does not already
// give, with "unset" for a base declaration effective lacks.
func cssDelta(effective, base string) string {
	e, b := cssPairs(effective), cssPairs(base)
	delta := map[string]string{}
	for k, v := range e {
		if bv, ok := b[k]; !ok || bv != v {
			delta[k] = v
		}
	}
	for k := range b {
		if _, ok := e[k]; !ok {
			delta[k] = "unset"
		}
	}
	var out strings.Builder
	for _, k := range slices.Sorted(maps.Keys(delta)) {
		out.WriteString(k + ":" + delta[k] + ";")
	}
	return out.String()
}
