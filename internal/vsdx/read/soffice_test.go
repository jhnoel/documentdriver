//go:build !wasip1

package read

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/soffice"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// sofficeForTest returns the LibreOffice binary for the test, skipping
// without one. The caller reads DD_SOFFICE and passes its value, as the
// shared package names no format's variable.
func sofficeForTest(t *testing.T) string {
	t.Helper()
	bin := soffice.Find(os.Getenv("DD_SOFFICE"), "soffice", soffice.DefaultPaths...)
	if bin == "" {
		t.Skip("no soffice (LibreOffice) on PATH; set DD_SOFFICE")
	}
	return bin
}

// convertToSVG converts the drawing at srcPath to SVG with headless
// LibreOffice in a private profile and returns the SVG text. The file
// is copied under drawing.vsdx so odd fixture names cannot confuse
// soffice.
func convertToSVG(t *testing.T, bin, srcPath string) string {
	t.Helper()
	dir := t.TempDir()
	input, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read %s: %v", srcPath, err)
	}
	src := filepath.Join(dir, "drawing.vsdx")
	if err := os.WriteFile(src, input, 0o600); err != nil {
		t.Fatalf("write %s: %v", src, err)
	}
	outdir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outdir, 0o700); err != nil {
		t.Fatalf("output dir: %v", err)
	}
	profile := filepath.Join(dir, "profile")
	out, err := soffice.Run(60*time.Second, dir, bin,
		"-env:UserInstallation=file://"+profile,
		"--headless", "--norestore", "--nolockcheck", "--nodefault", "--nologo",
		"--convert-to", "svg", "--outdir", outdir, src)
	if err != nil {
		t.Fatalf("soffice convert %s: %v: %s", srcPath, err, strings.TrimSpace(string(out)))
	}
	svg, err := os.ReadFile(filepath.Join(outdir, "drawing.svg"))
	if err != nil {
		t.Fatalf("read converted svg: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(svg)
}

// readTextsAndSize returns the distinct texts of read show on the
// fixture in input and its first page size in points.
func readTextsAndSize(t *testing.T, input []byte) (texts []string, wPt, hPt float64) {
	t.Helper()
	sum, err := Document(input, "B")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(sum.Pages) == 0 {
		t.Fatal("summary has no pages")
	}
	if len(sum.Pages[0].Size) != 2 {
		t.Fatalf("page size %v, want two numbers", sum.Pages[0].Size)
	}
	if wPt, err = sum.Pages[0].Size[0].Float64(); err != nil {
		t.Fatalf("page width: %v", err)
	}
	if hPt, err = sum.Pages[0].Size[1].Float64(); err != nil {
		t.Fatalf("page height: %v", err)
	}
	b, err := Page(input, "B", sum.Pages[0].ID, Options{})
	if err != nil {
		t.Fatalf("Page(%s): %v", sum.Pages[0].ID, err)
	}
	var v struct {
		Objects []struct {
			Text string `json:"text"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("Page JSON: %v", err)
	}
	seen := map[string]bool{}
	for _, o := range v.Objects {
		if o.Text == "" || seen[o.Text] {
			continue
		}
		seen[o.Text] = true
		texts = append(texts, o.Text)
	}
	return texts, wPt, hPt
}

// svgViewBox is an SVG viewBox.
type svgViewBox struct {
	minX, minY, w, h float64
}

// svgRect is one SVG BoundingBox rectangle origin.
type svgRect struct {
	x, y float64
}

// svgScan collects an SVG's texts, BoundingBox origins and viewBox
// with a streaming tree scan.
type svgScan struct {
	viewBox string
	texts   []string
	rects   []svgRect
	inText  int
	buf     strings.Builder
}

// Start records the svg viewBox, BoundingBox rectangles and text depth.
func (s *svgScan) Start(n *tree.Node) error {
	switch n.Local() {
	case "svg":
		if v, ok := n.Attr("viewBox"); ok {
			s.viewBox = v
		}
	case "rect":
		if v, ok := n.Attr("class"); !ok || v != "BoundingBox" {
			return nil
		}
		x, y, xok, yok := "", "", false, false
		if x, xok = n.Attr("x"); !xok {
			return nil
		}
		if y, yok = n.Attr("y"); !yok {
			return nil
		}
		xf, xerr := strconv.ParseFloat(x, 64)
		yf, yerr := strconv.ParseFloat(y, 64)
		if xerr != nil || yerr != nil {
			return nil
		}
		s.rects = append(s.rects, svgRect{x: xf, y: yf})
	case "text":
		if s.inText == 0 {
			s.buf.Reset()
		}
		s.inText++
	}
	return nil
}

// End closes a text element's collected characters.
func (s *svgScan) End(n *tree.Node) error {
	if n.Local() == "text" && s.inText > 0 {
		s.inText--
		if s.inText == 0 {
			s.texts = append(s.texts, s.buf.String())
		}
	}
	return nil
}

// Leaf appends character data inside a text element.
func (s *svgScan) Leaf(n *tree.Node) error {
	if s.inText > 0 && (n.Kind() == tree.TextNode || n.Kind() == tree.CDataNode) {
		s.buf.WriteString(n.Data())
	}
	return nil
}

// parseSVG reads an SVG's texts, BoundingBox origins and viewBox.
func parseSVG(t *testing.T, svg string) (texts []string, rects []svgRect, vb svgViewBox) {
	t.Helper()
	s := &svgScan{}
	if err := tree.ScanString(svg, s); err != nil {
		t.Fatalf("scan svg: %v", err)
	}
	if s.viewBox == "" {
		t.Fatal("svg has no viewBox")
	}
	fields := strings.Fields(s.viewBox)
	if len(fields) != 4 {
		t.Fatalf("viewBox %q, want four numbers", s.viewBox)
	}
	vals := make([]float64, 4)
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			t.Fatalf("viewBox %q: %v", s.viewBox, err)
		}
		vals[i] = v
	}
	vb = svgViewBox{minX: vals[0], minY: vals[1], w: vals[2], h: vals[3]}
	if vb.w == 0 || vb.h == 0 {
		t.Fatalf("viewBox %q has a zero size", s.viewBox)
	}
	return s.texts, s.rects, vb
}

// TestCorpusLibreOffice checks the L1c LibreOffice row by converting
// V1, V2 and V5 with headless LibreOffice to SVG (skipped without
// soffice): the SVG has each text read shows, and V1's s1 rectangle
// is at x 108 y 126 within 1 pt, mapping the viewBox onto the page
// size read shows.
func TestCorpusLibreOffice(t *testing.T) {
	bin := sofficeForTest(t)
	for _, name := range []string{"v1-minimal", "v2-master", "v5-glue"} {
		t.Run(name, func(t *testing.T) {
			srcPath := vsdxtest.Fixture(t, name)
			input, err := os.ReadFile(srcPath)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			want, wPt, hPt := readTextsAndSize(t, input)
			svg := convertToSVG(t, bin, srcPath)
			got, rects, vb := parseSVG(t, svg)
			present := make(map[string]bool, len(got))
			for _, s := range got {
				present[s] = true
			}
			for _, s := range want {
				if !present[s] {
					t.Errorf("svg texts %v, want %q among them", got, s)
				}
			}
			if name != "v1-minimal" {
				return
			}
			found := false
			for _, r := range rects {
				xPt := (r.x - vb.minX) * wPt / vb.w
				yPt := (r.y - vb.minY) * hPt / vb.h
				if math.Abs(xPt-108) <= 1 && math.Abs(yPt-126) <= 1 {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("svg rectangles %v (viewBox %v, page %v x %v pt), want one within 1 pt of (108, 126)", rects, vb, wPt, hPt)
			}
		})
	}
}
