package sheet

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// SST is an index of sharedStrings.xml: one byte offset per <si> item,
// and the item decoded only when asked for (no []string of the table;
// design budget: 4 bytes per item).
//
// An item's text is the character data of its <t>, or of the <t> of each
// <r> run joined, with phonetic runs (<rPh>) and properties left out, and
// _xHHHH_ escapes decoded (sml.UnescapeX). Elements in other namespaces
// are ignored.
type SST struct {
	src string
	// off[i] is the byte offset of item i's start tag; off[n] is the end
	// of the last item.
	off []uint32
	// root is the sst element of src, kept so an item scanned alone
	// resolves its names with tree.NSStack seeded from the root's
	// declarations (ns.Push without a copy of the string). It is only read.
	root *tree.Node
	// wrap is the start tag to scan an item inside, with the root's
	// declarations, when an item uses a namespace prefix (a fragment
	// alone would have an undeclared prefix); "" when items scan alone.
	wrap string
	// fed counts the bytes Text has given the tokenizer, for the test
	// that lookups stay linear (TestSSTLookupLinear); never read
	// otherwise.
	fed int64
}

// OpenSST indexes the shared strings part src in one streaming pass. The
// error is the tokenizer's when src is not well-formed XML, or an error
// when its root is not a main-namespace <sst> or src is over 4 GiB. The
// reader then notes the part as unreadable and leaves every t="s" cell
// out.
func OpenSST(src string) (*SST, error) {
	if len(src) > math.MaxUint32 {
		return nil, fmt.Errorf("sheet: shared strings of %d bytes exceed the 4 GiB offset range", len(src))
	}
	ix := &sstIndexer{s: &SST{src: src}}
	if err := tree.ScanString(src, ix); err != nil {
		return nil, err
	}
	if !ix.root {
		return nil, errors.New("sheet: the shared strings part has no sst root")
	}
	s := ix.s
	s.off = append(s.off, ix.lastEnd)
	if ix.prefixed {
		s.wrap = ix.wrapTmp
	}
	return s, nil
}

// Len is the number of items.
func (s *SST) Len() int { return len(s.off) - 1 }

// Text decodes item i (0-based). The error is for i out of range; the
// item itself scanned cleanly when the index was built, so decoding it
// cannot fail otherwise.
func (s *SST) Text(i int) (string, error) {
	d, err := s.decode(i)
	if err != nil {
		return "", err
	}
	return sml.UnescapeX(d.text.String()), nil
}

// Rich reports whether item i (0-based) is rich text: it has <r> runs, so
// its formatting lives in the runs and Text flattens it. The error is as
// Text's.
func (s *SST) Rich(i int) (bool, error) {
	d, err := s.decode(i)
	if err != nil {
		return false, err
	}
	return d.runs, nil
}

// decode decodes item i into a fresh decoder (Text and Rich).
func (s *SST) decode(i int) (*siDecoder, error) {
	if i < 0 || i >= s.Len() {
		return nil, fmt.Errorf("sheet: shared string %d of %d", i, s.Len())
	}
	d := &siDecoder{}
	// The item's span: from its start tag to the next item's start (or,
	// for the last item, its own end). Bounding it matters: the
	// tokenizer's setup is proportional to the text it is given, so an
	// unbounded tail would make n lookups quadratic. Whatever lies between
	// the item's end and the next start (space, comments, a foreign
	// element) is never reached unwrapped (the decoder stops at the
	// item's end) and is well-formed content inside the wrapper.
	item := s.src[s.off[i]:s.off[i+1]]
	var err error
	if s.wrap == "" {
		// The stack is seeded from the root's declarations, so the
		// item resolves its names without a copy of its bytes. The
		// dummy frame stands for the seeded root at depth 1 (it is
		// never popped: the decoder is discarded with the seed).
		d.base = 2
		d.ns.Push(s.root)
		d.frames = append(d.frames, frame{local: "_", main: true})
		s.fed += int64(len(item))
		err = tree.ScanString(item, d)
	} else {
		d.base = 2
		text := s.wrap + item + "</_>"
		s.fed += int64(len(text))
		err = tree.ScanString(text, d)
	}
	if err != nil && !errors.Is(err, errItemDone) {
		return nil, fmt.Errorf("sheet: shared string %d: %w", i, err)
	}
	return d, nil
}

// Each calls fn with every item in order, in one streaming pass (find's
// sst-first search, survey R12). An error from fn stops the pass and is
// returned.
func (s *SST) Each(fn func(i int, text string) error) error {
	d := &siDecoder{base: 2, each: fn}
	return tree.ScanString(s.src, d)
}

// errItemDone stops a single-item decode at the item's end.
var errItemDone = errors.New("sheet: item done")

// sstIndexer builds the offsets.
type sstIndexer struct {
	s        *SST
	ns       tree.NSStack
	frames   []frame
	skip     int
	root     bool
	inItem   bool
	prefixed bool
	lastEnd  uint32
	// wrapTmp is the wrapped start tag built at the root; OpenSST keeps
	// it only when an item turns out to need it.
	wrapTmp string
}

