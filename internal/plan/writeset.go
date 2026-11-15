package plan

import (
	"maps"
	"slices"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// WriteSet is what a plan's operations declared they may change. A
// format's edit layer declares into it as it hands out targets; Verify
// holds the working copy to it at commit.
//
// Five kinds of declaration: a part may change as a whole (relationships,
// content types, media, or a part without units); a target unit may
// change its own content (an object, every branch); a container unit may
// change its list of child units (a shape tree, a group, a branch); the
// part's root element may gain namespace declarations, and prefixes at
// the end of its mc:Ignorable list (DeclareRootNamespace); an element
// anywhere in the part may gain, or have rewritten, one identity
// attribute (DeclareRepair: a repaired w14:paraId). Which elements are
// units is the format's (Units).
type WriteSet struct {
	// parts are the declared parts, by part key.
	parts map[string]bool
	// targets and containers are declared unit elements, by part key.
	targets    map[string]map[*tree.Node]bool
	containers map[string]map[*tree.Node]bool
	// roots are the declared root namespaces, by part key and prefix.
	roots map[string]map[string]RootNamespace
	// repairs are the declared repaired attributes, by part key, element
	// and qualified name.
	repairs map[string]map[*tree.Node]map[string]bool
	// spliced are the declared spans of spliced parts, by part key.
	spliced map[string][]splice.Span
}

// RootNamespace is a namespace declaration a plan may add on a part's
// root element: xmlns:Prefix="URI", and, when Ignorable, Prefix appended
// to the root's mc:Ignorable list (made when absent).
type RootNamespace struct {
	URI       string
	Ignorable bool
}

// NewWriteSet returns an empty write set.
func NewWriteSet() *WriteSet {
	return &WriteSet{
		parts:      map[string]bool{},
		targets:    map[string]map[*tree.Node]bool{},
		containers: map[string]map[*tree.Node]bool{},
		roots:      map[string]map[string]RootNamespace{},
		repairs:    map[string]map[*tree.Node]map[string]bool{},
		spliced:    map[string][]splice.Span{},
	}
}

// PartKey returns the key a part name is declared under: with a leading
// slash, lower case (part names compare case-insensitively).
func PartKey(name string) string { return opc.PartKey(name) }

// DeclarePart declares a part the plan may change as a whole.
func (s *WriteSet) DeclarePart(name string) { s.parts[PartKey(name)] = true }

// DeclareTarget declares unit n of part as a target: its own content may
// change, and it may be removed. It declares the part too.
func (s *WriteSet) DeclareTarget(part string, n *tree.Node) { s.add(s.targets, part, n) }

// DeclareContainer declares unit n of part as a container: its list of
// child units may change. It declares the part too.
func (s *WriteSet) DeclareContainer(part string, n *tree.Node) { s.add(s.containers, part, n) }

// add declares part and records node n in the given target or container map.
func (s *WriteSet) add(m map[string]map[*tree.Node]bool, part string, n *tree.Node) {
	k := PartKey(part)
	s.parts[k] = true
	if m[k] == nil {
		m[k] = map[*tree.Node]bool{}
	}
	m[k][n] = true
}

// DeclareRootNamespace declares that the root element of part may gain
// the declaration xmlns:prefix="uri" and, when ignorable, prefix at the
// end of its mc:Ignorable list (the attribute made when absent, which
// needs mc declared too: declare it as well). Nothing else of the root's
// start tag may change through this declaration; Verify checks that an
// existing declaration of prefix keeps its value, that a new one names
// uri, and that the list only grew at its end by declared prefixes. It
// declares the part too.
func (s *WriteSet) DeclareRootNamespace(part, prefix, uri string, ignorable bool) {
	k := PartKey(part)
	s.parts[k] = true
	if s.roots[k] == nil {
		s.roots[k] = map[string]RootNamespace{}
	}
	d := s.roots[k][prefix]
	d.URI = uri
	d.Ignorable = d.Ignorable || ignorable
	s.roots[k][prefix] = d
}

// DeclareRepair declares that element n of part may gain the attribute
// qname, or have its value rewritten: the repair of an identity
// attribute (a missing, malformed or duplicate w14:paraId) that a format
// makes on any save, whether or not an operation targeted n. The
// attribute may not be removed, and nothing else of n changes through
// this declaration. It declares the part too.
func (s *WriteSet) DeclareRepair(part string, n *tree.Node, qname string) {
	k := PartKey(part)
	s.parts[k] = true
	if s.repairs[k] == nil {
		s.repairs[k] = map[*tree.Node]map[string]bool{}
	}
	if s.repairs[k][n] == nil {
		s.repairs[k][n] = map[string]bool{}
	}
	s.repairs[k][n][qname] = true
}

// DeclareSpliced declares that part may change by a splice patch
// (opc.Part.SetPatch) within spans of its original bytes, and nowhere
// else. Verify checks such a part with no tree: every replacement of its
// patch must lie inside the declared spans (an insertion point inside
// or at either end of one), and the part must have changed by its patch
// alone. Calls add spans. It declares the part too.
func (s *WriteSet) DeclareSpliced(part string, spans ...splice.Span) {
	k := PartKey(part)
	s.parts[k] = true
	s.spliced[k] = append(s.spliced[k], spans...)
}

// RepairedAttr is one declared repair: attribute QName of Node.
type RepairedAttr struct {
	Node  *tree.Node
	QName string
}

// byOffset sorts nodes by their original byte offset, nodes built since
// last, so listings are deterministic.
func byOffset(m map[*tree.Node]bool) []*tree.Node {
	out := make([]*tree.Node, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	slices.SortStableFunc(out, func(a, b *tree.Node) int {
		sa, _, oka := a.Span()
		sb, _, okb := b.Span()
		switch {
		case oka && okb:
			return sa - sb
		case oka:
			return -1
		case okb:
			return 1
		}
		return 0
	})
	return out
}

// Parts returns the declared part keys, sorted.
func (s *WriteSet) Parts() []string { return slices.Sorted(maps.Keys(s.parts)) }

// Targets returns the declared target units of part, in original byte
// order.
func (s *WriteSet) Targets(part string) []*tree.Node { return byOffset(s.targets[PartKey(part)]) }

// Containers returns the declared container units of part, in original
// byte order.
func (s *WriteSet) Containers(part string) []*tree.Node {
	return byOffset(s.containers[PartKey(part)])
}

// Repairs returns the declared repaired attributes of part, in original
// byte order of their elements, then by name.
func (s *WriteSet) Repairs(part string) []RepairedAttr {
	m := s.repairs[PartKey(part)]
	nodes := map[*tree.Node]bool{}
	for n := range m {
		nodes[n] = true
	}
	var out []RepairedAttr
	for _, n := range byOffset(nodes) {
		names := slices.Sorted(maps.Keys(m[n]))
		for _, q := range names {
			out = append(out, RepairedAttr{n, q})
		}
	}
	return out
}

// RootNamespaces returns the root namespace declarations of part, by
// prefix.
func (s *WriteSet) RootNamespaces(part string) map[string]RootNamespace {
	out := map[string]RootNamespace{}
	for p, d := range s.roots[PartKey(part)] {
		out[p] = d
	}
	return out
}
