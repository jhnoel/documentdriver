package tree_test

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	oracle "github.com/jhnoel/documentdriver/internal/ooxml/tree/internal/treeoracle"
)

// The differential fuzz runs one parse and one sequence of edits on tree
// and on treeoracle, the frozen copy of the implementation tree's storage
// replaced, and requires the same bytes, the same errors and the same
// answers to every query after each edit.

var (
	diffPrefixes = []string{"", "a", "p", "w", "x", "mc", "xml", "zz", "xmlns"}
	diffURIs     = []string{"", "urn:a", "urn:p", "urn:x", "urn:w", "urn:d", tree.XMLNamespace}
	diffAttrs    = []string{"a", "b", "id", "x:id", "w:val", "xmlns:x", "xmlns", "xml:space", "p:q", "zz:q", "xmlns:w", "c"}
	diffValues   = []string{"", "1", "a&b", `<"'>`, "  sp ", "\t\n\r", "\x01", "ünï", "urn:x", "urn:w"}
	diffLocals   = []string{"a", "p", "r", "t", "off", "sp", "bad:name", "1x"}
	diffFrags    = []string{
		`<x:a x:b="1"/>`, `text &amp; more`, `<a><b/>t</a>`, `<w:r><w:t>x</w:t></w:r>`,
		`<!--c--><?pi d?>`, `<a xmlns="urn:x"/>`, `<bad`, `<w:p xmlns:w="urn:w"><w:r/></w:p>`, `<![CDATA[z]]>`,
		`<zz:e/>`, `<x:y xmlns:x="urn:2"><x:z/></x:y>`,
	}
	diffNS  = []map[string]string{nil, {"x": "urn:x"}, {"w": "urn:w", "": "urn:d"}, {"a": "urn:a", "zz": "urn:zz"}}
	diffRaw = []struct {
		kind tree.Kind
		raw  string
	}{
		{tree.TextNode, "t &amp; u"}, {tree.CommentNode, "<!--x-->"}, {tree.PINode, "<?p q?>"},
		{tree.CDataNode, "<![CDATA[a<b]]>"}, {tree.TextNode, "<a/>"}, {tree.ElementNode, "<a/>"},
	}
	diffSelectors = []string{"//a", "/r/*", "//x:a[1]", "//*[@id]", "//w:p[@w:val='1']", "//p:sp[2]", "*", "//a:off"}
)

// diffSeeds are the differential fuzz's seed documents: a selection of
// the round-trip cases, a slide and a Word-like body.
var diffSeeds = [][]byte{
	[]byte(`<a/>`),
	[]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\r\n" + `<p:sld xmlns:p="urn:p"/>`),
	[]byte("\xEF\xBB\xBF<r/>"),
	[]byte("<?xml version=\"1.0\"?>\r\n<r\r\n  a=\"1\"\r\n>\r\n  <c>x\r\ny</c>\r\n</r>\r\n"),
	[]byte(`<p:sld xmlns:a="urn:a" xmlns:p="urn:p" xmlns:r="urn:r"><p:cSld><a:off x="1" y="2"/><p:pic r:embed="rId2"/></p:cSld></p:sld>`),
	[]byte(`<r xmlns="urn:1"><a xmlns="urn:2"><b xmlns=""><c/></b></a></r>`),
	[]byte(`<x:r xmlns:x="urn:1"><x:a xmlns:x="urn:2"><x:b/></x:a></x:r>`),
	[]byte(`<r a='1' b="2" c='say "hi"' d="it's"/>`),
	[]byte(`<r a="&amp;&lt;&gt;&quot;&apos;">x &amp; y &lt; z &#x41;&#66;&#x1F600;</r>`),
	[]byte("<?xml version=\"1.0\"?>\n<!-- before -->\n<?pi data?>\n<r><!--in--><?x y z?></r>\n<!-- after -->\n"),
	[]byte(`<r><![CDATA[<not> & markup]]]></r>`),
	[]byte(`<r><a></a><b/><c /><d	/></r>`),
	[]byte("<r\n\ta = \"1\"\n\tb\t=\t'2'\n   ><c\t\n/></r\n\t>"),
	[]byte(`<r><a></a  ></r >`),
	[]byte(`<!DOCTYPE r [ <!ELEMENT r ANY> <!-- ] > --> ]><r/>`),
	[]byte(`<ü:élan xmlns:ü="urn:u" ü:ñ="ß">日本語</ü:élan>`),
	[]byte(`<r xml:space="preserve" xml:lang="en"> x </r>`),
	[]byte(`<p:sp xmlns:p="urn:p" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:a14="urn:a14" mc:Ignorable="a14"><mc:AlternateContent><mc:Choice Requires="a14"><a14:x/></mc:Choice><mc:Fallback><p:y/></mc:Fallback></mc:AlternateContent></p:sp>`),
	[]byte(`<w:document xmlns:w="urn:w" xmlns:x="urn:x" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="x"><w:body>` +
		`<w:p w:val="1" x:id='2'><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t xml:space="preserve"> a &amp; b </w:t></w:r></w:p>` +
		"\n<!--c--><w:p><mc:AlternateContent><mc:Choice Requires=\"x\"><x:a/></mc:Choice><mc:Fallback><w:r><w:t>f</w:t></w:r></mc:Fallback></mc:AlternateContent></w:p>" +
		`<w:sectPr/></w:body></w:document>`),
	[]byte(strings.Repeat("<a>", 64) + strings.Repeat("</a>", 64)),
}

