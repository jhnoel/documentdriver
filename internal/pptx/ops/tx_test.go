package ops

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

const (
	nsA    = "http://schemas.openxmlformats.org/drawingml/2006/main"
	slide1 = "/ppt/slides/slide1.xml"
)

// prepare returns a fixture deck aged into the past, with its bytes and
// mtime, so that any write shows.
func prepare(t *testing.T) (string, []byte, time.Time) {
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
	return path, b, old
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
	info, _ := os.Stat(path)
	if !info.ModTime().Equal(mtime) {
		t.Fatal("file mtime changed")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory holds %v; temporary file left behind", names)
	}
}

// funcOp is a test-only operation with raw access to the working copy.
// Production operations never touch the package directly; these exist to
// exercise the transaction's own guarantees. It declares nothing, so any
// change it makes that survives to commit is rejected by the verifier.
type funcOp func(p *opc.Package) error

func (funcOp) Name() string { return "test.func" }

func (f funcOp) apply(t *Tx) error { return f(t.pkg) }

// declaredOp is a funcOp that declares one part as a whole.
type declaredOp struct {
	part string
	fn   funcOp
}

func (declaredOp) Name() string { return "test.declared" }

func (d declaredOp) apply(t *Tx) error {
	t.ws.DeclarePart(d.part)
	return d.fn(t.pkg)
}

// moveFirstShape is a plan step: set x on the first a:off (the shape
// tree's own transform).
var moveFirstShape = &DebugSetAttr{Part: slide1, Selector: "//a:off[1]", Attr: "x", Value: "42"}

func TestFailedPlanWritesNothing(t *testing.T) {
	errPlan := errors.New("step 3 failed")
	cases := []struct {
		name string
		plan funcOp
	}{
		{"error mid-plan", func(p *opc.Package) error {
			if _, err := p.AddPart("/ppt/media/x.bin", []byte{1, 2, 3}, ""); err != nil {
				return err
			}
			return errPlan
		}},
		{"malformed fragment", func(p *opc.Package) error {
			pt, _ := p.Part(slide1)
			doc, _ := pt.Tree()
			spPr := doc.Root().Find(nsA, "xfrm").Parent()
			_, err := spPr.InsertFragment(0, []byte(`<a:ln><a:noFill></a:ln>`), nil)
			return err
		}},
		{"undeclared prefix in fragment", func(p *opc.Package) error {
			pt, _ := p.Part(slide1)
			doc, _ := pt.Tree()
			_, err := doc.Root().InsertFragment(0, []byte(`<zz:x/>`), nil)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, before, mtime := prepare(t)
			if err := Apply(path, &DebugSetAttr{Part: slide1, Selector: "//a:off[1]", Attr: "x", Value: "42"}, c.plan); err == nil {
				t.Fatal("Apply succeeded")
			}
			assertUntouched(t, path, before, mtime)
		})
	}
}

func TestMalformedPartFailsCommit(t *testing.T) {
	path, before, mtime := prepare(t)
	err := Apply(path, moveFirstShape, declaredOp{"/ppt/presentation.xml", func(p *opc.Package) error {
		// Raw replacement bypasses the tree, so only the commit check can
		// catch it.
		pt, _ := p.Part("/ppt/presentation.xml")
		pt.SetBytes([]byte(`<p:presentation xmlns:p="urn:p"><p:sldIdLst></p:presentation>`))
		return nil
	}})
	if err == nil || !strings.Contains(err.Error(), "not well formed") {
		t.Fatalf("err = %v", err)
	}
	assertUntouched(t, path, before, mtime)
}

func TestCommit(t *testing.T) {
	path, before, _ := prepare(t)
	if err := Apply(path, moveFirstShape); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if bytes.Equal(before, after) {
		t.Fatal("nothing written")
	}
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	pt, _ := p.Part(slide1)
	b, _ := pt.Bytes()
	if !bytes.Contains(b, []byte(`<p:grpSpPr><a:xfrm><a:off x="42" y="0"/>`)) {
		t.Fatal("edit not saved")
	}
}

func TestNoOpPlanWritesNothing(t *testing.T) {
	path, before, mtime := prepare(t)
	err := Apply(path, funcOp(func(p *opc.Package) error {
		pt, _ := p.Part(slide1)
		doc, err := pt.Tree()
		if err != nil {
			return err
		}
		off := doc.Root().Find(nsA, "off")
		x, _ := off.Attr("x")
		return off.SetAttr("x", x)
	}))
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, path, before, mtime)
}

func TestFailedOpPoisonsCommit(t *testing.T) {
	path, before, mtime := prepare(t)
	tx, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Run(moveFirstShape); err != nil {
		t.Fatal(err)
	}
	if err := tx.Run(funcOp(func(*opc.Package) error { return errors.New("boom") })); err == nil {
		t.Fatal("op succeeded")
	}
	if err := tx.Commit(); !errors.Is(err, ErrTxFailed) {
		t.Fatalf("Commit err = %v", err)
	}
	assertUntouched(t, path, before, mtime)
}

func TestTxDone(t *testing.T) {
	path, _, _ := prepare(t)
	tx, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); !errors.Is(err, ErrTxDone) {
		t.Fatalf("second Commit err = %v", err)
	}
	tx.Rollback()
}
