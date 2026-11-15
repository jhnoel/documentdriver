package tree

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// FuzzRoundTrip checks that any input the tokenizer accepts is reproduced
// byte for byte, untouched and rebuilt, and that rejecting is consistent
// with Check.
func FuzzRoundTrip(f *testing.F) {
	for _, c := range roundTripCases {
		if len(c.in) < 4096 {
			f.Add([]byte(c.in))
		}
	}
	f.Add([]byte(slide))
	f.Add([]byte(`<a x="1"><b:c xmlns:b="u" b:y='2'/>t&amp;<![CDATA[z]]><!--c--><?p q?></a>`))
	f.Fuzz(func(t *testing.T, in []byte) {
		doc, err := Parse(in)
		if err != nil {
			if Check(in) == nil {
				t.Fatal("Parse failed but Check passed")
			}
			return
		}
		if err := Check(in); err != nil {
			t.Fatalf("Parse passed but Check failed: %v", err)
		}
		checkScan(t, doc, in)
		if got := doc.Bytes(); !bytes.Equal(got, in) {
			t.Fatalf("untouched round trip differs:\n got %q\nwant %q", got, in)
		}
		checkTags(t, doc, false)
		rebuildAll(doc)
		if got := doc.Bytes(); !bytes.Equal(got, in) {
			t.Fatalf("rebuilt round trip differs:\n got %q\nwant %q", got, in)
		}
		checkTags(t, doc, false)
	})
}

// events records what Scan reports, one line per call.
type events []string

func (e *events) Start(n *Node) error { *e = append(*e, startEvent(n)); return nil }
func (e *events) End(n *Node) error   { *e = append(*e, endEvent(n)); return nil }
func (e *events) Leaf(n *Node) error  { *e = append(*e, leafEvent(n)); return nil }

func startEvent(n *Node) string {
	s, _, _ := n.Span()
	return fmt.Sprintf("start %s %d %q", n.Name(), s, n.Attrs())
}

func endEvent(n *Node) string {
	s, end, _ := n.Span()
	return fmt.Sprintf("end %s %d %d", n.Name(), s, end)
}

func leafEvent(n *Node) string { return fmt.Sprintf("%s %q", n.Kind(), n.Raw()) }

// checkScan checks that Scan of in reports doc's nodes in document order.
func checkScan(t *testing.T, doc *Document, in []byte) {
	t.Helper()
	var want events
	var walk func(n *Node)
	walk = func(n *Node) {
		for _, c := range n.Children() {
			if c.Kind() != ElementNode {
				_ = want.Leaf(c) //nolint:errcheck // events never fails
				continue
			}
			_ = want.Start(c) //nolint:errcheck // events never fails
			walk(c)           // depth bounded by MaxDepth
			_ = want.End(c)   //nolint:errcheck // events never fails
		}
	}
	walk(doc.Node())
	var got events
	if err := Scan(in, &got); err != nil {
		t.Fatalf("Parse passed but Scan failed: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Scan reported\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// checkTags checks that every element is its start tag, its children and
// its end tag, and (unless modified is set) that no start tag reports a
// modification.
func checkTags(t *testing.T, doc *Document, modified bool) {
	t.Helper()
	doc.Node().Walk(func(n *Node) bool {
		if n.Kind() != ElementNode {
			return true
		}
		var b bytes.Buffer
		b.Write(n.StartTag())
		if n.EndTag() != nil {
			for _, c := range n.Children() {
				b.Write(c.Bytes())
			}
			b.Write(n.EndTag())
		}
		if !bytes.Equal(b.Bytes(), n.Bytes()) {
			t.Fatalf("start tag + children + end tag = %q, element = %q", b.Bytes(), n.Bytes())
		}
		if !modified && n.StartTagModified() {
			t.Fatalf("unmodified start tag of %s reports a modification", n.Name())
		}
		return true
	})
}

func TestStartTagModifiedComparesBytes(t *testing.T) {
	doc := mustParse(t, slide)
	off := mustSelect1(t, doc, "//a:off")
	if err := off.SetAttr("x", "1"); err != nil {
		t.Fatal(err)
	}
	if !off.StartTagModified() {
		t.Fatal("changed start tag not reported")
	}
	if err := off.SetAttr("x", "100"); err != nil {
		t.Fatal(err)
	}
	if !off.Modified() {
		t.Fatal("Modified is expected to stay set after A->B->A")
	}
	if off.StartTagModified() {
		t.Fatal("A->B->A start tag reported modified")
	}
	sp := off.Parent().Parent().Parent()
	if sp.StartTagModified() {
		t.Fatal("ancestor start tag reported modified")
	}
}

// FuzzSetAttr checks that any value SetAttr accepts is written so that the
// output is well formed and reads back as the same value, with the rest of
// the document unchanged.
func FuzzSetAttr(f *testing.F) {
	f.Add("plain", byte('"'))
	f.Add(`a"b'c<d>&e`, byte('\''))
	f.Add("tab\tnl\ncr\r", byte('"'))
	f.Add("\x00", byte('"'))
	f.Fuzz(func(t *testing.T, val string, q byte) {
		quote := "\""
		if q%2 == 1 {
			quote = "'"
		}
		in := `<r><e a=` + quote + `x` + quote + ` b="y"/><s/></r>`
		doc, err := Parse([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		e := doc.Root().Elements()[0]
		if err := e.SetAttr("a", val); err != nil {
			return // value not representable in XML
		}
		out := doc.Bytes()
		doc2, err := Parse(out)
		if err != nil {
			t.Fatalf("output not well formed: %v\n%q", err, out)
		}
		if got, _ := doc2.Root().Elements()[0].Attr("a"); got != val {
			t.Fatalf("read back %q, want %q", got, val)
		}
		if !bytes.HasSuffix(out, []byte(` b="y"/><s/></r>`)) || !bytes.HasPrefix(out, []byte(`<r><e a=`+quote)) {
			t.Fatalf("unrelated bytes changed: %q", out)
		}
	})
}
