package integrity

import (
	"cmp"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// Workspace is what the gate reads of a plan's edit workspace: the
// working trees, whose nodes keep their identity across the edit (an
// element parsed from the input keeps its byte span there, a created one
// has none), the write set, the created-node record, and copy provenance
// (SourceOf: the element a created one was copied or split from). The
// edit core's *edit.Workspace satisfies it.
type Workspace interface {
	Tree(part string) (*tree.Document, error)
	Declared() edit.Declared
	CreatedIn(part string) ([]*tree.Node, error)
	SourceOf(n *tree.Node) *tree.Node
}

// OpLog records which operation first declared or created each element,
// so that a refusal names the operation that introduced the fault. Call
// Record after each operation that succeeded. The zero value is not
// usable; use NewOpLog. A nil *OpLog records nothing.
type OpLog struct {
	nodes map[*tree.Node]int
	parts map[string]int // by plan.PartKey
}

// NewOpLog returns an empty log.
func NewOpLog() *OpLog { return &OpLog{nodes: map[*tree.Node]int{}, parts: map[string]int{}} }

// Record attributes to operation op everything ws declared or created
// that no earlier operation did: its declared parts, target and container
// units, and the elements it created in parts that have units.
func (l *OpLog) Record(op int, ws Workspace) error {
	if l == nil {
		return nil
	}
	d := ws.Declared()
	for _, part := range d.Parts() {
		if _, ok := l.parts[part]; !ok {
			l.parts[part] = op
		}
		targets, containers := d.Targets(part), d.Containers(part)
		if len(targets) == 0 && len(containers) == 0 {
			continue // declared as a whole: attributed by part
		}
		created, err := ws.CreatedIn(part)
		if err != nil {
			return err
		}
		for _, set := range [][]*tree.Node{created, targets, containers} {
			for _, n := range set {
				if _, ok := l.nodes[n]; !ok {
					l.nodes[n] = op
				}
			}
		}
	}
	return nil
}

// opOf returns the operation of n's nearest recorded ancestor-or-self,
// else of its part, else -1.
func (l *OpLog) opOf(part string, n *tree.Node) int {
	if l == nil {
		return -1
	}
	for p := n; p != nil; p = p.Parent() {
		if op, ok := l.nodes[p]; ok {
			return op
		}
	}
	if op, ok := l.parts[plan.PartKey(part)]; ok {
		return op
	}
	return -1
}

// Introduction is one fault an edit introduced: the error kind, the
// element it is located at, described ("new w:r in paragraph 1A2B3C4D";
// "" when the fault is of a part or the package as a whole), and the
// index of the operation that introduced it (-1 when no single operation
// can be named).
type Introduction struct {
	Finding
	Node string
	Op   int
}

// String names the introduction in one line.
func (i Introduction) String() string {
	part := i.Part
	if part == "" {
		part = "package"
	}
	if i.Node == "" {
		return part + ": " + i.Message
	}
	return part + ": " + i.Node + ": " + i.Message
}

// GateEdit is the commit rule of a plan, with its faults attributed to
// what the plan did. before and after are the input and output packages;
// ws is the plan's workspace after its last operation, whose trees after
// was written from; log (may be nil) names operations.
//
// A finding of after is refused only when all of these hold:
//
//   - it is located in an element the edit created or changed (the
//     element or anything inside it), or in an unchanged element whose
//     reference or pairing the edit broke (a fault an unchanged element
//     did not have can only come from a change elsewhere);
//   - that element did not have it in before: the same message, as often,
//     at the same element (identity through the working tree: an element
//     parsed from the input keeps its byte span there even when moved);
//   - for a created element, it is not a copy of an input element that
//     had the same message: the edit core's provenance (SourceOf, followed
//     through copies of copies) leads from the new half of a split or a
//     Copy to the element it came from. An element parsed or placed fresh
//     has no provenance and is never a copy, however much it resembles
//     one. Moved content keeps its identity, so its findings too.
//
// Faults of a part as a whole, of the package, or of a part the working
// tree does not account for (written as bytes, not through a tree) are
// compared by count per part and message, as Gate does, a schema message
// any input part had being known. Pre-existing faults never count.
//
// It returns nil or a *GateFailure with Located set.
func GateEdit(before, after []byte, ws Workspace, log *OpLog) error {
	_, err := GateEditReport(before, after, ws, log, opc.DefaultLimits())
	return err
}

// GateEditReport is GateEdit with explicit reading limits (zero fields
// take their opc.DefaultLimits value) that also returns the findings of
// after it read, so a caller can report the findings of the parts an edit
// changed without checking the output again. They are complete for every
// part whose bytes differ between the packages, and for a part added or
// removed (the gate then checks whole packages); a part outside that is
// listed only as far as the scoped check read it.
func GateEditReport(before, after []byte, ws Workspace, log *OpLog, l opc.Limits) (*Findings, error) {
	return GateEditReportAt(before, after, ws, log, l, nil)
}

// GateOptions control the plan gate (GateEditWith).
type GateOptions struct {
	// Limits bound reading the packages; zero fields take their
	// opc.DefaultLimits value.
	Limits opc.Limits
	// Mark (when not nil) is called once each package's check is done,
	// while its view is held: the checkpoints of the memory budget
	// (laws.WithinMemory).
	Mark func(string)
	// Sealed says after is the workspace's own package as sealed
	// (opc.Package.Seal) after its last change, so the working trees
	// account for every part of it without serialising them again.
	Sealed bool
	// Eager checks the input whether or not the output has findings:
	// the reference the lazy gate is tested against.
	Eager bool
}

// GateEditReportAt is GateEditReport that calls mark (when not nil) once
// each package's check is done (GateOptions.Mark).
func GateEditReportAt(before, after []byte, ws Workspace, log *OpLog, l opc.Limits, mark func(string)) (*Findings, error) {
	return GateEditWith(before, after, ws, log, GateOptions{Limits: l, Mark: mark})
}

// GateEditWith is GateEditReport with options.
//
// The output is checked first, in full over the scope; the input only
// when the output has a finding (unless o.Eager). Every fault the gate
// can refuse is an instance of the output, and the input's findings only
// excuse, so an output without findings is accepted without reading the
// input. The answer is the eager one (a law of the apply tests and of
// the attribution tests).
func GateEditWith(before, after []byte, ws Workspace, log *OpLog, o GateOptions) (*Findings, error) {
	mark, l, eager := o.Mark, o.Limits, o.Eager
	if mark == nil {
		mark = func(string) {}
	}
	l = withDefaults(l)
	in, out := unzip(before, l), unzip(after, l)
	g := &attribution{ws: ws, log: log, before: in, sealed: o.Sealed}
	scope, ok := changedScope(in, out)
	if !ok {
		scope = nil
	}
	g.scoped = scope != nil
	checkIn := func() {
		// Of the input, only what attribution reads is kept (its
		// findings and where its elements start), not its parsed parts.
		g.in = check(in, scope)
		g.inStarts = map[string][]int{}
		for name, d := range g.in.docs {
			if d != nil && d.starts != nil {
				g.inStarts[name] = d.starts
			}
		}
		g.in.docs = nil
		mark("gate input checked")
	}
	if eager {
		checkIn()
	}
	g.out = check(out, scope)
	mark("gate output checked")
	if len(g.out.instances) == 0 {
		// Nothing to refuse: every introduction is an output instance.
		g.out.docs = nil
		return g.out, nil
	}
	if !eager {
		checkIn()
	}
	g.inParts, g.outParts = entryMap(in), entryMap(out)
	intro := g.run()
	g.out.docs = nil // only the findings are returned
	if len(intro) == 0 {
		return g.out, nil
	}
	f := &GateFailure{Located: intro}
	seen := map[Finding]bool{}
	for _, i := range intro {
		if !seen[i.Finding] {
			seen[i.Finding] = true
			f.Introduced = append(f.Introduced, i.Finding)
		}
	}
	slices.SortFunc(f.Introduced, compareFindings)
	return g.out, f
}

// entryMap returns a package's entries by name (the last of duplicates;
// a package with duplicate names is never located by part anyway).
func entryMap(a *archive) map[string]string {
	m := make(map[string]string, len(a.entries))
	for _, e := range a.entries {
		m[e.name] = e.data
	}
	return m
}

type attribution struct {
	ws       Workspace
	log      *OpLog
	before   *archive
	scoped   bool
	sealed   bool
	in, out  *Findings
	inParts  map[string]string
	outParts map[string]string
	corr     map[string]*correspondence
	// inAt and outAt count instances per part, message and element.
	inAt, outAt map[nodeKey]int
	// inAll counts every input instance per part, message and element;
	// fullAll the same over the whole input (fullIn), built when a scoped
	// check needs it.
	inAll, fullAll map[nodeKey]int
	fullIn         *Findings
	// inStarts are the element offsets of the input parts check parsed
	// as they are.
	inStarts    map[string][]int
	knownSchema map[string]bool // schema messages of the whole input
	// index maps an input element of a part (its byte offset in the
	// part) to its position, for the parts whose working trees were read.
	index map[string]*partIndex
	// unlocatedIn and unlocatedOut count the findings compared by count,
	// unlocatedOrder lists the output's in the order found.
	unlocatedIn, unlocatedOut map[Finding]int
	unlocatedOrder            []Finding
}

type nodeKey struct {
	part, message string
	node          int
}

// correspondence maps the elements of an output part (document order) to
// the input: origin is the input element's position, or -1 for an
// element the edit created; nodes are the working tree's elements (nil
// for a part the edit left unchanged).
type correspondence struct {
	origin []int
	nodes  []*tree.Node
}

// partIndex is a part's working tree and its input elements' positions
// by start offset in the input bytes.
type partIndex struct {
	cur     *tree.Document
	offsets map[int]int
}

// run matches output instances to inputs and returns the introductions.
func (g *attribution) run() []Introduction {
	g.corr = map[string]*correspondence{}
	g.inAt, g.outAt = map[nodeKey]int{}, map[nodeKey]int{}
	g.index = map[string]*partIndex{}
	g.unlocatedIn, g.unlocatedOut = map[Finding]int{}, map[Finding]int{}

	// Which output parts are located: decided before the inputs are
	// indexed, since a part without correspondence is counted on both
	// sides.
	located := map[string]bool{}
	for _, inst := range g.out.instances {
		if len(inst.Nodes) == 0 {
			continue
		}
		if _, done := located[inst.Part]; done {
			continue
		}
		c := g.correspond(inst.Part)
		g.corr[inst.Part] = c
		located[inst.Part] = c != nil
	}
	for _, inst := range g.in.instances {
		if len(inst.Nodes) == 0 || !located[inst.Part] {
			g.unlocatedIn[inst.Finding]++
			continue
		}
		for _, n := range inst.Nodes {
			g.inAt[nodeKey{inst.Part, inst.Message, n}]++
		}
	}
	g.inAll = countAll(g.in)
	for _, inst := range g.out.instances {
		if len(inst.Nodes) == 0 || !located[inst.Part] {
			if g.unlocatedOut[inst.Finding] == 0 {
				g.unlocatedOrder = append(g.unlocatedOrder, inst.Finding)
			}
			g.unlocatedOut[inst.Finding]++
			continue
		}
		for _, n := range inst.Nodes {
			g.outAt[nodeKey{inst.Part, inst.Message, n}]++
		}
	}

	var out []Introduction
	seen := map[nodeKey]bool{}
	for _, inst := range g.out.instances {
		if len(inst.Nodes) == 0 || !located[inst.Part] {
			continue
		}
		c := g.corr[inst.Part]
		doc := g.out.docs[inst.Part]
		// The elements that make the instance new. When created elements
		// do (a new paragraph repeating an existing id), they alone are
		// named: the existing ones are only what they collided with.
		var news, created []int
		for _, n := range inst.Nodes {
			if n >= len(c.origin) || n >= doc.size() || !g.contributes(inst, c, n) {
				continue
			}
			news = append(news, n)
			if c.origin[n] < 0 {
				created = append(created, n)
			}
		}
		if len(created) > 0 {
			news = created
		}
		for _, n := range news {
			k := nodeKey{inst.Part, inst.Message, n}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, Introduction{
				Finding: inst.Finding,
				Node:    describe(doc.at(n), c.origin[n] < 0),
				Op:      g.log.opOf(inst.Part, c.nodes[n]),
			})
		}
	}
	out = append(out, g.counted()...)
	slices.SortStableFunc(out, func(a, b Introduction) int {
		return cmp.Or(compareFindings(a.Finding, b.Finding), strings.Compare(a.Node, b.Node))
	})
	return out
}

