package model

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// List numbering, ported from docxdriver's html/ctx.rs (itself a port of
// the reference engine's numbering module): the effective numId and level
// come from direct w:numPr or the paragraph style's w:basedOn chain;
// counters chain by abstractNumId; a startOverride restarts once per (numId,
// level); a lvlOverride's w:lvl replaces the level. The Current and Baseline
// views count only the paragraphs accept-all and reject-all leave, by the
// properties those leave (see SettledAway). Every bullet is "•".

// SettledAway reports whether settling v's revisions takes the paragraph's
// mark away, so it is not one of that view's paragraphs: accept-all
// (Current) joins a paragraph with a deleted mark into the next, reject-all
// (Baseline) does that with an inserted mark or drops the paragraph. The
// All view keeps every paragraph.
func SettledAway(p *tree.Node, v View) bool {
	switch v {
	case Current:
		return ParagraphMarkDel(p) != nil && ParagraphJoinTarget(p) != nil
	case Baseline:
		return ParagraphMarkIns(p) != nil && (ParagraphJoinTarget(p) != nil || RejectedInsertionVanishes(p))
	}
	return false
}

// NumberingPartName returns the numbering part of the main document: the
// target of its first internal relationship whose type ends in
// "/numbering", else numbering.xml beside it.
func (p *Package) NumberingPartName() string {
	main := p.DocumentPartName()
	if x, ok := p.Part(RelsPartFor(main)); ok {
		// Any element with a matching Type counts, as in docxdriver.
		found, done := "", false
		x.Doc.Node().Walk(func(n *tree.Node) bool {
			if done {
				return false
			}
			t, ok := Attr(n, "Type")
			if !ok || !strings.HasSuffix(t, "/numbering") || AttrOr(n, "TargetMode") == "External" {
				return true
			}
			if target, ok := Attr(n, "Target"); ok {
				found, done = ResolveRelTarget(main, target), true
				return false
			}
			return true
		})
		if done {
			return found
		}
	}
	return p.SiblingPart("numbering.xml")
}

type levelDef struct {
	fmt     string
	text    string
	start   int64
	restart uint32
	hasRst  bool
}

type levelOverride struct {
	start    int64
	hasStart bool
	level    *levelDef
}

type numDef struct {
	abstractID string
	overrides  map[uint32]*levelOverride
}

type styleNumbering struct {
	numID      string
	hasNumID   bool
	ilvl       uint32
	hasIlvl    bool
	basedOn    string
	hasBasedOn bool
}

// Numbering holds the numbering and paragraph-style tables of a package.
type Numbering struct {
	abstracts map[string]map[uint32]*levelDef
	nums      map[string]*numDef
	styles    map[string]*styleNumbering
}

// LoadNumbering reads the numbering part and the paragraph styles' numbering.
// Parts that are absent or not well formed contribute nothing.
func LoadNumbering(p *Package) *Numbering {
	t := &Numbering{
		abstracts: map[string]map[uint32]*levelDef{},
		nums:      map[string]*numDef{},
		styles:    map[string]*styleNumbering{},
	}
	if x, ok := p.Part(p.NumberingPartName()); ok {
		t.parseNumbering(x)
	}
	if x, ok := p.Part(p.StylesPartName()); ok {
		t.parseStyles(x)
	}
	return t
}

// parseLevel returns the level definition of a w:lvl element, defaulting to decimal from 1.
func parseLevel(lvl *tree.Node) *levelDef {
	d := &levelDef{fmt: "decimal", start: 1}
	for c := range lvl.ChildrenSeq() {
		switch {
		case IsW(c, "numFmt"):
			if v, ok := Attr(c, "val"); ok {
				d.fmt = v
			}
		case IsW(c, "lvlText"):
			if v, ok := Attr(c, "val"); ok {
				d.text = v
			}
		case IsW(c, "lvlRestart"):
			v, _ := Attr(c, "val")
			d.restart, d.hasRst = ParseU32(v)
		case IsW(c, "start"):
			if v, ok := Attr(c, "val"); ok {
				if n, ok := ParseI64(v); ok {
					d.start = n
				}
			}
		}
	}
	return d
}

