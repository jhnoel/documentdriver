package ops

// The five write laws (identity, inverse, untargeted, atomicity under
// injected operation and save-stage failures, sequencing) run through
// the format-neutral harness internal/laws, from internal/pptx/laws. What
// stays here needs operations only this package can define: the
// verifier's rejection of an operation that changes what it did not
// declare.

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// diagram returns an aged copy of the diagram fixture with its bytes and
// mtime.
func diagram(t *testing.T) (string, []byte, time.Time) {
	t.Helper()
	path := testdeck.Diagram(t)
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

// sneakyOp declares one object as its target but also changes another,
// bypassing its Handle: the commit-time verifier must reject it.
type sneakyOp struct{ declared, touched string }

func (sneakyOp) Name() string { return "test.sneaky" }

func (s sneakyOp) apply(t *Tx) error {
	if err := (&Move{Slide: "slide1", Target: s.declared, Units: prop.Pt, DX: 1}).apply(t); err != nil {
		return err
	}
	pt, err := t.pkg.Part("/ppt/slides/slide1.xml")
	if err != nil {
		return err
	}
	doc, err := pt.Tree()
	if err != nil {
		return err
	}
	sel := fmt.Sprintf("//p:cNvPr[@id='%s']", s.touched)
	n, err := doc.Select(sel)
	if err != nil || len(n) != 1 {
		return fmt.Errorf("select %s: %v", sel, err)
	}
	return n[0].SetAttr("name", "sneaky")
}

func TestVerifierRejectsSneakyOp(t *testing.T) {
	for _, c := range []sneakyOp{{"s10", "11"}, {"s20", "21"}, {"s30", "71"}} {
		path, before, mtime := diagram(t)
		err := Apply(path, c)
		if !errors.Is(err, edit.ErrUndeclared) || !strings.Contains(err.Error(), "object s"+c.touched) {
			t.Fatalf("%+v: err = %v", c, err)
		}
		assertUntouched(t, path, before, mtime)
	}
}

// TestPlanIdentityThroughJSON: a plan written from the values a view
// shows (rounded to 0.01 pt) is a no-op.
func TestPlanIdentityThroughJSON(t *testing.T) {
	path, before, mtime := diagram(t)
	p, err := DecodePlan(strings.NewReader(`{"ops": [
		{"op": "object.set", "at": "slide1/s10", "box": [100, 100, 100, 50]},
		{"op": "object.set", "at": "slide1/s21", "box": [650, 300, 50, 50], "flip_h": true},
		{"op": "object.move", "at": "slide1/s11", "dx": 0, "dy": 0}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	o, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(path, o...); err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, path, before, mtime)
}