// contributes reports whether element n of an output instance makes it
// new: an existing element that has the message more often than its
// input counterpart had, or a created element that is no copy of an input
// element with the message.
func (g *attribution) contributes(inst Instance, c *correspondence, n int) bool {
	if o := c.origin[n]; o >= 0 {
		return g.outAt[nodeKey{inst.Part, inst.Message, n}] > g.inAt[nodeKey{inst.Part, inst.Message, o}]
	}
	// Follow the provenance to an input element (bounded: a chain longer
	// than the elements created is a cycle, which the core never makes).
	seen := map[*tree.Node]bool{}
	for s := g.ws.SourceOf(c.nodes[n]); s != nil && !seen[s]; s = g.ws.SourceOf(s) {
		seen[s] = true
		part, o, ok := g.inputOrigin(inst.Part, s)
		if !ok {
			continue
		}
		return !g.inputHas(nodeKey{part, inst.Message, o})
	}
	return true
}

// inputOrigin returns the part and input position of s when s is an
// element parsed from the input: from the part being checked first, then
// from the other parts whose trees the gate or the plan read.
func (g *attribution) inputOrigin(part string, s *tree.Node) (string, int, bool) {
	try := func(p string) (int, bool) {
		ix := g.partIndex(p)
		if ix == nil {
			return 0, false
		}
		start, _, ok := s.SpanIn(ix.cur)
		if !ok {
			return 0, false
		}
		o, ok := ix.offsets[start]
		return o, ok
	}
	if o, ok := try(part); ok {
		return part, o, true
	}
	names := make([]string, 0, len(g.index))
	for p := range g.index {
		names = append(names, p)
	}
	for _, key := range g.ws.Declared().Parts() {
		names = append(names, strings.TrimPrefix(key, "/"))
	}
	slices.Sort(names)
	for _, p := range slices.Compact(names) {
		for name := range g.inParts {
			if !strings.EqualFold(name, p) || name == part {
				continue
			}
			if o, ok := try(name); ok {
				return name, o, true
			}
		}
	}
	return "", 0, false
}

