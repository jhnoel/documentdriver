package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

const slide1 = "/ppt/slides/slide1.xml"

// toyUnits treats the shape tree (spTree) and its element children as
// units; it stands in for a format's units, independent of any format
// package.
type toyUnits struct{}

func (toyUnits) HasUnits(doc *tree.Document) bool {
	found := false
	doc.Node().Walk(func(n *tree.Node) bool {
		if n.Kind() == tree.ElementNode && n.Local() == "spTree" {
			found = true
		}
		return !found
	})
	return found
}

func (toyUnits) IsUnit(e *tree.Node) bool {
	if e.Kind() != tree.ElementNode {
		return false
	}
	if e.Local() == "spTree" {
		return true
	}
	p := e.Parent()
	return p != nil && p.Kind() == tree.ElementNode && p.Local() == "spTree"
}

func (toyUnits) Describe(e *tree.Node) string { return e.Name() }

// toyWorkspace holds the package and the write set, as a format's edit
// layer would.
type toyWorkspace struct {
	pkg *opc.Package
	ws  *WriteSet
}

func (w *toyWorkspace) Verify() error { return Verify(w.pkg, w.ws, toyUnits{}) }

// begin opens a fixture deck aged into the past, so that any write
// shows, and returns the transaction, its workspace, and the deck's path,
// bytes and mtime.
func begin(t *testing.T, opts ...Option) (*Tx, *toyWorkspace, string, []byte, time.Time) {
	t.Helper()
	path := testdeck.Minimal(t)
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var w *toyWorkspace
	tx, err := Begin(path, func(pkg *opc.Package) Workspace {
		w = &toyWorkspace{pkg: pkg, ws: NewWriteSet()}
		return w
	}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tx.Rollback)
	return tx, w, path, b, old
}

func assertUntouched(t *testing.T, path string, want []byte, mtime time.Time) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("file bytes changed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(mtime) {
		t.Fatal("file mtime changed")
	}
}

// shapes returns the slide's shape tree children (the toy units).
func shapes(t *testing.T, w *toyWorkspace) []*tree.Node {
	t.Helper()
	pt, err := w.pkg.Part(slide1)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := pt.Tree()
	if err != nil {
		t.Fatal(err)
	}
	var st *tree.Node
	doc.Node().Walk(func(n *tree.Node) bool {
		if n.Kind() == tree.ElementNode && n.Local() == "spTree" {
			st = n
		}
		return st == nil
	})
	if st == nil {
		t.Fatal("no spTree")
	}
	var out []*tree.Node
	for _, e := range st.Elements() {
		if e.Local() == "sp" {
			out = append(out, e)
		}
	}
	return out
}

func TestNoOpCommitWritesNothing(t *testing.T) {
	tx, _, path, b, mtime := begin(t)
	if err := tx.Run("noop", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, path, b, mtime)
	if err := tx.Commit(); !errors.Is(err, ErrTxDone) {
		t.Fatalf("second commit: %v", err)
	}
}

func TestFailedOperationPoisons(t *testing.T) {
	tx, w, path, b, mtime := begin(t)
	boom := errors.New("boom")
	if err := tx.Run("test.fail", func() error {
		w.ws.DeclareTarget(slide1, shapes(t, w)[0])
		if err := shapes(t, w)[0].SetAttr("x", "1"); err != nil {
			t.Fatal(err)
		}
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("run: %v", err)
	}
	if err := tx.Run("later", func() error { return nil }); !errors.Is(err, ErrTxFailed) {
		t.Fatalf("run after failure: %v", err)
	}
	if err := tx.Commit(); !errors.Is(err, ErrTxFailed) || !errors.Is(err, boom) {
		t.Fatalf("commit: %v", err)
	}
	assertUntouched(t, path, b, mtime)
}

func TestVerifierHoldsTheWriteSet(t *testing.T) {
	for _, c := range []struct {
		name    string
		declare func(w *toyWorkspace, sp []*tree.Node)
		ok      bool
	}{
		{"undeclared part", func(*toyWorkspace, []*tree.Node) {}, false},
		{"part declared, unit not targeted", func(w *toyWorkspace, _ []*tree.Node) { w.ws.DeclarePart(slide1) }, false},
		{"other unit targeted", func(w *toyWorkspace, sp []*tree.Node) { w.ws.DeclareTarget(slide1, sp[1]) }, false},
		{"unit declared only as a container", func(w *toyWorkspace, sp []*tree.Node) { w.ws.DeclareContainer(slide1, sp[0]) }, false},
		{"unit targeted", func(w *toyWorkspace, sp []*tree.Node) { w.ws.DeclareTarget("PPT/slides/slide1.xml", sp[0]) }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			tx, w, path, b, mtime := begin(t)
			if err := tx.Run("test.set", func() error {
				sp := shapes(t, w)
				c.declare(w, sp)
				return sp[0].SetAttr("x", "1")
			}); err != nil {
				t.Fatal(err)
			}
			err := tx.Commit()
			if c.ok {
				if err != nil {
					t.Fatal(err)
				}
				if got, _ := os.ReadFile(path); bytes.Equal(got, b) {
					t.Fatal("commit wrote nothing")
				}
				return
			}
			if !errors.Is(err, ErrUndeclared) {
				t.Fatalf("commit: %v, want ErrUndeclared", err)
			}
			assertUntouched(t, path, b, mtime)
		})
	}
}