// parseNumbering parses the numbering part x into the numbering's abstracts and nums.
func (t *Numbering) parseNumbering(x *XML) {
	x.Doc.Node().Walk(func(n *tree.Node) bool {
		switch {
		case IsW(n, "abstractNum"):
			id, ok := Attr(n, "abstractNumId")
			if !ok {
				return true
			}
			levels := t.abstracts[id]
			if levels == nil {
				levels = map[uint32]*levelDef{}
				t.abstracts[id] = levels
			}
			for lvl := range n.ChildrenSeq() {
				if !IsW(lvl, "lvl") {
					continue
				}
				v, _ := Attr(lvl, "ilvl")
				if il, ok := ParseU32(v); ok {
					levels[il] = parseLevel(lvl)
				}
			}
		case IsW(n, "num"):
			numID, ok := Attr(n, "numId")
			if !ok {
				return true
			}
			var abstractID string
			hasAbstract := false
			overrides := map[uint32]*levelOverride{}
			for c := range n.ChildrenSeq() {
				switch {
				case IsW(c, "abstractNumId"):
					abstractID, hasAbstract = Attr(c, "val")
				case IsW(c, "lvlOverride"):
					v, _ := Attr(c, "ilvl")
					il, ok := ParseU32(v)
					if !ok {
						continue
					}
					o := &levelOverride{}
					for in := range c.ChildrenSeq() {
						switch {
						case IsW(in, "startOverride"):
							v, _ := Attr(in, "val")
							o.start, o.hasStart = ParseI64(v)
						case IsW(in, "lvl"):
							o.level = parseLevel(in)
						}
					}
					overrides[il] = o
				}
			}
			if hasAbstract {
				t.nums[numID] = &numDef{abstractID: abstractID, overrides: overrides}
			}
		}
		return true
	})
}

// parseStyles parses the paragraph styles' numbering in styles part x.
func (t *Numbering) parseStyles(x *XML) {
	x.Doc.Node().Walk(func(n *tree.Node) bool {
		if !IsW(n, "style") {
			return true
		}
		if typ, ok := Attr(n, "type"); !ok || typ != "paragraph" {
			return true
		}
		id, ok := Attr(n, "styleId")
		if !ok {
			return true
		}
		e := &styleNumbering{}
		for c := range n.ChildrenSeq() {
			switch {
			case IsW(c, "basedOn"):
				e.basedOn, e.hasBasedOn = Attr(c, "val")
			case IsW(c, "pPr"):
				for prop := range c.ChildrenSeq() {
					if !IsW(prop, "numPr") {
						continue
					}
					for np := range prop.ChildrenSeq() {
						switch {
						case IsW(np, "numId"):
							e.numID, e.hasNumID = Attr(np, "val")
						case IsW(np, "ilvl"):
							v, _ := Attr(np, "val")
							e.ilvl, e.hasIlvl = ParseU32(v)
						}
					}
				}
			}
		}
		t.styles[id] = e
		return true
	})
}

// effectiveLevel is the level definition of (numID, level): a lvlOverride's
// w:lvl wins over the abstract definition.
func (t *Numbering) effectiveLevel(numID string, level uint32) *levelDef {
	num := t.nums[numID]
	if num == nil {
		return nil
	}
	if o := num.overrides[level]; o != nil && o.level != nil {
		return o.level
	}
	return t.abstracts[num.abstractID][level]
}

// startOverride returns the level start override of numID and level, if present.
func (t *Numbering) startOverride(numID string, level uint32) (int64, bool) {
	if num := t.nums[numID]; num != nil {
		if o := num.overrides[level]; o != nil && o.hasStart {
			return o.start, true
		}
	}
	return 0, false
}

// effectiveStart returns the start of numID and level, defaulting to 1.
func (t *Numbering) effectiveStart(numID string, level uint32) int64 {
	if s, ok := t.startOverride(numID, level); ok {
		return s
	}
	if d := t.effectiveLevel(numID, level); d != nil {
		return d.start
	}
	return 1
}

// resolveStyle walks a style's w:basedOn chain (at most 32 steps) to the
// first style with a numId.
func (t *Numbering) resolveStyle(id string) (string, uint32, bool) {
	cur := id
	for range 32 {
		e := t.styles[cur]
		if e == nil {
			return "", 0, false
		}
		if e.hasNumID {
			if e.hasIlvl {
				return e.numID, e.ilvl, true
			}
			return e.numID, 0, true
		}
		if !e.hasBasedOn {
			return "", 0, false
		}
		cur = e.basedOn
	}
	return "", 0, false
}