// partIndex returns the index of a part present in the input whose
// working tree still has the input's bytes as its original, or nil.
func (g *attribution) partIndex(part string) *partIndex {
	if ix, ok := g.index[part]; ok {
		return ix
	}
	g.index[part] = nil
	inBytes, ok := g.inParts[part]
	if !ok {
		return nil
	}
	cur, err := g.ws.Tree(part)
	if err != nil || cur.OriginalString() != inBytes {
		return nil
	}
	ix := &partIndex{cur: cur, offsets: map[int]int{}}
	if starts, ok := g.inStarts[part]; ok {
		// The input check parsed the part as it is: its elements'
		// offsets are these, in document order.
		for i, s := range starts {
			ix.offsets[s] = i
		}
		g.index[part] = ix
		return ix
	}
	od, err := tree.ParseString(inBytes)
	if err != nil {
		return nil
	}
	i := 0
	walkElements(od, func(n *tree.Node) {
		s, _, _ := n.Span()
		ix.offsets[s] = i
		i++
	})
	g.index[part] = ix
	return ix
}

// inputHas reports whether the input had the message at the element,
// reading the whole input when a scoped check did not cover the part.
func (g *attribution) inputHas(k nodeKey) bool {
	if g.inAll[k] > 0 {
		return true
	}
	if !g.scoped {
		return false
	}
	if g.fullAll == nil {
		if g.fullIn == nil {
			g.fullIn = check(g.before, nil)
			g.fullIn.docs = nil
		}
		g.fullAll = countAll(g.fullIn)
	}
	return g.fullAll[k] > 0
}