func TestRemovedUnitMustBeTargeted(t *testing.T) {
	for _, target := range []bool{false, true} {
		tx, w, path, b, mtime := begin(t)
		if err := tx.Run("test.remove", func() error {
			sp := shapes(t, w)
			st := sp[0].Parent()
			w.ws.DeclareContainer(slide1, st)
			if target {
				w.ws.DeclareTarget(slide1, sp[0])
			}
			return st.RemoveChild(sp[0])
		}); err != nil {
			t.Fatal(err)
		}
		err := tx.Commit()
		if target && err != nil {
			t.Fatalf("targeted removal: %v", err)
		}
		if !target {
			if !errors.Is(err, ErrUndeclared) {
				t.Fatalf("untargeted removal: %v", err)
			}
			assertUntouched(t, path, b, mtime)
		}
	}
}

func TestFinish(t *testing.T) {
	build := func(snap string) (*Result, error) {
		if _, err := os.Stat(snap); err != nil {
			return nil, err
		}
		r := NewResult()
		return &r, nil
	}
	change := func(tx *Tx, w *toyWorkspace) {
		if err := tx.Run("test.set", func() error {
			sp := shapes(t, w)
			w.ws.DeclareTarget(slide1, sp[0])
			return sp[0].SetAttr("x", "1")
		}); err != nil {
			t.Fatal(err)
		}
	}

	tx, w, path, b, mtime := begin(t)
	change(tx, w)
	res, err := tx.Finish(Options{DryRun: true}, build)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Saved != "" {
		t.Errorf("dry run result: %+v", res)
	}
	tx.Rollback()
	assertUntouched(t, path, b, mtime)

	tx, w, path, b, mtime = begin(t)
	change(tx, w)
	out := filepath.Join(t.TempDir(), "out.pptx")
	res, err = tx.Finish(Options{Out: out}, build)
	if err != nil {
		t.Fatal(err)
	}
	if res.DryRun || res.Saved != out {
		t.Errorf("result: %+v", res)
	}
	assertUntouched(t, path, b, mtime)
	if got, err := os.ReadFile(out); err != nil || bytes.Equal(got, b) {
		t.Errorf("output not written: %v", err)
	}

	tx, w, path, b, _ = begin(t)
	change(tx, w)
	if res, err = tx.Finish(Options{}, build); err != nil {
		t.Fatal(err)
	}
	if res.Saved != path {
		t.Errorf("saved %q, want %q", res.Saved, path)
	}
	if got, _ := os.ReadFile(path); bytes.Equal(got, b) {
		t.Error("in-place commit wrote nothing")
	}

	// A failing build writes nothing.
	tx, w, path, b, mtime = begin(t)
	change(tx, w)
	if _, err := tx.Finish(Options{}, func(string) (*Result, error) { return nil, errors.New("no model") }); err == nil {
		t.Fatal("finish with a failing build succeeded")
	}
	assertUntouched(t, path, b, mtime)
}

// TestFinishNoOpWritesNothing: Finish of a transaction whose operations
// changed no part records no saved file and, when -o is given, does not
// create it, because a plan that changes nothing writes nothing.
func TestFinishNoOpWritesNothing(t *testing.T) {
	build := func(snap string) (*Result, error) {
		if _, err := os.Stat(snap); err != nil {
			return nil, err
		}
		r := NewResult()
		return &r, nil
	}
	tx, _, path, b, mtime := begin(t)
	out := filepath.Join(t.TempDir(), "out.pptx")
	res, err := tx.Finish(Options{Out: out}, build)
	if err != nil {
		t.Fatal(err)
	}
	if res.DryRun || res.Saved != "" {
		t.Errorf("no-op result: %+v", res)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("no-op Finish wrote %s", out)
	}
	assertUntouched(t, path, b, mtime)
}

