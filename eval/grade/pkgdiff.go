package grade

import (
	"bytes"
	"sort"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

type pkgChanges struct {
	changed      []string
	allowed      []string
	reserialised int
}

// diffPackages compares every part except the graded slide part (whose
// objects and skeleton are compared separately). A part counts as
// unchanged when its bytes are equal, or when its XML is canonically equal;
// relationship parts and [Content_Types].xml compare as sets of entries,
// since tools that rewrite them may reorder entries.
func diffPackages(in, out *opc.Package, slide string) (pkgChanges, error) {
	var pc pkgChanges
	outNames := map[string]string{}
	for _, n := range out.Names() {
		outNames[strings.ToLower(n)] = n
	}
	seen := map[string]bool{}
	for _, n := range in.Names() {
		key := strings.ToLower(n)
		seen[key] = true
		if key == strings.ToLower(slide) {
			continue
		}
		on, ok := outNames[key]
		if !ok {
			pc.changed = append(pc.changed, n+": part removed")
			continue
		}
		a, err := partBytes(in, n)
		if err != nil {
			return pc, err
		}
		b, err := partBytes(out, on)
		if err != nil {
			pc.changed = append(pc.changed, n+": unreadable in output")
			continue
		}
		if bytes.Equal(a, b) {
			continue
		}
		same := false
		if in.IsXML(n) {
			same = xmlSame(n, a, b)
		}
		switch {
		case same:
			pc.reserialised++
		case AllowedParts[n]:
			pc.allowed = append(pc.allowed, n)
		default:
			pc.changed = append(pc.changed, n+": changed")
		}
	}
	for _, n := range out.Names() {
		if !seen[strings.ToLower(n)] {
			pc.changed = append(pc.changed, n+": part added")
		}
	}
	return pc, nil
}

// partBytes returns the bytes of a named package part.
func partBytes(p *opc.Package, name string) ([]byte, error) {
	pt, err := p.Part(name)
	if err != nil {
		return nil, err
	}
	return pt.Bytes()
}

// xmlSame reports whether XML parts a and b match canonically, or as entry sets.
func xmlSame(name string, a, b []byte) bool {
	da, err1 := tree.Parse(a)
	db, err2 := tree.Parse(b)
	if err1 != nil || err2 != nil {
		return false
	}
	set := strings.HasSuffix(name, ".rels") || strings.HasSuffix(name, "[Content_Types].xml")
	if !set {
		return bytes.Equal(Canonical(da.Node(), false), Canonical(db.Node(), false))
	}
	return entries(da) == entries(db)
}

// entries is a root's children, canonicalised and sorted.
func entries(d *tree.Document) string {
	root := d.Root()
	if root == nil {
		return ""
	}
	var v []string
	for c := range root.ElementsSeq() {
		v = append(v, string(Canonical(c, false)))
	}
	sort.Strings(v)
	return "{" + root.Space() + "}" + root.Local() + "\n" + strings.Join(v, "\n")
}
