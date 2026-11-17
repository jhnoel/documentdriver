package model

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
)

// visitCopy is what a visit test keeps of one visited object: the
// fields Find and the other rows compare, copied out before the
// object is dropped. Glue, node and origins are excluded: VisitPage
// builds no glue and keeps no tree.
type visitCopy struct {
	ID, Kind, Type, Name, NameU, Master, Stored, Parent string
	ShapeID, Z                                          int
	HasText                                             bool
	Text                                                string
	Layers                                              []string
	Props                                               []string
}

// copyVisit returns the visitCopy of o, with its props as their IDs
// and printed values.
func copyVisit(o *Object) visitCopy {
	c := visitCopy{ID: o.ID, Kind: string(o.Kind), Type: o.Type, Name: o.Name, NameU: o.NameU,
		Master: o.Master, Stored: o.StoredMaster, Parent: o.Parent, ShapeID: int(o.ShapeID), Z: o.Z,
		HasText: o.HasText, Text: o.Text, Layers: o.Layers}
	for _, pr := range o.Props {
		c.Props = append(c.Props, pr.ID)
	}
	return c
}

// collectVisit walks fixture's page 0 with VisitPage, collecting a
// visitCopy per object, and returns the copies and the notes.
func collectVisit(t *testing.T, fixture string) ([]visitCopy, []doc.Note) {
	t.Helper()
	d, r := openFixture(t, fixture)
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("%s: no page 0", fixture)
	}
	var got []visitCopy
	notes, err := VisitPage(d, r, e, Options{}, func(o *Object) bool {
		got = append(got, copyVisit(o))
		return true
	})
	if err != nil {
		t.Fatalf("VisitPage(%s): %v", fixture, err)
	}
	return got, notes
}

// TestVisitPageMatchesBuild checks the RM2 visit rows: VisitPage's
// objects, in paint order with duplicate-ID suffixes, and its notes
// equal BuildPage's on the fixtures.
func TestVisitPageMatchesBuild(t *testing.T) {
	for _, fixture := range []string{"v1-minimal", "v2-master", "v4-groups", "v5-glue", "v10-defects", "v11-ns2011", "v15-foreign"} {
		d, r := openFixture(t, fixture)
		e, ok := d.Page(0)
		if !ok {
			t.Fatalf("%s: no page 0", fixture)
		}
		built, err := BuildPage(d, r, e, Options{})
		if err != nil {
			t.Fatalf("BuildPage(%s): %v", fixture, err)
		}
		var want []visitCopy
		for _, o := range built.Objects {
			want = append(want, copyVisit(o))
		}
		got, notes := collectVisit(t, fixture)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: VisitPage objects = %#v, want BuildPage %#v", fixture, got, want)
		}
		if !slices.Equal(notes, built.Notes) {
			t.Errorf("%s: VisitPage notes = %v, want BuildPage %v", fixture, notes, built.Notes)
		}
	}
}

// TestVisitPageDuplicateIDs checks the duplicate-ID row on V10: the
// two stored ID 7 shapes are visited as page0/s7 and page0/s7~2, and
// the page's connect note (FromSheet 0) is in the notes.
func TestVisitPageDuplicateIDs(t *testing.T) {
	got, notes := collectVisit(t, "v10-defects")
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	if len(ids) < 2 || ids[0] != "page0/s7" || ids[1] != "page0/s7~2" {
		t.Errorf("visited IDs = %v, want [page0/s7 page0/s7~2 ...]", ids)
	}
	found := false
	for _, n := range notes {
		if n == (doc.Note{At: "page0", Reason: "connect from sheet names no shape"}) {
			found = true
		}
	}
	if !found {
		t.Errorf("notes = %v, want the V10 connect note", notes)
	}
}

// TestVisitPageStoredMaster checks the StoredMaster row: a shape
// stores its own Master, a group sub-shape storing none has
// StoredMaster "" while its effective Master is its group's.
func TestVisitPageStoredMaster(t *testing.T) {
	got, _ := collectVisit(t, "v2-master")
	byID := map[string]visitCopy{}
	for _, c := range got {
		byID[c.ID] = c
	}
	if byID["page0/s1"].Stored != "master2" {
		t.Errorf("s1 StoredMaster = %q, want master2", byID["page0/s1"].Stored)
	}
	if s4 := byID["page0/s4"]; s4.Stored != "" || s4.Master != "master3" {
		t.Errorf("s4 StoredMaster/Master = %q/%q, want \"\"/master3", s4.Stored, s4.Master)
	}
}

