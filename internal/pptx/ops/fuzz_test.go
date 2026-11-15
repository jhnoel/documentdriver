package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// FuzzPlan decodes and compiles arbitrary plan JSON and, when it
// compiles, runs it against the minimal deck and snapshots the result
// (the write-set check and the well-formedness re-check). Any step may
// refuse; none may panic or hang. The deck is never committed.
func FuzzPlan(f *testing.F) {
	for _, s := range []string{
		`{"ops": [{"op": "object.set", "at": "slide1/s2", "box": [72, 120, 180, 54], "rot": 30}]}`,
		`{"ops": [{"op": "object.move", "at": "slide1/s3", "dx": 10, "dy": -5}]}`,
		`{"ops": [{"op": "object.add", "at": "slide1", "as": "c", "name": "Cache", "kind": "shape", "geom": "rect", "box": [300, 200, 90, 60], "fill": {"scheme": "accent2"}, "html": "<b>Cache</b>"}]}`,
		`{"units": "emu", "ops": [{"op": "object.add", "at": "slide1", "name": "", "kind": "shape", "box": [0, 0, 914400, 914400], "path": "M0 0 L10 10 Q20 0 30 10 Z"}]}`,
		`{"ops": [{"op": "text.set", "at": "slide1/s2", "html": "<p>a</p><ul><li data-char=\"-\">b</li></ul>"}]}`,
		`{"ops": [{"op": "text.replace", "at": "slide1/s3", "select": "Database", "with": "Store", "all": true}]}`,
		`{"ops": [{"op": "group.create", "as": "g", "name": "G", "at": ["slide1/s2", "slide1/s3"]}, {"op": "group.ungroup", "at": "$g"}]}`,
		`{"ops": [{"op": "object.duplicate", "at": "slide1/s2", "as": "d"}, {"op": "object.reorder", "at": "$d", "to": "back"}, {"op": "object.delete", "at": "slide1/s5"}]}`,
		`{"ops": [{"op": "object.set", "at": "slide1/s4", "line": {"w": 2, "dash": "dash", "tail": "triangle"}}]}`,
		`{"ops": [{"op": "slide.duplicate"}, {"op": "slide.add"}]}`,
	} {
		f.Add(s)
	}
	dir := f.TempDir()
	deck := filepath.Join(dir, "deck.pptx")
	src, err := os.ReadFile(testdeck.Minimal(f))
	if err != nil {
		f.Fatal(err)
	}
	if err := os.WriteFile(deck, src, 0o644); err != nil {
		f.Fatal(err)
	}
	snap := filepath.Join(dir, "snap.pptx")
	f.Fuzz(func(t *testing.T, s string) {
		plan, err := DecodePlan(strings.NewReader(s))
		if err != nil {
			return
		}
		steps, err := plan.Compile()
		if err != nil {
			return
		}
		tx, err := Begin(deck)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for _, op := range steps {
			if err := tx.Run(op); err != nil {
				return
			}
		}
		os.Remove(snap)
		if err := tx.Snapshot(snap); err != nil {
			return
		}
	})
}
