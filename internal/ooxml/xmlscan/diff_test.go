package xmlscan_test

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// event is one node as a scan reports it: an element's start (its
// attributes as written and decoded), an element's end (its whole span)
// or a leaf.
type event struct {
	what       string
	leaf       xmlscan.Kind
	start, end int
	name       string
	attrs      [][3]string
}

type recorder struct{ events []event }

func (r *recorder) Start(n *tree.Node) error {
	s, _, _ := n.Span()
	e := event{what: "start", start: s, end: -1, name: n.Name()}
	for name, v := range n.AttrsSeq() {
		raw, _ := n.AttrRaw(name)
		e.attrs = append(e.attrs, [3]string{name, string(raw), v})
	}
	r.events = append(r.events, e)
	return nil
}

func (r *recorder) End(n *tree.Node) error {
	s, e, _ := n.Span()
	r.events = append(r.events, event{what: "end", start: s, end: e})
	return nil
}

var leafKinds = map[tree.Kind]xmlscan.Kind{
	tree.TextNode: xmlscan.Text, tree.CDataNode: xmlscan.CData, tree.CommentNode: xmlscan.Comment,
	tree.PINode: xmlscan.PI, tree.DoctypeNode: xmlscan.Doctype,
}

func (r *recorder) Leaf(n *tree.Node) error {
	s, e, _ := n.Span()
	r.events = append(r.events, event{what: "leaf", leaf: leafKinds[n.Kind()], start: s, end: e})
	return nil
}

// scanEvents is xmlscan's view of src in the recorder's terms.
func scanEvents(src string) ([]event, error) {
	var out []event
	var open []int
	i := 0
	for i < len(src) {
		t, err := xmlscan.Next(src, i)
		if err != nil {
			return nil, fmt.Errorf("Next at %d: %w", i, err)
		}
		if t.Start != i || t.End <= i {
			return nil, fmt.Errorf("Next at %d: no progress (%+v)", i, t)
		}
		switch t.Kind {
		case xmlscan.StartTag, xmlscan.EmptyTag:
			e := event{what: "start", start: t.Start, end: -1, name: t.Name(src)}
			for a := range xmlscan.Attrs(src, t) {
				e.attrs = append(e.attrs, [3]string{a.Name(src), a.Raw(src), a.Value(src)})
				if f, ok := xmlscan.FindAttr(src, t, a.Name(src)); !ok || f != a {
					return nil, fmt.Errorf("FindAttr(%s) at %d = %+v, %v; Attrs gives %+v", a.Name(src), t.Start, f, ok, a)
				}
			}
			out = append(out, e)
			if t.Kind == xmlscan.EmptyTag {
				out = append(out, event{what: "end", start: t.Start, end: t.End})
			} else {
				open = append(open, t.Start)
			}
		case xmlscan.EndTag:
			if len(open) == 0 {
				return nil, fmt.Errorf("end tag at %d closes nothing", t.Start)
			}
			out = append(out, event{what: "end", start: open[len(open)-1], end: t.End})
			open = open[:len(open)-1]
		default:
			out = append(out, event{what: "leaf", leaf: t.Kind, start: t.Start, end: t.End})
		}
		i = t.End
	}
	if i != len(src) {
		return nil, fmt.Errorf("tokens end at %d of %d", i, len(src))
	}
	return out, nil
}

// differential compares xmlscan with tree.ScanString over src, which tree
// must accept, and checks ElementEnd against every element's span.
func differential(src string) error {
	var r recorder
	if err := tree.ScanString(src, &r); err != nil {
		return err
	}
	got, err := scanEvents(src)
	if err != nil {
		return err
	}
	if len(got) != len(r.events) {
		return fmt.Errorf("%d events, tree %d", len(got), len(r.events))
	}
	ends := map[int]int{}
	for i := range got {
		if !reflect.DeepEqual(got[i], r.events[i]) {
			return fmt.Errorf("event %d = %+v, tree %+v", i, got[i], r.events[i])
		}
		if got[i].what == "end" {
			ends[got[i].start] = got[i].end
		}
	}
	for start, end := range ends {
		t, err := xmlscan.Next(src, start)
		if err != nil {
			return err
		}
		if _, e, err := xmlscan.ElementEnd(src, t); err != nil || e != end {
			return fmt.Errorf("ElementEnd at %d = %d, %v; tree %d", start, e, err, end)
		}
	}
	return nil
}