// FuzzDifferential checks tree against treeoracle on random parses and
// edit sequences. prog is read a few bytes per edit: an opcode, then
// indexes into the node list and the vocabularies above.
func FuzzDifferential(f *testing.F) {
	progs := [][]byte{
		{0, 1, 2, 3, 1, 4, 5, 6, 2, 7, 8, 9},
		{4, 3, 1, 2, 5, 0, 1, 2, 3, 6, 0, 1, 7, 1, 2, 3, 14, 0, 9},
		{7, 0, 1, 2, 14, 1, 9, 0, 0, 1, 2, 3, 5, 2, 3, 1, 1},
		{11, 0, 1, 2, 3, 12, 3, 4, 5, 6, 10, 1, 2, 3, 13, 1, 2, 2, 8, 3, 4, 4},
		{9, 1, 2, 3, 4, 5, 6, 15, 1, 2, 4, 2, 0, 1, 0, 0, 1, 1},
	}
	for _, in := range diffSeeds {
		for _, p := range progs {
			f.Add(in, p)
		}
	}
	f.Fuzz(func(t *testing.T, in, prog []byte) {
		if len(in) > 1<<14 || len(prog) > 256 {
			return
		}
		d, err := tree.Parse(in)
		od, oerr := oracle.Parse(in)
		if !sameErr(err, oerr) {
			t.Fatalf("Parse: got %v, oracle %v", err, oerr)
		}
		if err != nil {
			return
		}
		s := &diffState{t: t, d: d, od: od}
		d.Node().Walk(func(n *tree.Node) bool { s.nodes = append(s.nodes, n); return true })
		od.Node().Walk(func(n *oracle.Node) bool { s.onodes = append(s.onodes, n); return true })
		s.check("parse", true)
		for len(prog) > 0 {
			var op string
			op, prog = s.step(prog)
			s.check(op, len(prog) == 0)
		}
		for _, sel := range diffSelectors {
			got, err := d.Select(sel)
			want, oerr := od.Select(sel)
			if !sameErr(err, oerr) {
				t.Fatalf("Select %q: got %v, oracle %v", sel, err, oerr)
			}
			s.sameList("Select "+sel, got, want)
		}
	})
}

// diffState is the pair of documents and the nodes known in both, by
// index: those the parse built and those edits returned.
type diffState struct {
	t      *testing.T
	d      *tree.Document
	od     *oracle.Document
	nodes  []*tree.Node
	onodes []*oracle.Node
	log    []string
}

