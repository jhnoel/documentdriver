package grade

import (
	"bytes"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
)

func parse(t *testing.T, s string) *tree.Node {
	t.Helper()
	d, err := tree.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d.Root()
}

func TestCanonicalIgnoresSerialisation(t *testing.T) {
	a := parse(t, `<p:sp xmlns:p="urn:p" xmlns:a="urn:a"><p:spPr><a:off x="1" y="2"/><a:t> x </a:t></p:spPr></p:sp>`)
	b := parse(t, "<sp xmlns='urn:p' xmlns:q='urn:a'>\n  <spPr>\n    <q:off y='2' x=\"1\"></q:off>\n    <q:t> x </q:t>\n  </spPr>\n</sp>")
	if !bytes.Equal(Canonical(a, false), Canonical(b, false)) {
		t.Errorf("canonical forms differ:\n%s\n%s", Canonical(a, false), Canonical(b, false))
	}
	c := parse(t, `<p:sp xmlns:p="urn:p" xmlns:a="urn:a"><p:spPr><a:off x="1" y="3"/><a:t> x </a:t></p:spPr></p:sp>`)
	if bytes.Equal(Canonical(a, false), Canonical(c, false)) {
		t.Error("attribute change not seen")
	}
	d := parse(t, `<p:sp xmlns:p="urn:p" xmlns:a="urn:a"><p:spPr><a:off x="1" y="2"/><a:t>x</a:t></p:spPr></p:sp>`)
	if bytes.Equal(Canonical(a, false), Canonical(d, false)) {
		t.Error("run text whitespace change not seen")
	}
}

func TestCanonicalStripsChildObjects(t *testing.T) {
	g1 := parse(t, `<p:grpSp xmlns:p="urn:p"><p:grpSpPr/><p:sp><p:x v="1"/></p:sp></p:grpSp>`)
	g2 := parse(t, `<p:grpSp xmlns:p="urn:p"><p:grpSpPr/><p:sp><p:x v="2"/></p:sp></p:grpSp>`)
	// Strip only applies to PresentationML object elements; urn:p is
	// not PML, so these differ.
	if bytes.Equal(Canonical(g1, true), Canonical(g2, true)) {
		t.Error("non-PML children were stripped")
	}
	pml := "http://schemas.openxmlformats.org/presentationml/2006/main"
	g3 := parse(t, `<p:grpSp xmlns:p="`+pml+`"><p:grpSpPr/><p:sp><p:x v="1"/></p:sp></p:grpSp>`)
	g4 := parse(t, `<p:grpSp xmlns:p="`+pml+`"><p:grpSpPr/><p:sp><p:x v="2"/></p:sp></p:grpSp>`)
	if !bytes.Equal(Canonical(g3, true), Canonical(g4, true)) {
		t.Error("group compared with its children")
	}
}

func TestLIS(t *testing.T) {
	keep := lis([]int{0, 1, 5, 2, 3, 4})
	want := []bool{true, true, false, true, true, true}
	for i := range want {
		if keep[i] != want[i] {
			t.Fatalf("lis = %v, want %v", keep, want)
		}
	}
}

func obj(id, name string, k model.Kind) *model.Object {
	return &model.Object{ID: id, Name: name, Kind: k}
}

func TestMatchRenumbered(t *testing.T) {
	in := &model.Slide{Objects: []*model.Object{obj("s2", "A", model.Shape), obj("s3", "B", model.Shape), obj("s4", "C", model.Connector)}}
	// A tool renumbered: A is now s3, B is s2; C kept its id.
	out := &model.Slide{Objects: []*model.Object{obj("s3", "A", model.Shape), obj("s2", "B", model.Shape), obj("s4", "C", model.Connector), obj("s9", "D", model.Shape)}}
	m := Match(in, out, nil, nil, nil)
	if m.InToOut["s2"].Name != "A" || m.InToOut["s3"].Name != "B" {
		t.Errorf("matched by id across different objects: %v %v", m.InToOut["s2"].Name, m.InToOut["s3"].Name)
	}
	if m.ByID != 1 || m.BySignature != 2 || len(m.New) != 1 || len(m.Deleted) != 0 {
		t.Errorf("by id %d, by signature %d, new %d, deleted %d", m.ByID, m.BySignature, len(m.New), len(m.Deleted))
	}
}

func TestGradeAnswer(t *testing.T) {
	sl := &model.Slide{Objects: []*model.Object{obj("s3", "Box", model.Shape), obj("s5", "Dup", model.Shape), obj("s6", "Dup", model.Shape), obj("s7", "Line", model.Connector)}}
	tk := &Task{Answer: &AnswerSpec{IDs: []string{"s3", "s7"}}}
	for ans, want := range map[string]bool{
		"3\n7":           true,
		"s7, slide2#3":   true,
		"slide2#s3;Line": true,
		"Box\n`s7`":      true,
		"3":              false,
		"3\n7\n5":        false,
		"Dup\n3\n7":      false, // ambiguous name
		"":               false,
	} {
		if got := gradeAnswer(tk, sl, ans).Pass; got != want {
			t.Errorf("answer %q: pass %v, want %v", ans, got, want)
		}
	}
}

func TestSideOf(t *testing.T) {
	o := &model.Object{Box: &derive.Xfrm{X: 0, Y: 0, W: 200, H: 100}}
	cases := map[derive.Pt]string{{X: 200, Y: 50}: "right", {X: 100, Y: 100}: "bottom", {X: 0, Y: 50}: "left", {X: 100, Y: 0}: "top"}
	for p, side := range cases {
		s := sideOf(o, derive.Site{Pt: p})
		if !s[side] || len(trueKeys(s)) != 1 {
			t.Errorf("%v: %v, want only %s", p, s, side)
		}
	}
	// An ellipse's diagonal site belongs to both neighbouring sides.
	s := sideOf(o, derive.Site{Pt: derive.Pt{X: 100 + 70.71, Y: 50 + 35.36}})
	if !s["right"] || !s["bottom"] {
		t.Errorf("diagonal: %v", s)
	}
}

func trueKeys(m map[string]bool) []string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	return out
}

func TestHexClose(t *testing.T) {
	if !hexClose("#4A7EBB", "4a7ebf") || hexClose("4A7EBB", "4A7ECB") || hexClose("zz", "zz") {
		t.Error("hexClose")
	}
}

func TestLoadTasks(t *testing.T) {
	ts, err := LoadTasks("../tasks")
	if err != nil {
		t.Fatal(err)
	}
	n := map[string]int{}
	for _, tk := range ts {
		n[tk.Type]++
	}
	if len(ts) < 30 {
		t.Errorf("%d tasks", len(ts))
	}
	for _, ty := range Types {
		if n[ty] == 0 {
			t.Errorf("no %s task", ty)
		}
	}
}
