package main

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// slotDef is one XSD content slot before any corpus refinement: the
// child element names it holds, whether the slot may repeat, and, for
// an interleaved (multi-name) slot, whether each member may repeat on
// its own (its declaration's maxOccurs).
type slotDef struct {
	Names    []string
	Multi    bool
	MemberMu []bool
}

// decl is one xsd:element declaration of an element name: its
// declared type and the types of its xsd:alternative alternatives,
// all of which are merged for the element.
type decl struct {
	typ   string
	alts  []string
	types []string // typ plus alts, in declaration order
}

// xsdData is the parsed MS-VSDX schema, reduced to the facts the
// generator needs: global (root) elements, the complex types by name
// (their nodes, kept in document order so element declarations can be
// scanned in that order), and the merged declarations by element name.
type xsdData struct {
	sha256  string
	types   map[string]*tree.Node
	order   []string // complex type names in document order
	globals []globalElem
	decls   map[string][]decl
}

// globalElem is a top-level element declaration of the XSD.
type globalElem struct {
	name string
	typ  string
}

// maxDeclDepth bounds the walk over a parsed XSD tree (the tokenizer
// already caps element depth; this bounds our own decl scan).
const maxDeclDepth = 64

// parseXSD reads the XSD bytes src and returns its reduced form,
// hashing src for the provenance record.
func parseXSD(src []byte, sha string) (*xsdData, error) {
	doc, err := tree.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("vsdorder: parse schema: %w", err)
	}
	root := doc.Root()
	x := &xsdData{sha256: sha, types: map[string]*tree.Node{}, decls: map[string][]decl{}}
	for _, e := range root.Elements() {
		switch e.Local() {
		case "complexType":
			name, _ := e.Attr("name")
			if name == "" {
				continue
			}
			x.types[name] = e
			x.order = append(x.order, name)
		case "element":
			name, _ := e.Attr("name")
			typ, _ := e.Attr("type")
			if name != "" {
				x.globals = append(x.globals, globalElem{name: name, typ: typ})
				// A global element is a declaration like any other:
				// its type is merged with the alternatives of any
				// later declaration of the same name.
				d := decl{typ: typ}
				if typ != "" {
					d.types = append(d.types, typ)
				}
				x.decls[name] = append(x.decls[name], d)
			}
		}
	}
	for _, tname := range x.order {
		walkDecls(x.types[tname], 0, x.addDecl)
	}
	return x, nil
}

// addDecl records the element declaration node n (an xsd:element).
func (x *xsdData) addDecl(n *tree.Node) {
	name, _ := n.Attr("name")
	if name == "" {
		return
	}
	typ, _ := n.Attr("type")
	d := decl{typ: typ}
	if typ != "" {
		d.types = append(d.types, typ)
	}
	for _, c := range n.Elements() {
		if c.Local() != "alternative" {
			continue
		}
		at, _ := c.Attr("type")
		if at != "" {
			d.alts = append(d.alts, at)
			d.types = append(d.types, at)
		}
	}
	x.decls[name] = append(x.decls[name], d)
}

// walkDecls visits every xsd:element descendant of n (in document
// order, iterative-ordered by a recursive descent bounded in depth),
// calling add for each. It does not descend into an element
// declaration: alternatives are read by addDecl, never declarations.
func walkDecls(n *tree.Node, depth int, add func(*tree.Node)) {
	if depth > maxDeclDepth {
		return
	}
	for _, c := range n.Elements() {
		switch c.Local() {
		case "element":
			add(c)
		case "annotation", "documentation":
			// Schema documentation holds prose, not declarations.
			continue
		default:
			walkDecls(c, depth+1, add)
		}
	}
}

// expand returns the slots of complex type typ with its extension
// base's slots first. Unknown and xsd built-in types have no slots.
// expanding guards a type-extension cycle.
func (x *xsdData) expand(typ string, expanding map[string]bool) ([]slotDef, bool, error) {
	if typ == "" || strings.HasPrefix(typ, "xsd:") {
		return nil, false, nil
	}
	node, ok := x.types[typ]
	if !ok {
		return nil, false, nil
	}
	if expanding[typ] {
		return nil, false, fmt.Errorf("vsdorder: schema type %s: extension cycle", typ)
	}
	expanding[typ] = true
	defer func() { delete(expanding, typ) }()
	var slots []slotDef
	wild := false
	appendSlots := func(ss []slotDef, w bool) {
		slots = append(slots, ss...)
		wild = wild || w
	}
	for _, c := range node.Elements() {
		switch c.Local() {
		case "complexContent":
			for _, ext := range c.Elements() {
				if ext.Local() != "extension" && ext.Local() != "restriction" {
					continue
				}
				base, _ := ext.Attr("base")
				bs, bw, err := x.expand(base, expanding)
				if err != nil {
					return nil, false, err
				}
				appendSlots(bs, bw)
				for _, cc := range ext.Elements() {
					if isCompositor(cc) {
						ss, w, err := compositor(cc)
						if err != nil {
							return nil, false, err
						}
						appendSlots(ss, w)
					}
				}
			}
		case "simpleContent":
			for _, ext := range c.Elements() {
				if ext.Local() != "extension" {
					continue
				}
				base, _ := ext.Attr("base")
				bs, bw, err := x.expand(base, expanding)
				if err != nil {
					return nil, false, err
				}
				appendSlots(bs, bw)
			}
		default:
			if isCompositor(c) {
				ss, w, err := compositor(c)
				if err != nil {
					return nil, false, err
				}
				appendSlots(ss, w)
			}
		}
	}
	return slots, wild, nil
}