func (s *diffState) add(n *tree.Node, o *oracle.Node) {
	if (n == nil) != (o == nil) {
		s.t.Fatalf("%s: one side returned no node", strings.Join(s.log, "; "))
	}
	if n != nil {
		s.nodes = append(s.nodes, n)
		s.onodes = append(s.onodes, o)
	}
}

func sameErr(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Error() == b.Error()
}

// step runs one edit read from prog and returns its description and the
// rest of prog.
func (s *diffState) step(prog []byte) (string, []byte) {
	arg := func() int {
		if len(prog) == 0 {
			return 0
		}
		b := prog[0]
		prog = prog[1:]
		return int(b)
	}
	pick := func(n int) int { return arg() % n }
	code := pick(16)
	ai := pick(len(s.nodes))
	a, oa := s.nodes[ai], s.onodes[ai]
	child := func(n *tree.Node, o *oracle.Node) (*tree.Node, *oracle.Node) {
		k := arg()
		ch, och := n.Children(), o.Children()
		if len(ch) != len(och) || len(ch) == 0 {
			return nil, nil
		}
		return ch[k%len(ch)], och[k%len(och)]
	}
	var op string
	var err, oerr error
	switch code {
	case 0:
		name, val := diffAttrs[pick(len(diffAttrs))], diffValues[pick(len(diffValues))]
		op = fmt.Sprintf("SetAttr(#%d, %q, %q)", ai, name, val)
		err, oerr = a.SetAttr(name, val), oa.SetAttr(name, val)
	case 1:
		name := diffAttrs[pick(len(diffAttrs))]
		op = fmt.Sprintf("RemoveAttr(#%d, %q)", ai, name)
		if a.RemoveAttr(name) != oa.RemoveAttr(name) {
			err = fmt.Errorf("RemoveAttr results differ")
		}
	case 2:
		val := diffValues[pick(len(diffValues))]
		op = fmt.Sprintf("SetText(#%d, %q)", ai, val)
		err, oerr = a.SetText(val), oa.SetText(val)
	case 3:
		op = fmt.Sprintf("Remove(#%d)", ai)
		err, oerr = a.Remove(), oa.Remove()
	case 4, 14:
		bi := pick(len(s.nodes))
		b, ob := s.nodes[bi], s.onodes[bi]
		k := arg()
		if a.Parent() != nil {
			_, _ = a.Remove(), oa.Remove() //nolint:errcheck // compared through the trees
		}
		if code == 14 {
			op = fmt.Sprintf("AppendChild(#%d, #%d)", bi, ai)
			err, oerr = b.AppendChild(a), ob.AppendChild(oa)
		} else {
			i := k % (len(b.Children()) + 2)
			op = fmt.Sprintf("InsertAt(#%d, %d, #%d)", bi, i, ai)
			err, oerr = b.InsertAt(i, a), ob.InsertAt(i, oa)
		}
	case 5:
		frag, ns := diffFrags[pick(len(diffFrags))], diffNS[pick(len(diffNS))]
		i := arg() % (len(a.Children()) + 2)
		op = fmt.Sprintf("InsertFragment(#%d, %d, %q, %v)", ai, i, frag, ns)
		got, e1 := a.InsertFragment(i, []byte(frag), ns)
		want, e2 := oa.InsertFragment(i, []byte(frag), ns)
		err, oerr = e1, e2
		s.addAll(op, got, want)
	case 6:
		local := diffLocals[pick(len(diffLocals))]
		op = fmt.Sprintf("Rename(#%d, %q)", ai, local)
		err, oerr = a.Rename(local), oa.Rename(local)
	case 7:
		uri, prefix, local := diffURIs[pick(len(diffURIs))], diffPrefixes[pick(len(diffPrefixes))], diffLocals[pick(len(diffLocals))]
		op = fmt.Sprintf("NewElement(%q, %q, %q)", uri, prefix, local)
		n, e1 := tree.NewElement(uri, prefix, local)
		o, e2 := oracle.NewElement(uri, prefix, local)
		err, oerr = e1, e2
		s.add(n, o)
	case 8:
		val := diffValues[pick(len(diffValues))]
		op = fmt.Sprintf("NewText(%q)", val)
		s.add(tree.NewText(val), oracle.NewText(val))
	case 9:
		uri, prefix := diffURIs[pick(len(diffURIs))], diffPrefixes[pick(len(diffPrefixes))]
		local, val := diffLocals[pick(len(diffLocals))], diffValues[pick(len(diffValues))]
		op = fmt.Sprintf("SetAttrNS(#%d, %q, %q, %q, %q)", ai, uri, prefix, local, val)
		err, oerr = a.SetAttrNS(uri, prefix, local, val), oa.SetAttrNS(uri, prefix, local, val)
	case 10:
		bi := pick(len(s.nodes))
		c, oc := child(s.nodes[bi], s.onodes[bi])
		op = fmt.Sprintf("ReplaceChild(#%d, #%d, child)", bi, ai)
		if c != nil {
			err, oerr = s.nodes[bi].ReplaceChild(a, c), s.onodes[bi].ReplaceChild(oa, oc)
		}
	case 11:
		frag, ns := diffFrags[pick(len(diffFrags))], diffNS[pick(len(diffNS))]
		c, oc := child(a, oa)
		op = fmt.Sprintf("ReplaceWithFragment(#%d, child, %q, %v)", ai, frag, ns)
		if c != nil {
			got, e1 := a.ReplaceWithFragment(c, []byte(frag), ns)
			want, e2 := oa.ReplaceWithFragment(oc, []byte(frag), ns)
			err, oerr = e1, e2
			s.addAll(op, got, want)
		}
	case 12:
		bi := pick(len(s.nodes))
		after := arg()%2 == 1
		c, oc := child(s.nodes[bi], s.onodes[bi])
		op = fmt.Sprintf("InsertBefore/After(%v, #%d, #%d, child)", after, bi, ai)
		switch {
		case after && c != nil:
			err, oerr = s.nodes[bi].InsertAfter(a, c), s.onodes[bi].InsertAfter(oa, oc)
		case !after:
			err, oerr = s.nodes[bi].InsertBefore(a, c), s.onodes[bi].InsertBefore(oa, oc)
		}
	case 13:
		r := diffRaw[pick(len(diffRaw))]
		op = fmt.Sprintf("NewRaw(%s, %q)", r.kind, r.raw)
		n, e1 := tree.NewRaw(r.kind, []byte(r.raw))
		o, e2 := oracle.NewRaw(oracle.Kind(r.kind), []byte(r.raw))
		err, oerr = e1, e2
		s.add(n, o)
	case 15:
		frag, ns := diffFrags[pick(len(diffFrags))], diffNS[pick(len(diffNS))]
		op = fmt.Sprintf("ParseFragment(%q, %v)", frag, ns)
		got, e1 := tree.ParseFragment([]byte(frag), ns)
		want, e2 := oracle.ParseFragment([]byte(frag), ns)
		err, oerr = e1, e2
		s.addAll(op, got, want)
	}
	s.log = append(s.log, op)
	if !sameErr(err, oerr) {
		s.t.Fatalf("%s: got error %v, oracle %v", strings.Join(s.log, "; "), err, oerr)
	}
	return op, prog
}

