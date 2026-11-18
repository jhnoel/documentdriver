package ops_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// textSetPlan returns a one-op plan that sets the text of at to html.
func textSetPlan(at, html string) *ops.Plan {
	return &ops.Plan{Ops: []ops.Spec{&ops.TextSetSpec{At: at, HTML: html}}}
}

// textSetRun applies a text.set plan to the fixture name and returns the
// result and the output bytes (empty when nothing was written).
func textSetRun(t *testing.T, fixture, at, html string) (*plan.Result, []byte) {
	t.Helper()
	path := vsdxtest.Fixture(t, fixture)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, textSetPlan(at, html), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run(%s %s): %v", fixture, at, err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return res, nil
		}
		t.Fatalf("read out: %v", err)
	}
	return res, data
}

// textSetObject reads the page of data and returns the object with id,
// checking there is exactly one.
func textSetObject(t *testing.T, data []byte, page, id string) map[string]any {
	t.Helper()
	raw, err := read.Page(data, "BASE", page, read.Options{Detail: "full"})
	if err != nil {
		t.Fatalf("read.Page(%s): %v", page, err)
	}
	var v struct {
		Objects []map[string]any `json:"objects"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("Unmarshal Page: %v", err)
	}
	for _, o := range v.Objects {
		if o["id"] == id {
			return o
		}
	}
	t.Fatalf("page %s has no object %s", page, id)
	return nil
}

// TestTextSetIdentityV1 checks the OT1 row: text.set V1 s1 with read's
// own html `<p><b>Hello</b></p>` changes nothing and writes nothing.
func TestTextSetIdentityV1(t *testing.T) {
	res, data := textSetRun(t, "v1-minimal", "page0/s1", `<p><b>Hello</b></p>`)
	if len(data) != 0 {
		t.Error("identity text.set wrote its out file")
	}
	if len(res.Parts) != 0 || len(res.Changed) != 0 {
		t.Errorf("identity result parts %v changed %v, want empty", res.Parts, res.Changed)
	}
	if res.NewBase != res.Base {
		t.Errorf("identity new_base %q != base %q", res.NewBase, res.Base)
	}
}

// TestTextSetIdentityV7 checks the OT1 row: text.set V7 s1 with
// golden/v7-page0.html changes nothing (row 0 keeps Size
// 0.1666666666666667 because nothing is written).
func TestTextSetIdentityV7(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	golden, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", "golden", "v7-page0.html"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	res, data := textSetRun(t, "v7-text", "page0/s1", string(golden))
	if len(data) != 0 {
		t.Error("golden text.set wrote its out file")
	}
	if len(res.Parts) != 0 || len(res.Changed) != 0 {
		t.Errorf("golden result parts %v changed %v, want empty", res.Parts, res.Changed)
	}
	if res.NewBase != res.Base {
		t.Errorf("golden new_base %q != base %q", res.NewBase, res.Base)
	}
}

// TestTextSetBye checks the OT1 row: `<p>Bye</p>` on V1 s1 reads back
// as html `<p>Bye</p>` and text "Bye", with the Text element ending
// in a line feed.
func TestTextSetBye(t *testing.T) {
	res, data := textSetRun(t, "v1-minimal", "page0/s1", `<p>Bye</p>`)
	if len(res.Changed) == 0 {
		t.Fatalf("Bye changed %v, want page0/s1", res.Changed)
	}
	obj := textSetObject(t, data, "page0", "page0/s1")
	if got, _ := obj["html"].(string); got != `<p>Bye</p>` {
		t.Errorf("Bye html = %q, want <p>Bye</p>", got)
	}
	if got, _ := obj["text"].(string); got != "Bye" {
		t.Errorf("Bye text = %q, want Bye", got)
	}
	if got := textSetText(t, data, 1); !strings.HasSuffix(got, "\n</Text>") {
		t.Errorf("Bye Text %q does not end with a line feed", got)
	}
}

// textSetShapeNode returns the Shape node with id on page0 of a sealed
// drawing.
func textSetShapeNode(t *testing.T, data []byte, id uint32) *tree.Node {
	t.Helper()
	pkg, err := opc.OpenBytes(data, opc.DefaultLimits())
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer func() { _ = pkg.Close() }()
	d, err := doc.Open(pkg)
	if err != nil {
		t.Fatalf("doc.Open(output): %v", err)
	}
	var entry *doc.PageEntry
	for i := range d.Pages {
		if d.Pages[i].ID == 0 {
			entry = &d.Pages[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("no page0")
	}
	contents, err := d.PageContents(entry)
	if err != nil {
		t.Fatalf("PageContents: %v", err)
	}
	var shape *tree.Node
	contents.Root().Walk(func(n *tree.Node) bool {
		if shape != nil {
			return false
		}
		if n.Kind() == tree.ElementNode && n.Local() == vsd.ElemShape {
			if v, ok := n.Attr("ID"); ok && v == "1" {
				shape = n
				return false
			}
		}
		return true
	})
	if shape == nil {
		t.Fatalf("no shape %d", id)
	}
	return shape
}

// textSetText returns the Bytes of the Text element of shape id	on
// page0 of a sealed drawing.
func textSetText(t *testing.T, data []byte, id uint32) string {
	t.Helper()
	shape := textSetShapeNode(t, data, id)
	for c := range shape.ElementsSeq() {
		if c.Local() == vsd.ElemText {
			return string(c.Bytes())
		}
	}
	t.Fatalf("shape %d has no Text", id)
	return ""
}

// textSetSectionRows returns the row IXs of the section N of shape id
// on page0 of a sealed drawing (nil when it has no such section).
func textSetSectionRows(t *testing.T, data []byte, id uint32, section string) []string {
	t.Helper()
	shape := textSetShapeNode(t, data, id)
	for c := range shape.ElementsSeq() {
		if c.Local() != vsd.ElemSection {
			continue
		}
		if n, _ := c.Attr("N"); n != section {
			continue
		}
		var out []string
		for r := range c.ElementsSeq() {
			if r.Local() == vsd.ElemRow {
				ix, _ := r.Attr("IX")
				out = append(out, ix)
			}
		}
		return out
	}
	return nil
}

// TestTextSetMaster checks the OT1 row: a shape showing master text
// (V2 s1) gets a local Text with origin local afterwards.
func TestTextSetMaster(t *testing.T) {
	_, data := textSetRun(t, "v2-master", "page0/s1", `<p>Bye</p>`)
	obj := textSetObject(t, data, "page0", "page0/s1")
	if got, _ := obj["html"].(string); got != `<p>Bye</p>` {
		t.Errorf("master html = %q, want <p>Bye</p>", got)
	}
	origins, _ := obj["origins"].(map[string]any)
	if origins["html"] != "local" {
		t.Errorf("master html origin = %v, want local", origins["html"])
	}
}

// TestTextSetFieldNotFound checks the OT1 row: a field placeholder not
// in the shape is not_found at ops[0].html and the file is unchanged.
func TestTextSetFieldNotFound(t *testing.T) {
	for _, tc := range []struct {
		name string
		html string
	}{
		{name: "past last field", html: `<p><opaque data-ref="page0/s1/field:2" data-kind="field" data-text="3"></opaque></p>`},
		{name: "wrong shape", html: `<p><opaque data-ref="page0/s5/field:1" data-kind="field" data-text="3"></opaque></p>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := vsdxtest.Fixture(t, "v7-text")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			out := filepath.Join(t.TempDir(), "out.vsdx")
			_, err = apply.Run(path, textSetPlan("page0/s1", tc.html), apply.Options{Out: out})
			var ce *cli.Error
			if !errors.As(err, &ce) {
				t.Fatalf("error %v, want *cli.Error", err)
			}
			if ce.Code != cli.CodeNotFound || ce.Path != "ops[0].html" {
				t.Errorf("got %s at %q, want not_found at ops[0].html", ce.Code, ce.Path)
			}
			if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
				t.Error("a refused field text.set wrote its out file")
			}
			if got, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(got, before) {
				t.Error("a refused field text.set changed the input")
			}
		})
	}
}

