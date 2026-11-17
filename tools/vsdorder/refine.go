package main

import (
	"maps"
	"slices"
	"strings"
)

// tableEntry is one rendered child-order entry (v:<name>).
type tableEntry struct {
	name     string
	slots    []slotDef
	wildcard bool
}

// pairRef is one corpus pair inside an interleaved XSD slot, with the
// evidence in both directions (one provenance refinement record):
// before is how often first precedes second, after how often second
// precedes first, and the file counts are the distinct files seen in
// each direction. A zero side means a consistent one-way order.
type pairRef struct {
	parent                  string
	first, second           string
	before, after           int
	beforeFiles, afterFiles int
}

// disagreement is one corpus/XSD disagreement: a child admitted only
// through a wildcard, or an element the XSD does not declare.
type disagreement struct {
	parent   string // v:<Parent>, or "" for the element form
	child    string
	kind     string // "wildcard_only", "not_admitted" or "undeclared"
	files    int
	parents  int      // distinct parent elements holding the child
	occurs   int      // child occurrences
	children []string // for "undeclared": child names in corpus order
}

// result is the rendered table plus its provenance inputs.
type result struct {
	xsdSHA256     string
	entries       []tableEntry
	roots         []globalElem
	disagreements []disagreement
	refinements   []pairRef
	wildcards     []string
	st            *corpusStats
	scannedFiles  []string
	acceptedFiles []string
}

// refine merges the XSD slots with the corpus evidence of st and
// returns the final table entries in sorted key order. The Text
// element (mixed content) is never refined.
func refine(x *xsdData, st *corpusStats, scanned, accepted []string) (*result, error) {
	declared := func(name string) bool {
		_, ok := x.decls[name]
		return ok
	}
	// Reachable element names from the global elements, following
	// merged XSD slots.
	reach := map[string]bool{}
	var queue []string
	for _, g := range x.globals {
		queue = append(queue, g.name)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if reach[name] {
			continue
		}
		reach[name] = true
		slots, _, err := x.merged(name)
		if err != nil {
			return nil, err
		}
		for _, s := range slots {
			for _, c := range s.Names {
				if !reach[c] {
					queue = append(queue, c)
				}
			}
		}
	}
	// Corpus parents with no XSD declaration join the table as
	// corpus-built entries (Trigger below); Corpus children the XSD
	// does not admit in a declared parent are added to that parent
	// and recorded as disagreements.
	entries := map[string]*tableEntry{}
	entryFor := func(name string) *tableEntry {
		if e, ok := entries[name]; ok {
			return e
		}
		e := &tableEntry{name: name}
		entries[name] = e
		return e
	}
	for _, name := range slices.Sorted(maps.Keys(reach)) {
		e := entryFor(name)
		if declared(name) {
			slots, wild, err := x.merged(name)
			if err != nil {
				return nil, err
			}
			e.slots = slots
			e.wildcard = wild
		} else if st != nil {
			e.slots = corpusSlots(st, name)
		}
	}
	var dis []disagreement
	if st != nil {
		seenDis := map[parentChildKey]bool{}
		for _, pc := range sortedParentChildKeys(st) {
			if !declared(pc.parent) {
				continue
			}
			e := entryFor(pc.parent)
			reach[pc.parent] = true
			if slotHas(e.slots, pc.child) {
				continue
			}
			kind := "wildcard_only"
			if !e.wildcard {
				kind = "not_admitted"
			}
			if !seenDis[pc] {
				seenDis[pc] = true
				dis = append(dis, disagreement{
					parent: "v:" + pc.parent, child: pc.child, kind: kind,
					files: len(st.pcFiles[pc]), parents: st.pcParents[pc], occurs: st.pcCount[pc],
				})
			}
			addChildToSlots(st, e, pc.child)
		}
		for _, p := range sortedParentNames(st) {
			if declared(p) {
				continue
			}
			e := entryFor(p)
			reach[p] = true
			if e.slots == nil {
				e.slots = corpusSlots(st, p)
			}
			files := map[string]bool{}
			for pc, fs := range st.pcFiles {
				if pc.parent != p {
					continue
				}
				for f := range fs {
					files[f] = true
				}
			}
			dis = append(dis, disagreement{
				parent: "", child: p, kind: "undeclared",
				files: len(files), parents: st.childCount[p], occurs: st.childCount[p],
				children: slices.Clone(st.parentFirst[p]),
			})
		}
	}
	// Corpus refinements of interleaved XSD slots.
	var refs []pairRef
	if st != nil {
		for _, e := range sortedEntries(entries) {
			if e.name == "Text" {
				continue
			}
			slots, slotRefs := refineSlots(st, e)
			e.slots = slots
			refs = append(refs, slotRefs...)
		}
	}
	slices.SortFunc(refs, func(a, b pairRef) int {
		if c := strings.Compare(a.parent, b.parent); c != 0 {
			return c
		}
		if c := strings.Compare(a.first, b.first); c != 0 {
			return c
		}
		return strings.Compare(a.second, b.second)
	})
	slices.SortFunc(dis, func(a, b disagreement) int {
		ra, rb := disKindRank(a.kind), disKindRank(b.kind)
		if ra != rb {
			if ra < rb {
				return -1
			}
			return 1
		}
		if c := strings.Compare(a.parent, b.parent); c != 0 {
			return c
		}
		return strings.Compare(a.child, b.child)
	})
	var wildcards []string
	for _, e := range sortedEntries(entries) {
		if e.wildcard {
			wildcards = append(wildcards, "v:"+e.name)
		}
	}
	out := make([]tableEntry, 0, len(entries))
	for _, e := range sortedEntries(entries) {
		out = append(out, *e)
	}
	return &result{
		xsdSHA256: x.sha256, entries: out, roots: slices.Clone(x.globals), disagreements: dis, refinements: refs,
		wildcards: wildcards, st: st, scannedFiles: scanned, acceptedFiles: accepted,
	}, nil
}

