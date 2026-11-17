//go:build !wasip1

package create

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/soffice"
	"github.com/jhnoel/documentdriver/internal/vsdx/integrity"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
)

// acceptanceCase is one CL1 input: the blank (no plan) or a c2/c3 plan.
type acceptanceCase struct {
	plan  string
	label string
}

var acceptanceCases = []acceptanceCase{
	{plan: "", label: "blank"},
	{plan: "c2-shapes.json", label: "c2"},
	{plan: "c3-pages.json", label: "c3"},
}

// acceptancePlan parses the plan, or returns nil for the blank.
func acceptancePlan(t *testing.T, name string) *ops.Plan {
	t.Helper()
	if name == "" {
		return nil
	}
	parsed, errs := ops.ParsePlan([]byte(createPlanText(t, name)))
	if len(errs) != 0 {
		t.Fatalf("ParsePlan(%s): %v", name, errs)
	}
	return parsed
}

// acceptanceBytes creates the case at the fixed clock, returns its bytes.
func acceptanceBytes(t *testing.T, plan string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if _, err := Run(acceptancePlan(t, plan), Options{Out: out, IDs: ids.Fixed(0, fixedClock)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read created drawing: %v", err)
	}
	return data
}

// acceptanceSoffice returns the LibreOffice binary, skipping without one.
func acceptanceSoffice(t *testing.T) string {
	t.Helper()
	bin := soffice.Find(os.Getenv("DD_SOFFICE"), "soffice", soffice.DefaultPaths...)
	if bin == "" {
		t.Skip("no soffice (LibreOffice) on PATH; set DD_SOFFICE")
	}
	return bin
}

// acceptanceSVG converts data to SVG with headless LibreOffice.
func acceptanceSVG(t *testing.T, bin string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "drawing.vsdx")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatalf("write drawing: %v", err)
	}
	outdir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outdir, 0o700); err != nil {
		t.Fatalf("output dir: %v", err)
	}
	out, err := soffice.Run(60*time.Second, dir, bin,
		"-env:UserInstallation=file://"+filepath.Join(dir, "profile"),
		"--headless", "--norestore", "--nolockcheck", "--nodefault", "--nologo",
		"--convert-to", "svg", "--outdir", outdir, src)
	if err != nil {
		t.Fatalf("soffice convert: %v: %s", err, strings.TrimSpace(string(out)))
	}
	svg, err := os.ReadFile(filepath.Join(outdir, "drawing.svg"))
	if err != nil {
		t.Fatalf("read converted svg: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(svg)
}

// acceptanceScan collects an SVG's viewBox, texts, BoundingBox rects,
// straight-line paths and Slide count in one streaming scan.
type acceptanceScan struct {
	viewBox string
	texts   []string
	rects   []acceptanceRect
	lines   []acceptanceLine
	slides  int
	inText  int
	buf     strings.Builder
}

// acceptanceRect is one BoundingBox rect in SVG units.
type acceptanceRect struct{ x, y, w, h float64 }

// acceptanceLine is one straight path M...L... in SVG units.
type acceptanceLine struct{ x1, y1, x2, y2 float64 }

// Start records the viewBox, Slide count, rects, line paths and text depth.
func (s *acceptanceScan) Start(n *tree.Node) error {
	switch n.Local() {
	case "svg":
		if v, ok := n.Attr("viewBox"); ok && s.viewBox == "" {
			s.viewBox = v
		}
	case "g":
		if v, ok := n.Attr("class"); ok && v == "Slide" {
			s.slides++
		}
	case "rect":
		if v, ok := n.Attr("class"); ok && v == "BoundingBox" {
			if r, ok := acceptanceRectOf(n); ok {
				s.rects = append(s.rects, r)
			}
		}
	case "path":
		if v, ok := n.Attr("d"); ok {
			if l, ok := acceptanceLineOf(v); ok {
				s.lines = append(s.lines, l)
			}
		}
	case "text":
		if s.inText == 0 {
			s.buf.Reset()
		}
		s.inText++
	}
	return nil
}

// End closes a text element's collected characters.
func (s *acceptanceScan) End(n *tree.Node) error {
	if n.Local() == "text" && s.inText > 0 {
		s.inText--
		if s.inText == 0 {
			s.texts = append(s.texts, s.buf.String())
		}
	}
	return nil
}

// Leaf appends character data inside a text element.
func (s *acceptanceScan) Leaf(n *tree.Node) error {
	if s.inText > 0 && (n.Kind() == tree.TextNode || n.Kind() == tree.CDataNode) {
		s.buf.WriteString(n.Data())
	}
	return nil
}

// acceptanceRectOf parses n's x, y, width and height attributes.
func acceptanceRectOf(n *tree.Node) (acceptanceRect, bool) {
	nums := make([]float64, 4)
	for i, name := range []string{"x", "y", "width", "height"} {
		v, ok := n.Attr(name)
		if !ok {
			return acceptanceRect{}, false
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return acceptanceRect{}, false
		}
		nums[i] = f
	}
	return acceptanceRect{x: nums[0], y: nums[1], w: nums[2], h: nums[3]}, true
}

// acceptanceLineOf parses a path d of the exact form "M x,y L x,y".
func acceptanceLineOf(d string) (acceptanceLine, bool) {
	f := strings.Fields(d)
	if len(f) != 4 || f[0] != "M" || f[2] != "L" {
		return acceptanceLine{}, false
	}
	xy := func(s string) (float64, float64, bool) {
		x, y, ok := strings.Cut(s, ",")
		if !ok {
			return 0, 0, false
		}
		xf, xe := strconv.ParseFloat(x, 64)
		yf, ye := strconv.ParseFloat(y, 64)
		if xe != nil || ye != nil {
			return 0, 0, false
		}
		return xf, yf, true
	}
	x1, y1, ok1 := xy(f[1])
	x2, y2, ok2 := xy(f[3])
	if !ok1 || !ok2 {
		return acceptanceLine{}, false
	}
	return acceptanceLine{x1: x1, y1: y1, x2: x2, y2: y2}, true
}

// acceptanceWithin reports whether got is within one point of want.
func acceptanceWithin(got, want float64) bool { return math.Abs(got-want) <= 1 }

// TestCreateAcceptanceIntegrity checks the CL1 row: integrity.Check
// finds nothing in the blank, c2 and c3.
func TestCreateAcceptanceIntegrity(t *testing.T) {
	for _, c := range acceptanceCases {
		pkg, err := opc.OpenBytes(acceptanceBytes(t, c.plan), opc.DefaultLimits())
		if err != nil {
			t.Fatalf("%s: OpenBytes: %v", c.label, err)
		}
		findings, err := integrity.Check(pkg)
		if err != nil {
			t.Fatalf("%s: integrity.Check: %v", c.label, err)
		}
		if len(findings) != 0 {
			t.Errorf("%s: integrity.Check = %v, want no finding", c.label, findings)
		}
		if err := pkg.Close(); err != nil {
			t.Errorf("%s: close package: %v", c.label, err)
		}
	}
}

// TestCreateAcceptanceLibreOffice checks the CL1 LibreOffice rows
// (skipped without soffice): each of the blank, c2 and c3 converts
// to SVG; c2's SVG holds Start and End, the rect's outline at x 72
// y 72 w 144 h 72 within 1 pt and the connector's line from
// (216,108) to (360,108) within 1 pt; c3 converts with two pages.
// Visio open: unverified — no Visio is available, so repair-free
// open in Visio is not claimed; LibreOffice import stands in.
func TestCreateAcceptanceLibreOffice(t *testing.T) {
	bin := acceptanceSoffice(t)
	for _, c := range acceptanceCases {
		data := acceptanceBytes(t, c.plan)
		sum, err := read.Document(data, "B")
		if err != nil {
			t.Fatalf("%s: read.Document: %v", c.label, err)
		}
		if len(sum.Pages) == 0 || len(sum.Pages[0].Size) != 2 {
			t.Fatalf("%s: summary pages %+v, want a first page with a size", c.label, sum.Pages)
		}
		wPt, err := sum.Pages[0].Size[0].Float64()
		if err != nil {
			t.Fatalf("%s: page width: %v", c.label, err)
		}
		hPt, err := sum.Pages[0].Size[1].Float64()
		if err != nil {
			t.Fatalf("%s: page height: %v", c.label, err)
		}
		scan := &acceptanceScan{}
		if err := tree.ScanString(acceptanceSVG(t, bin, data), scan); err != nil {
			t.Fatalf("%s: scan svg: %v", c.label, err)
		}
		wantSlides := 1
		if c.label == "c3" {
			wantSlides = 2
		}
		if scan.slides != wantSlides {
			t.Errorf("%s: svg has %d Slide groups, want %d", c.label, scan.slides, wantSlides)
		}
		if c.label != "c2" {
			continue
		}
		for _, want := range []string{"Start", "End"} {
			found := false
			for _, got := range scan.texts {
				if got == want {
					found = true
				}
			}
			if !found {
				t.Errorf("c2: svg texts %v, want %q among them", scan.texts, want)
			}
		}
		fields := strings.Fields(scan.viewBox)
		if len(fields) != 4 {
			t.Fatalf("c2: viewBox %q, want four numbers", scan.viewBox)
		}
		vb := make([]float64, 4)
		for i, f := range fields {
			v, err := strconv.ParseFloat(f, 64)
			if err != nil {
				t.Fatalf("c2: viewBox %q: %v", scan.viewBox, err)
			}
			vb[i] = v
		}
		if vb[2] == 0 || vb[3] == 0 {
			t.Fatalf("c2: viewBox %q has a zero size", scan.viewBox)
		}
		sx, sy := wPt/vb[2], hPt/vb[3]
		rectFound, lineFound := false, false
		for _, r := range scan.rects {
			x, y := (r.x-vb[0])*sx, (r.y-vb[1])*sy
			if acceptanceWithin(x, 72) && acceptanceWithin(y, 72) && acceptanceWithin(r.w*sx, 144) && acceptanceWithin(r.h*sy, 72) {
				rectFound = true
			}
		}
		for _, l := range scan.lines {
			x1, y1 := (l.x1-vb[0])*sx, (l.y1-vb[1])*sy
			x2, y2 := (l.x2-vb[0])*sx, (l.y2-vb[1])*sy
			if acceptanceWithin(x1, 216) && acceptanceWithin(y1, 108) && acceptanceWithin(x2, 360) && acceptanceWithin(y2, 108) {
				lineFound = true
			}
		}
		if !rectFound {
			t.Errorf("c2: svg rects %+v (viewBox %v, page %v x %v pt), want one at x 72 y 72 w 144 h 72 within 1 pt", scan.rects, vb, wPt, hPt)
		}
		if !lineFound {
			t.Errorf("c2: svg lines %+v (viewBox %v, page %v x %v pt), want one from (216,108) to (360,108) within 1 pt", scan.lines, vb, wPt, hPt)
		}
	}
}

// TestCreateAcceptanceDeterminism checks the CL1 determinism law:
// the same case and clock twice give equal bytes.
func TestCreateAcceptanceDeterminism(t *testing.T) {
	for _, c := range acceptanceCases {
		if !bytes.Equal(acceptanceBytes(t, c.plan), acceptanceBytes(t, c.plan)) {
			t.Errorf("%s: two creates gave different bytes", c.label)
		}
	}
}

// TestCreateAcceptanceDryRun checks the CL1 dry-run law: --dry-run
// writes nothing and equals the run (same aliases and changed).
func TestCreateAcceptanceDryRun(t *testing.T) {
	for _, c := range acceptanceCases {
		plan := acceptancePlan(t, c.plan)
		keptOut := filepath.Join(t.TempDir(), "kept.vsdx")
		kept, err := Run(plan, Options{Out: keptOut, IDs: ids.Fixed(0, fixedClock)})
		if err != nil {
			t.Fatalf("%s: create: %v", c.label, err)
		}
		dryOut := filepath.Join(t.TempDir(), "dry.vsdx")
		dry, err := Run(plan, Options{Out: dryOut, DryRun: true, IDs: ids.Fixed(0, fixedClock)})
		if err != nil {
			t.Fatalf("%s: dry run: %v", c.label, err)
		}
		if !dry.DryRun || dry.Saved != "" || dry.NewBase != "" {
			t.Errorf("%s: dry result %+v", c.label, dry)
		}
		if _, err := os.Stat(dryOut); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: dry run wrote %s", c.label, dryOut)
		}
		if len(dry.Changed) != len(kept.Changed) {
			t.Errorf("%s: dry changed %d, run changed %d", c.label, len(dry.Changed), len(kept.Changed))
		} else {
			for i := range dry.Changed {
				if !bytes.Equal(dry.Changed[i], kept.Changed[i]) {
					t.Errorf("%s: dry changed[%d] %s, run %s", c.label, i, dry.Changed[i], kept.Changed[i])
				}
			}
		}
		if len(dry.Aliases) != len(kept.Aliases) {
			t.Errorf("%s: dry aliases %v, run %v", c.label, dry.Aliases, kept.Aliases)
		} else {
			for k, v := range kept.Aliases {
				if dry.Aliases[k] != v {
					t.Errorf("%s: dry aliases %v, run %v", c.label, dry.Aliases, kept.Aliases)
					break
				}
			}
		}
	}
}
