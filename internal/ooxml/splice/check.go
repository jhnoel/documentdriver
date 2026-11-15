package splice

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// ErrSpan is returned by Check when a span is not a run of whole sibling
// nodes inside the document element.
var ErrSpan = errors.New("splice: span is not a run of whole sibling nodes")

// ErrReplacement is returned by Check when a replacement is not
// well-formed content where it lands.
var ErrReplacement = errors.New("splice: replacement is not well-formed content")

// Check proves that the patched result is a well-formed XML document
// without building it: it scans the original once (which must be well
// formed) and checks that every span starts and ends at node boundaries
// of the same element, at or below the document element, so that it
// covers whole sibling nodes (an insertion point is such a boundary), and
// that every replacement is well-formed content in the namespace scope of
// that element. Then the result is the original with runs of whole
// sibling nodes swapped for well-formed content, which is well formed.
// Memory is in the original's depth and the number of replacements, not
// its size. A passing result is remembered until the patch changes.
// Over an original from NewWellFormed the scan stops once the last span
// is placed: what follows was already proved well formed.
func (p *Patch) Check() error {
	if p.checked || p.Empty() {
		return nil
	}
	c := &checker{p: p, parent: make([]int, len(p.edits))}
	if err := tree.ScanString(p.orig, c); err != nil && !errors.Is(err, errPlaced) {
		if c.err != nil {
			return c.err
		}
		return fmt.Errorf("splice: original: %w", err)
	}
	if c.i < len(p.edits) {
		return c.spanErr(c.i, "starts")
	}
	if c.j < len(p.edits) {
		return c.spanErr(c.j, "ends")
	}
	if err := p.checkJunctions(); err != nil {
		return err
	}
	p.checked = true
	return nil
}

// checkJunctions checks the one fault that whole pieces cannot have
// alone but their concatenation can: "]]>" in character data across a
// junction, where text of one piece meets text of the next ("]]" before
// a deleted node and ">" after it). Pieces end at node boundaries, so a
// "]]>" across a junction is never markup (a CDATA section, comment or
// tag is whole within its piece), and nothing else in character data
// spans pieces: each piece is valid UTF-8 with whole references.
func (p *Patch) checkJunctions() error {
	pieces := make([]string, 0, 2*len(p.edits)+1)
	at := 0
	for _, e := range p.edits {
		pieces = append(pieces, p.orig[at:e.Start], e.repl)
		at = e.End
	}
	pieces = append(pieces, p.orig[at:])
	for q := 0; q+1 < len(pieces); q++ {
		var w [4]byte
		left, right := 0, 0
		for k := q; k >= 0 && left < 2; k-- {
			for i := len(pieces[k]) - 1; i >= 0 && left < 2; i-- {
				left++
				w[2-left] = pieces[k][i]
			}
		}
		for k := q + 1; k < len(pieces) && right < 2; k++ {
			for i := 0; i < len(pieces[k]) && right < 2; i++ {
				w[2+right] = pieces[k][i]
				right++
			}
		}
		if strings.Contains(string(w[2-left:2+right]), "]]>") {
			return fmt.Errorf(`%w: "]]>" in character data where a replacement meets the original`, ErrReplacement)
		}
	}
	return nil
}

// errPlaced ends Check's scan of a proved original once every span is
// placed.
var errPlaced = errors.New("splice: every span placed")

// checker is the Visitor Check scans with. Each element inside the
// document gets an id; a boundary is a position between two nodes of
// one element's content (its start, its end, or between children), and
// a span is valid when both its ends are boundaries of the same element.
type checker struct {
	p      *Patch
	frames []frame
	nextID int
	// i is the next edit whose start has not been reached, j the next
	// whose end has not; parent holds, for each edit whose start was
	// reached, the element it starts in.
	i, j   int
	parent []int
	err    error
}

// frame is an open element: its id and the namespace declarations on
// its start tag.
type frame struct {
	id    int
	decls []nsDecl
}

type nsDecl struct{ attr, value string }

