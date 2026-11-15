package integrity

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// partFault is a fault found in a part other than the one being checked.
type partFault struct {
	part, message string
	els           []*element
}

// addAll appends part faults faults to the package findings.
func (p *pkg) addAll(faults []partFault) {
	for _, f := range faults {
		p.add(f.part, f.message, f.els...)
	}
}

// checkComments checks comments, their anchors and the comment extension
// parts, and returns the comment ids.
func (p *pkg) checkComments(main string, stories []story) map[string]bool {
	commentIDs := map[string]bool{}
	var order []string                 // the comment ids in document order
	var comments map[string][]*element // by id
	lastPara := map[string]string{}    // paraId of a comment's last paragraph -> comment id
	commentsPart, hasComments := p.adoptedPart(main, typeIs(ctypes.RelComments), "comments.xml")
	var faults []partFault
	if hasComments {
		if d := p.doc(commentsPart); d != nil {
			ids := map[string][]*element{}
			for c := d.root.firstEl(); c != nil; c = c.nextEl() {
				if !c.is(wNS, "comment") {
					continue
				}
				id := c.attrOr(wNS, "id")
				ids[id] = append(ids[id], c)
				if !commentIDs[id] {
					order = append(order, id)
				}
				commentIDs[id] = true
				var last *element
				for q := range visible(c) {
					if q.is(wNS, "p") {
						last = q
					}
				}
				if last != nil {
					if para, ok := last.attr(w14NS, "paraId"); ok {
						lastPara[strings.ToUpper(para)] = id
					}
				}
			}
			if dup := repeated(ids); len(dup) > 0 {
				faults = append(faults, partFault{commentsPart, "duplicate w:comment id", dup})
			}
			comments = ids
		}
	}
	referenced := map[string]bool{}
	for _, s := range stories {
		d := p.doc(s.part)
		if d == nil {
			continue
		}
		for n := range visible(d.root) {
			if n.is(wNS, "commentReference") {
				id := n.attrOr(wNS, "id")
				if !commentIDs[id] {
					faults = append(faults, partFault{s.part, "commentReference names no w:comment", []*element{n}})
				}
				referenced[id] = true
			}
		}
	}
	if hasComments {
		// In document order: findings reach the gate's refusals in the
		// order found.
		for _, id := range order {
			if !referenced[id] {
				faults = append(faults, partFault{commentsPart, "w:comment has no commentReference", comments[id]})
			}
		}
	}

	if part, ok := p.adoptedPart(main, typeIs(ctypes.RelCommentsExtended), "commentsExtended.xml"); ok {
		if d := p.doc(part); d != nil {
			var entries []*element
			own := map[string][]*element{}
			for c := d.root.firstEl(); c != nil; c = c.nextEl() {
				if c.is(w15NS, "commentEx") {
					entries = append(entries, c)
					key := strings.ToUpper(c.attrOr(w15NS, "paraId"))
					own[key] = append(own[key], c)
				}
			}
			for _, e := range entries {
				if _, ok := lastPara[strings.ToUpper(e.attrOr(w15NS, "paraId"))]; !ok {
					faults = append(faults, partFault{part, "commentEx paraId is not the last paragraph of a comment", []*element{e}})
				}
				if parent, ok := e.attr(w15NS, "paraIdParent"); ok {
					if _, ok := own[strings.ToUpper(parent)]; !ok {
						faults = append(faults, partFault{part, "commentEx paraIdParent names no commentEx", []*element{e}})
					}
				}
			}
			if dup := repeated(own); len(dup) > 0 {
				faults = append(faults, partFault{part, "duplicate commentEx paraId", dup})
			}
		}
	}

	durable := map[string]bool{}
	if part, ok := p.adoptedPart(main, typeIs(ctypes.RelCommentsIds), "commentsIds.xml"); ok {
		if d := p.doc(part); d != nil {
			seen := map[string][]*element{}
			for e := d.root.firstEl(); e != nil; e = e.nextEl() {
				if !e.is(w16cidNS, "commentId") {
					continue
				}
				if _, ok := lastPara[strings.ToUpper(e.attrOr(w16cidNS, "paraId"))]; !ok {
					faults = append(faults, partFault{part, "commentId paraId is not the last paragraph of a comment", []*element{e}})
				}
				v := e.attrOr(w16cidNS, "durableId")
				seen[strings.ToUpper(v)] = append(seen[strings.ToUpper(v)], e)
				durable[strings.ToUpper(v)] = true
				if !isHex8Below(v, 0x7FFF_FFFF) || hexValue(v) == 0 {
					faults = append(faults, partFault{part, "durableId is not eight hex digits in (0, 7FFFFFFF)", []*element{e}})
				}
			}
			if dup := repeated(seen); len(dup) > 0 {
				faults = append(faults, partFault{part, "duplicate durableId", dup})
			}
		}
	}
	if part, ok := p.adoptedPart(main, typeIs(ctypes.RelCommentsExtensible), "commentsExtensible.xml"); ok {
		if d := p.doc(part); d != nil {
			for e := d.root.firstEl(); e != nil; e = e.nextEl() {
				if e.is(w16cexNS, "commentExtensible") && !durable[strings.ToUpper(e.attrOr(w16cexNS, "durableId"))] {
					faults = append(faults, partFault{part, "commentExtensible durableId is not in commentsIds", []*element{e}})
				}
			}
		}
	}
	p.addAll(faults)
	return commentIDs
}

