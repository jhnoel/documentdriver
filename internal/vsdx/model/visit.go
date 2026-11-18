package model

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// VisitPage walks one page of d in stored order, giving each object
// to visit as BuildPage would build it, but holding no page tree and
// no objects: the page part's bytes are the only page-sized hold, and
// each top-level Shape span is parsed on its own (tree.ParseString),
// walked with the same walker step BuildPage uses, and dropped with
// its tree before the next span. No placed map is kept, no
// linkConnects runs, and offPage adds nothing; the objects a visit
// sees have no glue (FromGlue, ToGlue, Glued). Duplicate-ID
// suffixes carry across spans, exactly as in the whole-tree walk.
//
// visit is called once per object (a group before its children, in
// paint order); it returns false to stop the walk, in which case
// VisitPage returns the notes gathered so far and runs no Connects
// pass. A nil visit walks the page and gathers its notes.
//
// The returned notes are BuildPage's page notes for the page, in
// order: the page-sheet notes, the walk notes (a shape whose ID does
// not parse, a layer index with no layer row) in walk order, and
// after all shapes the Connects notes of connectNotes over the
// stored IDs an object was built for. o.Checkpoint, when not nil, is
// called with "shape" after every 1024 top-level shapes have been
// walked and dropped, and with "notes" after the Connects notes
// pass. Chunk RM2.
func VisitPage(d *doc.Document, r *inherit.Resolver, e *doc.PageEntry, o Options, visit func(*Object) bool) ([]doc.Note, error) {
	if d == nil {
		return nil, fmt.Errorf("vsdx: no document")
	}
	if e == nil {
		return nil, fmt.Errorf("vsdx: no page")
	}
	if visit == nil {
		visit = func(*Object) bool { return true }
	}
	src, err := d.PageBytes(e)
	if err != nil {
		return nil, err
	}
	p := &Page{}
	pageFacts(p, d, r, e)
	container := vsd.PageID(e.ID, pageDup(d, e))
	p.ID = container
	w := &walker{
		p:         p,
		d:         d,
		r:         r,
		container: container,
		part:      e.Part,
		o:         o,
		seen:      map[uint32]int{},
		ids:       map[uint32]bool{},
		visit:     visit,
	}
	connects, rootDecls, err := w.scan(src, e.Part)
	if err != nil {
		return nil, err
	}
	if w.stop {
		return p.Notes, nil
	}
	if connects != "" {
		parsed, err := parseSpan(connects, rootDecls)
		if err != nil {
			return nil, pageErr(e.Part, src, err)
		}
		connectNotes(p, parsed.Root(), func(id uint32) bool { return w.ids[id] })
	}
	if o.Checkpoint != nil {
		o.Checkpoint("notes")
	}
	return p.Notes, nil
}

// pageErr reports a malformed page as PageContents reports it: the
// canonical parse error of the whole page when the whole page does
// not parse (so a malformed page's note is BuildPage's), and the
// fault found in the span otherwise.
func pageErr(part, src string, err error) error {
	if _, perr := tree.ParseString(src); perr != nil {
		return fmt.Errorf("vsdx: %s: %w", part, perr)
	}
	return fmt.Errorf("vsdx: %s: %w", part, err)
}

