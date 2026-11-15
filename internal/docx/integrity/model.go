package integrity

import (
	"cmp"
	"regexp"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
)

// The WordprocessingML content model comes from internal/ooxml/schema
// (schema.ContentModel, schema.RootType, schema.SimpleType), generated
// from the pinned ECMA-376 schemas by the algorithm docxdriver's
// scripts/generate-wml-model.py used. contentModel adapts it to the
// checker: namespaces for prefixes, children and attributes by name.

// tablePrefixes are the table prefixes the model uses, by namespace.
var tablePrefixes = map[string]string{
	wNS: "w", rNS: "r", xmlNS: "xml", wpNS: "wp",
	"http://schemas.openxmlformats.org/officeDocument/2006/math":        "m",
	"http://schemas.openxmlformats.org/officeDocument/2006/sharedTypes": "s",
	"http://schemas.openxmlformats.org/schemaLibrary/2006/main":         "sl",
	"http://www.w3.org/2001/XMLSchema":                                  "xsd",
}

// namespaceOf returns the namespace URI for prefix, or empty when unknown.
func namespaceOf(prefix string) string {
	for ns, p := range tablePrefixes {
		if p == prefix {
			return ns
		}
	}
	return ""
}

type qname struct {
	ns, local string
}

// compareQNames orders qualified names by namespace then local name.
func compareQNames(a, b qname) int {
	return cmp.Or(strings.Compare(a.ns, b.ns), strings.Compare(a.local, b.local))
}

type childModel struct {
	rank     int // schema.NoRank: none
	max      int // schema.Unbounded or a count
	kind     string
	required bool
	index    int // the child's position in its typeModel's order
}

type attrModel struct {
	simple   string
	required bool
}

type typeModel struct {
	children map[qname]childModel
	order    []qname // the children's names, sorted, for stable reporting
	anyNS    []string
	attrs    map[qname]attrModel
	attrKeys []qname
	anyAttr  bool
}

// contentModel reads the tables for one check, caching the complex types
// it adapted and the patterns it compiled.
type contentModel struct {
	types    map[string]*typeModel
	patterns map[string]*regexp.Regexp
}

// newContentModel returns an empty schema content model.
func newContentModel() *contentModel {
	return &contentModel{types: map[string]*typeModel{}, patterns: map[string]*regexp.Regexp{}}
}

// typeModel returns the model of complex type kind ("w:CT_P").
func (m *contentModel) typeModel(kind string) (*typeModel, bool) {
	if tm, ok := m.types[kind]; ok {
		return tm, tm != nil
	}
	sm, ok := schema.ContentModel(kind)
	if !ok {
		m.types[kind] = nil
		return nil, false
	}
	tm := &typeModel{children: map[qname]childModel{}, anyNS: sm.Wildcards, attrs: map[qname]attrModel{}, anyAttr: sm.AnyAttr}
	for _, c := range sm.Children {
		key := qname{namespaceOf(c.Prefix), c.Local}
		tm.children[key] = childModel{rank: c.Rank, max: c.Max, kind: c.Type, required: c.Required}
		tm.order = append(tm.order, key)
	}
	slices.SortFunc(tm.order, compareQNames)
	for i, key := range tm.order {
		cm := tm.children[key]
		cm.index = i
		tm.children[key] = cm
	}
	for _, a := range sm.Attrs {
		key := qname{namespaceOf(a.Prefix), a.Local}
		tm.attrs[key] = attrModel{simple: a.Type, required: a.Required}
		tm.attrKeys = append(tm.attrKeys, key)
	}
	slices.SortFunc(tm.attrKeys, compareQNames)
	m.types[kind] = tm
	return tm, true
}

// rootType returns the complex type of a part's root element.
func rootType(ns, local string) (string, bool) {
	if ns != wNS {
		return "", false
	}
	return schema.RootType("w", local)
}

// pattern returns a compiled pattern facet. The tables hold only
// patterns their generator compiled, so the error branch matches what
// docxdriver did with a pattern it could not compile: everything.
func (m *contentModel) pattern(p string) *regexp.Regexp {
	if re, ok := m.patterns[p]; ok {
		return re
	}
	re, err := regexp.Compile("^(?:" + p + ")$")
	if err != nil {
		re = nil
	}
	m.patterns[p] = re
	return re
}

// label names an element or attribute by the table prefix of its
// namespace, or {namespace}local.
func label(ns, local string) string {
	if p, ok := tablePrefixes[ns]; ok {
		return p + ":" + local
	}
	if ns == "" {
		return local
	}
	return "{" + ns + "}" + local
}
