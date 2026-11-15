// Package splice edits a large XML part without parsing it into a tree: a
// Patch is a list of ordered, non-overlapping replacements of whole-node
// spans of the part's original string, written out by streaming the
// original bytes between them. A worksheet's sheetData and a shared
// strings part are edited this way (docs/design.md, xlsx: Splice), so
// that an edit costs the part's bytes plus the edits, not a tree five
// times the part's size.
//
// A Patch is well formed by construction: each span must be a run of
// whole sibling nodes inside the document element, and each replacement
// must be well-formed content in the namespace scope it lands in. Check
// proves both with one streaming scan of the original (memory in its
// depth, not its size), so a caller never has to materialise the result
// to know it parses.
package splice

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
)

// Span is a byte range [Start, End) of a part's original string. A span
// with Start == End is an insertion point.
type Span struct {
	Start, End int
}

// ErrOverlap is returned by Replace when a span overlaps a replacement
// already in the patch.
var ErrOverlap = errors.New("splice: span overlaps an earlier replacement")

// Patch is a part's original string and the replacements made to it. The
// zero value is not usable; call New.
type Patch struct {
	orig  string
	edits []edit // by Start; insertions at one point in call order
	// delta is Len() - len(orig).
	delta int
	// checked is set when Check has passed and nothing changed since.
	checked bool
	// wellFormed says the caller proved orig a well-formed document
	// (NewWellFormed): Check may stop once every span is placed.
	wellFormed bool
}

type edit struct {
	Span
	repl string
}

// New returns an empty patch over orig. The patch keeps orig, not a
// copy.
func New(orig string) *Patch { return &Patch{orig: orig} }

// NewWellFormed is New over an original the caller has already proved
// to be a well-formed XML document: it tokenised exactly these bytes in
// full (tree.Scan, tree.Check or a parse) without an error. Check then
// trusts the original past the last span and stops its scan there, so
// a patch near the start of a large part costs the bytes up to its last
// span, not the part. Passing an original that was not proved breaks
// Check's promise.
func NewWellFormed(orig string) *Patch { return &Patch{orig: orig, wellFormed: true} }

// Original returns the string the patch is over.
func (p *Patch) Original() string { return p.orig }

// Replace replaces the span [start, end) of the original with repl
// (start == end inserts repl at start; an empty repl deletes the span).
// A replacement equal to the bytes it replaces is dropped, so an
// identity write leaves the patch empty. Spans may be given in any
// order, but may not overlap one another (an insertion point may touch
// a span's ends; several insertions at one point keep their call
// order). Whether the span is a run of whole nodes and repl is
// well-formed content is proved by Check.
func (p *Patch) Replace(start, end int, repl string) error {
	if start < 0 || end < start || end > len(p.orig) {
		return fmt.Errorf("splice: span [%d, %d) outside the original (%d bytes)", start, end, len(p.orig))
	}
	if p.orig[start:end] == repl {
		return nil
	}
	// The first edit that starts after start, or at start with a
	// non-empty span (an insertion at start goes after the insertions
	// already there and before a replacement starting there).
	i := sort.Search(len(p.edits), func(i int) bool {
		e := p.edits[i]
		return e.Start > start || (e.Start == start && e.End > e.Start)
	})
	if i > 0 && p.edits[i-1].End > start {
		return fmt.Errorf("%w: [%d, %d) and [%d, %d)", ErrOverlap, start, end, p.edits[i-1].Start, p.edits[i-1].End)
	}
	if i < len(p.edits) {
		next := p.edits[i]
		if next.Start < end || (start < end && next.Start == start) {
			return fmt.Errorf("%w: [%d, %d) and [%d, %d)", ErrOverlap, start, end, next.Start, next.End)
		}
	}
	p.edits = slices.Insert(p.edits, i, edit{Span{start, end}, repl})
	p.delta += len(repl) - (end - start)
	p.checked = false
	return nil
}

// Empty reports whether the patch changes nothing.
func (p *Patch) Empty() bool { return len(p.edits) == 0 }

// Len returns the length in bytes of the patched result.
func (p *Patch) Len() int { return len(p.orig) + p.delta }

// Spans returns the spans of the original the patch replaces, in order.
func (p *Patch) Spans() []Span {
	out := make([]Span, len(p.edits))
	for i, e := range p.edits {
		out[i] = e.Span
	}
	return out
}

// WriteTo writes the patched result to w: the original between the
// spans, the replacements in their place. The original's bytes are
// written as they are when w takes strings (io.StringWriter), and
// otherwise through one small buffer, never as a copy of a whole span.
func (p *Patch) WriteTo(w io.Writer) (int64, error) {
	sw, ok := w.(io.StringWriter)
	if !ok {
		sw = &chunkWriter{w: w}
	}
	var n int64
	put := func(s string) error {
		if s == "" {
			return nil
		}
		k, err := sw.WriteString(s)
		n += int64(k)
		return err
	}
	at := 0
	for _, e := range p.edits {
		if err := put(p.orig[at:e.Start]); err != nil {
			return n, err
		}
		if err := put(e.repl); err != nil {
			return n, err
		}
		at = e.End
	}
	if err := put(p.orig[at:]); err != nil {
		return n, err
	}
	return n, nil
}

// chunkWriter writes strings to a writer that takes only bytes, through
// one buffer of chunkSize, so a long string is not copied whole.
type chunkWriter struct {
	w   io.Writer
	buf []byte
}

const chunkSize = 32 << 10

// WriteString writes s to the underlying writer through a buffer, returning bytes written.
func (c *chunkWriter) WriteString(s string) (int, error) {
	if c.buf == nil {
		c.buf = make([]byte, chunkSize)
	}
	n := 0
	for n < len(s) {
		k := copy(c.buf, s[n:])
		m, err := c.w.Write(c.buf[:k])
		n += m
		if err != nil {
			return n, err
		}
		if m < k {
			return n, io.ErrShortWrite
		}
	}
	return n, nil
}

// String returns the patched result. It copies the whole part; WriteTo
// streams it instead.
func (p *Patch) String() string {
	if p.Empty() {
		return p.orig
	}
	var b strings.Builder
	b.Grow(p.Len())
	p.WriteTo(&b) //nolint:errcheck // strings.Builder writes never fail
	return b.String()
}