func (s *diffState) addAll(op string, got []*tree.Node, want []*oracle.Node) {
	if len(got) != len(want) {
		s.t.Fatalf("%s: %d nodes, oracle %d", op, len(got), len(want))
	}
	for i := range got {
		s.add(got[i], want[i])
	}
}

// index returns n's index in the node list, or -1.
func (s *diffState) index(n *tree.Node) int {
	if n == nil {
		return -2
	}
	return slices.Index(s.nodes, n)
}

func (s *diffState) oindex(n *oracle.Node) int {
	if n == nil {
		return -2
	}
	return slices.Index(s.onodes, n)
}

// sameList compares two node lists by identity where the nodes are known
// and by serialisation otherwise.
func (s *diffState) sameList(what string, got []*tree.Node, want []*oracle.Node) {
	s.t.Helper()
	if len(got) != len(want) {
		s.fail(what, len(got), len(want))
	}
	for i := range got {
		gi, wi := s.index(got[i]), s.oindex(want[i])
		if gi != wi {
			s.fail(fmt.Sprintf("%s[%d] index", what, i), gi, wi)
		}
		if gi == -1 && !bytes.Equal(got[i].Bytes(), want[i].Bytes()) {
			s.fail(fmt.Sprintf("%s[%d] bytes", what, i), got[i].Bytes(), want[i].Bytes())
		}
	}
}