// Start handles a start-tag event, recording the root and each item's byte offset.
func (x *sstIndexer) Start(n *tree.Node) error {
	x.ns.Push(n)
	local := n.Local()
	main := sml.IsMain(x.ns.Space(n))
	x.frames = append(x.frames, frame{local: local, main: main})
	d := len(x.frames)
	if x.skip > 0 {
		x.skip++
		x.frames[d-1].skipped = true
		return nil
	}
	switch {
	case d == 1 && main && local == "sst":
		x.root = true
		x.s.root = n
		var b strings.Builder
		b.WriteString("<_")
		n.Declarations(func(prefix, uri string) {
			b.WriteString(" xmlns")
			if prefix != "" {
				b.WriteByte(':')
				b.WriteString(prefix)
			}
			b.WriteString(`="`)
			b.WriteString(xmlscan.EscapeAttrMinimal(uri, '"'))
			b.WriteByte('"')
		})
		b.WriteByte('>')
		x.wrapTmp = b.String()
	case d == 2 && main && local == "si":
		start, _, _ := n.Span()
		x.s.off = append(x.s.off, uint32(start)) //nolint:gosec // G115: OpenSST checked len(src) fits
		x.inItem = true
		x.checkPrefix(n)
	case x.inItem:
		x.checkPrefix(n)
	default:
		x.frames[d-1].skipped = true
		x.skip = 1
	}
	return nil
}

// checkPrefix notes an item that names something with a prefix other
// than xml: it cannot be scanned alone.
func (x *sstIndexer) checkPrefix(n *tree.Node) {
	if x.prefixed {
		return
	}
	if strings.Contains(n.Name(), ":") {
		x.prefixed = true
		return
	}
	if !n.HasAttrs() {
		return
	}
	for name := range n.AttrNamesSeq() {
		if p, _, ok := strings.Cut(name, ":"); ok && p != "xml" && p != "xmlns" {
			x.prefixed = true
			return
		}
	}
}

// End handles an end-tag event, ending the current item and recording its end offset.
func (x *sstIndexer) End(n *tree.Node) error {
	x.frames = x.frames[:len(x.frames)-1]
	x.ns.Pop()
	if x.skip > 0 {
		x.skip--
		return nil
	}
	d := len(x.frames) + 1
	if d == 2 && x.inItem {
		x.inItem = false
		_, end, _ := n.Span()
		x.lastEnd = uint32(end) //nolint:gosec // G115: OpenSST checked len(src) fits
	}
	return nil
}

// Leaf ignores a leaf node and returns nil.
func (*sstIndexer) Leaf(*tree.Node) error { return nil }

// siDecoder collects the text of items whose <si> is at depth base: one
// item (each nil; it stops after the first) or every item (each set).
type siDecoder struct {
	base   int
	each   func(int, string) error
	n      int
	ns     tree.NSStack
	frames []frame
	skip   int
	in     bool
	intT   bool
	// runs: the current item has <r> runs (rich text).
	runs bool
	text strings.Builder
}

// Start handles a start-tag event, tracking item text, runs, and ignored subtrees.
func (d *siDecoder) Start(n *tree.Node) error {
	d.ns.Push(n)
	local := n.Local()
	main := sml.IsMain(d.ns.Space(n))
	d.frames = append(d.frames, frame{local: local, main: main})
	k := len(d.frames)
	if d.skip > 0 {
		d.skip++
		d.frames[k-1].skipped = true
		return nil
	}
	rel := k - d.base
	switch {
	case k < d.base:
		// The root (Each) or the wrapper (Text): read on.
		if k == 1 && d.base == 2 && d.each != nil && !(main && local == "sst") {
			d.skipTop()
		}
	case !main:
		d.skipTop()
	case rel == 0 && local == "si":
		d.in = true
		d.runs = false
		d.text.Reset()
	case rel == 1 && d.in && local == "t", rel == 2 && d.in && local == "t" && d.frames[k-2].local == "r":
		d.intT = true
	case rel == 1 && d.in && local == "r":
		d.runs = true
	default:
		// rPh, phoneticPr, rPr and anything unknown.
		d.skipTop()
	}
	return nil
}

// skipTop marks the current element so its subtree is skipped.
func (d *siDecoder) skipTop() {
	d.frames[len(d.frames)-1].skipped = true
	d.skip = 1
}

// End handles an end-tag event, ending an item and, in Each mode, yielding its text.
func (d *siDecoder) End(*tree.Node) error {
	k := len(d.frames)
	f := d.frames[k-1]
	d.frames = d.frames[:k-1]
	d.ns.Pop()
	if d.skip > 0 {
		d.skip--
		return nil
	}
	switch {
	case f.local == "t":
		d.intT = false
	case k == d.base && d.in:
		d.in = false
		if d.each == nil {
			return errItemDone
		}
		i := d.n
		d.n++
		return d.each(i, sml.UnescapeX(d.text.String()))
	}
	return nil
}

// Leaf appends text inside a t element to the current item's text.
func (d *siDecoder) Leaf(n *tree.Node) error {
	if d.skip == 0 && d.intT && isText(n) {
		d.text.WriteString(n.Data())
	}
	return nil
}