func TestDifferentialFixtures(t *testing.T) {
	for _, src := range []string{
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\r\n" + `<!DOCTYPE a [<!ENTITY x "y">]><a xmlns="u" xmlns:p='v'><p:b c="&lt;&#x41;" d='"'/>t&amp;u<![CDATA[<x>]]><!--c--><?pi d?><e
>
</e></a>` + "\n<!--tail-->",
		`<worksheet><sheetData><row r="1" spans="1:2"><c r="A1" t="s"><v>0</v></c><c r="B1"><f>SUM(A1:A2)&gt;0</f><v>1</v></c></row></sheetData></worksheet>`,
	} {
		if err := differential(src); err != nil {
			t.Error(err)
		}
	}
	root := repoRoot(t)
	var files []string
	for _, d := range []string{"testdata", "internal/ooxml/tree/testdata"} {
		_ = filepath.WalkDir(filepath.Join(root, d), func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() && isPackage(p) {
				files = append(files, p)
			}
			return nil
		})
	}
	if len(files) == 0 {
		t.Fatal("no fixture packages found")
	}
	for _, p := range files {
		n, err := checkPackage(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
		} else if n == 0 {
			t.Errorf("%s: no XML part checked", p)
		}
	}
}

// TestDifferentialCorpus runs the differential over every XML part (up to
// 64 MiB) of the packages under XLSX_CORPUS, PPTX_CORPUS and DOCX_CORPUS
// that tree accepts.
func TestDifferentialCorpus(t *testing.T) {
	var paths, roots []string
	for _, env := range []string{"XLSX_CORPUS", "PPTX_CORPUS", "DOCX_CORPUS"} {
		dir := os.Getenv(env)
		if dir == "" {
			continue
		}
		roots = append(roots, dir)
		_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() && isPackage(p) {
				paths = append(paths, p)
			}
			return nil
		})
	}
	if len(paths) == 0 {
		t.Skip("set XLSX_CORPUS, PPTX_CORPUS or DOCX_CORPUS")
	}
	res := corpustest.Run(t, corpustest.Files(paths, roots...), func(t *testing.T, f corpustest.File, n *int) {
		var err error
		if *n, err = checkPackage(f.Path); err != nil {
			t.Error(err)
		}
	})
	parts := 0
	for _, r := range res {
		parts += r.Value
	}
	t.Logf("%d packages, %d XML parts compared with tree", len(res), parts)
}

func isPackage(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".xlsx", ".xlsm", ".pptx", ".pptm", ".docx", ".docm":
		return true
	}
	return false
}

// checkPackage runs the differential on each XML part of a zip that tree
// accepts and returns how many it compared. A package that is not a zip
// counts nothing.
func checkPackage(path string) (int, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return 0, nil //nolint:nilerr // a damaged corpus file is not this package's concern
	}
	defer zr.Close() //nolint:errcheck // read-only
	n := 0
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") || f.UncompressedSize64 > 64<<20 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		_ = rc.Close()
		if err != nil || tree.Check(data) != nil {
			continue
		}
		if err := differential(string(data)); err != nil {
			return n, fmt.Errorf("%s: %w", f.Name, err)
		}
		n++
	}
	return n, nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// FuzzNext: on bytes tree accepts, xmlscan reports exactly tree's nodes;
// on any bytes, nothing panics and every token makes progress.
func FuzzNext(f *testing.F) {
	for _, s := range []string{
		`<a/>`, `<a b="1" c='2'>t</a>`, `<?xml version="1.0"?><r><![CDATA[x]]><!--c--></r>`,
		`<!DOCTYPE r [<!ENTITY a "b">]><r/>`, `<a b="1"c="2"/>`, `<a b="<"/>`, `</a>`, `<a`,
		`<x:a xmlns:x="u" x:b="&amp;"><x:c/></x:a >`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if tree.Check([]byte(s)) == nil {
			if err := differential(s); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < len(s); {
			tok, err := xmlscan.Next(s, i)
			if err != nil {
				i++
				continue
			}
			if tok.Start != i || tok.End <= i || tok.End > len(s) {
				t.Fatalf("Next(%q, %d) = %+v", s, i, tok)
			}
			_ = tok.Local(s)
			for a := range xmlscan.Attrs(s, tok) {
				_ = a.Value(s)
				_, _ = xmlscan.FindAttr(s, tok, a.Name(s))
			}
			_, _, _ = xmlscan.ElementEnd(s, tok)
			_, _ = xmlscan.EditTag(s, tok, xmlscan.AttrEdit{Name: "r", Value: "A1"}, xmlscan.AttrEdit{Name: "s", Remove: true})
			i = tok.End
		}
	})
}