func (s *diffState) fail(what string, got, want any) {
	s.t.Helper()
	s.t.Fatalf("after %s:\n%s: got %#v\n oracle %#v", strings.Join(s.log, "; "), what, got, want)
}

// check compares the documents and every known node; full adds the
// queries that search (Find, FindAll, Select-like lookups), which cost
// more and run after the parse and the last edit only.
func (s *diffState) check(op string, full bool) {
	s.t.Helper()
	if g, w := s.d.Bytes(), s.od.Bytes(); !bytes.Equal(g, w) {
		s.fail("document bytes ("+op+")", string(g), string(w))
	}
	if g, w := s.d.Modified(), s.od.Modified(); g != w {
		s.fail("document Modified", g, w)
	}
	if g, w := s.index(s.d.Root()), s.oindex(s.od.Root()); g != w {
		s.fail("Root", g, w)
	}
	for i, n := range s.nodes {
		o := s.onodes[i]
		s.checkNode(fmt.Sprintf("#%d", i), n, o, full && i < 8)
	}
}

func (s *diffState) checkNode(id string, n *tree.Node, o *oracle.Node, search bool) {
	s.t.Helper()
	eq := func(what string, got, want any) {
		s.t.Helper()
		if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
			s.fail(id+" "+what, got, want)
		}
	}
	eq("Kind", n.Kind().String(), o.Kind().String())
	eq("Name", n.Name(), o.Name())
	eq("Prefix", n.Prefix(), o.Prefix())
	eq("Local", n.Local(), o.Local())
	eq("Space", n.Space(), o.Space())
	eq("Text", n.Text(), o.Text())
	eq("Data", n.Data(), o.Data())
	eq("Modified", n.Modified(), o.Modified())
	eq("IsDecl", n.IsDecl(), o.IsDecl())
	eq("Raw", string(n.Raw()), string(o.Raw()))
	eq("Bytes", string(n.Bytes()), string(o.Bytes()))
	eq("StartTag", string(n.StartTag()), string(o.StartTag()))
	eq("EndTag", string(n.EndTag()), string(o.EndTag()))
	eq("StartTagModified", n.StartTagModified(), o.StartTagModified())
	eq("StartTagWithout", string(n.StartTagWithout(func(q string) bool { return q == "a" || q == "xmlns:x" })),
		string(o.StartTagWithout(func(q string) bool { return q == "a" || q == "xmlns:x" })))
	var ga, wa []string
	for _, a := range n.Attrs() {
		ga = append(ga, a.Name+"="+a.Value)
	}
	for _, a := range o.Attrs() {
		wa = append(wa, a.Name+"="+a.Value)
	}
	eq("Attrs", ga, wa)
	s1, e1, ok1 := n.Span()
	s2, e2, ok2 := o.Span()
	eq("Span", []any{s1, e1, ok1}, []any{s2, e2, ok2})
	s1, e1, ok1 = n.SpanIn(s.d)
	s2, e2, ok2 = o.SpanIn(s.od)
	eq("SpanIn", []any{s1, e1, ok1}, []any{s2, e2, ok2})
	eq("Parent", s.index(n.Parent()), s.oindex(o.Parent()))
	s.sameList(id+" Children", n.Children(), o.Children())
	s.sameList(id+" Elements", n.Elements(), o.Elements())
	s.sameList(id+" ChildrenSeq", slices.Collect(n.ChildrenSeq()), o.Children())
	s.sameList(id+" ElementsSeq", slices.Collect(n.ElementsSeq()), o.Elements())
	eq("HasElements", n.HasElements(), len(o.Elements()) > 0)
	oc := o.Children()
	eq("FirstChild", s.index(n.FirstChild()), s.oindex(firstOf(oc)))
	eq("LastChild", s.index(n.LastChild()), s.oindex(lastOf(oc)))
	var sibs []*tree.Node
	for c := n.LastChild(); c != nil; c = c.PrevSibling() {
		sibs = append(sibs, c)
	}
	slices.Reverse(sibs)
	s.sameList(id+" PrevSibling chain", sibs, oc)
	sibs = sibs[:0]
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		sibs = append(sibs, c)
	}
	s.sameList(id+" NextSibling chain", sibs, oc)
	eq("FirstElement", s.index(n.FirstElement()), s.oindex(firstOf(o.Elements())))
	eq("HasAttrs", n.HasAttrs(), len(o.Attrs()) > 0)
	var gs []string
	for k, v := range n.AttrsSeq() {
		gs = append(gs, k+"="+v)
	}
	eq("AttrsSeq", gs, attrStrings(o.Attrs()))
	eq("AttrNamesSeq", slices.Collect(n.AttrNamesSeq()), attrNames(o.Attrs()))
	for _, q := range diffAttrs {
		v1, ok1 := n.Attr(q)
		v2, ok2 := o.Attr(q)
		eq("Attr "+q, []any{v1, ok1}, []any{v2, ok2})
		r1, ok1 := n.AttrRaw(q)
		r2, ok2 := o.AttrRaw(q)
		eq("AttrRaw "+q, []any{string(r1), ok1}, []any{string(r2), ok2})
	}
	for _, p := range diffPrefixes {
		u1, ok1 := n.LookupPrefix(p)
		u2, ok2 := o.LookupPrefix(p)
		eq("LookupPrefix "+p, []any{u1, ok1}, []any{u2, ok2})
	}
	for _, u := range diffURIs {
		p1, ok1 := n.LookupNamespace(u)
		p2, ok2 := o.LookupNamespace(u)
		eq("LookupNamespace "+u, []any{p1, ok1}, []any{p2, ok2})
		for _, l := range diffLocals[:6] {
			v1, ok1 := n.AttrNS(u, l)
			v2, ok2 := o.AttrNS(u, l)
			eq("AttrNS "+u+" "+l, []any{v1, ok1}, []any{v2, ok2})
			eq("Is "+u+" "+l, n.Is(u, l), o.Is(u, l))
			eq("Child "+u+" "+l, s.index(n.Child(u, l)), s.oindex(o.Child(u, l)))
			if search {
				eq("Find "+u+" "+l, s.index(n.Find(u, l)), s.oindex(o.Find(u, l)))
				s.sameList(id+" FindAll "+u+" "+l, n.FindAll(u, l), o.FindAll(u, l))
			}
		}
	}
	if search {
		for _, q := range []string{"a", "x:a", "w:p", "p:sp"} {
			s.sameList(id+" FindQName "+q, n.FindQName(q), o.FindQName(q))
		}
	}
	var gd, wd []string
	n.Declarations(func(p, u string) { gd = append(gd, p+"="+u) })
	o.Declarations(func(p, u string) { wd = append(wd, p+"="+u) })
	eq("Declarations", gd, wd)
}

func firstOf(ns []*oracle.Node) *oracle.Node {
	if len(ns) == 0 {
		return nil
	}
	return ns[0]
}

func attrStrings(as []oracle.Attribute) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Name+"="+a.Value)
	}
	return out
}

func attrNames(as []oracle.Attribute) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Name)
	}
	return out
}

func lastOf(ns []*oracle.Node) *oracle.Node {
	if len(ns) == 0 {
		return nil
	}
	return ns[len(ns)-1]
}
