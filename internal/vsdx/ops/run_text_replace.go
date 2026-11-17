package ops

import (
	"errors"
	"sort"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run implements text.replace: it replaces exact text in a shape's
// plain text (at a shape) or in every shape of a page in paint order
// (at a page), keeping the formatting of the run where a match starts.
// With occurrence it replaces that one match (1-based, across the
// scope in order), with all it replaces every match, and with neither
// a scope with more than one match is ambiguous and a scope with none
// is not_found at select. A shape showing its master's text gains a
// local Text only, with the master's cp, pp and fld markup and no
// local Character or Paragraph rows copied. A match crossing a
// paragraph or line break or touching a field is operation_failed at
// select. Changed: the shapes written.
func (s *TextReplaceSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	if s.Select == "" {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "select", "select must not be empty")
	}
	if s.Occurrence < 0 {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "occurrence", "occurrence is 1 or more")
	}
	if s.Occurrence > 0 && s.All {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "all", "occurrence and all exclude each other")
	}
	for _, c := range s.With {
		if c == '\n' || c == '\r' || c == '\u2028' || !xmlscan.IsXMLChar(c) {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "with", "character %U cannot be stored in a text run", c)
		}
	}
	resolved, a, err := replaceAddress(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	page, err := loadReplacePage(ws, a, resolved)
	if err != nil {
		return "", nil, nil, "", err
	}
	switch a.Kind {
	case vsd.KindShape:
		return s.replaceShape(ws, page, a, resolved)
	case vsd.KindPage:
		return s.replacePage(ws, page, resolved)
	}
	return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a shape or page address", resolved)
}

// replaceAddress resolves s.At through aliases to a shape or page
// address (with what may follow them, $p/s5): an unknown alias is
// not_found at at, a master address is operation_failed at at, and
// anything else is not a shape or page address at at.
func replaceAddress(at string, aliases map[string]string) (string, vsd.Address, error) {
	resolved, err := ResolveAlias(at, aliases, "at")
	if err != nil {
		return "", vsd.Address{}, err
	}
	a, err := vsd.Parse(resolved)
	if err != nil || (a.Kind != vsd.KindShape && a.Kind != vsd.KindPage) {
		return "", vsd.Address{}, cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a shape or page address", resolved)
	}
	if a.InMaster {
		return "", vsd.Address{}, cli.Errorf(cli.CodeOperationFailed, "at", "%s is a master shape: masters cannot be edited", resolved)
	}
	return resolved, a, nil
}

// replacePage is the loaded page a text.replace scans: the document
// and resolver opened once for the whole scan, and the working tree
// of the page part, so a scan costs one open and one page walk per
// pass instead of one per shape.
type replacePage struct {
	res  *inherit.Resolver
	root *tree.Node
	addr string
}

// loadReplacePage opens the drawing of ws once and returns the page
// of address a (a shape's container, or the page itself) from the
// working tree: a stencil is operation_failed at at, a missing page
// is not_found at at, and a page with no part or a part that is not
// PageContents is unreadable_target.
func loadReplacePage(ws *edit.Workspace, a vsd.Address, resolved string) (*replacePage, error) {
	d, err := doc.Open(ws.Package())
	if err != nil {
		return nil, err
	}
	if d.Kind == doc.Stencil {
		return nil, cli.Errorf(cli.CodeOperationFailed, "at", "%s is in a stencil: stencils cannot be edited", resolved)
	}
	var entry *doc.PageEntry
	depth := 0
	for i := range d.Pages {
		p := &d.Pages[i]
		if p.ID != a.Container {
			continue
		}
		depth++
		if depth == a.PageDup {
			entry = p
			break
		}
	}
	if entry == nil {
		return nil, cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", vsd.PageID(a.Container, a.PageDup))
	}
	if entry.Part == "" {
		return nil, &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it has no part"}
	}
	coreDoc, err := ws.Core().Tree(entry.Part)
	if err != nil {
		return nil, &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: err.Error()}
	}
	root := coreDoc.Root()
	if root == nil || !vsd.IsMain(root.Space()) || root.Local() != vsd.ElemPageContents {
		return nil, &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it is not a " + vsd.ElemPageContents + " part"}
	}
	return &replacePage{res: inherit.New(d), root: root, addr: vsd.PageID(a.Container, a.PageDup)}, nil
}

