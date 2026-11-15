package integrity

import (
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// Check returns the error kinds of a .docx package, reading it within
// opc.DefaultLimits.
func Check(data []byte) *Findings {
	return CheckWithLimits(data, opc.DefaultLimits())
}

// CheckWithLimits is Check with explicit reading limits; zero fields take
// their opc.DefaultLimits value. An entry that exceeds a limit is a
// finding, not an error.
func CheckWithLimits(data []byte, l opc.Limits) *Findings {
	l = withDefaults(l)
	return check(unzip(data, l), nil)
}

// check checks a package read by unzip. A nil scope checks every part, as docxdriver
// did. Otherwise only the parts in scope and their partners are parsed
// and checked part by part: the relationships parts (always), the main
// document (whether it parses decides what runs), the sources of the
// relationships parts in scope, and, when any WordprocessingML part that
// refers to another is in scope, all of those (wmlParts), on which the
// cross-part checks run. The package-level checks (container, content
// types, relationships) always run. Findings of parts outside the scope
// are incomplete; Gate compares scoped findings of two packages whose
// out-of-scope parts are identical.
func check(a *archive, scope map[string]bool) *Findings {
	findings := &Findings{}
	for _, f := range a.faults {
		findings.Add(f.Part, f.Message)
	}
	if !a.ok {
		return findings
	}
	entries := a.entries
	p := &pkg{
		data: map[string]string{}, lower: map[string]string{}, docs: map[string]*document{},
		defaults: map[string]string{}, overrides: map[string]string{}, rels: map[string][]relationship{},
		findings: findings, model: newContentModel(),
	}
	findings.docs = p.docs
	for _, e := range entries {
		if _, ok := p.lower[strings.ToLower(e.name)]; !ok {
			p.lower[strings.ToLower(e.name)] = e.name
		}
		p.names = append(p.names, e.name)
		p.data[e.name] = e.data
	}
	p.checkContainer()
	if !p.checkContentTypes() {
		return findings
	}
	inScope := func(string) bool { return true }
	if scope == nil {
		p.parseXMLParts(inScope)
	} else {
		parsed := map[string]bool{}
		p.parseXMLParts(func(name string) bool {
			parsed[name] = isRelsName(name)
			return parsed[name]
		})
		var main string
		scope, main = p.expandScope(scope)
		p.parseXMLParts(func(name string) bool { return (scope[name] || name == main) && !parsed[name] })
		inScope = func(name string) bool { return scope[name] }
	}
	p.checkRelationships()

	mains := p.targets("", typeIs(ctypes.RelOfficeDocument))
	if len(mains) == 0 {
		return findings
	}
	main, ok := p.actual(mains[0])
	if !ok || p.doc(main) == nil {
		return findings
	}
	for _, name := range p.names {
		if p.doc(name) == nil || !inScope(name) {
			continue
		}
		if !strings.HasSuffix(name, ".rels") {
			p.checkReferences(name)
		}
		p.checkMarkupCompatibility(name)
		checkSchema(p.doc(name), name, p.model, findings)
	}
	if scope == nil || scope[main] {
		p.checkWML(main)
	}
	return findings
}

// isRelsName reports whether name ends in .rels.
func isRelsName(name string) bool { return strings.HasSuffix(ascii.Lower(name), ".rels") }

// expandScope adds a scope's partners (see check) and returns the main
// document, which is parsed even when out of scope. The relationships
// parts are parsed.
func (p *pkg) expandScope(scope map[string]bool) (map[string]bool, string) {
	out := map[string]bool{}
	for name := range scope {
		out[name] = true
		if !isRelsName(name) {
			continue
		}
		if source, ok := relsSource(name); ok {
			if a, ok := p.actual(source); ok {
				out[a] = true
			}
		}
	}
	mains := p.targets("", typeIs(ctypes.RelOfficeDocument))
	if len(mains) == 0 {
		return out, ""
	}
	main, ok := p.actual(mains[0])
	if !ok {
		return out, ""
	}
	wml := p.wmlParts(main)
	if slices.ContainsFunc(wml, func(n string) bool { return out[n] }) {
		for _, n := range wml {
			out[n] = true
		}
	}
	return out, main
}

// stories returns the parts holding the main document's stories.
func (p *pkg) stories(main string) []story {
	stories := []story{{main, "document"}}
	for _, k := range []struct{ rel, kind string }{
		{ctypes.RelHeader, "header"}, {ctypes.RelFooter, "footer"},
		{ctypes.RelFootnotes, "footnotes"}, {ctypes.RelEndnotes, "endnotes"},
	} {
		for _, t := range p.targets(main, typeIs(k.rel)) {
			if a, ok := p.actual(t); ok {
				stories = append(stories, story{a, k.kind})
			}
		}
	}
	if c, ok := p.adoptedPart(main, typeIs(ctypes.RelComments), "comments.xml"); ok {
		stories = append(stories, story{c, "comments"})
	}
	return stories
}

// wmlParts returns every part the cross-part checks read: the stories, the
// styles, numbering and settings parts and the comment extension parts.
func (p *pkg) wmlParts(main string) []string {
	var out []string
	for _, s := range p.stories(main) {
		out = append(out, s.part)
	}
	for _, t := range p.targets(main, typeIs(ctypes.RelStyles)) {
		if a, ok := p.actual(t); ok {
			out = append(out, a)
		}
	}
	for _, a := range []struct {
		match func(string) bool
		file  string
	}{
		{typeIs(ctypes.RelNumbering), "numbering.xml"}, {typeIs(ctypes.RelSettings), "settings.xml"},
		{typeIs(ctypes.RelCommentsExtended), "commentsExtended.xml"}, {typeIs(ctypes.RelCommentsIds), "commentsIds.xml"},
		{typeIs(ctypes.RelCommentsExtensible), "commentsExtensible.xml"},
	} {
		if part, ok := p.adoptedPart(main, a.match, a.file); ok {
			out = append(out, part)
		}
	}
	return out
}

// checkWML runs the WordprocessingML checks across the main document's
// stories and the parts they refer to.
func (p *pkg) checkWML(main string) {
	stories := p.stories(main)
	commentIDs := p.checkComments(main, stories)
	p.checkStyles(main)
	for _, s := range stories {
		d := p.doc(s.part)
		if d == nil {
			continue
		}
		roots := []*element{d.root}
		if s.kind == "footnotes" || s.kind == "endnotes" || s.kind == "comments" {
			roots = nil
			for c := d.root.firstEl(); c != nil; c = c.nextEl() {
				if c.is(wNS, "footnote") || c.is(wNS, "endnote") || c.is(wNS, "comment") {
					roots = append(roots, c)
				}
			}
		}
		for _, r := range roots {
			p.checkStory(s.part, r, commentIDs)
		}
		p.checkStoryContent(s.part, s.kind, d)
		p.checkTables(s.part, d)
		p.checkIDs(s.part, d)
	}
	p.checkParagraphIDsAcrossParts(stories)
	p.checkSections(main)
	numbered := make([]string, 0, len(stories))
	for _, s := range stories {
		numbered = append(numbered, s.part)
	}
	for _, t := range p.targets(main, typeIs(ctypes.RelStyles)) {
		if a, ok := p.actual(t); ok {
			numbered = append(numbered, a)
		}
	}
	p.checkNumbering(main, numbered)
	p.checkNotes(main, stories)
}

// story is a part holding WordprocessingML stories, and its kind.
type story struct {
	part, kind string
}

// withDefaults fills unset OPC limits with the defaults.
func withDefaults(l opc.Limits) opc.Limits {
	d := opc.DefaultLimits()
	if l.MaxPartSize <= 0 {
		l.MaxPartSize = d.MaxPartSize
	}
	if l.MaxTotalSize <= 0 {
		l.MaxTotalSize = d.MaxTotalSize
	}
	if l.MaxRatio <= 0 {
		l.MaxRatio = d.MaxRatio
	}
	if l.RatioMinSize <= 0 {
		l.RatioMinSize = d.RatioMinSize
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = d.MaxEntries
	}
	return l
}

// checkContainer reports invalid part names and case-duplicate names.
func (p *pkg) checkContainer() {
	seen := map[string]int{}
	for _, name := range p.names {
		seen[strings.ToLower(name)]++
		if partNameFault(name) {
			p.findings.Add(name, "package: invalid part name")
		}
	}
	var dups []string
	for n, c := range seen {
		if c > 1 {
			dups = append(dups, n)
		}
	}
	slices.Sort(dups)
	for _, n := range dups {
		p.add(n, "duplicate part name (part names are case-insensitive)")
	}
}

// checkContentTypes validates the content types part and reports whether it parsed.
func (p *pkg) checkContentTypes() bool {
	raw, ok := p.data[contentTypesName]
	if !ok {
		p.add(contentTypesName, "missing or malformed content types part")
		return false
	}
	d, msg := parsePart(raw)
	if msg != "" {
		p.findings.Add(contentTypesName, "xml: "+msg)
		p.add(contentTypesName, "missing or malformed content types part")
		return false
	}
	defaultSeen, overrideSeen := map[string]bool{}, map[string]bool{}
	for n := range d.elements() {
		switch {
		case n.is(pkgTypesNS, "Default"):
			ext := strings.ToLower(n.attrOr("", "Extension"))
			if defaultSeen[ext] {
				p.add(contentTypesName, "duplicate content type Default extension", n)
			}
			defaultSeen[ext] = true
			p.defaults[ext] = n.attrOr("", "ContentType")
		case n.is(pkgTypesNS, "Override"):
			part := strings.ToLower(n.attrOr("", "PartName"))
			if overrideSeen[part] {
				p.add(contentTypesName, "duplicate content type Override part name", n)
			}
			overrideSeen[part] = true
			if _, ok := p.lower[strings.TrimLeft(part, "/")]; !ok {
				p.add(contentTypesName, "content type Override names a missing part", n)
			}
			p.overrides[part] = n.attrOr("", "ContentType")
		}
	}
	p.docs[contentTypesName] = d
	for _, name := range p.names {
		// Word parks deleted parts under [trash]/ without registering them.
		if strings.HasSuffix(name, "/") || name == contentTypesName || strings.HasPrefix(name, trashPrefix) {
			continue
		}
		if _, ok := p.contentType(name); !ok {
			p.add(name, "part has no content type")
		}
	}
	return true
}

// isXMLContentType reports whether content type ct denotes XML.
func isXMLContentType(ct string) bool {
	base, _, _ := strings.Cut(ct, ";")
	base = ascii.Lower(strings.TrimSpace(base))
	return strings.HasSuffix(base, "+xml") || base == ctypes.CTApplicationXml || base == "text/xml"
}

// parseXMLParts parses the XML parts filter admits, in zip order.
func (p *pkg) parseXMLParts(filter func(string) bool) {
	for _, name := range p.names {
		if !filter(name) {
			continue
		}
		if strings.HasSuffix(name, "/") || name == contentTypesName || strings.HasPrefix(name, trashPrefix) {
			continue
		}
		xml := isXMLName(name)
		if !xml {
			ct, ok := p.contentType(name)
			xml = ok && isXMLContentType(ct)
		}
		if !xml {
			continue
		}
		d, msg := parsePart(p.data[name])
		if msg != "" {
			p.findings.Add(name, "xml: "+msg)
		}
		p.docs[name] = d
	}
}

// checkRelationships validates every .rels part's targets and sources.
func (p *pkg) checkRelationships() {
	for _, relsPart := range p.names {
		if !strings.HasSuffix(ascii.Lower(relsPart), ".rels") {
			continue
		}
		source, ok := relsSource(relsPart)
		if !ok {
			p.add(relsPart, "relationships part name has no source part")
			continue
		}
		if source != "" {
			if _, ok := p.actual(source); !ok {
				p.add(relsPart, "relationships part has no source part")
			}
		}
		d := p.doc(relsPart)
		if d == nil {
			continue
		}
		ids := map[string][]*element{}
		var faults []fault
		var order []string
		for n := range d.elements() {
			if !n.is(pkgRelsNS, "Relationship") {
				continue
			}
			id := n.attrOr("", "Id")
			if _, seen := ids[id]; !seen {
				order = append(order, id)
			}
			ids[id] = append(ids[id], n)
			if !isNCName(id) {
				faults = append(faults, at("relationship Id is not a valid xsd:ID", n))
			}
			if t, ok := n.attr("", "Type"); !ok || t == "" {
				faults = append(faults, at("relationship has no Type", n))
			}
			if _, ok := n.attr("", "Target"); !ok {
				faults = append(faults, at("relationship has no Target", n))
			}
			if mode, ok := n.attr("", "TargetMode"); ok && mode != "External" && mode != "Internal" {
				faults = append(faults, at("relationship TargetMode is neither External nor Internal", n))
			}
		}
		p.addFaults(relsPart, faults)
		var dup []*element
		for _, id := range order {
			if len(ids[id]) > 1 {
				dup = append(dup, ids[id]...)
			}
		}
		if len(dup) > 0 {
			p.add(relsPart, "duplicate relationship Id", dup...)
		}
		single := map[string][]*element{}
		for _, r := range p.relationships(source) {
			if short, ok := singleInstance(r.relType); ok {
				single[short] = append(single[short], r.el)
			}
		}
		var repeated []string
		for k, els := range single {
			if len(els) > 1 {
				repeated = append(repeated, k)
			}
		}
		slices.Sort(repeated)
		for _, k := range repeated {
			p.add(relsPart, fmt.Sprintf("more than one %s relationship", k), single[k]...)
		}
		for _, r := range p.relationships(source) {
			short := ctypes.Last(r.relType)
			expected, checked := expectedContentTypes(r.relType)
			if r.external {
				if checked {
					p.add(relsPart, fmt.Sprintf("relationship %s must target a package part", short), r.el)
				}
				continue
			}
			actual, ok := p.actual(r.target)
			if !ok {
				p.add(relsPart, fmt.Sprintf("relationship %s targets a missing part", short), r.el)
				continue
			}
			ct, hasCT := p.contentType(actual)
			if checked && !(hasCT && slices.Contains(expected, ct)) {
				sorted := slices.Sorted(slices.Values(expected))
				shown := "None"
				if hasCT {
					shown = ct
				}
				p.add(actual, fmt.Sprintf("content type %s is not what Word writes for %s parts (%s)",
					shown, short, strings.Join(sorted, " or ")))
			}
			if r.relType == ctypes.RelImage && hasCT && !isImageType(ct) {
				p.add(actual, "image relationship targets content type "+ct)
			}
		}
	}
	if len(p.targets("", typeIs(ctypes.RelOfficeDocument))) == 0 &&
		len(p.targets("", typeIs(strictRel(ctypes.RelOfficeDocument)))) == 0 {
		p.add("_rels/.rels", "no officeDocument relationship")
	}
}

// isImageType reports whether a content type's top-level media type is
// image.
func isImageType(ct string) bool {
	top, _, ok := strings.Cut(ct, "/")
	return ok && top == "image"
}

// referenceType returns the relationship type an r: attribute of e must
// name, if any.
func referenceType(e *element, attr string) (string, bool) {
	switch {
	case e.is(wNS, "headerReference"):
		return ctypes.RelHeader, true
	case e.is(wNS, "footerReference"):
		return ctypes.RelFooter, true
	case e.is(wNS, "hyperlink"):
		return ctypes.RelHyperlink, true
	case e.is(wNS, "altChunk"):
		return aFChunkRel, true
	case e.is(aNS, "blip") && attr == "embed":
		return ctypes.RelImage, true
	}
	return "", false
}

// checkReferences reports dangling relationship references in part.
func (p *pkg) checkReferences(part string) {
	d := p.doc(part)
	if d == nil {
		return
	}
	byID := map[string]relationship{} // relationships dedups ids, first wins
	for _, r := range p.relationships(part) {
		byID[r.id] = r
	}
	var faults []fault
	for n := range visible(d.root) {
		for _, a := range n.attrs() {
			if a.ns != rNS || a.value == "" {
				continue
			}
			r, ok := byID[a.value]
			if !ok {
				faults = append(faults, at(fmt.Sprintf("%s/@r:%s names no relationship of this part", n.local, a.local), n))
				continue
			}
			if want, ok := referenceType(n, a.local); ok && r.relType != want {
				faults = append(faults, at(fmt.Sprintf("%s r:id names a %s relationship, not %s",
					n.local, ctypes.Last(r.relType), ctypes.Last(want)), n))
			}
			if n.is(wNS, "hyperlink") && !r.external {
				faults = append(faults, at(`hyperlink relationship is not TargetMode="External"`, n))
			}
		}
	}
	p.addFaults(part, faults)
}

// visible yields e and its descendants in document order, without
// mc:Fallback subtrees: Word reads one branch of mc:AlternateContent, and
// textbox content repeated in the Fallback legitimately duplicates ids.
func visible(e *element) iter.Seq[*element] {
	// A subtree is a run of the document's elements: skip each Fallback's.
	return func(yield func(*element) bool) {
		for i := e.index; i < e.end; i++ {
			n := e.doc.el(i)
			if n.is(mcNS, "Fallback") {
				i = n.end - 1
				continue
			}
			if !yield(n) {
				return
			}
		}
	}
}

// checkMarkupCompatibility validates markup-compatibility annotations in part.
func (p *pkg) checkMarkupCompatibility(part string) {
	d := p.doc(part)
	if d == nil {
		return
	}
	var faults []fault
	// Prefix lists must name declared prefixes, on the raw tree.
	for n := range d.elements() {
		for _, l := range []struct{ attr, label string }{
			{"Ignorable", "mc:Ignorable"}, {"MustUnderstand", "mc:MustUnderstand"}, {"ProcessContent", "mc:ProcessContent"},
		} {
			list, ok := n.attr(mcNS, l.attr)
			if !ok {
				continue
			}
			for _, entry := range strings.Fields(list) {
				prefix, _, _ := strings.Cut(entry, ":")
				if _, ok := n.lookupPrefix(prefix); !ok {
					faults = append(faults, at(fmt.Sprintf("%s names undeclared prefix %s", l.label, prefix), n))
				}
			}
		}
		if list, ok := n.attr(mcNS, "MustUnderstand"); ok {
			for _, prefix := range strings.Fields(list) {
				if uri, ok := n.lookupPrefix(prefix); ok && !understood(uri) {
					faults = append(faults, at("mc:MustUnderstand names namespace Word does not understand "+uri, n))
				}
			}
		}
		if n.is(mcNS, "AlternateContent") && n.firstChild(mcNS, "Choice") == nil {
			faults = append(faults, at("mc:AlternateContent has no mc:Choice", n))
		}
		if n.is(mcNS, "Choice") {
			for _, prefix := range strings.Fields(n.attrOr("", "Requires")) {
				if _, ok := n.lookupPrefix(prefix); !ok {
					faults = append(faults, at("mc:Choice Requires names undeclared prefix "+prefix, n))
				}
			}
		}
	}
	// Content in a namespace Word does not understand must be ignorable or
	// inside mc:AlternateContent; otherwise Word refuses the part. Custom
	// XML data parts carry arbitrary namespaces and are not MC-processed.
	if d.root.ns == wNS {
		ignorable := map[string]bool{}
		shown := visible(d.root)
		for n := range shown {
			for _, prefix := range strings.Fields(n.attrOr(mcNS, "Ignorable")) {
				if uri, ok := n.lookupPrefix(prefix); ok {
					ignorable[uri] = true
				}
			}
		}
		for n := range shown {
			if inChoice(n) {
				continue
			}
			names := make([]string, 0, 1+int(n.nattr))
			names = append(names, n.ns)
			for _, a := range n.attrs() {
				names = append(names, a.ns)
			}
			for _, ns := range names {
				if ns == "" || ns == mcNS || ignorable[ns] {
					continue
				}
				if !understood(ns) {
					faults = append(faults, at("non-ignorable unknown namespace "+ns, n))
				}
			}
		}
	}
	p.addFaults(part, faults)
}

// inChoice reports whether n lies inside an mc:Choice element.
func inChoice(n *element) bool {
	for a := n.up(); a != nil; a = a.up() {
		if a.is(mcNS, "Choice") {
			return true
		}
	}
	return false
}
