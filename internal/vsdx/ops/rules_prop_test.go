package ops

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestPropNameInvalidCC8 checks the CC8 rows: a prop.add name "my
// name", "Größe" and "Gr\u00f6\u00dfe" is invalid_operation at
// ops[0].name, before open (ParsePlan opens no file).
func TestPropNameInvalidCC8(t *testing.T) {
	names := []string{"my name", "Größe", "Gr\u00f6\u00dfe"}
	for _, name := range names {
		text := `{"ops":[{"op":"prop.add","at":"page0/s1","name":` + strconv.Quote(name) + `,"type":"string","value":"x"}]}`
		p, errs := ParsePlan([]byte(text))
		if p != nil || len(errs) != 1 {
			t.Errorf("prop.add name %q: plan %v errors %v, want one invalid_operation", name, p, errs)
			continue
		}
		e := errs[0]
		if e.Code != cli.CodeInvalidOperation || e.Path != "ops[0].name" {
			t.Errorf("prop.add name %q: got %s@%s, want invalid_operation@ops[0].name", name, e.Code, e.Path)
		}
		if e.Message != "name must be ASCII letters, digits and _, not starting with a digit" {
			t.Errorf("prop.add name %q: message %q, want the ASCII name message", name, e.Message)
		}
	}
}

// TestPropDateInvalidCC8 checks the CC8 rows: a prop.set value
// {"date":"garbage"} and a prop.add of type date with that value are
// invalid_operation at ops[0].value, before open.
func TestPropDateInvalidCC8(t *testing.T) {
	texts := []string{
		`{"ops":[{"op":"prop.set","at":"page0/s1/prop:Due","value":{"date":"garbage"}}]}`,
		`{"ops":[{"op":"prop.add","at":"page0/s1","name":"Due","type":"date","value":{"date":"garbage"}}]}`,
	}
	for _, text := range texts {
		p, errs := ParsePlan([]byte(text))
		if p != nil || len(errs) != 1 {
			t.Errorf("%s: plan %v errors %v, want one invalid_operation", text, p, errs)
			continue
		}
		e := errs[0]
		if e.Code != cli.CodeInvalidOperation || e.Path != "ops[0].value" {
			t.Errorf("%s: got %s@%s, want invalid_operation@ops[0].value", text, e.Code, e.Path)
		}
	}
}

