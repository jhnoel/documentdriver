package ops_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// htmlErrors returns the code@path of each error, in order.
func htmlErrors(errs cli.Errors) []string {
	var out []string
	for _, e := range errs {
		out = append(out, e.Code+"@"+e.Path)
	}
	return out
}

// TestParsePlanHTML checks the CC5 rows in the plan decoder: the two-op
// plan reports both html faults together, with the element and the
// attribute on the path, before any document opens.
func TestParsePlanHTML(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string // code@path
	}{
		{`{"ops":[{"op":"text.set","at":"page0/s1","html":"<p><a href=\"x\">a</a></p>"},{"op":"text.set","at":"page0/s1","html":"<p bogus=\"1\">x</p>"}]}`,
			[]string{"invalid_operation@ops[0].html<a>", "invalid_operation@ops[1].html@bogus"}},
		{`{"ops":[{"op":"text.set","at":"page0/s1","html":"<p><span style=\"letter-spacing:1pt\">x</span></p>"}]}`,
			[]string{"invalid_operation@ops[0].html@style"}},
		{`{"ops":[{"op":"text.set","at":"page0/s1","html":"<p><span style=\"color:#FF0000\" data-color=\"#00FF00\">x</span></p>"}]}`,
			[]string{"invalid_operation@ops[0].html@data-color"}},
		{`{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"html":"<p><a href=\"x\">a</a></p>"}]}`,
			[]string{"invalid_operation@ops[0].html<a>"}},
	} {
		_, errs := ops.ParsePlan([]byte(tc.text))
		if got := htmlErrors(errs); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.text, got, tc.want)
		}
	}
}

// TestParsePlanHTMLStyleMessage checks the CC5 letter-spacing row: the
// path names the style attribute and the message names the property.
func TestParsePlanHTMLStyleMessage(t *testing.T) {
	_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"text.set","at":"page0/s1","html":"<p><span style=\"letter-spacing:1pt\">x</span></p>"}]}`))
	if len(errs) != 1 {
		t.Fatalf("letter-spacing: errors %v, want one", errs)
	}
	if errs[0].Code != cli.CodeInvalidOperation || errs[0].Path != "ops[0].html@style" {
		t.Errorf("letter-spacing: got %s@%s, want invalid_operation@ops[0].html@style", errs[0].Code, errs[0].Path)
	}
	if !strings.Contains(errs[0].Message, "letter-spacing") {
		t.Errorf("letter-spacing: message %q does not name the property", errs[0].Message)
	}
}

// TestCheckHTML checks the pre-open check itself: a plan of text.set
// and object.add specs reports every html fault in op order, and an
// op without html is skipped.
func TestCheckHTML(t *testing.T) {
	plan := &ops.Plan{Ops: []ops.Spec{
		&ops.TextSetSpec{At: "page0/s1", HTML: `<p><a href="x">a</a></p>`},
		&ops.PageRenameSpec{At: "page0", Name: "X"},
		&ops.ObjectAddSpec{At: "page0", Geom: "rect", Box: []float64{0, 0, 10, 10}, HTML: new(`<p bogus="1">x</p>`)},
		&ops.ObjectAddSpec{At: "page0", Geom: "rect", Box: []float64{0, 0, 10, 10}},
		&ops.TextSetSpec{At: "page0/s1", HTML: `<p><b>Hello</b></p>`},
	}}
	got := htmlErrors(ops.CheckHTML(plan))
	want := []string{"invalid_operation@ops[0].html<a>", "invalid_operation@ops[2].html@bogus"}
	if !slices.Equal(got, want) {
		t.Errorf("CheckHTML: got %v, want %v", got, want)
	}
	if errs := ops.CheckHTML(nil); errs != nil {
		t.Errorf("CheckHTML(nil) = %v, want nil", errs)
	}
}

// htmlFixtures are the fixture drawings of testdata/vsdx whose every
// read html must pass the pre-open check (the CC5 round trip law).
var htmlFixtures = []string{
	"v1-minimal", "v2-master", "v3-styles", "v4-groups", "v5-glue", "v6-data",
	"v7-text", "v8-pages", "v9-rotation", "v10-defects", "v11-ns2011",
	"v12-stencil", "v12-vsdm", "v13-scale", "v15-foreign", "v17-orientation",
}

// TestReadHTMLRoundTrip checks the CC5 round trip law: read's html of
// every fixture shape passes the pre-open check unchanged.
func TestReadHTMLRoundTrip(t *testing.T) {
	for _, name := range htmlFixtures {
		checkReadHTML(t, name, readFixtureBytes(t, name))
	}
}

// TestReadHTMLRoundTripCorpus checks the CC5 round trip law on every
// VSDX_CORPUS drawing. It skips only when VSDX_CORPUS is unset.
func TestReadHTMLRoundTripCorpus(t *testing.T) {
	if os.Getenv("VSDX_CORPUS") == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	dir := corpusDir(t)
	paths := corpusDrawings(dir)
	if len(paths) == 0 {
		t.Fatalf("VSDX_CORPUS=%q: no drawings under %s", os.Getenv("VSDX_CORPUS"), dir)
	}
	checked := 0
	for _, path := range paths {
		data, err := os.ReadFile(path) //nolint:gosec // G304: corpus path from VSDX_CORPUS, never from a document
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		checked += checkReadHTML(t, path, data)
	}
	if checked == 0 {
		t.Log("no corpus shape carried html")
	}
}

// checkReadHTML reports every page and master shape whose read html
// fails the pre-open check in data, naming the drawing name, and
// returns how many html strings it checked.
func checkReadHTML(t *testing.T, name string, data []byte) int {
	t.Helper()
	sum, err := read.Document(data, "B")
	if err != nil {
		// A file that cannot be read as a drawing has no read html;
		// the corpus sweep classifies it elsewhere.
		return 0
	}
	checked := 0
	for _, p := range sum.Pages {
		checked += checkPageHTML(t, name, data, p.ID)
	}
	for _, m := range sum.Masters {
		checked += checkPageHTML(t, name, data, m.ID)
	}
	return checked
}

// checkPageHTML checks the read html of every object of page or master
// address in data against the pre-open check.
func checkPageHTML(t *testing.T, name string, data []byte, address string) int {
	t.Helper()
	raw, err := read.Page(data, "B", address, read.Options{Detail: "full"})
	if err != nil {
		return 0
	}
	var v struct {
		Objects []struct {
			ID   string `json:"id"`
			HTML string `json:"html"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s %s: unmarshal Page: %v", name, address, err)
	}
	checked := 0
	for _, o := range v.Objects {
		if o.HTML == "" {
			continue
		}
		checked++
		plan := &ops.Plan{Ops: []ops.Spec{&ops.TextSetSpec{At: "page0/s1", HTML: o.HTML}}}
		if errs := ops.CheckHTML(plan); len(errs) != 0 {
			t.Errorf("%s %s: read html of %s fails the pre-open check: %v", name, address, o.ID, errs)
		}
	}
	return checked
}

