package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/plan"
)

const w14 = "http://schemas.microsoft.com/office/word/2010/wordml"

func TestRepairAndRootNamespace(t *testing.T) {
	cases := []struct {
		name  string
		after func(w *Workspace, t *testing.T) // undeclared extra change
		ok    bool
	}{
		{"declared only", nil, true},
		{"another attribute of the repaired paragraph", func(w *Workspace, t *testing.T) {
			doc, _ := w.Tree(docPart)
			p := paras(doc.Root().Elements()[0])[0]
			if err := p.SetAttr("w:rsidR", "00AB"); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"repaired attribute removed", func(w *Workspace, _ *testing.T) {
			doc, _ := w.Tree(docPart)
			p := paras(doc.Root().Elements()[0])[0]
			p.RemoveAttr("w14:paraId")
		}, false},
		{"another root declaration", func(w *Workspace, t *testing.T) {
			doc, _ := w.Tree(docPart)
			if err := doc.Root().SetAttr("xmlns:w15", "x"); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"ignorable list reordered", func(w *Workspace, t *testing.T) {
			doc, _ := w.Tree(docPart)
			if err := doc.Root().SetAttr("mc:Ignorable", "w15 w14"); err != nil {
				t.Fatal(err)
			}
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, body := open(t)
			p := paras(body)[0]
			if err := w.EnsureRootNamespace(docPart, "w14", w14, true); err != nil {
				t.Fatal(err)
			}
			if err := w.Repair(docPart, p, "w14:paraId", "1234ABCD"); err != nil {
				t.Fatal(err)
			}
			doc, _ := w.Tree(docPart)
			root := string(doc.Root().StartTag())
			for _, want := range []string{`xmlns:w14="` + w14 + `"`, `xmlns:mc="`, `mc:Ignorable="w14"`} {
				if !strings.Contains(root, want) {
					t.Fatalf("root %s lacks %s", root, want)
				}
			}
			if c.after != nil {
				c.after(w, t)
			}
			err := w.Verify()
			if c.ok != (err == nil) {
				t.Fatalf("Verify = %v", err)
			}
			if !c.ok && !errors.Is(err, plan.ErrUndeclared) {
				t.Fatalf("Verify = %v", err)
			}
		})
	}
}

func TestRepairUndeclaredFails(t *testing.T) {
	w, body := open(t)
	if err := paras(body)[0].SetAttr("w:rsidR", "1"); err != nil {
		t.Fatal(err)
	}
	if err := w.EnsureRootNamespace(docPart, "w14", w14, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Verify(); !errors.Is(err, plan.ErrUndeclared) {
		t.Fatalf("Verify = %v", err)
	}
}

func TestEnsureRootNamespaceRules(t *testing.T) {
	w, _ := open(t)
	// The root's own namespace is never ignorable.
	if err := w.EnsureRootNamespace(docPart, "w", wURI, true); err != nil {
		t.Fatal(err)
	}
	doc, _ := w.Tree(docPart)
	if doc.Modified() {
		t.Fatalf("declaring the root's own namespace changed %s", doc.Root().StartTag())
	}
	if err := w.EnsureRootNamespace(docPart, "w", "other", false); err == nil {
		t.Fatal("rebinding w accepted")
	}
	// Idempotent.
	for range 2 {
		if err := w.EnsureRootNamespace(docPart, "w14", w14, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := string(doc.Root().StartTag()); strings.Count(got, "w14") != 2 {
		t.Fatalf("root %s", got)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}
