package apply

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/view"
	"testing"

	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// insetRun applies ops to the minimal deck's slide1/s2 (a shape with a
// text body, anchor ctr) in a dry run and returns the result JSON.
func insetRun(t *testing.T, ops string) (string, *Result) {
	t.Helper()
	path := testdeck.MinimalWith(t, nil)
	res, err := Run(path, []byte(`{"ops": [`+ops+`]}`), Options{DryRun: true})
	if err != nil {
		t.Fatalf("%s: %v", ops, err)
	}
	j, err := res.JSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(j), res
}

const header = `{"op": "object.set", "at": "slide1/s2", "box": [0, 0, 720, 46]`

// TestInsetOverflowHazard: the owner's field case. [3, 24, 3, 230] read
// as CSS order puts bIns = 230 pt in a 46 pt box: apply reports
// inset-overflow (new) on the shape; sane insets and the same values in
// the intended named form report none.
func TestInsetOverflowHazard(t *testing.T) {
	for _, c := range []struct {
		name, text, want string
	}{
		{"owner case", `{"anchor": "ctr", "insets": [3, 24, 3, 230]}`, `"kind":"inset-overflow","id":"slide1/s2","detail":"top + bottom insets 24 + 230 pt >= height 46 pt: no room for text (insets are [left, top, right, bottom], not CSS order)","new":true`},
		{"width", `{"insets": [400, 0, 320, 0]}`, `"kind":"inset-overflow","id":"slide1/s2","detail":"left + right insets 400 + 320 pt >= width 720 pt: no room`},
		{"exactly full height", `{"insets": [0, 23, 0, 23]}`, `"kind":"inset-overflow"`},
		{"intended named form", `{"anchor": "ctr", "insets": {"top": 3, "right": 24, "bottom": 3, "left": 230}}`, ""},
		{"sane", `{"insets": [7.2, 3.6, 7.2, 3.6]}`, ""},
	} {
		j, _ := insetRun(t, header+`, "text": `+c.text+`}`)
		has := strings.Contains(j, `"inset-overflow"`)
		if c.want == "" && has {
			t.Errorf("%s: unexpected hazard in %s", c.name, j)
		}
		if c.want != "" && !strings.Contains(j, c.want) {
			t.Errorf("%s: want %s in %s", c.name, c.want, j)
		}
	}
}

// TestInsetsNamedForm: the named form writes the sides it gives and
// keeps the rest (subset semantics); the full named form equals the
// array; the result shows the array either way, so feeding it back is a
// no-op.
func TestInsetsNamedForm(t *testing.T) {
	base := `{"op": "object.set", "at": "slide1/s2", "text": {"insets": [1, 2, 3, 4]}}`
	for _, c := range []struct{ name, ops, want string }{
		{"array", base, `"insets":[1,2,3,4]`},
		{"full named", `{"op": "object.set", "at": "slide1/s2", "text": {"insets": {"left": 1, "top": 2, "right": 3, "bottom": 4}}}`, `"insets":[1,2,3,4]`},
		{"subset bottom", base + `, {"op": "object.set", "at": "slide1/s2", "text": {"insets": {"bottom": 9}}}`, `"insets":[1,2,3,9]`},
		{"subset left right", base + `, {"op": "object.set", "at": "slide1/s2", "text": {"insets": {"right": 0, "left": 5}}}`, `"insets":[5,2,0,4]`},
	} {
		out := t.TempDir() + "/o.pptx"
		if _, err := Run(testdeck.MinimalWith(t, nil), []byte(`{"ops": [`+c.ops+`]}`), Options{Out: out}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := shownInsets(t, out); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
	// Round trip: the shown array written back changes nothing.
	path := testdeck.MinimalWith(t, nil)
	out := t.TempDir() + "/a.pptx"
	if _, err := Run(path, []byte(`{"ops": [`+`{"op": "object.set", "at": "slide1/s2", "text": {"insets": {"left": 1, "top": 2, "right": 3, "bottom": 4}}}`+`]}`), Options{Out: out}); err != nil {
		t.Fatal(err)
	}
	res, err := Run(out, []byte(`{"ops": [{"op": "object.set", "at": "slide1/s2", "text": {"insets": [1, 2, 3, 4]}}]}`), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changed) != 0 {
		t.Errorf("round trip changed %s", res.Changed)
	}
}

// TestInsetsNamedFormRefusals: unknown sides, an empty object and a
// wrong kind are invalid_plan, with the order in the message.
func TestInsetsNamedFormRefusals(t *testing.T) {
	path := testdeck.MinimalWith(t, nil)
	for _, c := range []struct{ text, want string }{
		{`{"insets": {"bottom": 1, "start": 2}}`, `unknown field "start"`},
		{`{"insets": {}}`, "at least one of left, top, right, bottom"},
		{`{"insets": 3}`, "not CSS order"},
		{`{"insets": [1, 2, 3]}`, ""},
	} {
		_, err := Run(path, []byte(`{"ops": [{"op": "object.set", "at": "slide1/s2", "text": `+c.text+`}]}`), Options{DryRun: true})
		if err == nil || !strings.Contains(err.Error(), "invalid_plan") && !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v", c.text, err)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q in %v", c.text, c.want, err)
		}
	}
}

// shownInsets returns the "insets" member slide1/s2 shows in --detail
// full JSON.
func shownInsets(t *testing.T, path string) string {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		t.Fatal(err)
	}
	part, err := d.ResolveSlide("slide1")
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.ReadSlide(part)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range s.Objects {
		if o.ID != "s2" {
			continue
		}
		b, err := view.ObjectJSON(o, view.Options{Slide: "slide1", Detail: view.Full, Units: "pt"})
		if err != nil {
			t.Fatal(err)
		}
		i := strings.Index(string(b), `"insets":`)
		if i < 0 {
			t.Fatalf("no insets in %s", b)
		}
		return string(b[i : i+strings.Index(string(b[i:]), "]")+1])
	}
	t.Fatal("no s2")
	return ""
}

// TestInsetOverflowPreexisting: a later plan that changes the shape again
// reports the hazard with new false; one that leaves it alone does not
// report it.
func TestInsetOverflowPreexisting(t *testing.T) {
	out := t.TempDir() + "/o.pptx"
	if _, err := Run(testdeck.MinimalWith(t, nil), []byte(`{"ops": [`+header+`, "text": {"insets": [3, 24, 3, 230]}}]}`), Options{Out: out}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ op, want string }{
		{`{"op": "object.set", "at": "slide1/s2", "name": "Header"}`, `"kind":"inset-overflow","id":"slide1/s2","detail":"top + bottom insets 24 + 230 pt >= height 46 pt: no room for text (insets are [left, top, right, bottom], not CSS order)","new":false`},
		{`{"op": "object.set", "at": "slide1/s3", "name": "Other"}`, ""},
	} {
		res, err := Run(out, []byte(`{"ops": [`+c.op+`]}`), Options{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		j, _ := res.JSON()
		if c.want == "" && strings.Contains(string(j), "inset-overflow") || !strings.Contains(string(j), c.want) {
			t.Errorf("%s: want %q in %s", c.op, c.want, j)
		}
	}
}
