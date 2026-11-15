package integrity

import (
	"cmp"
	"maps"
	"slices"
	"strings"
)

// Finding is one error kind: the part it was found in (a zip entry name,
// "" for the package as a whole) and a message without positions.
type Finding struct {
	Part    string
	Message string
}

// compareFindings orders findings by part then message.
func compareFindings(a, b Finding) int {
	return cmp.Or(strings.Compare(a.Part, b.Part), strings.Compare(a.Message, b.Message))
}

// Instance is one occurrence of an error kind, located: the elements of
// its part it is about, by their position in the part's elements in
// document order (the root element is 0). A fault of one element (a
// schema fault, a dangling reference) names that element; a fault of
// several (a duplicate id, a range without its end) names each one
// involved; a fault of a story or table names the nearest element that
// holds it. Nodes is empty for a fault of a part as a whole (it does not
// parse, it has no content type) or of the package.
type Instance struct {
	Finding
	Nodes []int
}

// Findings counts the error kinds of one package and keeps each
// occurrence with its location. The zero value is empty and ready to use.
type Findings struct {
	counts    map[Finding]int
	instances []Instance
	// docs are the parsed parts the instances index into, kept by check
	// for the gate's attribution (GateEdit); nil for findings built by
	// hand.
	docs map[string]*document
}

// Add records one occurrence of the error kind (part, message), located
// at the elements with document-order positions nodes (none: the part as
// a whole).
func (f *Findings) Add(part, message string, nodes ...int) {
	if f.counts == nil {
		f.counts = map[Finding]int{}
	}
	k := Finding{Part: part, Message: message}
	f.counts[k]++
	ns := slices.Compact(slices.Sorted(slices.Values(nodes)))
	f.instances = append(f.instances, Instance{Finding: k, Nodes: ns})
}

// addAt records an occurrence located at elements els.
func (f *Findings) addAt(part, message string, els []*element) {
	nodes := make([]int, 0, len(els))
	for _, e := range els {
		if e != nil {
			nodes = append(nodes, int(e.index))
		}
	}
	f.Add(part, message, nodes...)
}

// Kinds returns the error kinds, ordered by part, then message.
func (f *Findings) Kinds() []Finding {
	out := slices.Collect(maps.Keys(f.counts))
	slices.SortFunc(out, compareFindings)
	return out
}

// Instances returns every occurrence, located, in the order found.
func (f *Findings) Instances() []Instance { return slices.Clone(f.instances) }

// Count returns how often the error kind k occurs.
func (f *Findings) Count(k Finding) int { return f.counts[k] }

// Has reports whether the error kind k occurs.
func (f *Findings) Has(k Finding) bool { return f.counts[k] > 0 }

// Len returns the number of distinct error kinds.
func (f *Findings) Len() int { return len(f.counts) }

// Empty reports whether there are no findings.
func (f *Findings) Empty() bool { return len(f.counts) == 0 }

// Messages returns the messages of the error kinds in Kinds order, a
// message once per part it occurs in.
func (f *Findings) Messages() []string {
	kinds := f.Kinds()
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = k.Message
	}
	return out
}
