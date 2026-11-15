package ops

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
)

// TestBodyProperties: insets, anchor and wrap are written to the shape's
// own a:bodyPr, only those that differ from what is shown, keeping the
// rest of the element; written back as shown they change nothing.
func TestBodyProperties(t *testing.T) {
	path, _, _ := diagram(t)
	run := func(op string) error {
		return Apply(path, compile(t, `{"ops": [`+op+`]}`)...)
	}
	if err := run(`{"op": "object.set", "at": "slide1/s10", "text": {"insets": [7.2, 3.6, 7.2, 10], "wrap": "none"}}`); err != nil {
		t.Fatal(err)
	}
	// lIns, tIns and rIns are the defaults shown (origin spec): only bIns
	// and wrap are written.
	if s := slideXML(t, path); !strings.Contains(s, `<a:bodyPr wrap="none" bIns="127000"/>`) && !strings.Contains(s, `<a:bodyPr bIns="127000" wrap="none"/>`) {
		t.Fatalf("bodyPr:\n%s", s)
	}
	if err := run(`{"op": "object.set", "at": "slide1/s10", "text": {"anchor": "ctr"}}`); err != nil {
		t.Fatal(err)
	}
	s := slideXML(t, path)
	i := strings.Index(s, `name="API"`)
	body := s[i:]
	body = body[strings.Index(body, "<a:bodyPr"):]
	body = body[:strings.Index(body, "/>")+2]
	if !strings.HasPrefix(body, `<a:bodyPr `) || !strings.Contains(body, `anchor="ctr"`) || !strings.Contains(body, `bIns="127000"`) || !strings.Contains(body, `wrap="none"`) {
		t.Fatalf("bodyPr after anchor: %s", body)
	}
	// Written back as shown: no change.
	before, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	if err := run(`{"op": "object.set", "at": "slide1/s10", "text": {"anchor": "ctr", "wrap": "none", "insets": [7.2, 3.6, 7.2, 10]}}`); err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, path, before, st.ModTime())
}

// TestBodyPropertiesOnAdd: a new shape with html takes body properties.
func TestBodyPropertiesOnAdd(t *testing.T) {
	path, _, _ := diagram(t)
	if err := Apply(path, compile(t, `{"ops": [
	  {"op": "object.add", "at": "slide1", "kind": "shape", "name": "T", "geom": "rect", "box": [10, 10, 100, 50],
	   "html": "Hi", "text": {"anchor": "b", "insets": [0, 0, 0, 0]}}]}`)...); err != nil {
		t.Fatal(err)
	}
	if s := slideXML(t, path); !strings.Contains(s, `anchor="b"`) || !strings.Contains(s, `lIns="0" tIns="0" rIns="0" bIns="0"`) {
		t.Fatalf("new shape:\n%s", s[strings.LastIndex(s, `name="T"`):])
	}
}

func TestBodyPropertiesRejects(t *testing.T) {
	for _, op := range []struct{ op, want string }{
		{`{"op": "object.add", "at": "slide1", "kind": "shape", "name": "x", "geom": "rect", "box": [1, 2, 3, 4], "text": {"anchor": "b"}}`, "html"},
		{`{"op": "object.add", "at": "slide1", "kind": "connector", "name": "x", "geom": "line", "from": [0, 0], "to": [1, 1], "text": {"anchor": "b"}}`, "connectors hold no text"},
		{`{"op": "object.set", "at": "slide1/s10", "text": {"anchor": "middle"}}`, "a:ST_TextAnchoringType"},
		{`{"op": "object.set", "at": "slide1/s10", "text": {"wrap": "auto"}}`, "a:ST_TextWrappingType"},
		{`{"op": "object.set", "at": "slide1/s10", "text": {"insets": [0, 0, 0, 200000000]}}`, "a:ST_Coordinate32"},
	} {
		p, err := DecodePlan(strings.NewReader(`{"ops": [` + op.op + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Compile(); err == nil || !strings.Contains(err.Error(), op.want) {
			t.Errorf("%s: %v, want %q", op.op, err, op.want)
		}
	}
	// A shape without a text body (s11) and a connector (s12) refuse at
	// run time, writing nothing.
	for _, c := range []struct {
		op   string
		want error
	}{
		{`{"op": "object.set", "at": "slide1/s11", "text": {"anchor": "b"}}`, prop.ErrNoTextBody},
		{`{"op": "object.set", "at": "slide1/s12", "text": {"anchor": "b"}}`, prop.ErrNoTextBody},
	} {
		path, before, mtime := diagram(t)
		if err := Apply(path, compile(t, `{"ops": [`+c.op+`]}`)...); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.op, err)
		}
		assertUntouched(t, path, before, mtime)
	}
}