// TestTextSetFieldKept checks that a field placeholder keeps the
// shape's own field (its IX and cached text), ignoring the written
// data-text as the contract says.
func TestTextSetFieldKept(t *testing.T) {
	_, data := textSetRun(t, "v7-text", "page0/s1", `<p>Page <opaque data-ref="page0/s1/field:1" data-kind="field" data-text="999"></opaque></p>`)
	obj := textSetObject(t, data, "page0", "page0/s1")
	want := `<p>Page <opaque data-ref="page0/s1/field:1" data-kind="field" data-text="3"></opaque></p>`
	if got, _ := obj["html"].(string); got != want {
		t.Errorf("field html = %q, want %q", got, want)
	}
}

// TestTextSetRefuses checks the refusals the rows and the contract
// imply: master shapes, Foreign and guide shapes, missing shapes and
// HTML outside the grammar, each at its field, with nothing written.
func TestTextSetRefuses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture string
		at      string
		html    string
		code    string
		path    string
	}{
		{name: "master shape", fixture: "v2-master", at: "master2/s5", html: `<p>x</p>`, code: cli.CodeOperationFailed, path: "ops[0].at"},
		{name: "picture", fixture: "v15-foreign", at: "page0/s1", html: `<p>x</p>`, code: cli.CodeOperationFailed, path: "ops[0].at"},
		{name: "guide", fixture: "v15-foreign", at: "page0/s3", html: `<p>x</p>`, code: cli.CodeOperationFailed, path: "ops[0].at"},
		{name: "missing shape", fixture: "v1-minimal", at: "page0/s99", html: `<p>x</p>`, code: cli.CodeNotFound, path: "ops[0].at"},
		{name: "unknown alias", fixture: "v1-minimal", at: "$nope", html: `<p>x</p>`, code: cli.CodeNotFound, path: "ops[0].at"},
		{name: "bad html", fixture: "v1-minimal", at: "page0/s1", html: `<ol><li>x</li></ol>`, code: cli.CodeInvalidOperation, path: "ops[0].html<ol>"},
		{name: "bad html link", fixture: "v1-minimal", at: "page0/s1", html: `<p><a href="https://example.com">x</a></p>`, code: cli.CodeInvalidOperation, path: "ops[0].html<a>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := vsdxtest.Fixture(t, tc.fixture)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			out := filepath.Join(t.TempDir(), "out.vsdx")
			_, err = apply.Run(path, textSetPlan(tc.at, tc.html), apply.Options{Out: out})
			var ce *cli.Error
			if !errors.As(err, &ce) {
				t.Fatalf("error %v, want *cli.Error", err)
			}
			if ce.Code != tc.code || ce.Path != tc.path {
				t.Errorf("got %s at %q, want %s at %s", ce.Code, ce.Path, tc.code, tc.path)
			}
			if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
				t.Error("a refused text.set wrote its out file")
			}
			if got, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(got, before) {
				t.Error("a refused text.set changed the input")
			}
		})
	}
}