// hexValue parses v as hexadecimal, returning zero on failure.
func hexValue(v string) uint64 {
	n, err := strconv.ParseUint(v, 16, 64)
	if err != nil {
		return 0
	}
	return n
}

// checkStyles checks style definitions. References from stories to
// undefined styles are not checked: Word falls back to the default style,
// and the engine's comments name CommentText and CommentReference without
// defining them.
func (p *pkg) checkStyles(main string) {
	part, ok := p.firstTarget(main, typeIs(ctypes.RelStyles))
	if !ok {
		return
	}
	d := p.doc(part)
	if d == nil {
		return
	}
	ids := map[string][]*element{}
	var styles []*element
	for c := d.root.firstEl(); c != nil; c = c.nextEl() {
		if c.is(wNS, "style") {
			styles = append(styles, c)
			id := c.attrOr(wNS, "styleId")
			ids[id] = append(ids[id], c)
		}
	}
	var faults []fault
	if dup := repeated(ids); len(dup) > 0 {
		faults = append(faults, at("duplicate styleId", dup...))
	}
	for _, s := range styles {
		for _, ref := range []string{"basedOn", "next", "link"} {
			r := s.firstChild(wNS, ref)
			if r == nil {
				continue
			}
			if v, ok := r.attr(wNS, "val"); ok {
				if _, ok := ids[v]; !ok {
					faults = append(faults, at("style "+ref+" names no style", r))
				}
			}
		}
	}
	p.addFaults(part, faults)
}

// checkSections validates section header and footer references.
func (p *pkg) checkSections(main string) {
	d := p.doc(main)
	if d == nil {
		return
	}
	var faults []fault
	for section := range visible(d.root) {
		if !section.is(wNS, "sectPr") {
			continue
		}
		for _, ref := range []string{"headerReference", "footerReference"} {
			seen := map[string]bool{}
			for c := section.firstEl(); c != nil; c = c.nextEl() {
				if !c.is(wNS, ref) {
					continue
				}
				t, ok := c.attr(wNS, "type")
				if !ok {
					t = "default"
				}
				if seen[t] {
					faults = append(faults, at("duplicate "+ref+" type in a section", c))
				}
				seen[t] = true
			}
		}
	}
	p.addFaults(main, faults)
}