// isCompositor reports whether n is a sequence, choice or all node.
func isCompositor(n *tree.Node) bool {
	switch n.Local() {
	case "sequence", "choice", "all":
		return true
	}
	return false
}

// compositor returns the slots of one compositor node and whether it
// holds an xsd:any wildcard. A sequence gives one slot per item; a
// choice or all gives one interleaved slot holding every item, in
// schema order.
func compositor(n *tree.Node) ([]slotDef, bool, error) {
	compUnbounded := false
	if v, ok := n.Attr("maxOccurs"); ok && v == "unbounded" {
		compUnbounded = true
	}
	var names []string
	var memberMu []bool
	var seq []slotDef
	wild := false
	for _, c := range n.Elements() {
		switch {
		case c.Local() == "element":
			name, _ := c.Attr("name")
			if name == "" {
				continue
			}
			own := false
			if v, ok := c.Attr("maxOccurs"); ok && v == "unbounded" {
				own = true
			}
			mu := compUnbounded || own
			names = append(names, name)
			memberMu = append(memberMu, own)
			if n.Local() == "sequence" {
				seq = append(seq, slotDef{Names: []string{name}, Multi: mu})
			}
		case c.Local() == "any":
			wild = true
		case isCompositor(c):
			sub, w, err := compositor(c)
			if err != nil {
				return nil, false, err
			}
			wild = wild || w
			if n.Local() == "sequence" {
				seq = append(seq, sub...)
			} else {
				for _, s := range sub {
					names = append(names, s.Names...)
					memberMu = append(memberMu, s.MemberMu...)
				}
			}
		}
	}
	if n.Local() == "sequence" {
		return seq, wild, nil
	}
	if len(names) == 0 {
		return nil, wild, nil
	}
	multi := compUnbounded
	for _, m := range memberMu {
		multi = multi || m
	}
	if n.Local() == "choice" && compUnbounded {
		multi = true
	}
	// An all's members each occur at most once: only a member whose
	// own declaration is unbounded repeats (a single-typed all).
	if n.Local() == "all" && len(names) == 1 {
		multi = memberMu[0]
	}
	return []slotDef{{Names: names, Multi: multi, MemberMu: memberMu}}, wild, nil
}

// merged returns the slots of element name: the slots of every type
// declared for it (primary plus alternatives) merged by child name.
// A single-member slot that is already present keeps its first
// position; a multi-member (interleaved) slot takes its members out
// of earlier slots and lands where it was declared. The returned
// wildcard is xsd:any in any of the merged types.
func (x *xsdData) merged(name string) ([]slotDef, bool, error) {
	var slots []slotDef
	wild := false
	for _, d := range x.decls[name] {
		for _, t := range d.types {
			ss, w, err := x.expand(t, map[string]bool{})
			if err != nil {
				return nil, false, err
			}
			wild = wild || w
			for _, s := range ss {
				slots = mergeSlot(slots, s)
			}
		}
	}
	return slots, wild, nil
}

// mergeSlot folds incoming slot s into slots: multi-member slots move
// their members into themselves; single-member slots are appended
// only when the member is new.
func mergeSlot(slots []slotDef, s slotDef) []slotDef {
	if len(s.Names) > 1 {
		for _, name := range s.Names {
			slots = removeName(slots, name)
		}
		return append(slots, s)
	}
	if len(s.Names) == 0 {
		return slots
	}
	name := s.Names[0]
	for _, slot := range slots {
		for _, n := range slot.Names {
			if n == name {
				return slots
			}
		}
	}
	return append(slots, s)
}

// removeName drops name from every slot in slots, deleting slots
// that become empty.
func removeName(slots []slotDef, name string) []slotDef {
	out := slots[:0]
	for _, s := range slots {
		var kept []string
		var keptMu []bool
		for i, n := range s.Names {
			if n == name {
				continue
			}
			kept = append(kept, n)
			if i < len(s.MemberMu) {
				keptMu = append(keptMu, s.MemberMu[i])
			}
		}
		if len(kept) == 0 {
			continue
		}
		s.Names = kept
		s.MemberMu = keptMu
		out = append(out, s)
	}
	return out
}