// replaceShape is one shape a text.replace may write: its address,
// its node in the working tree, its resolver shape, the shown body
// and formats, where the shown Text came from, and the bytes of its
// own Character and Paragraph sections.
type replaceShape struct {
	addr     string
	node     *tree.Node
	shape    *inherit.Shape
	body     text.Body
	form     text.Formats
	origin   inherit.Origin
	sections []byte
	matches  []replaceMatch
}

// buildReplaceShape builds the replaceShape of node at address addr
// from the page's resolver r: the shown body parsed once, the shown
// formats of the sheet that owns the shown Text, and the shape's own
// Character and Paragraph sections as stored.
func buildReplaceShape(node *tree.Node, addr string, r *inherit.Resolver) *replaceShape {
	sh := textShapeOf(node)
	eff, origin := r.Text(sh)
	st := &replaceShape{addr: addr, node: node, shape: sh, origin: origin, sections: replaceSections(node)}
	if eff == nil {
		return st
	}
	st.body, _ = text.Parse(eff)
	owner := sh
	if origin == inherit.Master {
		if master, ok := r.MasterShape(sh); ok {
			owner = master
		}
	}
	st.form = textSetFormats(r, owner)
	return st
}

// replaceSections returns the bytes of node's own Character and
// Paragraph Sections in document order, so a write through SetText
// (which replaces those sections) puts back exactly the rows the
// shape already stored.
func replaceSections(node *tree.Node) []byte {
	var out []byte
	for c := range node.ElementsSeq() {
		if c.Kind() != tree.ElementNode || c.Local() != vsd.ElemSection || !vsd.IsMain(c.Space()) {
			continue
		}
		n, _ := c.Attr("N")
		if n != vsd.SectionCharacter && n != vsd.SectionParagraph {
			continue
		}
		out = append(out, c.Bytes()...)
	}
	return out
}

// replaceShape runs text.replace at one shape: count the matches in
// its plain text, choose them, and write the one shape when a match
// is chosen.
func (s *TextReplaceSpec) replaceShape(ws *edit.Workspace, page *replacePage, a vsd.Address, resolved string) (string, []string, []string, string, error) {
	node := findTextShape(page.root, a.Shape, a.Dup)
	if node == nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", resolved)
	}
	if err := nodeRefusal(page.res, node, resolved, "at", false); err != nil {
		return "", nil, nil, "", err
	}
	if typ, _ := node.Attr("Type"); typ == vsd.ShapeTypeForeign || typ == vsd.ShapeTypeGuide {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is a %s shape: text cannot be replaced", resolved, typ)
	}
	st := buildReplaceShape(node, resolved, page.res)
	matches, err := replaceMatches(st.body, s.Select)
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "select", "%s", err)
	}
	choice, err := replaceChoiceFor(len(matches), s.Occurrence, s.All, s.Select)
	if err != nil {
		return "", nil, nil, "", err
	}
	if s.Select == s.With {
		return "", nil, nil, "", nil
	}
	if err := st.write(ws, replaceSelected(matches, 0, choice), s.With); err != nil {
		return "", nil, nil, "", err
	}
	return "", []string{resolved}, nil, "", nil
}

