package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// allOptions are the view variants the equivalence test covers.
var allOptions = []Options{
	{}, {Detail: Sites}, {Detail: Full}, {Units: "emu"}, {Detail: Full, Units: "emu"},
}

// checkDeck reads every slide of the deck at path and requires the JSON
// and SVG views to carry the same facts under every option set.
func checkDeck(t *testing.T, path string) (slides []*model.Slide, fails []string) {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		return nil, []string{err.Error()}
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		return nil, []string{err.Error()}
	}
	for _, part := range d.Slides {
		s, err := d.ReadSlide(part)
		if err != nil {
			fails = append(fails, part+": read: "+err.Error())
			continue
		}
		slides = append(slides, s)
		for _, o := range allOptions {
			if msg := equivalent(s, o); msg != "" {
				fails = append(fails, part+": "+msg)
			}
		}
	}
	return
}

// equivalent renders both views and compares their facts.
func equivalent(s *model.Slide, o Options) string {
	jb, err := JSON(s, o)
	if err != nil {
		return "json: " + err.Error()
	}
	sb, err := SVG(s, o)
	if err != nil {
		return "svg: " + err.Error()
	}
	jf, err := fromJSON(jb)
	if err != nil {
		return "parse json: " + err.Error()
	}
	sf, err := fromSVG(sb)
	if err != nil {
		return "parse svg: " + err.Error()
	}
	if d := diff(jf, sf); len(d) > 0 {
		if len(d) > 5 {
			d = append(d[:5], "...")
		}
		return "options " + o.Detail + "/" + o.Units + ": " + strings.Join(d, "; ")
	}
	return ""
}

func TestEquivalenceFixtures(t *testing.T) {
	for name, path := range testdeck.All(t) {
		_, fails := checkDeck(t, path)
		for _, f := range fails {
			t.Errorf("%s: %s", name, f)
		}
	}
}

// TestEquivalenceDetectsDifference checks that the comparison is not
// vacuous: a changed coordinate, colour or html in the SVG is reported.
func TestEquivalenceDetectsDifference(t *testing.T) {
	p, err := opc.Open(testdeck.Diagram(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	d, _ := model.OpenDeck(p)
	s, err := d.ReadSlide(d.Slides[0])
	if err != nil {
		t.Fatal(err)
	}
	jb, _ := JSON(s, Options{Detail: Full})
	sb, _ := SVG(s, Options{Detail: Full})
	jf, err := fromJSON(jb)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ old, new string }{
		{`x="100" y="100" width="100"`, `x="101" y="100" width="100"`},
		{`fill="var(--accent1)"`, `fill="var(--accent2)"`},
		{`<b>API</b>`, `<b>APl</b>`},
		{`data-from-glue="s10:3"`, `data-from-glue="s10:1"`},
		{`x2="300" y2="125"`, `x2="300" y2="126"`},
		{`data-level="2"`, `data-level="3"`},
	} {
		mut := strings.Replace(string(sb), c.old, c.new, 1)
		if mut == string(sb) {
			t.Fatalf("mutation %q did not apply", c.old)
		}
		sf, err := fromSVG([]byte(mut))
		if err != nil {
			t.Fatal(err)
		}
		if len(diff(jf, sf)) == 0 {
			t.Errorf("mutation %q -> %q not detected", c.old, c.new)
		}
	}
	sf, _ := fromSVG(sb)
	if d := diff(jf, sf); len(d) > 0 {
		t.Errorf("unmutated views differ: %v", d)
	}
	if len(jf) < len(s.Objects) {
		t.Errorf("parsed %d objects from JSON, model has %d", len(jf)-1, len(s.Objects))
	}
}

// TestDumpDiagram writes the diagram fixture's views to $PPTX_DUMP, for
// inspection by hand.
func TestDumpDiagram(t *testing.T) {
	dir := os.Getenv("PPTX_DUMP")
	if dir == "" {
		t.Skip("PPTX_DUMP not set")
	}
	src := testdeck.Diagram(t)
	b, _ := os.ReadFile(src)
	if err := os.WriteFile(filepath.Join(dir, "diagram.pptx"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := opc.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	d, _ := model.OpenDeck(p)
	s, err := d.ReadSlide(d.Slides[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []Options{{}, {Detail: Full}} {
		j, _ := JSON(s, o)
		v, _ := SVG(s, o)
		if err := os.WriteFile(filepath.Join(dir, "diagram"+o.Detail+".json"), j, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "diagram"+o.Detail+".svg"), v, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
