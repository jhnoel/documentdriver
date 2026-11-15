package integrity

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// pairing collects the starts and ends of one kind of range by id.
type pairing struct {
	starts, ends map[string][]*element
}

// newPairing returns an empty start and end pairing.
func newPairing() pairing {
	return pairing{starts: map[string][]*element{}, ends: map[string][]*element{}}
}

// ids returns the ids seen as a start or an end, sorted.
func (p pairing) ids() []string {
	var ids []string
	for id := range p.starts {
		ids = append(ids, id)
	}
	for id := range p.ends {
		if _, ok := p.starts[id]; !ok {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// report adds a fault for each id with more than one start (located at
// the starts) or a start or end without its counterpart (at those).
func (p pairing) report(faults []fault, start, end string) []fault {
	for _, id := range p.ids() {
		s, e := p.starts[id], p.ends[id]
		if len(s) > 1 {
			faults = append(faults, at("duplicate "+start+" id", s...))
		}
		if len(s) > 0 && len(e) == 0 {
			faults = append(faults, at(start+" without a "+end, s...))
		}
		if len(e) > 0 && len(s) == 0 {
			faults = append(faults, at(end+" without a "+start, e...))
		}
	}
	return faults
}

// checkStory checks the pairing of ranges, fields and bookmarks within one
// story.
func (p *pkg) checkStory(part string, root *element, commentIDs map[string]bool) {
	var faults []fault
	bookmarks, comments, moveFrom, moveTo := newPairing(), newPairing(), newPairing(), newPairing()
	// Open fields, by their begin; true once the field reached its
	// separator.
	type field struct {
		begin     *element
		separated bool
	}
	var fields []field
	for n := range visible(root) {
		if n.ns != wNS {
			continue
		}
		id := n.attrOr(wNS, "id")
		switch n.local {
		case "bookmarkStart":
			bookmarks.starts[id] = append(bookmarks.starts[id], n)
		case "bookmarkEnd":
			bookmarks.ends[id] = append(bookmarks.ends[id], n)
		case "commentRangeStart":
			comments.starts[id] = append(comments.starts[id], n)
		case "commentRangeEnd":
			comments.ends[id] = append(comments.ends[id], n)
		case "moveFromRangeStart":
			moveFrom.starts[id] = append(moveFrom.starts[id], n)
		case "moveFromRangeEnd":
			moveFrom.ends[id] = append(moveFrom.ends[id], n)
		case "moveToRangeStart":
			moveTo.starts[id] = append(moveTo.starts[id], n)
		case "moveToRangeEnd":
			moveTo.ends[id] = append(moveTo.ends[id], n)
		case "fldChar":
			switch t, _ := n.attr(wNS, "fldCharType"); t {
			case "begin":
				fields = append(fields, field{begin: n})
			case "separate":
				switch {
				case len(fields) == 0:
					faults = append(faults, at("fldChar separate outside a field", n))
				case fields[len(fields)-1].separated:
					faults = append(faults, at("field has more than one fldChar separate", n))
				default:
					fields[len(fields)-1].separated = true
				}
			case "end":
				if len(fields) == 0 {
					faults = append(faults, at("fldChar end without a begin", n))
				} else {
					fields = fields[:len(fields)-1]
				}
			}
		case "instrText", "delInstrText":
			if len(fields) == 0 || fields[len(fields)-1].separated {
				faults = append(faults, at(n.local+" outside a field code", n))
			}
		}
	}
	if len(fields) > 0 {
		open := make([]*element, len(fields))
		for i, f := range fields {
			open[i] = f.begin
		}
		faults = append(faults, at("fldChar begin without an end", open...))
	}
	faults = bookmarks.report(faults, "bookmarkStart", "bookmarkEnd")
	faults = comments.report(faults, "commentRangeStart", "commentRangeEnd")
	faults = moveFrom.report(faults, "moveFromRangeStart", "moveFromRangeEnd")
	faults = moveTo.report(faults, "moveToRangeStart", "moveToRangeEnd")
	for _, id := range comments.ids() {
		if !commentIDs[id] {
			faults = append(faults, at("comment range names no w:comment", append(slices.Clone(comments.starts[id]), comments.ends[id]...)...))
		}
	}
	p.addFaults(part, faults)
}

// inDeletion returns the deletion n is in ("del" or "moveFrom"), looking
// no further out than a text box.
func inDeletion(n *element) string {
	for a := n.up(); a != nil; a = a.up() {
		if a.ns != wNS {
			continue
		}
		switch a.local {
		case "del", "moveFrom":
			return a.local
		case "txbxContent":
			return ""
		}
	}
	return ""
}

// preserved reports whether the nearest xml:space on n or an ancestor is
// "preserve".
func preserved(n *element) bool {
	for a := n; a != nil; a = a.up() {
		if v, ok := a.attr(xmlNS, "space"); ok {
			return v == "preserve"
		}
	}
	return false
}

// isEdgeSpace reports whether c is ASCII whitespace.
func isEdgeSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// hasParagraph reports whether e or its visible descendants include a paragraph.
func hasParagraph(e *element, self bool) bool {
	for d := range visible(e) {
		if (self || d != e) && d.is(wNS, "p") {
			return true
		}
	}
	return false
}

// checkStoryContent checks text markup and story shape: deleted text,
// significant white space, paragraphs Word requires, drawing ids, bookmark
// names, move range names.
func (p *pkg) checkStoryContent(part, kind string, d *document) {
	var faults []fault
	docPrIDs, bookmarkNames := map[string][]*element{}, map[string][]*element{}
	moveFromNames, moveToNames := map[string][]*element{}, map[string][]*element{}
	for n := range visible(d.root) {
		if n.is(wpNS, "docPr") {
			if id, ok := n.attr("", "id"); ok {
				docPrIDs[id] = append(docPrIDs[id], n)
			}
			continue
		}
		if n.ns != wNS {
			continue
		}
		switch n.local {
		case "t", "delText", "instrText", "delInstrText":
			text := n.text()
			if text != "" && (isEdgeSpace(text[0]) || isEdgeSpace(text[len(text)-1])) && !preserved(n) {
				// Named per pair: deleting or restoring converts t and
				// delText (instrText and delInstrText) faithfully.
				what := "run text"
				if strings.Contains(n.local, "nstr") {
					what = "field code"
				}
				faults = append(faults, at(what+` has leading or trailing space without xml:space="preserve"`, n))
			}
			del := inDeletion(n)
			switch {
			case n.local == "delText" && del == "":
				faults = append(faults, at("w:delText outside a deletion", n))
			case n.local == "delInstrText" && del == "":
				faults = append(faults, at("w:delInstrText outside a deletion", n))
			case n.local == "t" && del == "del":
				faults = append(faults, at("w:t inside a deletion", n))
			case n.local == "instrText" && del == "del":
				faults = append(faults, at("w:instrText inside a deletion", n))
			}
		case "bookmarkStart":
			if name, ok := n.attr(wNS, "name"); ok {
				bookmarkNames[name] = append(bookmarkNames[name], n)
			}
		case "moveFromRangeStart":
			name := n.attrOr(wNS, "name")
			moveFromNames[name] = append(moveFromNames[name], n)
		case "moveToRangeStart":
			name := n.attrOr(wNS, "name")
			moveToNames[name] = append(moveToNames[name], n)
		case "txbxContent":
			if !hasParagraph(n, false) {
				faults = append(faults, at("w:txbxContent has no paragraph", n))
			}
		case "footnote", "endnote", "comment":
			if n.up() == d.root && !hasParagraph(n, true) {
				faults = append(faults, at("w:"+n.local+" has no paragraph", n))
			}
		}
	}
	if (kind == "header" || kind == "footer") && !hasParagraph(d.root, true) {
		faults = append(faults, at(kind+" has no paragraph", d.root))
	}
	if dup := repeated(docPrIDs); len(dup) > 0 {
		faults = append(faults, at("duplicate wp:docPr id", dup...))
	}
	if dup := repeated(bookmarkNames); len(dup) > 0 {
		faults = append(faults, at("duplicate bookmark name", dup...))
	}
	if odd := unmatched(moveFromNames, moveToNames); len(odd) > 0 {
		faults = append(faults, at("move range name has no counterpart", odd...))
	}
	p.addFaults(part, faults)
}

// repeated returns the elements of every key that more than one element
// has, in document order.
func repeated(by map[string][]*element) []*element {
	var out []*element
	for _, els := range by {
		if len(els) > 1 {
			out = append(out, els...)
		}
	}
	slices.SortFunc(out, func(a, b *element) int { return int(a.index) - int(b.index) })
	return out
}

// unmatched returns the elements whose key the other map lacks, in
// document order.
func unmatched(a, b map[string][]*element) []*element {
	var out []*element
	for k, els := range a {
		if _, ok := b[k]; !ok {
			out = append(out, els...)
		}
	}
	for k, els := range b {
		if _, ok := a[k]; !ok {
			out = append(out, els...)
		}
	}
	slices.SortFunc(out, func(x, y *element) int { return int(x.index) - int(y.index) })
	return out
}

// wrapped appends the children of parent named name, looking through
// content-control and custom XML wrappers (depth bounded by the tree).
func wrapped(parent *element, name string, out []*element) []*element {
	for c := parent.firstEl(); c != nil; c = c.nextEl() {
		switch {
		case c.is(wNS, name):
			out = append(out, c)
		case c.is(wNS, "sdt"), c.is(wNS, "sdtContent"), c.is(wNS, "customXml"):
			out = wrapped(c, name, out)
		}
	}
	return out
}

// intVal returns the integer w:val of n, or def when n is nil or its value
// is not an integer.
func intVal(n *element, def int64) int64 {
	if n == nil {
		return def
	}
	v, ok := n.attr(wNS, "val")
	if !ok {
		return def
	}
	i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return def
	}
	return i
}

// childOf returns the child of n (which may be nil) named name in w:.
func childOf(n *element, name string) *element {
	if n == nil {
		return nil
	}
	return n.firstChild(wNS, name)
}

// mergeState is the vertical merge state of a grid column in a row.
type mergeState uint8

const (
	mergeNone mergeState = iota
	mergeRestart
	mergeContinue
)

// checkTables validates table grids, rows and cells in document d.
func (p *pkg) checkTables(part string, d *document) {
	var faults []fault
	for table := range visible(d.root) {
		if !table.is(wNS, "tbl") {
			continue
		}
		var columns int64
		if grid := table.firstChild(wNS, "tblGrid"); grid != nil {
			for c := grid.firstEl(); c != nil; c = c.nextEl() {
				if c.is(wNS, "gridCol") {
					columns++
				}
			}
		}
		rows := wrapped(table, "tr", nil)
		if len(rows) == 0 {
			faults = append(faults, at("table has no rows", table))
		}
		above := map[int64]mergeState{}
		for _, row := range rows {
			here := map[int64]mergeState{}
			trPr := row.firstChild(wNS, "trPr")
			position := intVal(childOf(trPr, "gridBefore"), 0)
			cells := wrapped(row, "tc", nil)
			if len(cells) == 0 {
				faults = append(faults, at("table row has no cells", row))
			}
			for _, cell := range cells {
				var last *element
				for c := cell.firstEl(); c != nil; c = c.nextEl() {
					if c.is(wNS, "p") || c.is(wNS, "tbl") || c.is(wNS, "sdt") || c.is(wNS, "customXml") {
						last = c
					}
				}
				switch {
				case last == nil:
					faults = append(faults, at("table cell has no paragraph", cell))
				case last.is(wNS, "tbl"):
					faults = append(faults, at("table cell does not end with a paragraph", cell))
				}
				tcPr := cell.firstChild(wNS, "tcPr")
				span := intVal(childOf(tcPr, "gridSpan"), 1)
				state := mergeNone
				if vm := childOf(tcPr, "vMerge"); vm != nil {
					state = mergeContinue
					if v, _ := vm.attr(wNS, "val"); v == "restart" {
						state = mergeRestart
					}
				}
				if s, ok := above[position]; state == mergeContinue && !(ok && s != mergeNone) {
					faults = append(faults, at("vMerge continue cell has no merged cell above it", cell))
				}
				here[position] = state
				position += span
			}
			position += intVal(childOf(trPr, "gridAfter"), 0)
			if columns != 0 && position != columns {
				faults = append(faults, at("row spans a different number of grid columns than tblGrid declares", row))
			}
			above = here
		}
	}
	p.addFaults(part, faults)
}

// isHex8Below reports whether v is eight hex digits whose value is below
// limit.
func isHex8Below(v string, limit uint64) bool {
	if len(v) != 8 {
		return false
	}
	n, err := strconv.ParseUint(v, 16, 64)
	return err == nil && n < limit
}

// checkIDs validates paragraph and revision ids in document d.
func (p *pkg) checkIDs(part string, d *document) {
	var faults []fault
	paraIDs, revisionIDs := map[string][]*element{}, map[string][]*element{}
	for n := range visible(d.root) {
		for _, attr := range []string{"paraId", "textId"} {
			v, ok := n.attr(w14NS, attr)
			if !ok {
				continue
			}
			if !isHex8Below(v, 0x8000_0000) {
				faults = append(faults, at("w14:"+attr+" is not eight hex digits below 80000000", n))
			} else if attr == "paraId" {
				paraIDs[strings.ToUpper(v)] = append(paraIDs[strings.ToUpper(v)], n)
			}
		}
		if n.ns == wNS && revisionElements[n.local] {
			if id, ok := n.attr(wNS, "id"); ok {
				revisionIDs[id] = append(revisionIDs[id], n)
			}
		}
	}
	if dup := repeated(paraIDs); len(dup) > 0 {
		faults = append(faults, at("duplicate w14:paraId", dup...))
	}
	if dup := repeated(revisionIDs); len(dup) > 0 {
		faults = append(faults, at("duplicate revision w:id", dup...))
	}
	p.addFaults(part, faults)
}

// checkParagraphIDsAcrossParts checks that a w14:paraId identifies one
// paragraph across the whole document: comment extension parts address
// comment paragraphs by it.
func (p *pkg) checkParagraphIDsAcrossParts(stories []story) {
	owner := map[string]string{}
	var parts []string
	shared := map[string][]*element{}
	for _, s := range stories {
		d := p.doc(s.part)
		if d == nil {
			continue
		}
		own := map[string][]*element{}
		for n := range visible(d.root) {
			if v, ok := n.attr(w14NS, "paraId"); ok {
				own[strings.ToUpper(v)] = append(own[strings.ToUpper(v)], n)
			}
		}
		for _, v := range slices.Sorted(maps.Keys(own)) {
			first, ok := owner[v]
			switch {
			case !ok:
				owner[v] = s.part
			case first != s.part:
				if _, seen := shared[s.part]; !seen {
					parts = append(parts, s.part)
				}
				shared[s.part] = append(shared[s.part], own[v]...)
			}
		}
	}
	for _, part := range parts {
		p.add(part, "w14:paraId also used in another part", shared[part]...)
	}
}
