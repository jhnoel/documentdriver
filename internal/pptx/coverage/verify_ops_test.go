package coverage

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/pptx/ops"
)

// kindTargets are the probe deck's object of each kind.
var kindTargets = map[string]string{"shape": "s2", "connector": "s5", "group": "s7", "picture": "s9", "other": "s10"}

// cellOps builds the operations a matrix cell runs.
func cellOps(row OpRow, kind string) []map[string]any {
	id := kindTargets[kind]
	set := func(fields map[string]any) []map[string]any {
		op := map[string]any{"op": "object.set", "at": id}
		maps.Copy(op, fields)
		return []map[string]any{op}
	}
	one := func(fields map[string]any) []map[string]any {
		op := map[string]any{"op": row.Op}
		maps.Copy(op, fields)
		return []map[string]any{op}
	}
	switch row.Op + "/" + row.Field {
	case "object.add/":
		switch kind {
		case "shape":
			return one(map[string]any{"kind": "shape", "name": "New", "geom": "rect", "box": []float64{10, 10, 50, 50}})
		case "connector":
			return one(map[string]any{"kind": "connector", "name": "New", "geom": "straightConnector1", "from": []float64{10, 10}, "to": []float64{60, 60}})
		case "picture":
			return one(map[string]any{"kind": "picture", "name": "New", "geom": "rect", "box": []float64{10, 10, 50, 50}, "image": probePNG(), "fit": "stretch"})
		case "group":
			return one(map[string]any{"kind": "group", "name": "New", "geom": "rect", "box": []float64{10, 10, 50, 50}})
		case "other":
			return one(map[string]any{"kind": "graphicFrame", "name": "New", "geom": "rect", "box": []float64{10, 10, 50, 50}})
		}
	case "object.set/name":
		return set(map[string]any{"name": "Renamed"})
	case "object.set/box, rot, flip_h, flip_v":
		return set(map[string]any{"box": []float64{40, 40, 60, 30}, "rot": 15})
	case "object.set/geom, adj":
		return set(map[string]any{"geom": "roundRect", "adj": map[string]any{"adj": 20000}})
	case "object.set/path":
		return set(map[string]any{"path": "M0 0 L10 10 Z"})
	case "object.set/custom (cxn, rect)":
		return set(map[string]any{"path": "M0 0 L10 10 Z", "custom": map[string]any{"cxn": []any{[]any{0, 10, 10}}, "rect": []any{"l", "t", "r", "b"}}})
	case "object.add/path":
		return one(map[string]any{"kind": kind, "name": "New", "path": "M 0 0 L 50 20 L 0 40", "box": []float64{10, 10, 50, 40}, "fill": "none"})
	case "object.add/at (a group)":
		if kind == "connector" {
			return one(map[string]any{"kind": "connector", "name": "New", "at": "s7", "geom": "straightConnector1", "from": []float64{80, 300}, "to": []float64{120, 310}})
		}
		if kind == "picture" {
			return one(map[string]any{"kind": "picture", "name": "New", "at": "s7", "box": []float64{80, 300, 20, 20}, "image": probePNG()})
		}
		return one(map[string]any{"kind": "shape", "name": "New", "at": "s7", "geom": "rect", "box": []float64{80, 300, 20, 20}})
	case "object.set/fill":
		return set(map[string]any{"fill": map[string]any{"hex": "#FF0000"}})
	case "object.set/line":
		return set(map[string]any{"line": map[string]any{"color": map[string]any{"hex": "#FF0000"}, "w": 2}})
	case "object.set/from_glue, to_glue":
		return set(map[string]any{"from_glue": "s6:1"})
	case "object.set/from, to":
		return set(map[string]any{"from": []float64{10, 10}, "to": []float64{80, 40}})
	case "object.set/text (anchor, wrap, insets)":
		return set(map[string]any{"text": map[string]any{"anchor": "ctr", "wrap": "none", "insets": []float64{1, 2, 3, 4}}})
	case "object.set/text (autofit, vert, columns, flags)":
		return set(map[string]any{"text": map[string]any{"autofit": "shrink", "vert": "vert270", "num_col": 2, "rtl_col": true}})
	case "object.set/fill none":
		return set(map[string]any{"fill": "none"})
	case "object.set/descr, title, hidden":
		return set(map[string]any{"descr": "Alt text", "title": "Title", "hidden": true})
	case "object.set/tx_box":
		return set(map[string]any{"tx_box": true})
	case "object.set/image, svg":
		return set(map[string]any{"image": probePNG(), "svg": "none"})
	case "object.set/crop, fit, fill_rect":
		return set(map[string]any{"crop": []float64{10, 0, 10, 0}, "fit": "stretch", "fill_rect": []float64{1, 1, 1, 1}})
	case "object.set/locks":
		return set(map[string]any{"locks": map[string]any{"no_change_aspect": true}})
	case "object.set/html":
		return set(map[string]any{"html": "<b>New</b> text"})
	case "object.move/":
		return one(map[string]any{"at": id, "dx": 10, "dy": 5})
	case "object.delete/":
		return one(map[string]any{"at": id})
	case "object.duplicate/":
		return one(map[string]any{"at": id})
	case "object.reorder/":
		return one(map[string]any{"at": id, "to": "back"})
	case "group.create/":
		return one(map[string]any{"at": []string{id, "s6"}, "name": "Group"})
	case "group.ungroup/":
		// The probe group's child takes the group's fill, which
		// ungrouping refuses; give it its own first.
		return []map[string]any{{"op": "object.set", "at": "s8", "fill": "none"}, {"op": "group.ungroup", "at": id}}
	case "group.fit/":
		return one(map[string]any{"at": id})
	case "text.set/":
		return one(map[string]any{"at": id, "html": "New"})
	case "text.replace/":
		return one(map[string]any{"at": id, "select": "e", "with": "E", "all": true})
	case "xml.set/":
		return one(map[string]any{"at": id, "xml": "<p:sp/>"})
	case "slide.add/":
		return one(map[string]any{})
	case "slide.delete/", "slide.duplicate/":
		return one(map[string]any{"slide": "slide1"})
	}
	return nil
}

