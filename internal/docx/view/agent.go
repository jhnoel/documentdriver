package view

import (
	"maps"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
)

// The agent form of the markup (AgentHTML) is emitted directly by a
// render in agent mode (RenderAgent): it writes what AgentHTML would
// leave of the full markup, from the values the full markup is written
// from, never re-reading it. AgentHTML over Render stays the reference
// (TestAgentRender).

// noteKey names a footnote or endnote by kind and id.
type noteKey struct {
	kind, id string
}

// sink collects, for one stretch of agent markup (a part's body, a
// header or footer, a note body), what the full markup's later string
// passes would find in it: the note links it holds, the style ids its
// paragraphs name, and whether agent mode left anything of the full
// markup out.
type sink struct {
	refs    map[noteKey]bool
	classes map[string]bool
	sources map[string]bool
	omitted bool
}

// newSink returns an empty sink for collecting agent-markup references, classes and sources.
func newSink() *sink {
	return &sink{refs: map[noteKey]bool{}, classes: map[string]bool{}, sources: map[string]bool{}}
}

// add merges what s holds into d.
func (d *sink) add(s *sink) {
	maps.Copy(d.refs, s.refs)
	maps.Copy(d.classes, s.classes)
	maps.Copy(d.sources, s.sources)
}

// decoded is an attribute value as AgentHTML reads it back from the
// markup: escapeAttr(v) parsed as a double-quoted XML attribute, which
// normalises white space (decodeAttr), false when it does not parse.
func (r *renderer) decoded(v string) (string, bool) {
	if plainASCII(v) {
		return v, true
	}
	if d, ok := r.decodedAttrs[v]; ok {
		return d.v, d.ok
	}
	s, ok := decodeAttr(escapeAttr(v))
	r.decodedAttrs[v] = decodedAttr{s, ok}
	return s, ok
}

type decodedAttr struct {
	v  string
	ok bool
}

// plainASCII reports whether v is printable ASCII, which an attribute
// reads back unchanged.
func plainASCII(v string) bool {
	for i := range len(v) {
		if v[i] < 0x20 || v[i] >= 0x7f {
			return false
		}
	}
	return true
}

// agentStyle is the style attribute agent markup gives a paragraph whose
// full markup has style css (built from the declarations m) and names
// style class when hasClass: only the declarations that differ from the
// named style's, with "unset" for one the style has and the paragraph
// lacks (cssDelta), the full css when the style is unknown, and none
// when nothing differs.
func (c *partCtx) agentStyle(css string, m map[string]string, class string, hasClass bool) string {
	s := c.r.styles
	var base map[string]string
	var baseCSS string
	hasBase := false
	if hasClass {
		if dec, ok := c.r.decoded(class); ok {
			base, baseCSS, hasBase = s.catalogDecls(dec)
		}
	} else if s.hasDef {
		base, baseCSS, hasBase = s.catalogDecls(s.def)
	}
	if !hasBase {
		return ` style="` + escapeAttr(css) + `"`
	}
	var delta string
	if strings.ContainsAny(css, `'"`) || strings.ContainsAny(baseCSS, `'"`) || !plainASCII(css) {
		// cssPairs splits only outside quotes; read back as the
		// reference does.
		dec, ok := c.r.decoded(css)
		if !ok {
			return ` style="` + escapeAttr(css) + `"`
		}
		delta = cssDelta(dec, baseCSS)
	} else {
		delta = declDelta(m, base)
	}
	if delta == "" {
		c.sink.omitted = true
		return ""
	}
	return ` style="` + escapeAttr(delta) + `"`
}

// declDelta is cssDelta on declaration maps: the declarations of e that
// b does not already give, and "unset" for those of b e lacks.
func declDelta(e, b map[string]string) string {
	var keys []string
	for k, v := range e {
		if bv, ok := b[k]; !ok || bv != v {
			keys = append(keys, k)
		}
	}
	for k := range b {
		if _, ok := e[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var out strings.Builder
	for _, k := range keys {
		v, ok := e[k]
		if !ok {
			v = "unset"
		}
		out.WriteString(k + ":" + v + ";")
	}
	return out.String()
}

// catalogDecls is style id's resolved declarations (Catalog's entry) as
// a map and as CSS, and false when there is no such style. The maps are
// shared: callers must not modify them.
func (s *Styles) catalogDecls(id string) (map[string]string, string, bool) {
	if _, ok := s.nodes[id]; !ok {
		return nil, "", false
	}
	if e, ok := s.catalog[id]; ok {
		return e.decls, e.css, true
	}
	m := map[string]string{}
	s.defaults(false, m)
	s.applyChain(id, true, false, m)
	s.defaults(true, m)
	s.applyChain(id, true, true, m)
	e := catalogEntry{m, cssString(m)}
	s.catalog[id] = e
	return e.decls, e.css, true
}

type catalogEntry struct {
	decls map[string]string
	css   string
}

// RenderAgent renders the document in view v as the markup agents read:
// Render's markup as AgentHTML leaves it, emitted directly. StyleIDs of
// the result are the style ids its markup names (StyleIDs).
func RenderAgent(d *model.Document, v View, opt Options) (*Rendered, error) {
	return renderView(d, v, opt, true)
}