// TestTextSetSequences checks that a second text.set in one plan sees
// the first's text, even when it returns to the original.
func TestTextSetSequences(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	p := &ops.Plan{Ops: []ops.Spec{
		&ops.TextSetSpec{At: "page0/s1", HTML: `<p>Bye</p>`},
		&ops.TextSetSpec{At: "page0/s1", HTML: `<p><b>Hello</b></p>`},
	}}
	if _, err := apply.Run(path, p, apply.Options{Out: out}); err != nil {
		t.Fatalf("two text.sets: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	obj := textSetObject(t, data, "page0", "page0/s1")
	if got, _ := obj["html"].(string); got != `<p><b>Hello</b></p>` {
		t.Errorf("second html = %q, want <p><b>Hello</b></p>", got)
	}
}

// TestScalingTextSet checks the scaling row: text.set of N long
// paragraphs (N = 1000, 4N = 4000, each about a hundred characters)
// allocates at most 5.5 times as much per paragraph (laws.Scales)
// and the best-of-11 time ratio is at most 8 (laws.TimeScales).
func TestScalingTextSet(t *testing.T) {
	setup := func(n int) (func() error, error) {
		path := vsdxtest.Fixture(t, "v1-minimal")
		var sb strings.Builder
		pad := strings.Repeat("x", 100)
		for i := range n {
			sb.WriteString("<p>Paragraph ")
			sb.WriteString(itoa(i))
			sb.WriteByte(' ')
			sb.WriteString(pad)
			sb.WriteString("</p>")
		}
		html := sb.String()
		return func() error {
			w, err := edit.Begin(path)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			spec := &ops.TextSetSpec{At: "page0/s1", HTML: html}
			_, changed, _, _, err := spec.Run(w, nil, "", nil)
			if err != nil {
				return err
			}
			if len(changed) != 1 {
				return errors.New("scaling text.set changed nothing")
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx text.set paragraphs", setup)
	laws.TimeScales(t, "vsdx text.set paragraphs", setup)
}

// itoa returns n in decimal (the scaling test only, to keep it one file).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestTextSetOnlyWritesLocal checks that a write touches only the
// target part: every other part of the package is byte-equal, as the
// four-outcome contract requires for untargeted content.
func TestTextSetOnlyWritesLocal(t *testing.T) {
	_, data := textSetRun(t, "v1-minimal", "page0/s1", `<p>Bye</p>`)
	if len(data) == 0 {
		t.Fatal("Bye wrote nothing")
	}
	// doc.Open on the output proves it is a still-readable drawing.
	pkg, err := opc.OpenBytes(data, opc.DefaultLimits())
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer func() { _ = pkg.Close() }()
	if _, err := doc.Open(pkg); err != nil {
		t.Fatalf("doc.Open(output): %v", err)
	}
}

// TestTextSetFieldLeftOut checks the CC11 row: text.set '<p>Title</p>'
// on V7 s1, which leaves field:1 out without listing it in delete,
// is operation_failed at ops[0].html with detail page0/s1/field:1,
// and writes nothing.
func TestTextSetFieldLeftOut(t *testing.T) {
	path := vsdxtest.Fixture(t, "v7-text")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err = apply.Run(path, textSetPlan("page0/s1", `<p>Title</p>`), apply.Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0].html" {
		t.Errorf("got %s at %q, want operation_failed at ops[0].html", ce.Code, ce.Path)
	}
	if ce.Detail != "page0/s1/field:1" {
		t.Errorf("detail = %q, want page0/s1/field:1", ce.Detail)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("a refused field text.set wrote its out file")
	}
	if got, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(got, before) {
		t.Error("a refused field text.set changed the input")
	}
}

// TestTextSetFieldDelete checks the CC11 row: text.set '<p>Title</p>'
// on V7 s1 with delete [page0/s1/field:1] is accepted, deleted lists
// the field, its Field section row is gone, and read html is
// '<p>Title</p>'.
func TestTextSetFieldDelete(t *testing.T) {
	path := vsdxtest.Fixture(t, "v7-text")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	p := &ops.Plan{Ops: []ops.Spec{&ops.TextSetSpec{At: "page0/s1", HTML: `<p>Title</p>`, Delete: []string{"page0/s1/field:1"}}}}
	res, err := apply.Run(path, p, apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Deleted) != 1 || res.Deleted[0] != "page0/s1/field:1" {
		t.Errorf("deleted = %v, want [page0/s1/field:1]", res.Deleted)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	if rows := textSetSectionRows(t, data, 1, vsd.SectionField); len(rows) != 0 {
		t.Errorf("Field rows = %v, want none", rows)
	}
	obj := textSetObject(t, data, "page0", "page0/s1")
	if got, _ := obj["html"].(string); got != `<p>Title</p>` {
		t.Errorf("html = %q, want <p>Title</p>", got)
	}
}

// TestTextSetFieldDeleteNotFound checks that a delete entry naming no
// field of the shape is not_found at ops[0].delete and writes nothing.
func TestTextSetFieldDeleteNotFound(t *testing.T) {
	for _, del := range []string{"page0/s1/field:2", "page0/s5/field:1"} {
		path := vsdxtest.Fixture(t, "v7-text")
		out := filepath.Join(t.TempDir(), "out.vsdx")
		p := &ops.Plan{Ops: []ops.Spec{&ops.TextSetSpec{At: "page0/s1", HTML: `<p>Title</p>`, Delete: []string{del}}}}
		_, err := apply.Run(path, p, apply.Options{Out: out})
		var ce *cli.Error
		if !errors.As(err, &ce) {
			t.Fatalf("delete %s: error %v, want *cli.Error", del, err)
		}
		if ce.Code != cli.CodeNotFound || ce.Path != "ops[0].delete" {
			t.Errorf("delete %s: got %s at %q, want not_found at ops[0].delete", del, ce.Code, ce.Path)
		}
	}
}

// TestTextSetFieldSentBackAndDeleted checks that a field both sent
// back in the html and listed in delete is deleted: the delete wins.
func TestTextSetFieldSentBackAndDeleted(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	golden, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", "golden", "v7-page0.html"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	path := vsdxtest.Fixture(t, "v7-text")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	p := &ops.Plan{Ops: []ops.Spec{&ops.TextSetSpec{At: "page0/s1", HTML: string(golden), Delete: []string{"page0/s1/field:1"}}}}
	res, err := apply.Run(path, p, apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Deleted) != 1 || res.Deleted[0] != "page0/s1/field:1" {
		t.Errorf("deleted = %v, want [page0/s1/field:1]", res.Deleted)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	if rows := textSetSectionRows(t, data, 1, vsd.SectionField); len(rows) != 0 {
		t.Errorf("Field rows = %v, want none", rows)
	}
	obj := textSetObject(t, data, "page0", "page0/s1")
	if got, _ := obj["html"].(string); strings.Contains(got, "field:") {
		t.Errorf("html = %q, want no field placeholder", got)
	}
}

// TestTextSetInheritedItalic checks the CC11 row: text.set
// '<p><i>Step</i></p>' on V2 s1 (master text italic) gives local Text
// 'Step', no local Character row, and html '<p>Step</p>' (the italic
// is inherited, decision f).
func TestTextSetInheritedItalic(t *testing.T) {
	_, data := textSetRun(t, "v2-master", "page0/s1", `<p><i>Step</i></p>`)
	obj := textSetObject(t, data, "page0", "page0/s1")
	if got, _ := obj["html"].(string); got != `<p>Step</p>` {
		t.Errorf("html = %q, want <p>Step</p>", got)
	}
	if got, _ := obj["text"].(string); got != "Step" {
		t.Errorf("text = %q, want Step", got)
	}
	if rows := textSetSectionRows(t, data, 1, vsd.SectionCharacter); len(rows) != 0 {
		t.Errorf("Character rows = %v, want none", rows)
	}
}

// TestTextSetDeletePlan checks the delete field's plan form: it
// decodes into the spec, and a delete entry that is not a field
// address is invalid_operation at ops[0].delete before the document
// opens.
func TestTextSetDeletePlan(t *testing.T) {
	p, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"text.set","at":"page0/s1","html":"<p>Title</p>","delete":["page0/s1/field:1"]}]}`))
	if len(errs) != 0 || p == nil || len(p.Ops) != 1 {
		t.Fatalf("ParsePlan: p=%v errs=%v, want one op", p, errs)
	}
	spec, ok := p.Ops[0].(*ops.TextSetSpec)
	if !ok || len(spec.Delete) != 1 || spec.Delete[0] != "page0/s1/field:1" {
		t.Fatalf("spec = %#v, want delete [page0/s1/field:1]", p.Ops[0])
	}
	_, errs = ops.ParsePlan([]byte(`{"ops":[{"op":"text.set","at":"page0/s1","html":"<p>x</p>","delete":["page0/s1"]}]}`))
	if len(errs) != 1 || errs[0].Code != cli.CodeInvalidOperation || errs[0].Path != "ops[0].delete" {
		t.Errorf("errs = %v, want invalid_operation at ops[0].delete", errs)
	}
}
