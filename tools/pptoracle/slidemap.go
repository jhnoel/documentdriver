package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// Namespaces as PowerPoint writes them, in the
// Transitional and Strict conformance classes.
var (
	nsPresentationML = []string{
		"http://schemas.openxmlformats.org/presentationml/2006/main",
		"http://purl.oclc.org/ooxml/presentationml/main",
	}
	nsRelationships = []string{
		"http://schemas.openxmlformats.org/officeDocument/2006/relationships",
		"http://purl.oclc.org/ooxml/officeDocument/relationships",
	}
)

// pmlChild returns n's first PresentationML child named local.
func pmlChild(n *tree.Node, local string) *tree.Node {
	for _, ns := range nsPresentationML {
		if c := n.Child(ns, local); c != nil {
			return c
		}
	}
	return nil
}

// isPML reports whether n is the PresentationML element local.
func isPML(n *tree.Node, local string) bool {
	return slices.ContainsFunc(nsPresentationML, func(ns string) bool { return n.Is(ns, local) })
}

// relID returns the r:id attribute in either conformance class.
func relID(n *tree.Node) (string, bool) {
	for _, ns := range nsRelationships {
		if v, ok := n.AttrNS(ns, "id"); ok {
			return v, true
		}
	}
	return "", false
}

// slidePartName turns "slide3", "slides/slide3.xml" or
// "/ppt/slides/slide3.xml" into the part name "/ppt/slides/slide3.xml".
// Absolute names are returned unchanged.
func slidePartName(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "/") {
		return s
	}
	if !strings.HasSuffix(s, ".xml") {
		s += ".xml"
	}
	if !strings.Contains(s, "/") {
		s = "slides/" + s
	}
	return "/ppt/" + s
}

// slideOrder returns the slide part names of the deck at path in
// presentation order, as listed by presentation.xml's sldIdLst.
func slideOrder(path string) ([]string, error) {
	p, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	pkgRels, err := p.Rels("/")
	if err != nil {
		return nil, err
	}
	var docs []opc.Relationship
	for _, r := range pkgRels.All() {
		if ctypes.Is(r.Type, ctypes.RelOfficeDocument) {
			docs = append(docs, r)
		}
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("%s: no officeDocument relationship", path)
	}
	presName, err := pkgRels.TargetPart(docs[0])
	if err != nil {
		return nil, err
	}
	pres, err := p.Part(presName)
	if err != nil {
		return nil, err
	}
	doc, err := pres.Tree()
	if err != nil {
		return nil, err
	}
	rels, err := p.Rels(presName)
	if err != nil {
		return nil, err
	}
	var out []string
	lst := pmlChild(doc.Root(), "sldIdLst")
	if lst == nil {
		return nil, nil
	}
	for _, sld := range lst.Elements() {
		if !isPML(sld, "sldId") {
			continue
		}
		rid, ok := relID(sld)
		if !ok {
			return nil, fmt.Errorf("%s: sldId without r:id", presName)
		}
		rel, ok := rels.Get(rid)
		if !ok {
			return nil, fmt.Errorf("%s: sldId r:id %q has no relationship", presName, rid)
		}
		name, err := rels.TargetPart(rel)
		if err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, nil
}

// slideIndex maps a slide part ("slide3") to its 1-based position in the
// presentation's slide list.
func slideIndex(path, slide string) (int, error) {
	order, err := slideOrder(path)
	if err != nil {
		return 0, err
	}
	want := strings.ToLower(slidePartName(slide))
	for i, name := range order {
		if strings.ToLower(name) == want {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("%s: %s is not in the slide list", path, slide)
}

// partBase returns "slide3" for "/ppt/slides/slide3.xml".
func partBase(name string) string {
	name = name[strings.LastIndex(name, "/")+1:]
	return strings.TrimSuffix(name, ".xml")
}

// slidesHidden reports, for each slide part in order, whether the slide
// is hidden (p:sld show="0").
func slidesHidden(path string, order []string) ([]bool, error) {
	p, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	out := make([]bool, len(order))
	for i, name := range order {
		pt, err := p.Part(name)
		if err != nil {
			return nil, err
		}
		doc, err := pt.Tree()
		if err != nil {
			return nil, err
		}
		v, ok := doc.Root().Attr("show")
		out[i] = ok && (v == "0" || v == "false")
	}
	return out, nil
}