// sortedParentNames returns the sorted parent names seen in st.
func sortedParentNames(st *corpusStats) []string {
	seen := map[string]bool{}
	for pc := range st.pcCount {
		seen[pc.parent] = true
	}
	return slices.Sorted(maps.Keys(seen))
}

// sortedParentChildKeys returns the parent/child pairs of st sorted.
func sortedParentChildKeys(st *corpusStats) []parentChildKey {
	return slices.SortedFunc(maps.Keys(st.pcCount), func(a, b parentChildKey) int {
		if c := strings.Compare(a.parent, b.parent); c != 0 {
			return c
		}
		return strings.Compare(a.child, b.child)
	})
}

// sortedEntries returns the entries sorted by name.
func sortedEntries(m map[string]*tableEntry) []*tableEntry {
	out := make([]*tableEntry, 0, len(m))
	for _, name := range slices.Sorted(maps.Keys(m)) {
		out = append(out, m[name])
	}
	return out
}

// slotHas reports whether child occurs in any slot.
func slotHas(slots []slotDef, child string) bool {
	for _, s := range slots {
		if slices.Contains(s.Names, child) {
			return true
		}
	}
	return false
}

// corpusSlots builds the slots of a corpus-only parent from its
// children's first-appearance order, each its own slot, repeated
// when the corpus repeats it within one parent instance.
func corpusSlots(st *corpusStats, parent string) []slotDef {
	var out []slotDef
	for _, child := range st.parentFirst[parent] {
		mu := st.parentMaxRep[parentChildKey{parent: parent, child: child}] > 1
		out = append(out, slotDef{Names: []string{child}, Multi: mu})
	}
	return out
}

// addChildToSlots adds a corpus child to entry e: into the
// interleaved slot it has the most pair evidence with, or as a new
// single slot before the first slot the corpus always shows after
// the child.
func addChildToSlots(st *corpusStats, e *tableEntry, child string) {
	bestSlot, bestScore := -1, 0
	for i, s := range e.slots {
		if len(s.Names) < 2 {
			continue
		}
		score := 0
		for _, m := range s.Names {
			score += st.pairCount[pairKey{parent: e.name, first: m, second: child}]
			score += st.pairCount[pairKey{parent: e.name, first: child, second: m}]
		}
		if score > bestScore {
			bestScore, bestSlot = score, i
		}
	}
	mu := st.parentMaxRep[parentChildKey{parent: e.name, child: child}] > 1
	if bestSlot >= 0 {
		e.slots[bestSlot].Names = append(e.slots[bestSlot].Names, child)
		e.slots[bestSlot].MemberMu = append(e.slots[bestSlot].MemberMu, mu)
		return
	}
	at := len(e.slots)
	for i, s := range e.slots {
		have := false
		for _, m := range s.Names {
			if st.pairCount[pairKey{parent: e.name, first: child, second: m}] > 0 {
				have = true
			}
		}
		if have {
			at = i
			break
		}
	}
	slot := slotDef{Names: []string{child}, Multi: mu}
	e.slots = append(e.slots, slotDef{})
	copy(e.slots[at+1:], e.slots[at:])
	e.slots[at] = slot
}

// refineSlots refines the interleaved slots of entry e using the
// corpus pairs of st, returning the refined slots and their pair
// records.
func refineSlots(st *corpusStats, e *tableEntry) ([]slotDef, []pairRef) {
	var out []slotDef
	var refs []pairRef
	for _, s := range e.slots {
		if len(s.Names) < 2 {
			out = append(out, s)
			continue
		}
		refined, slotRefs, ok := refineInterleaved(st, e.name, s)
		if !ok {
			out = append(out, s)
			refs = append(refs, slotRefs...)
			continue
		}
		out = append(out, refined...)
		refs = append(refs, slotRefs...)
	}
	return out, refs
}

// comp is a both-orders component of one interleaved slot.
type comp struct {
	members []string
	index   int // first member's position in the XSD slot
}