// countAll counts every located instance of f per part, message and
// element.
func countAll(f *Findings) map[nodeKey]int {
	out := map[nodeKey]int{}
	for _, inst := range f.instances {
		for _, n := range inst.Nodes {
			out[nodeKey{inst.Part, inst.Message, n}]++
		}
	}
	return out
}

// counted compares the unlocated findings by count per part and message.
func (g *attribution) counted() []Introduction {
	var out []Introduction
	for _, k := range g.unlocatedOrder {
		if g.unlocatedOut[k] <= g.unlocatedIn[k] {
			continue
		}
		if strings.HasPrefix(k.Message, "schema: ") {
			if g.knownSchema == nil {
				full := g.in
				if g.scoped {
					if g.fullIn == nil {
						g.fullIn = check(g.before, nil)
						g.fullIn.docs = nil
					}
					full = g.fullIn
				}
				g.knownSchema = map[string]bool{}
				for _, f := range full.Kinds() {
					g.knownSchema[f.Message] = true
				}
			}
			if g.knownSchema[k.Message] {
				continue
			}
		}
		out = append(out, Introduction{Finding: k, Op: g.log.opOf(k.Part, nil)})
	}
	return out
}

// correspond maps an output part's elements to the input through the
// working tree. It returns nil, and the part's findings are compared by
// count, when the tree does not account for the part's bytes: the part
// was written as bytes rather than through its tree, the workspace cannot
// parse it, or it was replaced wholesale.
func (g *attribution) correspond(part string) *correspondence {
	outBytes, ok := g.outParts[part]
	doc := g.out.docs[part]
	if !ok || doc == nil {
		return nil
	}
	inBytes, existed := g.inParts[part]
	if existed && inBytes == outBytes {
		// Unchanged: every element is its own counterpart.
		c := &correspondence{origin: make([]int, doc.size()), nodes: make([]*tree.Node, doc.size())}
		for i := range c.origin {
			c.origin[i] = i
		}
		return c
	}
	cur, err := g.ws.Tree(part)
	if err != nil || !g.sealed && string(cur.Bytes()) != outBytes {
		return nil
	}
	var offsets map[int]int // input element start offset -> position
	if existed {
		ix := g.partIndex(part)
		if ix == nil {
			return nil // replaced wholesale: no identity
		}
		offsets = ix.offsets
	}
	c := &correspondence{}
	walkElements(cur, func(n *tree.Node) {
		o := -1
		if s, _, ok := n.SpanIn(cur); ok && offsets != nil {
			if i, ok := offsets[s]; ok {
				o = i
			}
		}
		c.origin = append(c.origin, o)
		c.nodes = append(c.nodes, n)
	})
	if doc.size() != len(c.origin) {
		return nil
	}
	return c
}

// walkElements calls fn on every element of d in document order.
func walkElements(d *tree.Document, fn func(*tree.Node)) {
	root := d.Root()
	if root == nil {
		return
	}
	root.Walk(func(n *tree.Node) bool {
		if n.Kind() != tree.ElementNode {
			return false
		}
		fn(n)
		return true
	})
}

// describe names an element for a refusal: its name, "new" when the edit
// created it, and the paragraph it is in when that has a w14:paraId.
func describe(e *element, created bool) string {
	name := label(e.ns, e.local)
	if created {
		name = "new " + name
	}
	for p := e; p != nil; p = p.up() {
		if !p.is(wNS, "p") {
			continue
		}
		id, ok := p.attr(w14NS, "paraId")
		if !ok {
			break
		}
		if p == e {
			return name + " " + id
		}
		return name + " in paragraph " + id
	}
	return name
}