// replacePage runs text.replace at a page: a first walk counts the
// matches of every shape in paint order without writing anything, a
// second walk collects the shapes the choice picks, and only then
// are they written, so an ambiguous or missing match writes nothing.
// A container, list or callout is skipped in both walks (chunk CC6),
// like a Foreign or guide shape, and never counted.
func (s *TextReplaceSpec) replacePage(ws *edit.Workspace, page *replacePage, _ string) (string, []string, []string, string, error) {
	total := 0
	var walkErr error
	seen := map[uint32]int{}
	page.root.Walk(func(n *tree.Node) bool {
		if walkErr != nil {
			return false
		}
		if !replaceIsShape(n) {
			return true
		}
		id, ok := shapeIDOf(n)
		if !ok {
			return true
		}
		seen[id]++
		if typ, _ := n.Attr("Type"); typ == vsd.ShapeTypeForeign || typ == vsd.ShapeTypeGuide {
			return true
		}
		if isContainerShape(page.res, n) {
			return true
		}
		eff, _ := page.res.Text(textShapeOf(n))
		if eff == nil {
			return true
		}
		body, _ := text.Parse(eff)
		matches, err := replaceMatches(body, s.Select)
		if err != nil {
			walkErr = err
			return false
		}
		total += len(matches)
		return true
	})
	if walkErr != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "select", "%s", walkErr)
	}
	choice, err := replaceChoiceFor(total, s.Occurrence, s.All, s.Select)
	if err != nil {
		return "", nil, nil, "", err
	}
	if s.Select == s.With {
		return "", nil, nil, "", nil
	}
	var targets []*replaceShape
	var selected []replaceMatch
	cumulative := 0
	seen = map[uint32]int{}
	page.root.Walk(func(n *tree.Node) bool {
		if !replaceIsShape(n) {
			return true
		}
		id, ok := shapeIDOf(n)
		if !ok {
			return true
		}
		seen[id]++
		if typ, _ := n.Attr("Type"); typ == vsd.ShapeTypeForeign || typ == vsd.ShapeTypeGuide {
			return true
		}
		if isContainerShape(page.res, n) {
			return true
		}
		sh := textShapeOf(n)
		eff, origin := page.res.Text(sh)
		if eff == nil {
			return true
		}
		body, _ := text.Parse(eff)
		matches, err := replaceMatches(body, s.Select)
		if err != nil {
			walkErr = err
			return false
		}
		selected = replaceSelected(matches, cumulative, choice)
		cumulative += len(matches)
		if len(selected) == 0 {
			return true
		}
		owner := sh
		if origin == inherit.Master {
			if master, ok := page.res.MasterShape(sh); ok {
				owner = master
			}
		}
		st := &replaceShape{addr: vsd.ShapeID(page.addr, id, seen[id]), node: n, shape: sh, body: body, origin: origin, sections: replaceSections(n)}
		st.form = textSetFormats(page.res, owner)
		st.matches = selected
		targets = append(targets, st)
		return true
	})
	if walkErr != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "select", "%s", walkErr)
	}
	changed := make([]string, 0, len(targets))
	for _, st := range targets {
		if err := st.write(ws, st.matches, s.With); err != nil {
			return "", nil, nil, "", err
		}
		changed = append(changed, st.addr)
	}
	return "", changed, nil, "", nil
}

// replaceIsShape reports whether n is a Shape element in a Visio
// main namespace.
func replaceIsShape(n *tree.Node) bool {
	return n.Kind() == tree.ElementNode && vsd.IsMain(n.Space()) && n.Local() == vsd.ElemShape
}

// replaceMatch is one occurrence of the selection in a body, as
// (para, ks, os, ke, oe): the paragraph, the run where it starts and
// the byte offset in that run, and the run where it ends and the byte
// offset past it in that run.
type replaceMatch struct {
	para, ks, os, ke, oe int
}

// replaceChoice is which matches of a scope are replaced: all of
// them, or the one at index (0-based) in scope order.
type replaceChoice struct {
	all   bool
	index int
}

// replaceChoiceFor returns the choice for a scope of total matches: no
// match is not_found at select, an occurrence past the matches is
// not_found at select, more than one match without occurrence or all
// is ambiguous at select with Matches set to total, and otherwise
// the scope's one or all matches are chosen.
func replaceChoiceFor(total, occurrence int, all bool, sel string) (replaceChoice, error) {
	switch {
	case total == 0:
		return replaceChoice{}, cli.Errorf(cli.CodeNotFound, "select", "%q not found", sel)
	case occurrence > total:
		return replaceChoice{}, cli.Errorf(cli.CodeNotFound, "select", "%q occurs %d times, not %d", sel, total, occurrence)
	case occurrence > 0:
		return replaceChoice{index: occurrence - 1}, nil
	case all:
		return replaceChoice{all: true}, nil
	case total > 1:
		e := cli.Errorf(cli.CodeAmbiguous, "select", "%q occurs %d times", sel, total)
		e.Matches = total
		e.Hint = "give occurrence (1-based) or all"
		return replaceChoice{}, e
	default:
		return replaceChoice{}, nil
	}
}

// replaceSelected returns the matches of one shape, whose matches
// start at cumulative in scope order, that choice picks.
func replaceSelected(matches []replaceMatch, cumulative int, choice replaceChoice) []replaceMatch {
	if choice.all {
		return matches
	}
	if choice.index >= cumulative && choice.index < cumulative+len(matches) {
		return matches[choice.index-cumulative : choice.index-cumulative+1 : choice.index-cumulative+1]
	}
	return nil
}

// replaceSegment maps a byte range of the plain text to its source:
// a run, a line break or a paragraph separator.
type replaceSegment struct {
	kind        int
	start, para int
	item, n     int
}

// segment kinds of replaceSegment.
const (
	segmentRun = iota
	segmentBreak
	segmentPara
)