// refineInterleaved splits one interleaved slot by its consistent
// corpus pairs and keeps both-orders pairs together. ok is false
// when the corpus gives no pair evidence for the slot in at least
// two files: the slot is kept whole and no refinement is recorded.
func refineInterleaved(st *corpusStats, parent string, s slotDef) ([]slotDef, []pairRef, bool) {
	names := s.Names
	parentOf := make([]int, len(names))
	for i := range parentOf {
		parentOf[i] = i
	}
	find := func(i int) int {
		for parentOf[i] != i {
			parentOf[i] = parentOf[parentOf[i]]
			i = parentOf[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if rb < ra {
			ra, rb = rb, ra
		}
		parentOf[rb] = ra
	}
	var refs []pairRef
	evidenced := make([]bool, len(names))
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := names[i], names[j]
			fwd := st.pairCount[pairKey{parent: parent, first: a, second: b}]
			rev := st.pairCount[pairKey{parent: parent, first: b, second: a}]
			if fwd == 0 && rev == 0 {
				continue
			}
			evidenced[i], evidenced[j] = true, true
			ref := pairRef{parent: "v:" + parent, before: fwd, after: rev,
				beforeFiles: len(st.pairFiles[pairKey{parent: parent, first: a, second: b}]),
				afterFiles:  len(st.pairFiles[pairKey{parent: parent, first: b, second: a}])}
			switch {
			case fwd > 0 && rev > 0:
				ref.first, ref.second = a, b
				union(i, j)
			case fwd > 0:
				ref.first, ref.second = a, b
			default:
				ref.first, ref.second = b, a
				ref.before, ref.after = rev, fwd
				ref.beforeFiles, ref.afterFiles = ref.afterFiles, ref.beforeFiles
			}
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil, nil, false
	}
	eligible := slices.ContainsFunc(refs, func(r pairRef) bool { return r.beforeFiles >= 2 || r.afterFiles >= 2 })
	if !eligible {
		return nil, nil, false
	}
	var leftover []string
	var leftoverMu []bool
	for i, n := range names {
		if !evidenced[i] {
			leftover = append(leftover, n)
			if i < len(s.MemberMu) {
				leftoverMu = append(leftoverMu, s.MemberMu[i])
			}
		}
	}
	compOf := map[int]int{}
	var comps []*comp
	for i, n := range names {
		if !evidenced[i] {
			continue
		}
		r := find(i)
		if ci, ok := compOf[r]; ok {
			comps[ci].members = append(comps[ci].members, n)
			continue
		}
		compOf[r] = len(comps)
		comps = append(comps, &comp{members: []string{n}, index: i})
	}
	indeg := make([]int, len(comps))
	edges := map[[2]int]bool{}
	for i, a := range names {
		for j, b := range names {
			if i == j || !evidenced[i] || !evidenced[j] {
				continue
			}
			if st.pairCount[pairKey{parent: parent, first: a, second: b}] == 0 || st.pairCount[pairKey{parent: parent, first: b, second: a}] > 0 {
				continue
			}
			ci, cj := compOf[find(i)], compOf[find(j)]
			if ci != cj && !edges[[2]int{ci, cj}] {
				edges[[2]int{ci, cj}] = true
				indeg[cj]++
			}
		}
	}
	var out []slotDef
	for _, c := range topoComps(comps, indeg, edges) {
		if len(c.members) == 1 {
			mu := s.Multi
			if idx := slices.Index(names, c.members[0]); idx >= 0 && idx < len(s.MemberMu) {
				mu = s.MemberMu[idx]
			}
			out = append(out, slotDef{Names: c.members, Multi: mu, MemberMu: []bool{mu}})
		} else {
			out = append(out, slotDef{Names: c.members, Multi: s.Multi})
		}
	}
	if len(leftover) > 0 {
		out = append(out, slotDef{Names: leftover, Multi: s.Multi, MemberMu: leftoverMu})
	}
	return out, refs, true
}

// disKindRank ranks disagreement kinds for provenance order:
// wildcard children first, then unadmitted, then undeclared.
func disKindRank(kind string) int {
	switch kind {
	case "wildcard_only":
		return 0
	case "not_admitted":
		return 1
	default:
		return 2
	}
}

// topoComps returns comps in topological order, breaking ties by
// each component's first member's position in the XSD slot. Cycles
// (never expected from a consistent corpus) fall back to XSD order.
func topoComps(comps []*comp, indeg []int, edges map[[2]int]bool) []*comp {
	n := len(comps)
	done := make([]bool, n)
	var ordered []*comp
	for len(ordered) < n {
		best := -1
		for i, c := range comps {
			if done[i] || indeg[i] > 0 {
				continue
			}
			if best < 0 || c.index < comps[best].index {
				best = i
			}
		}
		if best < 0 {
			for i, c := range comps {
				if !done[i] {
					ordered = append(ordered, c)
					done[i] = true
				}
			}
			break
		}
		done[best] = true
		ordered = append(ordered, comps[best])
		for k := range edges {
			if k[0] == best {
				indeg[k[1]]--
			}
		}
	}
	return ordered
}