// Start records the element start tag as a boundary and opens its frame for span checking.
func (c *checker) Start(n *tree.Node) error {
	s, _, _ := n.Span()
	src := c.p.orig
	tok, err := xmlscan.Next(src, s)
	if err != nil {
		return err // the scan accepted this start tag, so Next reads it
	}
	end := tok.End
	if len(c.frames) > 0 {
		if err := c.boundary(s); err != nil {
			return err
		}
	}
	if src[end-2] == '/' {
		return nil // self-closing: no content, End follows
	}
	f := frame{id: c.nextID}
	c.nextID++
	if strings.Contains(src[s:end], "xmlns") {
		for name, value := range n.AttrsSeq() {
			if name == "xmlns" || strings.HasPrefix(name, "xmlns:") {
				f.decls = append(f.decls, nsDecl{name, value})
			}
		}
	}
	c.frames = append(c.frames, f)
	return c.boundary(end)
}

// End records the element end tag as a boundary and closes its frame.
func (c *checker) End(n *tree.Node) error {
	s, e, _ := n.Span()
	src := c.p.orig
	if src[e-2] != '/' {
		// Not self-closing: the content ends where the end tag starts.
		if err := c.boundary(s + strings.LastIndex(src[s:e], "</")); err != nil {
			return err
		}
		c.frames = c.frames[:len(c.frames)-1]
	}
	if len(c.frames) == 0 {
		return nil
	}
	return c.boundary(e)
}

// Leaf records a leaf node's start and end as boundaries of the innermost element.
func (c *checker) Leaf(n *tree.Node) error {
	if len(c.frames) == 0 {
		return nil // prolog, or after the document element
	}
	s, e, _ := n.Span()
	if err := c.boundary(s); err != nil {
		return err
	}
	return c.boundary(e)
}

// boundary records that x is a boundary of the innermost open element:
// edits starting at x start there (and their replacement is checked in
// its scope), edits ending at x must have started in the same element.
// Boundaries arrive in increasing order, so an edit end or start passed
// over is not at a boundary.
func (c *checker) boundary(x int) error {
	edits := c.p.edits
	id := c.frames[len(c.frames)-1].id
	for c.i < len(edits) && edits[c.i].Start <= x {
		if edits[c.i].Start < x {
			return c.fail(c.spanErr(c.i, "starts"))
		}
		c.parent[c.i] = id
		if err := c.checkRepl(edits[c.i].repl); err != nil {
			return c.fail(fmt.Errorf("%w: replacement of [%d, %d): %w", ErrReplacement, edits[c.i].Start, edits[c.i].End, err))
		}
		c.i++
	}
	for c.j < c.i && edits[c.j].End <= x {
		if edits[c.j].End < x {
			return c.fail(c.spanErr(c.j, "ends"))
		}
		if c.parent[c.j] != id {
			return c.fail(fmt.Errorf("%w: [%d, %d) starts and ends in different elements", ErrSpan, edits[c.j].Start, edits[c.j].End))
		}
		c.j++
	}
	if c.p.wellFormed && c.j == len(edits) {
		return errPlaced
	}
	return nil
}

// spanErr returns an ErrSpan error stating edit k's start or end is misplaced.
func (c *checker) spanErr(k int, which string) error {
	e := c.p.edits[k]
	return fmt.Errorf("%w: [%d, %d) %s inside a node or outside the document element", ErrSpan, e.Start, e.End, which)
}

// fail records err for Check to return and returns it to stop the scan.
func (c *checker) fail(err error) error {
	c.err = err
	return err
}

// checkRepl checks that repl is well-formed content in the namespace
// scope of the open elements: it scans repl inside a wrapper element
// declaring every namespace in scope.
func (c *checker) checkRepl(repl string) error {
	if repl == "" {
		return nil
	}
	var b strings.Builder
	b.WriteString("<_")
	seen := map[string]bool{}
	for _, v := range slices.Backward(c.frames) {
		for _, d := range v.decls {
			if seen[d.attr] {
				continue // shadowed by an inner declaration
			}
			seen[d.attr] = true
			b.WriteString(" " + d.attr + `="`)
			b.WriteString(xmlscan.EscapeAttr(d.value, '"'))
			b.WriteByte('"')
		}
	}
	b.WriteByte('>')
	b.WriteString(repl)
	b.WriteString("</_>")
	return tree.ScanString(b.String(), nopVisitor{})
}

type nopVisitor struct{}

// Start returns nil, as nopVisitor ignores scanned elements.
func (nopVisitor) Start(*tree.Node) error { return nil }

// End returns nil, as nopVisitor ignores scanned elements.
func (nopVisitor) End(*tree.Node) error { return nil }

// Leaf returns nil, as nopVisitor ignores scanned nodes.
func (nopVisitor) Leaf(*tree.Node) error { return nil }