// scan walks src's token stream with xmlscan, validating the root as
// PageContents does, and walks each top-level Shape span as it
// reaches it. It returns the span of the root's first Connects
// element ("" when there is none) and the root's namespace
// declarations. w.stop is set when visit stops the walk.
func (w *walker) scan(src, part string) (string, map[string]string, error) {
	var stack []string
	inShapes := false
	shapesSeen := false
	connectsSeen := false
	var connects string
	var rootDecls, shapesDecls map[string]string
	top := 0
	sawRoot := false
	for i := 0; i < len(src); {
		t, err := xmlscan.Next(src, i)
		if err != nil {
			return "", nil, pageErr(part, src, err)
		}
		switch t.Kind {
		case xmlscan.StartTag:
			if !sawRoot {
				if err := checkRoot(src, t, part); err != nil {
					return "", nil, err
				}
				sawRoot = true
				rootDecls = declsOf(src, t)
				stack = append(stack, t.Name(src))
				i = t.End
				continue
			}
			local := t.Local(src)
			switch {
			case len(stack) == 1 && local == vsd.ElemShapes && !shapesSeen:
				shapesSeen, inShapes = true, true
				shapesDecls = mergeDecls(rootDecls, declsOf(src, t))
				stack = append(stack, t.Name(src))
			case len(stack) == 1 && local == vsd.ElemConnects && !connectsSeen:
				connectsSeen = true
				_, end, err := xmlscan.ElementEnd(src, t)
				if err != nil {
					return "", nil, pageErr(part, src, err)
				}
				connects = src[t.Start:end]
				i = end
				continue
			case inShapes && len(stack) == 2 && local == vsd.ElemShape:
				_, end, err := xmlscan.ElementEnd(src, t)
				if err != nil {
					return "", nil, pageErr(part, src, err)
				}
				if err := w.walkSpan(src[t.Start:end], shapesDecls, part, src); err != nil {
					return "", nil, err
				}
				if w.stop {
					return "", nil, nil
				}
				top++
				if top%1024 == 0 && w.o.Checkpoint != nil {
					w.o.Checkpoint("shape")
				}
				i = end
				continue
			default:
				stack = append(stack, t.Name(src))
			}
		case xmlscan.EmptyTag:
			if !sawRoot {
				if err := checkRoot(src, t, part); err != nil {
					return "", nil, err
				}
				sawRoot = true
				rootDecls = declsOf(src, t)
			} else {
				local := t.Local(src)
				switch {
				case len(stack) == 1 && local == vsd.ElemShapes && !shapesSeen:
					shapesSeen = true
				case len(stack) == 1 && local == vsd.ElemConnects && !connectsSeen:
					connectsSeen = true
					connects = src[t.Start:t.End]
				case inShapes && len(stack) == 2 && local == vsd.ElemShape:
					if err := w.walkSpan(src[t.Start:t.End], shapesDecls, part, src); err != nil {
						return "", nil, err
					}
					if w.stop {
						return "", nil, nil
					}
					top++
					if top%1024 == 0 && w.o.Checkpoint != nil {
						w.o.Checkpoint("shape")
					}
				}
			}
		case xmlscan.EndTag:
			name := t.Name(src)
			if len(stack) == 0 || stack[len(stack)-1] != name {
				return "", nil, pageErr(part, src, xmlscan.ErrSyntax)
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 1 && t.Local(src) == vsd.ElemShapes {
				inShapes = false
			}
		case xmlscan.Text:
			if len(stack) == 0 && strings.TrimSpace(src[t.Start:t.End]) != "" {
				return "", nil, pageErr(part, src, xmlscan.ErrSyntax)
			}
		}
		i = t.End
	}
	if !sawRoot || len(stack) != 0 {
		return "", nil, pageErr(part, src, xmlscan.ErrSyntax)
	}
	return connects, rootDecls, nil
}

// checkRoot reports whether the first element of src (the token t)
// is a PageContents element in a Visio main namespace, as
// PageContents requires, resolving the root's own namespace
// declarations.
func checkRoot(src string, t xmlscan.Token, part string) error {
	if t.Local(src) != vsd.ElemPageContents {
		return fmt.Errorf("vsdx: %s is not a %s part", part, vsd.ElemPageContents)
	}
	var def string
	prefixes := map[string]string{}
	for a := range xmlscan.Attrs(src, t) {
		name := a.Name(src)
		switch {
		case name == "xmlns":
			def = a.Value(src)
		case strings.HasPrefix(name, "xmlns:"):
			prefixes[name[len("xmlns:"):]] = a.Value(src)
		}
	}
	uri := def
	if p := strings.IndexByte(t.Name(src), ':'); p >= 0 {
		uri = prefixes[t.Name(src)[:p]]
	}
	if !vsd.IsMain(uri) {
		return fmt.Errorf("vsdx: %s is not a %s part", part, vsd.ElemPageContents)
	}
	return nil
}

// declsOf returns the namespace declarations of the tag t of src,
// by attribute name ("xmlns" or "xmlns:prefix"), as the raw
// attribute text (leading whitespace and quotes included).
func declsOf(src string, t xmlscan.Token) map[string]string {
	decls := map[string]string{}
	for a := range xmlscan.Attrs(src, t) {
		name := a.Name(src)
		if name == "xmlns" || strings.HasPrefix(name, "xmlns:") {
			decls[name] = src[a.Start:a.End()]
		}
	}
	return decls
}

// mergeDecls returns the declarations in scope inside an element
// that declares inner within outer: inner wins on a shared name.
func mergeDecls(outer, inner map[string]string) map[string]string {
	if len(inner) == 0 {
		return outer
	}
	merged := maps.Clone(outer)
	maps.Copy(merged, inner)
	return merged
}

// parseSpan parses one span (a top-level Shape or the Connects
// element) on its own with tree.ParseString, redeclaring on the
// span's root element any in-scope prefixed namespace a name inside
// the span uses but the span does not declare itself. A span in a
// document that declares a prefix only on PageContents (a Foreign
// shape's Rel with r:id in V15) is otherwise an undeclared-prefix
// parse error; an unprefixed span needs no declaration and is
// parsed as it stands.
func parseSpan(span string, decls map[string]string) (*tree.Document, error) {
	var needed []string
	for name := range decls {
		if prefix, ok := strings.CutPrefix(name, "xmlns:"); ok && strings.Contains(span, prefix+":") {
			needed = append(needed, name)
		}
	}
	if len(needed) == 0 {
		return tree.ParseString(span)
	}
	t, err := xmlscan.Next(span, 0)
	if err != nil {
		return nil, err
	}
	declared := map[string]bool{}
	for a := range xmlscan.Attrs(span, t) {
		if name := a.Name(span); name == "xmlns" || strings.HasPrefix(name, "xmlns:") {
			declared[name] = true
		}
	}
	slices.Sort(needed)
	var extra string
	for _, name := range needed {
		if !declared[name] {
			extra += decls[name]
		}
	}
	if extra == "" {
		return tree.ParseString(span)
	}
	return tree.ParseString(span[:t.NameEnd] + extra + span[t.NameEnd:])
}

// walkSpan parses one top-level Shape span and walks its subtree
// with the walker's stack, dropping the parsed tree when the walk
// ends. decls are the namespace declarations in scope at the span.
// src is the whole page, for a canonical error report.
func (w *walker) walkSpan(span string, decls map[string]string, part, src string) error {
	parsed, err := parseSpan(span, decls)
	if err != nil {
		return pageErr(part, src, err)
	}
	root := parsed.Root()
	if root == nil {
		return pageErr(part, src, xmlscan.ErrSyntax)
	}
	w.stack = append(w.stack, frame{node: root, f: place.Page})
	for len(w.stack) > 0 && !w.stop {
		fr := w.stack[len(w.stack)-1]
		w.stack = w.stack[:len(w.stack)-1]
		w.step(fr)
	}
	w.stack = nil
	return nil
}