// checkNumbering validates numbering definitions and references.
func (p *pkg) checkNumbering(main string, numbered []string) {
	nums := map[string]bool{}
	var faults []partFault
	if part, ok := p.adoptedPart(main, typeIs(ctypes.RelNumbering), "numbering.xml"); ok {
		if d := p.doc(part); d != nil {
			abstracts := map[string][]*element{}
			for a := d.root.firstEl(); a != nil; a = a.nextEl() {
				if !a.is(wNS, "abstractNum") {
					continue
				}
				id := a.attrOr(wNS, "abstractNumId")
				abstracts[id] = append(abstracts[id], a)
				levels := map[string]bool{}
				for l := a.firstEl(); l != nil; l = l.nextEl() {
					if !l.is(wNS, "lvl") {
						continue
					}
					ilvl := l.attrOr(wNS, "ilvl")
					if levels[ilvl] {
						faults = append(faults, partFault{part, "duplicate lvl ilvl in an abstractNum", []*element{l}})
					}
					levels[ilvl] = true
				}
			}
			if dup := repeated(abstracts); len(dup) > 0 {
				faults = append(faults, partFault{part, "duplicate abstractNumId", dup})
			}
			numCounts := map[string][]*element{}
			for n := d.root.firstEl(); n != nil; n = n.nextEl() {
				if !n.is(wNS, "num") {
					continue
				}
				id := n.attrOr(wNS, "numId")
				numCounts[id] = append(numCounts[id], n)
				nums[id] = true
				ok := false
				if r := n.firstChild(wNS, "abstractNumId"); r != nil {
					if v, has := r.attr(wNS, "val"); has {
						_, ok = abstracts[v]
					}
				}
				if !ok {
					faults = append(faults, partFault{part, "w:num names no abstractNum", []*element{n}})
				}
			}
			if dup := repeated(numCounts); len(dup) > 0 {
				faults = append(faults, partFault{part, "duplicate w:num numId", dup})
			}
		}
	}
	for _, part := range numbered {
		d := p.doc(part)
		if d == nil {
			continue
		}
		for numPr := range visible(d.root) {
			if !numPr.is(wNS, "numPr") {
				continue
			}
			if n := numPr.firstChild(wNS, "numId"); n != nil {
				if v, ok := n.attr(wNS, "val"); ok && v != "0" && !nums[v] {
					faults = append(faults, partFault{part, "numPr numId names no w:num", []*element{numPr}})
				}
			}
			if l := numPr.firstChild(wNS, "ilvl"); l != nil {
				v, ok := l.attr(wNS, "val")
				if !ok || len(v) != 1 || v[0] < '0' || v[0] > '8' {
					faults = append(faults, partFault{part, "numPr ilvl is outside 0-8", []*element{numPr}})
				}
			}
		}
	}
	p.addAll(faults)
}

// checkNotes validates footnote and endnote references and settings.
func (p *pkg) checkNotes(main string, stories []story) {
	settings, hasSettings := p.adoptedPart(main, typeIs(ctypes.RelSettings), "settings.xml")
	var faults []partFault
	for _, k := range []struct{ kind, rel string }{{"footnote", ctypes.RelFootnotes}, {"endnote", ctypes.RelEndnotes}} {
		ids := map[string]bool{}
		if notes, ok := p.firstTarget(main, typeIs(k.rel)); ok {
			if d := p.doc(notes); d != nil {
				for c := d.root.firstEl(); c != nil; c = c.nextEl() {
					if c.is(wNS, k.kind) {
						ids[c.attrOr(wNS, "id")] = true
					}
				}
			}
		}
		for _, s := range stories {
			d := p.doc(s.part)
			if d == nil {
				continue
			}
			for n := range visible(d.root) {
				if n.is(wNS, k.kind+"Reference") && !ids[n.attrOr(wNS, "id")] {
					faults = append(faults, partFault{s.part, k.kind + "Reference names no w:" + k.kind, []*element{n}})
				}
			}
		}
		// Separator notes named by the document-wide note properties.
		if !hasSettings {
			continue
		}
		d := p.doc(settings)
		if d == nil {
			continue
		}
		if props := d.root.firstChild(wNS, k.kind+"Pr"); props != nil {
			for note := props.firstEl(); note != nil; note = note.nextEl() {
				if note.is(wNS, k.kind) && !ids[note.attrOr(wNS, "id")] {
					faults = append(faults, partFault{settings, "settings " + k.kind + "Pr names no w:" + k.kind, []*element{note}})
				}
			}
		}
	}
	p.addAll(faults)
}
