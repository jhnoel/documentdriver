package read

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// goldenBytes returns the bytes of the designer-authored golden name.
func goldenBytes(t *testing.T, name string) []byte {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	b, err := os.ReadFile(root + "/testdata/vsdx/golden/" + name)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

// pageIDs returns the object ids of a Page result in order.
func pageIDs(t *testing.T, b []byte) []string {
	t.Helper()
	var v struct {
		Objects []struct {
			ID string `json:"id"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("Unmarshal Page: %v", err)
	}
	ids := make([]string, len(v.Objects))
	for i, o := range v.Objects {
		ids[i] = o.ID
	}
	return ids
}

// TestPageGoldens checks the R2 golden rows: Page plus a newline equals
// byte for byte v1-page0.json (V1 default), v1-page0-full.json (V1 full),
// v1-page0-in.json (V1 in), v2-page0.json, v4-page0.json and v5-page0.json.
func TestPageGoldens(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture string
		golden  string
		opts    Options
	}{
		{"v1 default", "v1-minimal", "v1-page0.json", Options{}},
		{"v1 full", "v1-minimal", "v1-page0-full.json", Options{Detail: "full"}},
		{"v1 in", "v1-minimal", "v1-page0-in.json", Options{Units: "in"}},
		{"v2", "v2-master", "v2-page0.json", Options{}},
		{"v4", "v4-groups", "v4-page0.json", Options{}},
		{"v5", "v5-glue", "v5-page0.json", Options{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Page(fixtureBytes(t, tc.fixture), "BASE", "page0", tc.opts)
			if err != nil {
				t.Fatalf("Page(%s): %v", tc.fixture, err)
			}
			if want := goldenBytes(t, tc.golden); string(got) != string(want) {
				t.Errorf("Page = %s, want %s", got, want)
			}
		})
	}
}

// TestPageIDs checks the R2 ids row: --ids page0/s3 on V2 gives s3, s4, s5.
func TestPageIDs(t *testing.T) {
	got, err := Page(fixtureBytes(t, "v2-master"), "BASE", "page0", Options{IDs: []string{"page0/s3"}})
	if err != nil {
		t.Fatalf("Page(--ids page0/s3): %v", err)
	}
	want := []string{"page0/s3", "page0/s4", "page0/s5"}
	if ids := pageIDs(t, got); !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}

// TestPageRegion checks the R2 region rows: 0,0,150,150 on V1 includes
// s1, 300,300,10,10 excludes it.
func TestPageRegion(t *testing.T) {
	input := fixtureBytes(t, "v1-minimal")
	in := [4]float64{0, 0, 150, 150}
	got, err := Page(input, "BASE", "page0", Options{Region: &in})
	if err != nil {
		t.Fatalf("Page(--region 0,0,150,150): %v", err)
	}
	if ids := pageIDs(t, got); !reflect.DeepEqual(ids, []string{"page0/s1"}) {
		t.Errorf("in-region ids = %v, want [page0/s1]", ids)
	}
	out := [4]float64{300, 300, 10, 10}
	got, err = Page(input, "BASE", "page0", Options{Region: &out})
	if err != nil {
		t.Fatalf("Page(--region 300,300,10,10): %v", err)
	}
	if ids := pageIDs(t, got); len(ids) != 0 {
		t.Errorf("out-region ids = %v, want none", ids)
	}
}

// TestPageNotFound checks the R2 unknown-page row: an unknown page and
// a non-page address are not_found.
func TestPageNotFound(t *testing.T) {
	input := fixtureBytes(t, "v1-minimal")
	for _, address := range []string{"page9", "bogus", "page0/s1", "master9"} {
		_, err := Page(input, "BASE", address, Options{})
		var e *cli.Error
		if !errors.As(err, &e) || e.Code != cli.CodeNotFound {
			t.Errorf("Page(%s) error = %v, want a not_found error", address, err)
		}
	}
	for _, in := range [][]byte{[]byte("not a zip"), {}} {
		if got, err := Page(in, "B", "page0", Options{}); err == nil || got != nil {
			t.Errorf("Page(%q) = %q, %v, want nil, an error", in, got, err)
		}
	}
}

// TestPageUnreadable checks the R2 V10 row: page2 is unreadable_target.
func TestPageUnreadable(t *testing.T) {
	_, err := Page(fixtureBytes(t, "v10-defects"), "BASE", "page2", Options{})
	var e *cli.Error
	if !errors.As(err, &e) || e.Code != cli.CodeUnreadableTarget {
		t.Fatalf("Page(page2) error = %v, want an unreadable_target error", err)
	}
}

// TestPageMaster checks a master read: V2 master2 has master2/s5 and
// master3 has master3/s5, master3/s6, master3/s7.
func TestPageMaster(t *testing.T) {
	input := fixtureBytes(t, "v2-master")
	got, err := Page(input, "BASE", "master2", Options{})
	if err != nil {
		t.Fatalf("Page(master2): %v", err)
	}
	var v struct {
		Page    string `json:"page"`
		Objects []struct {
			ID string `json:"id"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if v.Page != "master2" || len(v.Objects) != 1 || v.Objects[0].ID != "master2/s5" {
		t.Errorf("master2 = page %q objects %v, want master2 [master2/s5]", v.Page, v.Objects)
	}
	got, err = Page(input, "BASE", "master3", Options{})
	if err != nil {
		t.Fatalf("Page(master3): %v", err)
	}
	if ids := pageIDs(t, got); !reflect.DeepEqual(ids, []string{"master3/s5", "master3/s6", "master3/s7"}) {
		t.Errorf("master3 ids = %v, want master3/s5, master3/s6, master3/s7", ids)
	}
}

// TestPageSites checks detail sites on V5: s1 sites are [180,144] and [108,144].
func TestPageSites(t *testing.T) {
	got, err := Page(fixtureBytes(t, "v5-glue"), "BASE", "page0", Options{Detail: "sites"})
	if err != nil {
		t.Fatalf("Page(--detail sites): %v", err)
	}
	var v struct {
		Objects []struct {
			ID    string      `json:"id"`
			Sites [][]float64 `json:"sites"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, o := range v.Objects {
		if o.ID != "page0/s1" {
			continue
		}
		if !reflect.DeepEqual(o.Sites, [][]float64{{180, 144}, {108, 144}}) {
			t.Errorf("s1 sites = %v, want [[180 144] [108 144]]", o.Sites)
		}
		return
	}
	t.Errorf("no s1 in %v", v.Objects)
}

// TestPageLayers checks V8: page0 background_page page3, two layers,
// and s1's layer addresses.
func TestPageLayers(t *testing.T) {
	got, err := Page(fixtureBytes(t, "v8-pages"), "BASE", "page0", Options{})
	if err != nil {
		t.Fatalf("Page(v8 page0): %v", err)
	}
	var v struct {
		BackgroundPage string `json:"background_page"`
		Layers         []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Index   int    `json:"index"`
			Visible bool   `json:"visible"`
			Print   bool   `json:"print"`
		} `json:"layers"`
		Objects []struct {
			ID     string   `json:"id"`
			Layers []string `json:"layers"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if v.BackgroundPage != "page3" {
		t.Errorf("background_page = %q, want page3", v.BackgroundPage)
	}
	if len(v.Layers) != 2 || v.Layers[0].ID != "page0/layer:Flowchart" || v.Layers[0].Index != 0 || !v.Layers[0].Visible || !v.Layers[0].Print {
		t.Errorf("Layers[0] = %v, want Flowchart index 0 visible print", v.Layers)
	}
	if len(v.Layers) != 2 || v.Layers[1].ID != "page0/layer:Connector" || v.Layers[1].Index != 1 || v.Layers[1].Visible {
		t.Errorf("Layers[1] = %v, want Connector index 1 not visible", v.Layers)
	}
	if len(v.Objects) != 1 || !reflect.DeepEqual(v.Objects[0].Layers, []string{"page0/layer:Flowchart", "page0/layer:Connector"}) {
		t.Errorf("Objects = %v, want s1 with both layers", v.Objects)
	}
}

// TestPagePropsV6 checks V6 props: Size unit MM, Hidden hidden, Linked linked.
func TestPagePropsV6(t *testing.T) {
	got, err := Page(fixtureBytes(t, "v6-data"), "BASE", "page0", Options{})
	if err != nil {
		t.Fatalf("Page(v6): %v", err)
	}
	var v struct {
		Objects []struct {
			ID    string `json:"id"`
			Props []struct {
				ID     string `json:"id"`
				Type   string `json:"type"`
				Value  any    `json:"value"`
				Unit   string `json:"unit"`
				Hidden bool   `json:"hidden"`
				Linked bool   `json:"linked"`
			} `json:"props"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	var foundSize, foundHidden, foundLinked bool
	for _, o := range v.Objects {
		for _, p := range o.Props {
			if p.ID == "page0/s1/prop:Size" {
				foundSize = p.Unit == "MM"
			}
			if p.ID == "page0/s1/prop:Hidden" {
				foundHidden = p.Hidden
			}
			if p.ID == "page0/s1/prop:Linked" {
				foundLinked = p.Linked
			}
		}
	}
	if !foundSize || !foundHidden || !foundLinked {
		t.Errorf("V6 props Size unit / Hidden / Linked = %v / %v / %v, want MM / true / true in %v", foundSize, foundHidden, foundLinked, v.Objects)
	}
}

// TestPageOmittedV10 checks full on V10: s9's omitted holds the themed fill reason.
func TestPageOmittedV10(t *testing.T) {
	got, err := Page(fixtureBytes(t, "v10-defects"), "BASE", "page0", Options{Detail: "full"})
	if err != nil {
		t.Fatalf("Page(v10 full): %v", err)
	}
	var v struct {
		Objects []struct {
			ID      string `json:"id"`
			Omitted []struct {
				Prop   string `json:"prop"`
				Reason string `json:"reason"`
			} `json:"omitted"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, o := range v.Objects {
		if o.ID != "page0/s9" {
			continue
		}
		want := struct {
			Prop   string `json:"prop"`
			Reason string `json:"reason"`
		}{Prop: "fill", Reason: "themed value not derived"}
		for _, om := range o.Omitted {
			if om.Prop == want.Prop && om.Reason == want.Reason {
				return
			}
		}
		t.Errorf("s9 omitted = %v, want fill themed value not derived", o.Omitted)
		return
	}
	t.Errorf("no s9 in full page")
}

// TestPageOtherV15 checks V15 kinds: s2 other with type Foreign, s3 other type Guide.
func TestPageOtherV15(t *testing.T) {
	got, err := Page(fixtureBytes(t, "v15-foreign"), "BASE", "page0", Options{})
	if err != nil {
		t.Fatalf("Page(v15): %v", err)
	}
	var v struct {
		Objects []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Type string `json:"type"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	byID := map[string]struct {
		Kind string `json:"kind"`
		Type string `json:"type"`
	}{}
	for _, o := range v.Objects {
		byID[o.ID] = struct {
			Kind string `json:"kind"`
			Type string `json:"type"`
		}{Kind: o.Kind, Type: o.Type}
	}
	if byID["page0/s1"].Kind != "picture" {
		t.Errorf("s1 kind = %q, want picture", byID["page0/s1"].Kind)
	}
	if byID["page0/s2"].Kind != "other" || byID["page0/s2"].Type != "Foreign" {
		t.Errorf("s2 = %v, want other Foreign", byID["page0/s2"])
	}
	if byID["page0/s3"].Kind != "other" || byID["page0/s3"].Type != "Guide" {
		t.Errorf("s3 = %v, want other Guide", byID["page0/s3"])
	}
}

// TestScalingPage checks the scaling row: Page on Generated(1000) and
// Generated(4000) (N = 1000, 4N = 4000 shapes) allocates at most 5.5
// times as much (laws.Scales) and the laws.TimeScales time ratio is at most 8.
func TestScalingPage(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			got, err := Page(data, "B", "page0", Options{})
			if err != nil {
				return err
			}
			var v struct {
				Objects []struct {
					ID string `json:"id"`
				} `json:"objects"`
			}
			if err := json.Unmarshal(got, &v); err != nil {
				return err
			}
			if want := n + n/2; len(v.Objects) != want {
				return fmt.Errorf("Generated(%d): len(Objects) = %d, want %d", n, len(v.Objects), want)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Page", setup)
	laws.TimeScales(t, "vsdx Page", setup)
}
