package text

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// Errors from text.replace.
var (
	// ErrNoMatch: the text was not found.
	ErrNoMatch = errors.New("text: text not found")
	// ErrManyMatches: the text was found more than once and all was not
	// set.
	ErrManyMatches = errors.New("text: text found more than once; give occurrence, or all to replace every one")
	// ErrCrossing: a match crosses a paragraph or line break, a field, or
	// content in mc:AlternateContent, which a replacement in one run
	// cannot keep.
	ErrCrossing = errors.New("text: match cannot be replaced in place")
)

// segment kinds in the searched text.
const (
	segRun = iota
	segBreak
	segPara
)

// seg maps a byte range of the searched text to its source.
type seg struct {
	kind  int
	start int // byte offset in the searched text
	para  int // paragraph index
	item  int // item index in the paragraph (segRun, segBreak)
	n     int // length in bytes
}

// match is one occurrence, as (item, byte offset) of its first byte and
// of the byte after its last, within one paragraph.
type match struct {
	para           int
	ks, os, ke, oe int
}

// Replace returns the bytes for the range from body's first a:p to its
// last with every match of find replaced by with (all set), or the one
// match (all unset), and the number of matches. The text replacing a
// match goes in the run where the match starts, keeping that run's
// formatting; runs a match spans into are merged into it, keeping what
// follows the match in the last one. Everything else keeps its bytes.
//
// It fails when find is empty, is not found, is found more than once
// without all or occurrence (which picks one match, counting from 1), or a match crosses a paragraph break, a line break, a
// field (a:fld, whose text PowerPoint regenerates) or content in
// mc:AlternateContent.
func Replace(body *tree.Node, find, with string, all bool, occurrence int) ([]byte, int, error) {
	if find == "" {
		return nil, 0, errors.New("text: empty find")
	}
	for _, c := range with {
		if !storable(c) {
			return nil, 0, fmt.Errorf("text: character %U cannot be stored in XML", c)
		}
	}
	var paras []*tree.Node
	for _, c := range body.Elements() {
		if derive.Is(c, derive.DML, "p") {
			paras = append(paras, c)
		}
	}
	var sb strings.Builder
	var segs []seg
	its := make([][]item, len(paras))
	for pi, p := range paras {
		if pi > 0 {
			segs = append(segs, seg{kind: segPara, start: sb.Len(), para: pi, n: 1})
			sb.WriteByte('\n')
		}
		its[pi] = items(p, nil)
		for k, it := range its[pi] {
			if it.run.Br {
				segs = append(segs, seg{kind: segBreak, start: sb.Len(), para: pi, item: k, n: 1})
				sb.WriteByte('\v')
				continue
			}
			t := ""
			if it.text != nil {
				t = it.text.Text()
			}
			segs = append(segs, seg{kind: segRun, start: sb.Len(), para: pi, item: k, n: len(t)})
			sb.WriteString(t)
		}
	}
	s := sb.String()
	var ms []match
	for from := 0; from <= len(s); {
		i := strings.Index(s[from:], find)
		if i < 0 {
			break
		}
		a, z := from+i, from+i+len(find)
		m, err := locate(segs, its, a, z)
		if err != nil {
			return nil, 0, err
		}
		ms = append(ms, m)
		from = z
	}
	switch {
	case len(ms) == 0:
		return nil, 0, fmt.Errorf("%w: %q", ErrNoMatch, find)
	case occurrence > len(ms):
		return nil, len(ms), fmt.Errorf("%w: %q occurs %d times, not %d", ErrNoMatch, find, len(ms), occurrence)
	case occurrence > 0:
		ms = ms[occurrence-1 : occurrence]
	case len(ms) > 1 && !all:
		return nil, len(ms), &ManyError{Find: find, Matches: len(ms)}
	}
	found := len(ms)
	byPara := map[int][]match{}
	for _, m := range ms {
		byPara[m.para] = append(byPara[m.para], m)
	}
	var out bytes.Buffer
	first, last := -1, -1
	kids := body.Children()
	for i, c := range kids {
		if derive.Is(c, derive.DML, "p") {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	pi := 0
	for _, c := range kids[first : last+1] {
		if !derive.Is(c, derive.DML, "p") {
			out.Write(c.Raw())
			continue
		}
		if m := byPara[pi]; m != nil {
			replacePara(&out, c, its[pi], m, with)
		} else {
			out.Write(c.Bytes())
		}
		pi++
	}
	return out.Bytes(), found, nil
}

// ManyError is ErrManyMatches with the number of matches.
type ManyError struct {
	Find    string
	Matches int
}

// Error says how often the text occurs.
func (e *ManyError) Error() string {
	return fmt.Sprintf("%v: %q occurs %d times", ErrManyMatches, e.Find, e.Matches)
}

// Unwrap returns ErrManyMatches.
func (e *ManyError) Unwrap() error { return ErrManyMatches }

// locate maps the byte range [a, z) of the searched text to one
// paragraph's items, failing when it crosses what a replacement cannot.
func locate(segs []seg, its [][]item, a, z int) (match, error) {
	k := sort.Search(len(segs), func(i int) bool { return segs[i].start+segs[i].n > a })
	m := match{para: -1}
	for ; k < len(segs) && segs[k].start < z; k++ {
		sg := segs[k]
		if sg.n == 0 {
			continue // an empty run inside the match: merged away
		}
		switch sg.kind {
		case segPara:
			return m, fmt.Errorf("%w: it crosses a paragraph break", ErrCrossing)
		case segBreak:
			return m, fmt.Errorf("%w: it crosses a line break", ErrCrossing)
		}
		it := its[sg.para][sg.item]
		if it.mc != nil {
			return m, fmt.Errorf("%w: it touches content in mc:AlternateContent", ErrCrossing)
		}
		if derive.Is(it.el, derive.DML, "fld") {
			typ, _ := it.el.Attr("type")
			return m, fmt.Errorf("%w: it touches a field (a:fld type %q), whose text PowerPoint regenerates", ErrCrossing, typ)
		}
		if m.para < 0 {
			m.para, m.ks, m.os = sg.para, sg.item, a-sg.start
		}
		m.ke, m.oe = sg.item, min(z, sg.start+sg.n)-sg.start
	}
	return m, nil
}

// replacePara writes paragraph p with the matches replaced, processing
// them from last to first so that earlier offsets stay valid.
func replacePara(out *bytes.Buffer, p *tree.Node, its []item, ms []match, with string) {
	texts := make([]string, len(its))
	for i, it := range its {
		if it.text != nil {
			texts[i] = it.text.Text()
		}
	}
	changed := map[int]bool{}
	removed := map[int]bool{}
	for j := len(ms) - 1; j >= 0; j-- {
		m := ms[j]
		texts[m.ks] = texts[m.ks][:m.os] + with + texts[m.ke][m.oe:]
		changed[m.ks] = true
		for k := m.ks + 1; k <= m.ke; k++ {
			removed[k] = true
		}
	}
	idx := map[*tree.Node]int{}
	for i, it := range its {
		idx[it.el] = i
	}
	out.Write(p.StartTag())
	for _, c := range p.Children() {
		i, isItem := idx[c]
		switch {
		case !isItem:
			out.Write(c.Raw())
		case removed[i]:
		case changed[i]:
			writeRunText(out, c, its[i].text, texts[i])
		default:
			out.Write(c.Bytes())
		}
	}
	out.Write(p.EndTag())
}

// writeRunText writes run r with the text of its a:t t replaced.
func writeRunText(out *bytes.Buffer, r, t *tree.Node, s string) {
	out.Write(r.StartTag())
	for _, c := range r.Children() {
		if c != t {
			out.Write(c.Raw())
			continue
		}
		writeElem(out, t.Name(), t.Attrs(), tree.EscapeText(s))
	}
	out.Write(r.EndTag())
}
