package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

func runOK(t *testing.T, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(args, &out, &errb); code != 0 {
		t.Fatalf("pptx %v: exit %d: %s", args, code, errb.String())
	}
	return out.String()
}

func TestReadList(t *testing.T) {
	deck := testdeck.Diagram(t)
	data, _ := os.ReadFile(deck)
	var list struct {
		Base   string           `json:"base"`
		Slides []map[string]any `json:"slides"`
		Notes  []any            `json:"notes"`
	}
	if err := json.Unmarshal([]byte(runOK(t, "read", deck)), &list); err != nil || len(list.Slides) != 1 ||
		list.Slides[0]["title"] != "Diagram" || list.Slides[0]["part"] != "/ppt/slides/slide1.xml" || list.Base != cli.Hash(data) || list.Notes == nil {
		t.Errorf("list %+v %v", list, err)
	}
	// --pretty indents; the default is compact.
	if out := runOK(t, "read", deck, "--pretty"); !strings.Contains(out, "\n  \"slides\": [\n    {\n") {
		t.Errorf("--pretty:\n%s", out)
	}
}

func TestReadSlide(t *testing.T) {
	deck := testdeck.Diagram(t)
	before, _ := os.ReadFile(deck)
	var v struct {
		Base    string           `json:"base"`
		Notes   []map[string]any `json:"notes"`
		Slide   string           `json:"slide"`
		Units   string           `json:"units"`
		Objects []map[string]any `json:"objects"`
		Hazards []map[string]any `json:"hazards"`
	}
	if err := json.Unmarshal([]byte(runOK(t, "read", deck, "--slide", "slide1")), &v); err != nil {
		t.Fatal(err)
	}
	if v.Base != cli.Hash(before) || v.Notes == nil {
		t.Errorf("read: base %q, notes %v", v.Base, v.Notes)
	}
	if v.Slide != "slide1" || v.Units != "pt" || len(v.Objects) < 20 || len(v.Hazards) != 3 {
		t.Errorf("read: %s %s %d objects %d hazards", v.Slide, v.Units, len(v.Objects), len(v.Hazards))
	}
	// Filters.
	if err := json.Unmarshal([]byte(runOK(t, "read", deck, "--slide=slide1", "--ids", "slide1/s20")), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Objects) != 5 { // the group and its four children
		t.Errorf("--ids s20: %d objects", len(v.Objects))
	}
	if err := json.Unmarshal([]byte(runOK(t, "read", deck, "--slide", "slide1", "--region", "605,105,5,5")), &v); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range v.Objects {
		ids = append(ids, o["id"].(string))
	}
	if strings.Join(ids, ",") != "slide1/s50" {
		t.Errorf("--region: %v", ids)
	}
	// Exact EMU.
	out := runOK(t, "read", deck, "--slide", "slide1", "--ids", "s10", "--units", "emu")
	if !strings.Contains(out, `"box":[1270000,1270000,1270000,635000]`) || !strings.Contains(out, `"units": "emu"`) {
		t.Errorf("emu:\n%s", out)
	}
	// SVG and detail levels.
	svg := runOK(t, "read", deck, "--slide", "slide1", "--format", "svg", "--detail", "sites")
	if !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, `data-site="3"`) || !strings.Contains(svg, `--accent1: #4472C4`) {
		t.Errorf("svg:\n%.400s", svg)
	}
	full := runOK(t, "read", deck, "--slide", "slide1", "--detail", "full", "--ids", "s2")
	if !strings.Contains(full, `"origins"`) || !strings.Contains(full, `"notes"`) || !strings.Contains(full, `"text.lvl1.size"`) {
		t.Errorf("full:\n%s", full)
	}
	after, _ := os.ReadFile(deck)
	if !bytes.Equal(before, after) {
		t.Error("read changed the deck")
	}
}

func TestXML(t *testing.T) {
	deck := testdeck.Diagram(t)
	out := runOK(t, "xml", deck, "slide1/s40~2")
	if !strings.HasPrefix(out, "<p:sp>") || !strings.Contains(out, `name="Dup B"`) {
		t.Errorf("xml 40~2: %s", out)
	}
	// An object in mc:AlternateContent prints the whole element.
	if out := runOK(t, "xml", deck, "slide1/s30"); !strings.HasPrefix(out, "<mc:AlternateContent") {
		t.Errorf("xml 30: %.80s", out)
	}
	if code, errs := runErr(t, "xml", deck, "slide1/s999"); code != 1 || errs[0].Code != cli.CodeNotFound {
		t.Errorf("missing id: exit %d %+v", code, errs)
	}
}

func TestReadErrors(t *testing.T) {
	deck := testdeck.Diagram(t)
	notADeck := filepath.Join(t.TempDir(), "x.pptx")
	if err := os.WriteFile(notADeck, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args       []string
		exit       int
		code, path string
	}{
		{[]string{"read", deck, "--slide", "slide9"}, 1, cli.CodeNotFound, "--slide"},
		{[]string{"read", deck, "--slide"}, 2, cli.CodeUsage, "--slide"},
		{[]string{"read", deck, "--detail", "most"}, 2, cli.CodeUsage, "--detail"},
		{[]string{"read", deck, "--units", "px"}, 2, cli.CodeUsage, "--units"},
		{[]string{"read", deck, "--slide", "slide1", "--region", "1,2,3"}, 2, cli.CodeUsage, "--region"},
		{[]string{"read", deck, "--json"}, 2, cli.CodeUsage, "--json"},
		{[]string{"read", filepath.Join(t.TempDir(), "nonexistent.pptx")}, 1, cli.CodeIO, ""},
		{[]string{"read", notADeck}, 1, cli.CodeInvalidDocument, ""},
		{[]string{"xml", deck, "slide1"}, 2, cli.CodeUsage, ""},
	} {
		code, errs := runErr(t, c.args...)
		if code != c.exit || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("%v: exit %d %+v, want %d %s at %q", c.args, code, errs, c.exit, c.code, c.path)
		}
	}
}