// continuationPrefix marks a bookmark that makes its paragraph a
// continuation of a list item ("_docxdriver_cont_<numId>_<level>").
const continuationPrefix = "_docxdriver_cont_"

// continuationMembership reads the first continuation bookmark directly in
// the paragraph; a malformed one gives no membership.
func continuationMembership(p *tree.Node) (string, uint32, bool) {
	for n := range p.ChildrenSeq() {
		if !IsW(n, "bookmarkStart") {
			continue
		}
		name, ok := Attr(n, "name")
		if !ok {
			continue
		}
		rest, ok := strings.CutPrefix(name, continuationPrefix)
		if !ok {
			continue
		}
		parts := strings.Split(rest, "_")
		if len(parts) < 2 {
			return "", 0, false
		}
		level, ok := ParseU32(parts[1])
		if !ok {
			return "", 0, false
		}
		return parts[0], level, true
	}
	return "", 0, false
}

// paragraphNumbering is a paragraph's effective (numId, level): a
// continuation bookmark, else direct w:numPr with a numId, else its style's.
func (t *Numbering) paragraphNumbering(p *tree.Node, v View) (string, uint32, bool) {
	if id, level, ok := continuationMembership(p); ok {
		return id, level, true
	}
	ppr := PropertyBag(p, "pPr", v == Baseline)
	if ppr == nil {
		return "", 0, false
	}
	if numPr := ChildW(ppr, "numPr"); numPr != nil {
		var numID string
		hasNum := false
		var ilvl uint32
		for c := range numPr.ChildrenSeq() {
			switch {
			case IsW(c, "numId"):
				numID, hasNum = Attr(c, "val")
			case IsW(c, "ilvl"):
				v, _ := Attr(c, "val")
				ilvl, _ = ParseU32(v)
			}
		}
		if hasNum {
			return numID, ilvl, true
		}
	}
	ps := ChildW(ppr, "pStyle")
	if ps == nil {
		return "", 0, false
	}
	style, ok := Attr(ps, "val")
	if !ok {
		return "", 0, false
	}
	return t.resolveStyle(style)
}

// ListMarkers returns the rendered list label of every numbered story
// paragraph under scope ("1.", "a)", "•", ...) as view v counts them.
func (t *Numbering) ListMarkers(scope *tree.Node, v View) map[*tree.Node]string {
	markers := map[*tree.Node]string{}
	if scope == nil || len(t.nums) == 0 {
		return markers
	}
	// Counters chain by abstractNumId; a slot absent from the map is
	// docxdriver's None.
	counters := map[string]map[uint32]int64{}
	applied := map[string]map[uint32]bool{}
	for _, para := range ParagraphsIn(scope) {
		p := para.Node
		if SettledAway(p, v) {
			continue
		}
		numID, level, ok := t.paragraphNumbering(p, v)
		if !ok {
			continue
		}
		if _, _, cont := continuationMembership(p); cont {
			continue
		}
		if numID == "0" || t.effectiveLevel(numID, level) == nil {
			continue
		}
		key := t.nums[numID].abstractID
		slots := counters[key]
		if slots == nil {
			slots = map[uint32]int64{}
			counters[key] = slots
		}
		if applied[numID] == nil {
			applied[numID] = map[uint32]bool{}
		}
		if start, ok := t.startOverride(numID, level); ok && !applied[numID][level] {
			slots[level] = start
			applied[numID][level] = true
		} else if cur, ok := slots[level]; !ok {
			slots[level] = t.effectiveStart(numID, level)
		} else {
			slots[level] = cur + 1
		}
		for idx := range slots {
			if idx <= level {
				continue
			}
			d := t.effectiveLevel(numID, idx)
			if d == nil || !d.hasRst || d.restart == level+1 {
				delete(slots, idx)
			}
		}
		if label := t.renderLabel(numID, level, slots); label != "" {
			markers[p] = label
		}
	}
	return markers
}