// errReplaceCrossing is reported when a match crosses a paragraph or
// line break or touches a field, which replacing text in the run
// where it starts cannot keep.
var errReplaceCrossing = errors.New("text.replace: match crosses a paragraph or line break or touches a field")

// replaceMatches returns every non-overlapping occurrence of sel in
// body's plain text (text.Plain) in order, as run ranges, or
// errReplaceCrossing when one crosses a break or a field.
func replaceMatches(body text.Body, sel string) ([]replaceMatch, error) {
	plain := text.Plain(body)
	var segs []replaceSegment
	offset := 0
	for pi, para := range body.Paras {
		if pi > 0 {
			segs = append(segs, replaceSegment{kind: segmentPara, start: offset, para: pi, n: 1})
			offset++
		}
		for ri, run := range para.Runs {
			if run.Br {
				segs = append(segs, replaceSegment{kind: segmentBreak, start: offset, para: pi, item: ri, n: 1})
				offset++
				continue
			}
			segs = append(segs, replaceSegment{kind: segmentRun, start: offset, para: pi, item: ri, n: len(run.Text)})
			offset += len(run.Text)
		}
	}
	var out []replaceMatch
	for from := 0; from <= len(plain); {
		i := strings.Index(plain[from:], sel)
		if i < 0 {
			break
		}
		a, z := from+i, from+i+len(sel)
		m, err := replaceLocate(body, segs, a, z)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
		from = z
	}
	return out, nil
}

// replaceLocate maps the plain-text byte range [a, z) to a
// replaceMatch, failing with errReplaceCrossing when it covers a
// paragraph or line break or a field run.
func replaceLocate(body text.Body, segs []replaceSegment, a, z int) (replaceMatch, error) {
	k := sort.Search(len(segs), func(i int) bool { return segs[i].start+segs[i].n > a })
	m := replaceMatch{para: -1}
	for ; k < len(segs) && segs[k].start < z; k++ {
		sg := segs[k]
		if sg.n == 0 {
			continue
		}
		if sg.kind != segmentRun {
			return replaceMatch{}, errReplaceCrossing
		}
		if body.Paras[sg.para].Runs[sg.item].Field >= 0 {
			return replaceMatch{}, errReplaceCrossing
		}
		if m.para < 0 {
			m.para, m.ks, m.os = sg.para, sg.item, a-sg.start
		}
		m.ke, m.oe = sg.item, min(z, sg.start+sg.n)-sg.start
	}
	if m.para < 0 {
		return replaceMatch{}, errReplaceCrossing
	}
	return m, nil
}

// applyReplace returns a copy of body with matches replaced by with:
// each match's replacement goes in the run where it starts, keeping
// that run's formatting, and the runs it spans into are merged into
// it, keeping what follows the match in the last one.
func applyReplace(body text.Body, matches []replaceMatch, with string) text.Body {
	paras := make([]text.Para, len(body.Paras))
	for i, para := range body.Paras {
		runs := make([]text.Run, len(para.Runs))
		copy(runs, para.Runs)
		paras[i] = text.Para{PP: para.PP, Runs: runs}
	}
	for j := len(matches) - 1; j >= 0; j-- {
		m := matches[j]
		para := &paras[m.para]
		start := &para.Runs[m.ks]
		start.Text = start.Text[:m.os] + with + para.Runs[m.ke].Text[m.oe:]
		if m.ke > m.ks {
			para.Runs = append(para.Runs[:m.ks+1], para.Runs[m.ke+1:]...)
		}
	}
	return text.Body{Paras: paras, Terminated: body.Terminated}
}

// write replaces the shown text of st with matches replaced by with:
// the Text element is emitted from the new body under the shown
// formats (so cp and pp markers are kept), and the shape's own
// Character and Paragraph sections are put back as stored, which
// for a shape showing its master's text copies no master rows.
func (st *replaceShape) write(ws *edit.Workspace, matches []replaceMatch, with string) error {
	body := applyReplace(st.body, matches, with)
	_, textEl, err := emit.TextXML(body, st.form)
	if err != nil {
		return cli.Errorf(cli.CodeOperationFailed, "select", "%s", err)
	}
	if err := ws.SetText(st.addr, st.sections, textEl); err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		var un *edit.Unreadable
		if errors.As(err, &un) {
			return err
		}
		return cli.Errorf(cli.CodeOperationFailed, "at", "%s", err)
	}
	return nil
}
