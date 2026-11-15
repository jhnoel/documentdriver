package integrity

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// sstScan reads the shared strings with the streaming tokenizer. It
// counts the si items, finds over-long ones and, for the indexes in want,
// collects their text (the t of an item and of its runs, never rPh).
type sstScan struct {
	f      *Findings
	part   string
	want   map[int]struct{}
	text   map[int]string
	ns     tree.NSStack
	frames []frame
	skip   int
	n      int // si seen
	unique string
	hasU   bool
	// the open si
	inSI, inRPh bool
	cur         strings.Builder
	curLen      int
	keep        bool
}

// Start handles a shared-strings start element and prepares the current string item, returning nil.
func (s *sstScan) Start(n *tree.Node) error {
	s.ns.Push(n)
	local, main := n.Local(), s.ns.Space(n) == sml.NSMain
	s.frames = append(s.frames, frame{local: local})
	d := len(s.frames)
	if s.skip > 0 || !main {
		s.skip++
		s.frames[d-1].skipped = true
		return nil
	}
	switch {
	case d == 1:
		s.unique, s.hasU = n.Attr("uniqueCount")
		if local != "sst" {
			s.skip++
			s.frames[d-1].skipped = true
		}
	case d == 2 && local == "si":
		s.inSI, s.inRPh = true, false
		s.cur.Reset()
		s.curLen = 0
		_, s.keep = s.want[s.n]
	case s.inSI && local == "rPh":
		s.inRPh = true
	}
	return nil
}

// End closes a shared-string item, checking its length and storing wanted text, returning nil.
func (s *sstScan) End(*tree.Node) error {
	f := s.frames[len(s.frames)-1]
	s.frames = s.frames[:len(s.frames)-1]
	s.ns.Pop()
	if f.skipped {
		s.skip--
		return nil
	}
	d := len(s.frames) + 1
	switch {
	case d == 2 && s.inSI:
		if s.curLen > maxCellChars {
			s.f.add(LimitSSTItemLen, s.part, "", "item "+strconv.Itoa(s.n)+" has "+strconv.Itoa(s.curLen)+" characters")
		}
		if s.keep {
			s.text[s.n] = s.cur.String()
		}
		s.n++
		s.inSI = false
	case s.inSI && f.local == "rPh":
		s.inRPh = false
	}
	return nil
}

// Leaf accumulates a shared-string item's t text and its length, returning nil.
func (s *sstScan) Leaf(n *tree.Node) error {
	if s.skip > 0 || !s.inSI || s.inRPh || !isText(n) {
		return nil
	}
	// Text counts only under a t element: the frame on top.
	if k := len(s.frames); k < 2 || s.frames[k-1].local != "t" {
		return nil
	}
	data := n.Data()
	s.curLen += utf16Len(data)
	if s.keep && s.cur.Len() < 4*maxCellChars {
		s.cur.WriteString(data)
	}
	return nil
}

// scanSST scans the shared strings text. It returns the number of items
// (-1 when the part does not scan) and the texts of the indexes in want.
func scanSST(f *Findings, part, src string, want map[int]struct{}) (int, map[int]string) {
	s := &sstScan{f: f, part: part, want: want, text: map[int]string{}}
	if err := tree.ScanString(src, s); err != nil {
		f.add(XMLMalformed, part, "", err.Error())
		return -1, s.text
	}
	if s.hasU {
		if u, ok := sml.ParseIndex(s.unique); !ok || u != s.n {
			f.add(SSTUniqueCount, part, "", "uniqueCount "+s.unique+", "+strconv.Itoa(s.n)+" items")
		}
	}
	return s.n, s.text
}