// renderLabel expands the level's lvlText: %1..%9 become the formatted
// counter of levels 0..8 (an uncounted level shows its start value).
// Bullets are "•"; the "none" format is the literal text.
func (t *Numbering) renderLabel(numID string, level uint32, counters map[uint32]int64) string {
	d := t.effectiveLevel(numID, level)
	if d == nil {
		return ""
	}
	if d.fmt == "bullet" {
		return "•"
	}
	if d.fmt == "none" {
		return strings.TrimSpace(d.text)
	}
	var b strings.Builder
	rs := []rune(d.text)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '%' {
			b.WriteRune(rs[i])
			continue
		}
		if i+1 < len(rs) && rs[i+1] >= '1' && rs[i+1] <= '9' {
			i++
			ref := uint32(rs[i] - '1')
			rd := t.effectiveLevel(numID, ref)
			if rd == nil {
				continue
			}
			val, ok := counters[ref]
			if !ok {
				val = t.effectiveStart(numID, ref)
			}
			b.WriteString(formatNumber(val, rd.fmt))
			continue
		}
		b.WriteRune('%')
	}
	return strings.TrimSpace(b.String())
}

// formatNumber formats v as decimal, zero-padded decimal, letters or Roman numerals per f.
func formatNumber(v int64, f string) string {
	switch {
	case f == "decimalZero" && v < 10:
		return "0" + strconv.FormatInt(v, 10)
	case f == "lowerLetter":
		return alpha(v)
	case f == "upperLetter":
		return strings.ToUpper(alpha(v))
	case f == "lowerRoman":
		return roman(v)
	case f == "upperRoman":
		return strings.ToUpper(roman(v))
	}
	return strconv.FormatInt(v, 10)
}

// alpha is spreadsheet-style letters: 1 a, 26 z, 27 aa.
func alpha(v int64) string {
	if v <= 0 {
		return strconv.FormatInt(v, 10)
	}
	var out []byte
	for v > 0 {
		v--
		out = append(out, byte('a'+v%26))
		v /= 26
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// roman returns the lowercase Roman numeral of v, or decimal when v is not positive.
func roman(v int64) string {
	if v <= 0 {
		return strconv.FormatInt(v, 10)
	}
	table := []struct {
		n int64
		s string
	}{{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"}, {100, "c"}, {90, "xc"}, {50, "l"}, {40, "xl"}, {10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"}}
	var b strings.Builder
	for _, e := range table {
		for v >= e.n {
			b.WriteString(e.s)
			v -= e.n
		}
	}
	return b.String()
}

// ListInfo is a numbered paragraph's list membership.
type ListInfo struct {
	Continuation bool // a continuation paragraph: no item of its own
	ID           string
	Level        uint32
	Format       string
	Value        int64
	Start        int64
	Continued    bool // the count does not start at Start here
}

type listKey struct {
	id    string
	level uint32
}

// ListInfo returns the list membership of every numbered story paragraph
// under scope as view v counts them. Unlike the markers, counters here run
// per (numId, level).
func (t *Numbering) ListInfo(scope *tree.Node, v View) map[*tree.Node]ListInfo {
	out := map[*tree.Node]ListInfo{}
	if scope == nil {
		return out
	}
	counters := map[listKey]int64{}
	for _, para := range ParagraphsIn(scope) {
		p := para.Node
		if SettledAway(p, v) {
			continue
		}
		id, level, ok := t.paragraphNumbering(p, v)
		if !ok {
			continue
		}
		d := t.effectiveLevel(id, level)
		if d == nil {
			continue
		}
		_, _, cont := continuationMembership(p)
		if !cont {
			// level+1 wraps like docxdriver's release build.
			for deeper := level + 1; deeper < 9; deeper++ {
				dd := t.effectiveLevel(id, deeper)
				if dd == nil || !dd.hasRst || dd.restart == level+1 {
					delete(counters, listKey{id, deeper})
				}
			}
		}
		k := listKey{id, level}
		start := t.effectiveStart(id, level)
		count, ok := counters[k]
		if !ok {
			count = start - 1
		}
		if !cont {
			count++
		}
		counters[k] = count
		out[p] = ListInfo{
			Continuation: cont, ID: id, Level: level, Format: d.fmt,
			Value: count, Start: start, Continued: count != start,
		}
	}
	return out
}
