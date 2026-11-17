package model

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestOffPageV1 checks the OPF read-level rows: V1 as it stands has
// no off-page hazard; V1 with its page size 11 x 8.5 inches written
// by page.set orientation landscape, read back, has exactly
// [{off-page, page0/s1, "box [108,-54,72,36] lies outside the page
// [0,0,792,612]"}].
func TestOffPageV1(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	if got := buildPageOf(t, d, r, 0).Hazards; len(got) != 0 {
		t.Errorf("V1 as is: Hazards = %+v, want none", got)
	}
	w, err := edit.Begin(vsdxtest.Fixture(t, "v1-minimal"))
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() { w.Tx().Rollback() })
	if _, _, _, _, err := (&ops.PageSetSpec{At: "page0", Orientation: "landscape"}).Run(w, map[string]string{}, "pt", nil); err != nil {
		t.Fatalf("page.set: %v", err)
	}
	snap := filepath.Join(t.TempDir(), "landscape.vsdx")
	if err := w.Tx().Snapshot(snap); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	pkg, err := opc.Open(snap)
	if err != nil {
		t.Fatalf("opc.OpenBytes: %v", err)
	}
	t.Cleanup(func() {
		if err := pkg.Close(); err != nil {
			t.Errorf("close snapshot: %v", err)
		}
	})
	land, err := doc.Open(pkg)
	if err != nil {
		t.Fatalf("doc.Open(snapshot): %v", err)
	}
	got := buildPageOf(t, land, inherit.New(land), 0).Hazards
	want := []Hazard{{Kind: "off-page", ID: "page0/s1", Detail: "box [108,-54,72,36] lies outside the page [0,0,792,612]"}}
	if !slices.Equal(got, want) {
		t.Errorf("V1 landscape: Hazards = %+v, want %+v", got, want)
	}
}

// TestOffPage checks the OPF wholly-outside rows on synthetic pages:
// a box partly outside the page has no hazard, and neither do a
// group child, a master page, a page with no derived size, or an
// object with no box or ends; a 1-D shape whose ends are both
// outside on one side has one hazard for their bounding box, and a
// 1-D shape with an end on the page has none.
func TestOffPage(t *testing.T) {
	box := func(x, y, w, h float64) *geom.Box { return &geom.Box{X: x, Y: y, W: w, H: h} }
	pt := func(x, y float64) *geom.Pt { return &geom.Pt{X: x, Y: y} }
	rows := []struct {
		name string
		page *Page
		want []Hazard
	}{
		{"wholly above", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Box: box(108, -54, 72, 36)}}},
			[]Hazard{{Kind: "off-page", ID: "page0/s1", Detail: "box [108,-54,72,36] lies outside the page [0,0,612,792]"}}},
		{"partly outside left", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Box: box(-36, 100, 72, 36)}}}, nil},
		{"partly outside top", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Box: box(100, -18, 72, 36)}}}, nil},
		{"partly outside right", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Box: box(580, 100, 72, 36)}}}, nil},
		{"partly outside bottom", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Box: box(100, 770, 72, 36)}}}, nil},
		{"wholly right", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Box: box(612, 100, 72, 36)}}},
			[]Hazard{{Kind: "off-page", ID: "page0/s1", Detail: "box [612,100,72,36] lies outside the page [0,0,612,792]"}}},
		{"group child", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s9", Parent: "page0/s1", Box: box(108, -54, 72, 36)}}}, nil},
		{"master", &Page{ID: "master2", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "master2/s1", Box: box(108, -54, 72, 36)}}}, nil},
		{"no size", &Page{ID: "page0", Objects: []*Object{{ID: "page0/s1", Box: box(108, -54, 72, 36)}}}, nil},
		{"no box or ends", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1"}}}, nil},
		{"ends both above", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Kind: Connector, From: pt(100, -100), To: pt(200, -20)}}},
			[]Hazard{{Kind: "off-page", ID: "page0/s1", Detail: "box [100,-100,100,80] lies outside the page [0,0,612,792]"}}},
		{"ends one on page", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Kind: Connector, From: pt(100, -100), To: pt(200, 100)}}}, nil},
		{"ends across page", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Kind: Connector, From: pt(-100, 100), To: pt(800, 100)}}}, nil},
		{"one end only", &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: []*Object{{ID: "page0/s1", Kind: Connector, From: pt(100, -100)}}}, nil},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			offPage(row.page)
			if !slices.Equal(row.page.Hazards, row.want) {
				t.Errorf("Hazards = %+v, want %+v", row.page.Hazards, row.want)
			}
		})
	}
}

// TestScalingOffPage checks the scaling row: offPage over N and 4N
// shapes (N = 1000), every one wholly off the page, allocates at
// most 5.5 times as much (laws.Scales) and the laws.TimeScales time
// ratio is at most 8, linear in the shapes.
func TestScalingOffPage(t *testing.T) {
	setup := func(n int) (func() error, error) {
		return func() error {
			objects := make([]*Object, n)
			for i := range objects {
				objects[i] = &Object{ID: fmt.Sprintf("page0/s%d", i+1), Box: &geom.Box{X: 108, Y: -540, W: 72, H: 36}}
			}
			p := &Page{ID: "page0", W: 8.5, H: 11, HasSize: true, Objects: objects}
			offPage(p)
			if len(p.Hazards) != n {
				return fmt.Errorf("offPage: Hazards = %d, want %d", len(p.Hazards), n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx offPage", setup)
	laws.TimeScales(t, "vsdx offPage", setup)
}
