package integrity

import (
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// relationship is one relationship of a source part.
type relationship struct {
	id, relType string
	target      string // resolved part name; "" when external
	external    bool
	el          *element // the Relationship element
}

// pkg is a package being checked: its entries by zip name, the parsed XML
// parts, the content types and the relationships read so far.
type pkg struct {
	names     []string             // entry names in zip order, duplicates included
	data      map[string]string    // by name; a later duplicate wins
	lower     map[string]string    // lowercased name -> the first entry with it
	docs      map[string]*document // parsed XML parts; nil when malformed
	defaults  map[string]string    // extension -> content type
	overrides map[string]string    // "/lowercased part name" -> content type
	rels      map[string][]relationship
	findings  *Findings
	model     *contentModel
}

// add records an "integrity: " finding located at els (none: the part
// as a whole).
func (p *pkg) add(part, message string, els ...*element) {
	p.findings.addAt(part, "integrity: "+message, els)
}

// fault is a finding of a check, located at the elements it is about.
type fault struct {
	message string
	els     []*element
}

// at returns a fault with message and located elements.
func at(message string, els ...*element) fault { return fault{message, els} }

// addFaults records faults found in part.
func (p *pkg) addFaults(part string, faults []fault) {
	for _, f := range faults {
		p.add(part, f.message, f.els...)
	}
}

// doc returns the parsed document for part, or nil when unparsed.
func (p *pkg) doc(part string) *document { return p.docs[part] }

// contentType returns the content type of a part, from its Override or
// its extension's Default.
func (p *pkg) contentType(part string) (string, bool) {
	if ct, ok := p.overrides["/"+strings.ToLower(part)]; ok {
		return ct, true
	}
	file := part[strings.LastIndexByte(part, '/')+1:]
	ext := ""
	if i := strings.LastIndexByte(file, '.'); i >= 0 {
		ext = strings.ToLower(file[i+1:])
	}
	ct, ok := p.defaults[ext]
	return ct, ok
}

// relationships returns the relationships of a part in document order;
// the first of duplicate ids wins.
func (p *pkg) relationships(source string) []relationship {
	if found, ok := p.rels[source]; ok {
		return found
	}
	var found []relationship
	if d := p.doc(relsName(source)); d != nil {
		seen := map[string]bool{}
		for n := range d.elements() {
			if !n.is(pkgRelsNS, "Relationship") {
				continue
			}
			id := n.attrOr("", "Id")
			if seen[id] {
				continue
			}
			seen[id] = true
			mode, _ := n.attr("", "TargetMode")
			r := relationship{id: id, relType: n.attrOr("", "Type"), external: mode == "External", el: n}
			if !r.external {
				r.target = resolveTarget(source, n.attrOr("", "Target"))
			}
			found = append(found, r)
		}
	}
	p.rels[source] = found
	return found
}

// targets returns the non-empty internal targets of the relationships of
// source whose type matches.
func (p *pkg) targets(source string, match func(string) bool) []string {
	var out []string
	for _, r := range p.relationships(source) {
		if match(r.relType) && !r.external && r.target != "" {
			out = append(out, r.target)
		}
	}
	return out
}

// typeIs matches one relationship type exactly.
func typeIs(rel string) func(string) bool { return func(t string) bool { return t == rel } }

// actual returns the stored name of a resolved target (part names are
// case-insensitive).
func (p *pkg) actual(target string) (string, bool) {
	name, ok := p.lower[strings.ToLower(target)]
	return name, ok
}

// firstTarget returns the stored name of the first target of source's
// relationships of a type.
func (p *pkg) firstTarget(source string, match func(string) bool) (string, bool) {
	t := p.targets(source, match)
	if len(t) == 0 {
		return "", false
	}
	return p.actual(t[0])
}

// adoptedPart returns the part the engine edits for a relationship type:
// the related part, or, when the main document has no such relationship,
// the part at the name the engine adopts (word/comments.xml). Faults
// already latent in an unlinked part are then pre-existing, not
// introduced by the edit that links it.
func (p *pkg) adoptedPart(main string, match func(string) bool, file string) (string, bool) {
	if len(p.targets(main, match)) > 0 {
		return p.firstTarget(main, match)
	}
	name := file
	if i := strings.LastIndexByte(main, '/'); i > 0 {
		name = main[:i] + "/" + file
	}
	return p.actual(name)
}

// isXMLName reports whether name ends in .xml or .rels.
func isXMLName(name string) bool {
	lower := ascii.Lower(name)
	return strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".rels")
}

// partNameFault applies the OPC part-name rules (ECMA-376 Part 2, 6.2.2)
// to a zip entry name.
func partNameFault(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return true
	}
	for _, seg := range strings.Split(strings.TrimRight(name, "/"), "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasSuffix(seg, ".") {
			return true
		}
	}
	return false
}

// percentDecode decodes %XX escapes; a result that is not UTF-8 leaves the
// value as it was.
func percentDecode(v string) string {
	out := make([]byte, 0, len(v))
	for i := 0; i < len(v); i++ {
		if v[i] == '%' && i+2 < len(v) {
			if b, ok := hexByte(v[i+1 : i+3]); ok {
				out = append(out, b)
				i += 2
				continue
			}
		}
		out = append(out, v[i])
	}
	if !utf8.Valid(out) {
		return v
	}
	return string(out)
}

// hexByte parses two characters as Rust's u8::from_str_radix(_, 16) does:
// an optional + and hex digits.
func hexByte(s string) (byte, bool) {
	v, ok := parseRustUint(s, 16, 0xFF)
	return byte(v), ok //nolint:gosec // G115: parseRustUint bounds v at 0xFF
}

// resolveTarget returns the part name a relationship of source targets.
func resolveTarget(source, target string) string {
	target, _, _ = strings.Cut(target, "#")
	target = percentDecode(target)
	var joined string
	if abs, ok := strings.CutPrefix(target, "/"); ok {
		joined = abs
	} else if i := strings.LastIndexByte(source, '/'); i > 0 {
		joined = source[:i] + "/" + target
	} else {
		joined = target
	}
	var parts []string
	for _, seg := range strings.Split(joined, "/") {
		switch seg {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

// relsName returns the relationships part of a part ("" for the package).
func relsName(part string) string {
	if part == "" {
		return "_rels/.rels"
	}
	if i := strings.LastIndexByte(part, '/'); i >= 0 {
		return part[:i] + "/_rels/" + part[i+1:] + ".rels"
	}
	return "_rels/" + part + ".rels"
}

// relsSource returns the source part of a relationships part, and false
// when the name is not one.
func relsSource(name string) (string, bool) {
	if name == "_rels/.rels" {
		return "", true
	}
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return "", false
	}
	head, file := name[:i], name[i+1:]
	source, ok := strings.CutSuffix(file, ".rels")
	if !ok {
		return "", false
	}
	dir, ok := strings.CutSuffix(head, "_rels")
	if !ok {
		return "", false
	}
	return dir + source, true
}