// TestBuildPageReleaseNodes checks the ReleaseNodes row: with
// ReleaseNodes every Object.Node is nil after BuildPage; without it
// every object keeps its node.
func TestBuildPageReleaseNodes(t *testing.T) {
	d, r := openFixture(t, "v2-master")
	e, ok := d.Page(0)
	if !ok {
		t.Fatal("v2-master: no page 0")
	}
	kept, err := BuildPage(d, r, e, Options{})
	if err != nil {
		t.Fatalf("BuildPage: %v", err)
	}
	for _, o := range kept.Objects {
		if o.Node == nil {
			t.Errorf("without ReleaseNodes: %s Node is nil, want kept", o.ID)
		}
		if o.StoredMaster == "" && o.ID != "page0/s4" && o.ID != "page0/s5" {
			t.Errorf("without ReleaseNodes: %s StoredMaster is empty", o.ID)
		}
	}
	released, err := BuildPage(d, r, e, Options{ReleaseNodes: true})
	if err != nil {
		t.Fatalf("BuildPage with ReleaseNodes: %v", err)
	}
	if len(released.Objects) != len(kept.Objects) {
		t.Fatalf("released has %d objects, want %d", len(released.Objects), len(kept.Objects))
	}
	for _, o := range released.Objects {
		if o.Node != nil {
			t.Errorf("with ReleaseNodes: %s Node is not nil, want released", o.ID)
		}
	}
	if released.Objects[0].StoredMaster != "master2" {
		t.Errorf("released s1 StoredMaster = %q, want master2 (from the field, not the node)", released.Objects[0].StoredMaster)
	}
}

// TestVisitPageCheckpoints checks the checkpoint row: "notes" is
// called once, after the shapes, and "shape" at most every 1024
// top-level shapes.
func TestVisitPageCheckpoints(t *testing.T) {
	d, r := openGenerated(t, 2048)
	e, ok := d.Page(0)
	if !ok {
		t.Fatal("Generated(2048): no page 0")
	}
	var got []string
	_, err := VisitPage(d, r, e, Options{Checkpoint: func(s string) { got = append(got, s) }}, func(*Object) bool { return true })
	if err != nil {
		t.Fatalf("VisitPage(Generated(2048)): %v", err)
	}
	if len(got) == 0 || got[len(got)-1] != "notes" {
		t.Fatalf("checkpoints = %v, want shape checkpoints then notes", got)
	}
	shape := 0
	for _, s := range got[:len(got)-1] {
		if s != "shape" {
			t.Fatalf("checkpoints = %v, want only shape before notes", got)
		}
		shape++
	}
	// 2048 rectangles plus their connectors are 3072 top-level
	// shapes, so exactly three "shape" checkpoints (at 1024, 2048
	// and 3072).
	if shape != 3 {
		t.Errorf("shape checkpoints = %d, want 3 for 3072 top-level shapes", shape)
	}
}

// TestVisitPageEarlyStop checks the visit contract: returning false
// stops the walk at that object, with the notes gathered so far and
// no Connects pass.
func TestVisitPageEarlyStop(t *testing.T) {
	d, r := openFixture(t, "v5-glue")
	e, ok := d.Page(0)
	if !ok {
		t.Fatal("v5-glue: no page 0")
	}
	count := 0
	_, err := VisitPage(d, r, e, Options{}, func(*Object) bool {
		count++
		return false
	})
	if err != nil {
		t.Fatalf("VisitPage: %v", err)
	}
	if count != 1 {
		t.Errorf("visited %d objects, want 1 (stop at the first)", count)
	}
}

// TestVisitPageUnreadable checks VisitPage on V10's pages: page 2
// is malformed and page 4 has no part, so both are errors.
func TestVisitPageUnreadable(t *testing.T) {
	d, r := openFixture(t, "v10-defects")
	for _, id := range []uint32{2, 4} {
		e, ok := d.Page(id)
		if !ok {
			t.Fatalf("v10-defects: no page %d", id)
		}
		if _, err := VisitPage(d, r, e, Options{}, func(*Object) bool { return true }); err == nil {
			t.Errorf("VisitPage(page%d) succeeded, want an error", id)
		}
	}
	if _, err := VisitPage(nil, r, nil, Options{}, nil); err == nil {
		t.Error("VisitPage(nil) succeeded, want an error")
	}
	// PageBytes on a good page returns the part's bytes twice.
	e, ok := d.Page(0)
	if !ok {
		t.Fatal("v10-defects: no page 0")
	}
	first, err := d.PageBytes(e)
	if err != nil || first == "" {
		t.Errorf("PageBytes: %q, %v, want the page bytes", first, err)
	}
	second, err := d.PageBytes(e)
	if err != nil || second != first {
		t.Errorf("second PageBytes differs: %v", err)
	}
}

// TestScalingVisitPage checks the scaling row: VisitPage on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes)
// allocates at most 5.5 times as much (laws.Scales) and the
// laws.TimeScales time ratio is at most 8.
func TestScalingVisitPage(t *testing.T) {
	setup := func(n int) (func() error, error) {
		d, r := openGenerated(t, n)
		e, ok := d.Page(0)
		if !ok {
			return nil, fmt.Errorf("Generated(%d): no page 0", n)
		}
		return func() error {
			count := 0
			_, err := VisitPage(d, r, e, Options{}, func(*Object) bool {
				count++
				return true
			})
			if err != nil {
				return err
			}
			if want := n + n/2; count != want {
				return fmt.Errorf("Generated(%d): visited %d objects, want %d", n, count, want)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx VisitPage shapes", setup)
	laws.TimeScales(t, "vsdx VisitPage shapes", setup)
}
