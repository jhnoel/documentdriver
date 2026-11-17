// Package text maps Visio text to the HTML of the contract's text
// markup, the pptx grammar's subset (docs/vsdx-design.md, decision f).
// A Text element is mixed content: characters, with cp, pp and tp
// markers that start a character, paragraph or tab run (by the IX of a
// Character, Paragraph or Tabs row) and fld elements whose content is a
// field's cached text. A line feed (\n) terminates a paragraph: a
// trailing \n adds no empty paragraph, so "A\n" is one paragraph "A"
// and "A\n\n" is "A" then an empty one (257 of the 285 page Text
// elements of the corpus, and 95 of 144 master ones, end in \n).
// U+2028 (\u2028) is a line break inside a paragraph. Parse and Plain
// are pure over the tree; Render is pure over Body and the resolved row
// formats.
package text

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Body is a text body: its paragraphs in order, and whether the stored
// text ended in \n (Terminated), which a P2 write reproduces as Visio
// writes it.
type Body struct {
	Paras      []Para
	Terminated bool
}

// Para is one paragraph and the Paragraph row (pp IX) in force at its
// start, -1 when no pp marker precedes it (row 0 applies, as Visio
// does).
type Para struct {
	PP   int
	Runs []Run
}

// Run is a stretch of text under one Character row (cp IX, -1 when no
// marker precedes it), a line break (Br), or a field: Field >= 0 is the
// Field row IX of a field read from the file, Text its cached text. A
// field placeholder read from HTML (ParseHTML) is not resolved yet: Ref
// is its data-ref as written ("page0/s5/field:2", the shape's second
// field in text order, as Render numbers them) and Field is -1; the
// operation that writes the text checks Ref's shape and sets Field to
// the IX of that shape's n-th fld (docs/vsdx-design.md, K5 ruling).
// A run is a field when Field >= 0 or Ref is set.
type Run struct {
	CP    int
	Text  string
	Br    bool
	Field int
	Ref   string
}

// MaxText bounds the characters one Text element may yield; a longer
// one is cut with a note (the contract's output bound, as xlsx's
// 32,767).
const MaxText = 1 << 20

// Parse reads a Text element into a body. Unknown child elements are
// skipped and counted in skipped (a note). Chunk T1.
func Parse(el *tree.Node) (b Body, skipped int) {
	if el == nil {
		return Body{}, 0
	}
	var paras []Para
	var buf strings.Builder
	cp, pp, runes := -1, -1, 0
	started, terminated, cut := false, false, false
	// ensure starts a paragraph if none is open, taking the pp row set
	// for it. flush ends the buffered text as one run of the current
	// paragraph under the cp row in force; text separated only by a
	// marker that is not a run boundary (pp, tp, unknown) is one run.
	ensure := func() {
		if !started {
			paras, pp, started = append(paras, Para{PP: pp}), -1, true
		}
	}
	flush := func() {
		if buf.Len() > 0 {
			last := &paras[len(paras)-1]
			last.Runs = append(last.Runs, Run{CP: cp, Text: buf.String(), Field: -1})
			buf.Reset()
		}
	}
	// ix reads a marker's IX, returning -1 and counting a skip when it
	// is missing or not a non-negative integer. take counts s against
	// MaxText, returns its prefix within the bound, and cuts (counting
	// one skip) when s runs past it.
	ix := func(n *tree.Node) int {
		v, ok := n.Attr("IX")
		i, err := strconv.Atoi(v)
		if !ok || err != nil || i < 0 {
			skipped++
			return -1
		}
		return i
	}
	take := func(s string) string {
		if rc := utf8.RuneCountInString(s); runes+rc <= MaxText {
			runes += rc
			return s
		}
		keep := MaxText - runes
		for i := range s {
			if keep == 0 {
				s = s[:i]
				break
			}
			keep--
		}
		runes, cut = MaxText, true
		skipped++
		return s
	}
	for c := range el.ChildrenSeq() {
		switch c.Kind() {
		case tree.TextNode, tree.CDataNode:
			for s := c.Data(); len(s) > 0 && !cut; {
				nl, br := strings.IndexByte(s, '\n'), strings.Index(s, "\u2028")
				idx, sep, isNL := -1, 0, false
				if nl >= 0 && (br < 0 || nl < br) {
					idx, sep, isNL = nl, 1, true
				} else if br >= 0 {
					idx, sep = br, utf8.RuneLen('\u2028')
				}
				chunk := s
				if idx >= 0 {
					chunk = s[:idx]
				}
				if chunk = take(chunk); chunk != "" {
					ensure()
					terminated = false
					buf.WriteString(chunk)
				}
				if idx < 0 || cut {
					break
				}
				s = s[idx+sep:]
				if runes >= MaxText {
					cut, skipped = true, skipped+1
					break
				}
				runes++
				flush()
				ensure()
				if isNL {
					started, terminated = false, true
				} else {
					terminated = false
					last := &paras[len(paras)-1]
					last.Runs = append(last.Runs, Run{CP: cp, Br: true, Field: -1})
				}
			}
		case tree.ElementNode:
			switch c.Local() {
			case vsd.ElemCharMark:
				flush()
				cp = ix(c)
			case vsd.ElemParaMark:
				pp = ix(c)
			case vsd.ElemTabMark:
			case vsd.ElemField:
				field := ix(c)
				flush()
				if !cut {
					text := take(c.Text())
					ensure()
					terminated = false
					last := &paras[len(paras)-1]
					last.Runs = append(last.Runs, Run{CP: cp, Text: text, Field: field})
				}
			default:
				skipped++
			}
		}
	}
	flush()
	if started && len(paras) > 0 && len(paras[len(paras)-1].Runs) == 0 {
		paras = paras[:len(paras)-1]
	}
	return Body{Paras: paras, Terminated: terminated}, skipped
}

// Plain returns the body's text as find matches it and read's text
// shows it: each paragraph followed by "\n" except the last, a break as
// "\n", a tab as "\t", a field as its cached text. It does not restore
// a trailing "\n" the stored text had (the model has no paragraph for
// it). Chunk T1.
func Plain(b Body) string {
	var sb strings.Builder
	for i, para := range b.Paras {
		if i > 0 {
			sb.WriteByte('\n')
		}
		for _, r := range para.Runs {
			if r.Br {
				sb.WriteByte('\n')
			} else {
				sb.WriteString(r.Text)
			}
		}
	}
	return sb.String()
}
