package ops_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestAliasSubAddress checks the CC12 rows: a $name alias takes
// what may follow the address it names ($r/prop:Cost, $r/field:1,
// $p/s5), and an undefined alias names itself in the refusal.
func TestAliasSubAddress(t *testing.T) {
	t.Run("add-and-set-atomically", func(t *testing.T) {
		src := vsdxtest.Fixture(t, "v6-data")
		plan := parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","geom":"rect","as":"r","box":[72,72,144,72]},{"op":"prop.add","at":"$r","name":"Cost","type":"number","value":1},{"op":"prop.set","at":"$r/prop:Cost","value":2}]}`)
		out := filepath.Join(t.TempDir(), "out.vsdx")
		res, err := apply.Run(src, plan, apply.Options{Out: out})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		addr := res.Aliases["r"]
		if addr == "" {
			t.Fatalf("aliases %v, want r defined", res.Aliases)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read out: %v", err)
		}
		raw, err := read.Page(data, "B", "page0", read.Options{})
		if err != nil {
			t.Fatalf("read.Page: %v", err)
		}
		var v struct {
			Objects []struct {
				ID    string `json:"id"`
				Props []struct {
					ID    string          `json:"id"`
					Value json.RawMessage `json:"value"`
				} `json:"props"`
			} `json:"objects"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("Unmarshal Page: %v", err)
		}
		for _, o := range v.Objects {
			if o.ID != addr {
				continue
			}
			for _, p := range o.Props {
				if p.ID == addr+"/prop:Cost" && string(p.Value) == "2" {
					return
				}
			}
		}
		t.Errorf("read %s props show no Cost 2, page %s", addr, raw)
	})
	t.Run("missing-prop", func(t *testing.T) {
		src := vsdxtest.Fixture(t, "v6-data")
		plan := parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","geom":"rect","as":"r","box":[72,72,144,72]},{"op":"prop.set","at":"$r/prop:Nope","value":1}]}`)
		out := filepath.Join(t.TempDir(), "out.vsdx")
		_, err := apply.Run(src, plan, apply.Options{Out: out})
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeNotFound || ce.Path != "ops[1].at" {
			t.Errorf("got %v, want not_found at ops[1].at", err)
		}
	})
	t.Run("undefined-alias", func(t *testing.T) {
		src := vsdxtest.Fixture(t, "v6-data")
		plan := parsePlan(t, `{"ops":[{"op":"prop.set","at":"$undefined/prop:X","value":1}]}`)
		out := filepath.Join(t.TempDir(), "out.vsdx")
		_, err := apply.Run(src, plan, apply.Options{Out: out})
		var ce *cli.Error
		if !errors.As(err, &ce) {
			t.Fatalf("got %v, want *cli.Error", err)
		}
		if ce.Code != cli.CodeNotFound || ce.Path != "ops[0].at" {
			t.Errorf("got %s at %q, want not_found at ops[0].at", ce.Code, ce.Path)
		}
		if !strings.Contains(ce.Message, "alias $undefined is not defined") {
			t.Errorf("message %q, want alias $undefined is not defined", ce.Message)
		}
	})
	t.Run("page-alias-and-text", func(t *testing.T) {
		src := vsdxtest.Fixture(t, "v6-data")
		plan := parsePlan(t, `{"ops":[{"op":"page.add","name":"P","as":"p"},{"op":"object.add","at":"$p","geom":"rect","box":[72,72,144,72]}]}`)
		out := filepath.Join(t.TempDir(), "out.vsdx")
		res, err := apply.Run(src, plan, apply.Options{Out: out})
		if err != nil {
			t.Fatalf("Run page alias: %v", err)
		}
		if res.Aliases["p"] == "" {
			t.Fatalf("aliases %v, want p", res.Aliases)
		}
		src = vsdxtest.Fixture(t, "v6-data")
		plan = parsePlan(t, `{"ops":[{"op":"object.add","at":"page0","geom":"rect","as":"r","box":[72,72,144,72]},{"op":"text.set","at":"$r","html":"<p>Hi</p>"}]}`)
		out = filepath.Join(t.TempDir(), "out2.vsdx")
		res, err = apply.Run(src, plan, apply.Options{Out: out})
		if err != nil {
			t.Fatalf("Run text after add: %v", err)
		}
		if res.Aliases["r"] == "" {
			t.Fatalf("aliases %v, want r", res.Aliases)
		}
	})
	t.Run("page-alias-shape", func(t *testing.T) {
		src := vsdxtest.Fixture(t, "v4-groups")
		w, err := edit.Begin(src)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer w.Tx().Rollback()
		aliases := map[string]string{"p": "page0"}
		as, _, _, _, err := (&ops.ObjectAddSpec{At: "$p/s10", Geom: "rect", Box: []float64{72, 72, 72, 72}}).Run(w, aliases, "pt", nil)
		if err != nil {
			t.Fatalf("object.add at $p/s10: %v", err)
		}
		if as == "" {
			t.Error("object.add at $p/s10 created nothing")
		}
		if err := w.Verify(); err != nil {
			t.Fatalf("Verify: %v", err)
		}
		_, _, _, _, err = (&ops.ObjectSetSpec{At: "$p/s11", Name: new("Renamed")}).Run(w, aliases, "pt", nil)
		if err != nil {
			t.Errorf("object.set at $p/s11: %v", err)
		}
	})
	t.Run("sub-address-syntax", func(t *testing.T) {
		for _, text := range []string{
			`{"ops":[{"op":"prop.set","at":"$r/prop:Cost","value":2}]}`,
			`{"ops":[{"op":"prop.set","at":"$p/s5/prop:Cost","value":2}]}`,
			`{"ops":[{"op":"prop.set","at":"$r/field:1","value":2}]}`,
			`{"ops":[{"op":"prop.set","at":"$p/layer:X","value":2}]}`,
			`{"ops":[{"op":"object.add","at":"$p/s5","geom":"rect","box":[0,0,10,10]}]}`,
			`{"ops":[{"op":"object.add","at":"page0","kind":"connector","from_glue":"$p/s5:2","to":[10,10]}]}`,
			`{"ops":[{"op":"object.set","at":"page0/s1","to_glue":"$p/s5"}]}`,
		} {
			if _, errs := ops.ParsePlan([]byte(text)); len(errs) != 0 {
				t.Errorf("ParsePlan(%s): %v, want no error", text, errs)
			}
		}
		got, err := ops.ResolveAlias("$r/prop:Cost", map[string]string{"r": "page0/s1"}, "at")
		if err != nil || got != "page0/s1/prop:Cost" {
			t.Errorf("ResolveAlias prop = %q, %v", got, err)
		}
		got, err = ops.ResolveAlias("$r/field:1", map[string]string{"r": "page0/s1"}, "at")
		if err != nil || got != "page0/s1/field:1" {
			t.Errorf("ResolveAlias field = %q, %v", got, err)
		}
		got, err = ops.ResolveAlias("$p/s5", map[string]string{"p": "page0"}, "at")
		if err != nil || got != "page0/s5" {
			t.Errorf("ResolveAlias shape = %q, %v", got, err)
		}
		got, err = ops.ResolveAlias("$p/layer:X", map[string]string{"p": "page0"}, "at")
		if err != nil || got != "page0/layer:X" {
			t.Errorf("ResolveAlias layer = %q, %v", got, err)
		}
	})
	t.Run("layer-refused", func(t *testing.T) {
		src := vsdxtest.Fixture(t, "v6-data")
		w, err := edit.Begin(src)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer w.Tx().Rollback()
		aliases := map[string]string{"p": "page0"}
		_, _, _, _, err = (&ops.PropSetSpec{At: "$p/layer:X", Value: float64(1)}).Run(w, aliases, "", nil)
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed {
			t.Errorf("prop.set at layer: got %v, want operation_failed", err)
		}
		_, _, _, _, err = (&ops.TextSetSpec{At: "$p/layer:X", HTML: "<p>x</p>"}).Run(w, aliases, "", nil)
		if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed {
			t.Errorf("text.set at layer: got %v, want operation_failed", err)
		}
	})
}