// FinishSealed builds from the sealed bytes and writes exactly those
// bytes; once sealed, no operation runs; a failing build, a dry run and a
// file changed on disk write nothing.
func TestFinishSealed(t *testing.T) {
	var built []byte
	build := func(sealed []byte) (*Result, error) {
		built = bytes.Clone(sealed)
		r := NewResult()
		return &r, nil
	}
	change := func(tx *Tx, w *toyWorkspace) {
		if err := tx.Run("test.set", func() error {
			sp := shapes(t, w)
			w.ws.DeclareTarget(slide1, sp[0])
			return sp[0].SetAttr("x", "1")
		}); err != nil {
			t.Fatal(err)
		}
	}

	tx, w, path, b, mtime := begin(t)
	change(tx, w)
	res, err := tx.FinishSealed(Options{DryRun: true}, func(sealed []byte) (*Result, error) {
		if err := tx.Run("test.late", func() error { return nil }); !errors.Is(err, ErrTxSealed) {
			t.Errorf("Run after sealing = %v, want ErrTxSealed", err)
		}
		return build(sealed)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Saved != "" || bytes.Equal(built, b) {
		t.Errorf("dry run result: %+v", res)
	}
	tx.Rollback()
	assertUntouched(t, path, b, mtime)

	tx, w, path, b, mtime = begin(t)
	change(tx, w)
	out := filepath.Join(t.TempDir(), "out.pptx")
	if res, err = tx.FinishSealed(Options{Out: out}, build); err != nil {
		t.Fatal(err)
	}
	if res.DryRun || res.Saved != out {
		t.Errorf("result: %+v", res)
	}
	assertUntouched(t, path, b, mtime)
	if got, err := os.ReadFile(out); err != nil || !bytes.Equal(got, built) {
		t.Errorf("the output is not the sealed bytes: %v", err)
	}

	tx, w, path, _, _ = begin(t)
	change(tx, w)
	if res, err = tx.FinishSealed(Options{}, build); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); res.Saved != path || !bytes.Equal(got, built) {
		t.Errorf("in place: saved %q, the file is the sealed bytes: %v", res.Saved, bytes.Equal(got, built))
	}

	tx, w, path, b, mtime = begin(t)
	change(tx, w)
	if _, err := tx.FinishSealed(Options{}, func([]byte) (*Result, error) { return nil, errors.New("no model") }); err == nil {
		t.Fatal("finish with a failing build succeeded")
	}
	assertUntouched(t, path, b, mtime)

	// Someone else writes the file after it was opened.
	tx, w, path, _, _ = begin(t)
	change(tx, w)
	other := append(bytes.Clone(b), 0)
	if err := os.WriteFile(path, other, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.FinishSealed(Options{}, build); !errors.Is(err, opc.ErrChangedOnDisk) {
		t.Fatalf("finish over a changed file = %v, want ErrChangedOnDisk", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, other) {
		t.Error("the changed file was overwritten")
	}
}

func TestResultJSON(t *testing.T) {
	r := NewResult()
	got, err := r.JSON(Field{"slide", "slide1"}, Field{"units", "pt"})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n \"slide\": \"slide1\",\n \"units\": \"pt\",\n \"dry_run\": false,\n \"aliases\": {},\n \"changed\": [],\n \"deleted\": [],\n \"hazards\": [],\n \"parts\": []\n}\n"
	if string(got) != want {
		t.Errorf("empty result:\n%s\nwant\n%s", got, want)
	}
	r.DryRun = true
	r.Saved = "a<b>.pptx"
	r.Aliases["x"] = "s7"
	r.Changed = append(r.Changed, json.RawMessage(`{"id":"s7"}`), json.RawMessage(`{"id":"s8"}`))
	r.Hazards = MarkNew([]Hazard{{Kind: "k", ID: "s1"}}, []Hazard{{Kind: "k", ID: "s1"}, {Kind: "k", ID: "s2", Detail: "d"}})
	r.Parts = []string{"/p"}
	got, err = r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	want = "{\n \"dry_run\": true,\n \"saved\": \"a<b>.pptx\",\n \"aliases\": {\"x\":\"s7\"},\n \"changed\": [\n  {\"id\":\"s7\"},\n  {\"id\":\"s8\"}\n ],\n \"deleted\": [],\n" +
		" \"hazards\": [\n  {\"kind\":\"k\",\"id\":\"s1\",\"detail\":\"\",\"new\":false},\n  {\"kind\":\"k\",\"id\":\"s2\",\"detail\":\"d\",\"new\":true}\n ],\n \"parts\": [\"/p\"]\n}\n"
	if string(got) != want {
		t.Errorf("result:\n%s\nwant\n%s", got, want)
	}
}
