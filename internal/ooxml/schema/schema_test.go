package schema

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestGeneratorPin keeps the generator's SHA-256 check equal to the pin
// the rest of the module (the coverage universe) uses.
func TestGeneratorPin(t *testing.T) {
	src, err := os.ReadFile("gen_schema.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `const zipSHA256 = "`+ZipSHA256+`"`) {
		t.Errorf("gen_schema.go does not check ZipSHA256 %s", ZipSHA256)
	}
	if !strings.Contains(string(src), ZipURL) {
		t.Errorf("gen_schema.go does not name ZipURL")
	}
}

// TestTablesResolve checks that every tabulated name stays in its own
// vocabulary's table and that every simple type an attribute names, and
// every union member, is tabulated or an XSD built-in.
func TestTablesResolve(t *testing.T) {
	vocabs := []struct {
		prefix string
		types  map[string][]Slot
		attrs  map[string]map[string]string
		simple map[string]Simple
	}{
		{"p", pmlTypes, pmlAttrs, pmlSimple},
		{"a", dmlTypes, dmlAttrs, dmlSimple},
		{"w", wmlTypes, wmlAttrs, wmlSimple},
		{"x", smlTypes(), smlAttrs(), smlSimple()},
		{"s", sharedTypes, sharedAttrs, sharedSimple},
	}
	resolves := func(typ string) bool {
		if strings.HasPrefix(typ, "xsd:") {
			return true
		}
		_, ok := SimpleType(typ)
		return ok
	}
	for _, v := range vocabs {
		for name := range v.types {
			if !strings.HasPrefix(name, v.prefix+":") {
				t.Errorf("%s in the %s table", name, v.prefix)
			}
			if _, ok := Slots(name); !ok {
				t.Errorf("Slots(%s) not found", name)
			}
		}
		for ct, attrs := range v.attrs {
			if !strings.HasPrefix(ct, v.prefix+":") {
				t.Errorf("attributes of %s in the %s table", ct, v.prefix)
			}
			for a, typ := range attrs {
				if !resolves(typ) {
					t.Errorf("%s@%s: simple type %s not tabulated", ct, a, typ)
				}
			}
		}
		for name, st := range v.simple {
			if !strings.HasPrefix(name, v.prefix+":") {
				t.Errorf("%s in the %s table", name, v.prefix)
			}
			for _, m := range st.Union {
				if !resolves(m) {
					t.Errorf("%s: union member %s not tabulated", name, m)
				}
			}
			if st.List != "" && !resolves(st.List) {
				t.Errorf("%s: list item type %s not tabulated", name, st.List)
			}
		}
	}
	for ct := range wmlUntabulated {
		if _, ok := wmlTypes[ct]; ok {
			t.Errorf("%s is both tabulated and untabulated", ct)
		}
	}
	for ct := range smlUntabulated() {
		if _, ok := smlTypes()[ct]; ok {
			t.Errorf("%s is both tabulated and untabulated", ct)
		}
	}
}

func TestWordprocessingML(t *testing.T) {
	order, ok := ChildOrder("w:CT_P")
	if !ok || len(order) != 2 || !slices.Equal(order[0], []string{"w:pPr"}) || !slices.Contains(order[1], "w:r") {
		t.Fatalf("w:CT_P: %v %v", order, ok)
	}
	slots, _ := Slots("w:CT_P")
	if !slots[1].Multi || slots[0].Multi {
		t.Errorf("w:CT_P multi: %v %v", slots[0].Multi, slots[1].Multi)
	}
	// The pinned transitional schema makes a run's properties
	// (EG_RPrBase) a repeated choice, so their order is free; the change
	// record comes after them.
	slots, _ = Slots("w:CT_RPr")
	b, _ := Find(slots, "w", "b")
	sz, c := Find(slots, "w", "sz")
	chg, _ := Find(slots, "w", "rPrChange")
	if b < 0 || sz != b || !slots[b].Multi || chg <= b || c.Type != "w:CT_HpsMeasure" {
		t.Errorf("w:CT_RPr: b %d, sz %d (%s), rPrChange %d", b, sz, c.Type, chg)
	}
	// An extension's content follows its base type's.
	if typ, ok := ChildType("w:CT_SdtBlock", "w:sdtContent"); !ok || typ != "w:CT_SdtContentBlock" {
		t.Errorf("w:CT_SdtBlock sdtContent: %q %v", typ, ok)
	}
	// Elements of schemas that are not tabulated keep their prefix.
	slots, _ = Slots("w:CT_Body")
	if k, c := Find(slots, "m", "oMath"); k < 0 || c.Type != "" {
		t.Errorf("w:CT_Body m:oMath: %d %+v", k, c)
	}
	if why, ok := Untabulated("w:CT_Tbl"); !ok || !strings.Contains(why, "two positions") {
		t.Errorf("w:CT_Tbl: %q %v", why, ok)
	}
	if _, ok := Untabulated("w:CT_P"); ok {
		t.Error("w:CT_P untabulated")
	}
}

func TestCheckAttr(t *testing.T) {
	for _, c := range []struct {
		ctype, attr, v string
		ok             bool
		why            string
	}{
		{"a:CT_PresetGeometry2D", "prst", "roundRect", true, ""},
		{"a:CT_PresetGeometry2D", "prst", "notAShape", false, "a:ST_ShapeType"},
		{"a:CT_LineProperties", "w", "20116801", false, "from 0 to 20116800"},
		{"w:CT_Jc", "val", "center", true, ""},
		{"w:CT_Jc", "val", "middle", false, "w:ST_Jc"},
		{"w:CT_OnOff", "val", "true", true, ""},
		{"w:CT_OnOff", "val", "on", true, ""},
		{"w:CT_OnOff", "val", "yes", false, "s:ST_OnOff"},
		{"w:CT_Color", "val", "auto", true, ""},
		{"w:CT_Color", "val", "FF0000", true, ""},
		{"w:CT_Color", "val", "red", false, "w:ST_HexColor"},
		{"w:CT_HpsMeasure", "val", "24", true, ""},
	} {
		err := CheckAttr(c.ctype, c.attr, c.v)
		switch {
		case c.ok && err != nil:
			t.Errorf("%s@%s=%q: %v", c.ctype, c.attr, c.v, err)
		case !c.ok && (!errors.Is(err, ErrSchema) || !strings.Contains(err.Error(), c.why)):
			t.Errorf("%s@%s=%q: err = %v, want ErrSchema naming %q", c.ctype, c.attr, c.v, err, c.why)
		}
	}
	if err := CheckAttr("w:CT_Jc", "nope", "x"); err == nil || errors.Is(err, ErrSchema) {
		t.Errorf("untabulated attribute: %v", err)
	}
	if got := Enum("a:ST_TextAnchoringType"); !slices.Equal(got, []string{"t", "ctr", "b", "just", "dist"}) {
		t.Errorf("Enum: %v", got)
	}
}