// readFixtureBytes builds fixture name and returns its bytes.
func readFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(vsdxtest.Fixture(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// corpusDir returns the vsdx corpus directory: VSDX_CORPUS, below
// corpus/vsdx when VSDX_CORPUS is a worktree root. With it set and
// unusable the test fails, naming it.
func corpusDir(t *testing.T) string {
	t.Helper()
	d := os.Getenv("VSDX_CORPUS")
	for _, cand := range []string{d, filepath.Join(d, "corpus", "vsdx"), filepath.Join(d, "vsdx")} {
		if _, err := os.Stat(filepath.Join(cand, "repos")); err == nil {
			return cand
		}
	}
	if _, err := os.Stat(d); err == nil {
		return d
	}
	t.Fatalf("VSDX_CORPUS=%q: no corpus found", d)
	return ""
}

// corpusDrawings lists the drawings under dir in traversal order, sorted.
func corpusDrawings(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".vsdx", ".vsdm", ".vstx", ".vstm", ".vssx", ".vssm":
			out = append(out, p)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

// TestScalingHTMLCheck checks the scaling row: the pre-open check of
// N long paragraphs (N = 1000, 4N = 4000, each about a hundred
// characters) allocates at most 5.5 times as much per paragraph
// (laws.Scales) and the best-of-11 time ratio is at most 8
// (laws.TimeScales).
func TestScalingHTMLCheck(t *testing.T) {
	setup := func(n int) (func() error, error) {
		var sb strings.Builder
		pad := strings.Repeat("x", 100)
		for i := range n {
			sb.WriteString("<p>Paragraph ")
			sb.WriteString(strconvItoa(i))
			sb.WriteByte(' ')
			sb.WriteString(pad)
			sb.WriteString("</p>")
		}
		html := sb.String()
		return func() error {
			plan := &ops.Plan{Ops: []ops.Spec{&ops.TextSetSpec{At: "page0/s1", HTML: html}}}
			if errs := ops.CheckHTML(plan); len(errs) != 0 {
				return errs[0]
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx pre-open html check paragraphs", setup)
	laws.TimeScales(t, "vsdx pre-open html check paragraphs", setup)
}

// strconvItoa returns n in decimal (the scaling test only, to keep it one file).
func strconvItoa(n int) string {
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