// TestPropSetIdentityCC8 checks the CC8 rows: a prop.set of V6
// page0/s1/prop:MasterOnly (shown, inherited) to "m" writes nothing
// (no changed, no local row stored), and to "n" is stored locally and
// reads back "n".
func TestPropSetIdentityCC8(t *testing.T) {
	src := vsdxtest.Fixture(t, "v6-data")
	w, err := edit.Begin(src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	spec := &PropSetSpec{At: "page0/s1/prop:MasterOnly", Value: "m"}
	_, changed, _, _, err := spec.Run(w, nil, "", nil)
	if err != nil {
		t.Fatalf("prop.set MasterOnly to m: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("prop.set to shown value: changed = %v, want empty (no-op)", changed)
	}
	if err := w.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if err := w.Tx().Snapshot(out); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w.Tx().Rollback()
	if got := propDeletePageXML(t, out); strings.Contains(got, "MasterOnly") {
		t.Errorf("no-op sealed page stores a MasterOnly row: %s", got)
	}
	props := pageProps(t, out, "page0", "page0/s1")
	if !propDeleteHas(props, "page0/s1/prop:MasterOnly") {
		t.Errorf("props lost MasterOnly: %v", props)
	}
	w2, err := edit.Begin(out)
	if err != nil {
		t.Fatalf("Begin(out): %v", err)
	}
	spec = &PropSetSpec{At: "page0/s1/prop:MasterOnly", Value: "n"}
	_, changed, _, _, err = spec.Run(w2, nil, "", nil)
	if err != nil {
		t.Fatalf("prop.set MasterOnly to n: %v", err)
	}
	if len(changed) != 1 || changed[0] != "page0/s1/prop:MasterOnly" {
		t.Errorf("prop.set to n: changed = %v, want [page0/s1/prop:MasterOnly]", changed)
	}
	if err := w2.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	out2 := filepath.Join(t.TempDir(), "out2.vsdx")
	if err := w2.Tx().Snapshot(out2); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	w2.Tx().Rollback()
	if got := propDeletePageXML(t, out2); !strings.Contains(got, `<Row N='MasterOnly'>`) {
		t.Errorf("sealed page has no local MasterOnly row: %s", got)
	}
	props = pageProps(t, out2, "page0", "page0/s1")
	for _, p := range props {
		if p.ID == "page0/s1/prop:MasterOnly" && string(p.Value) != `"n"` {
			t.Errorf("MasterOnly = %+v %s, want value n stored locally", p, p.Value)
		}
	}
}

// TestPropNonASCIIRowsCC8 checks the CC8 rows: a stored row "Größe"
// (and one "my name") on a test-built V6-style shape is read, set to
// "x" and deleted by its address.
func TestPropNonASCIIRowsCC8(t *testing.T) {
	for _, name := range []string{"Größe", "my name"} {
		t.Run(name, func(t *testing.T) {
			src := vsdxtest.Fixture(t, "v1-minimal")
			w, err := edit.Begin(src)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			add := &PropAddSpec{At: "page0/s1", Name: name, Type: "string", Value: "orig"}
			if _, _, _, _, err := add.Run(w, nil, "", nil); err != nil {
				t.Fatalf("prop.add %q: %v", name, err)
			}
			if err := w.Verify(); err != nil {
				t.Fatalf("Verify: %v", err)
			}
			out := filepath.Join(t.TempDir(), "out.vsdx")
			if err := w.Tx().Snapshot(out); err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			w.Tx().Rollback()
			id := "page0/s1/prop:" + name
			props := pageProps(t, out, "page0", "page0/s1")
			if !propDeleteHas(props, id) {
				t.Fatalf("read lists no %s: %v", id, props)
			}
			w2, err := edit.Begin(out)
			if err != nil {
				t.Fatalf("Begin(out): %v", err)
			}
			set := &PropSetSpec{At: id, Value: "x"}
			if _, _, _, _, err := set.Run(w2, nil, "", nil); err != nil {
				t.Fatalf("prop.set %s: %v", id, err)
			}
			if err := w2.Verify(); err != nil {
				t.Fatalf("Verify: %v", err)
			}
			out2 := filepath.Join(t.TempDir(), "out2.vsdx")
			if err := w2.Tx().Snapshot(out2); err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			w2.Tx().Rollback()
			props = pageProps(t, out2, "page0", "page0/s1")
			found := false
			for _, p := range props {
				if p.ID == id {
					found = true
					if string(p.Value) != `"x"` {
						t.Errorf("%s = %s, want x", id, p.Value)
					}
				}
			}
			if !found {
				t.Fatalf("read lists no %s after set: %v", id, props)
			}
			w3, err := edit.Begin(out2)
			if err != nil {
				t.Fatalf("Begin(out2): %v", err)
			}
			del := &PropDeleteSpec{At: id}
			if _, _, deleted, _, err := del.Run(w3, nil, "", nil); err != nil {
				t.Fatalf("prop.delete %s: %v", id, err)
			} else if len(deleted) != 1 || deleted[0] != id {
				t.Errorf("prop.delete %s: deleted = %v, want [%s]", id, deleted, id)
			}
			if err := w3.Verify(); err != nil {
				t.Fatalf("Verify: %v", err)
			}
			out3 := filepath.Join(t.TempDir(), "out3.vsdx")
			if err := w3.Tx().Snapshot(out3); err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			w3.Tx().Rollback()
			props = pageProps(t, out3, "page0", "page0/s1")
			if propDeleteHas(props, id) {
				t.Errorf("props still has %s after delete: %v", id, props)
			}
		})
	}
}
