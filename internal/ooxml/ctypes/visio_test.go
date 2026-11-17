package ctypes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// TestVisioTypes checks the Visio entries the vsdx create template needs:
// the drawing, pages, page, masters, master and windows content and
// relationship types, each seen in at least two Visio-authored corpus
// files, the core, extended and custom properties types Visio shares with
// the other products, and the accepted Visio scan count.
func TestVisioTypes(t *testing.T) {
	ctByValue := map[string]Entry{}
	for _, e := range ContentTypes() {
		ctByValue[e.Value] = e
	}
	relByValue := map[string]Entry{}
	for _, e := range RelTypes() {
		relByValue[e.Value] = e
	}
	visio := []struct {
		kind  string
		byval map[string]Entry
		value string
	}{
		{"content type", ctByValue, CTMsVisioDrawingMain},
		{"content type", ctByValue, CTMsVisioDrawingMacroEnabledMain},
		{"content type", ctByValue, CTMsVisioPages},
		{"content type", ctByValue, CTMsVisioPage},
		{"content type", ctByValue, CTMsVisioMasters},
		{"content type", ctByValue, CTMsVisioMaster},
		{"content type", ctByValue, CTMsVisioWindows},
		{"relationship type", relByValue, RelDocument},
		{"relationship type", relByValue, RelPages},
		{"relationship type", relByValue, RelPage},
		{"relationship type", relByValue, RelMasters},
		{"relationship type", relByValue, RelMaster},
		{"relationship type", relByValue, RelWindows},
	}
	for _, w := range visio {
		e, ok := w.byval[w.value]
		if !ok {
			t.Errorf("%s %s: missing from table", w.kind, w.value)
			continue
		}
		if e.Visio < 2 {
			t.Errorf("%s %s: %d Visio files, want at least 2", w.kind, w.value, e.Visio)
		}
		if !strings.HasPrefix(e.Source, "vsdx-repos/") {
			t.Errorf("%s %s: source %q, want under vsdx-repos", w.kind, w.value, e.Source)
		}
	}
	props := []struct {
		kind  string
		byval map[string]Entry
		value string
	}{
		{"content type", ctByValue, CTPackageCoreProperties},
		{"content type", ctByValue, CTExtendedProperties},
		{"content type", ctByValue, CTCustomProperties},
		{"relationship type", relByValue, RelMetadataCoreProperties},
		{"relationship type", relByValue, RelExtendedProperties},
		{"relationship type", relByValue, RelCustomProperties},
	}
	for _, w := range props {
		if _, ok := w.byval[w.value]; !ok {
			t.Errorf("%s %s: missing from table", w.kind, w.value)
		}
	}
	for _, e := range append(ContentTypes(), RelTypes()...) {
		if e.Word+e.PowerPoint+e.Excel+e.Visio != e.Files {
			t.Errorf("%s: Word+PowerPoint+Excel+Visio != Files", e.Name)
		}
	}
	for _, tg := range Targets() {
		if tg.Word+tg.PowerPoint+tg.Excel+tg.Visio != tg.Files {
			t.Errorf("target %s -> %s: Word+PowerPoint+Excel+Visio != Files", tg.Rel, tg.ContentType)
		}
	}
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "internal", "ooxml", "ctypes", "provenance.json")) //nolint:gosec // G304: a fixed repo path
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Scans []struct {
			Label    string `json:"label"`
			Accepted int    `json:"accepted"`
		} `json:"scans"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Scans {
		if s.Label != "vsdx-repos" {
			continue
		}
		if s.Accepted != 53 {
			t.Errorf("vsdx-repos accepted %d files, want 53", s.Accepted)
		}
		return
	}
	t.Error("vsdx-repos scan missing from provenance.json scans")
}