// runCell applies a cell's operations to a fresh probe deck.
func runCell(t *testing.T, dir string, opsm []map[string]any) error {
	f := newFixture(t)
	// Give the "other" object text-free content that a delete or move
	// can act on; the frame as built is enough.
	src := f.write(t, dir)
	dst := strings.TrimSuffix(src, ".pptx") + "-out.pptx"
	var compiled []ops.Op
	for _, m := range opsm {
		c, err := compilePlan(m)
		if err != nil {
			return err
		}
		compiled = append(compiled, c...)
	}
	return ops.ApplyTo(src, dst, compiled...)
}

// TestOperationsMatchBehaviour runs every cell of the operation matrix.
func TestOperationsMatchBehaviour(t *testing.T) {
	loadUniverse(t)
	dir, err := os.MkdirTemp("", "coverage-ops")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	var bad []string
	for _, row := range Operations {
		for _, kind := range Kinds {
			want, ok := row.Cells[kind]
			if !ok {
				continue
			}
			name := row.Op
			if row.Field != "" {
				name += " " + row.Field
			}
			plan := cellOps(row, kind)
			if plan == nil {
				bad = append(bad, fmt.Sprintf("%s on %s: no plan", name, kind))
				continue
			}
			err := runCell(t, dir, plan)
			var got OpStatus
			switch {
			case err == nil:
				got = Implemented
			case errors.Is(err, ops.ErrNotImplemented):
				got = NotYet
			default:
				got = RefusedByDesign
			}
			t.Logf("%s on %s: %s (%v)", name, kind, got, err)
			match := got == want || got == RefusedByDesign && want == OpNonGoal
			if !match {
				bad = append(bad, fmt.Sprintf("%s on %s: declared %s, got %s (%v)", name, kind, want, got, err))
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("%d operation cells disagree with the plans:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}
