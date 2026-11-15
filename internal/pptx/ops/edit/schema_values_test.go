package edit

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckAttr(t *testing.T) {
	for _, c := range []struct {
		ctype, attr, v string
		ok             bool
		why            string
	}{
		{"a:CT_PresetGeometry2D", "prst", "roundRect", true, ""},
		{"a:CT_PresetGeometry2D", "prst", "notAShape", false, "a:ST_ShapeType (want one of line, lineInv"},
		{"a:CT_PresetLineDashProperties", "val", "sysDash", true, ""},
		{"a:CT_PresetLineDashProperties", "val", "foo", false, "a:ST_PresetLineDashVal"},
		{"a:CT_LineProperties", "cap", "rnd", true, ""},
		{"a:CT_LineProperties", "cap", "round", false, "want one of rnd, sq, flat"},
		{"a:CT_LineProperties", "w", "20116800", true, ""},
		{"a:CT_LineProperties", "w", "20116801", false, "from 0 to 20116800"},
		{"a:CT_LineProperties", "w", "-1", false, "from 0 to 20116800"},
		{"a:CT_LineEndProperties", "type", "foo", false, "a:ST_LineEndType"},
		{"a:CT_LineEndProperties", "w", "huge", false, "want one of sm, med, lg"},
		{"a:CT_LineEndProperties", "len", "lg", true, ""},
		{"a:CT_SchemeColor", "val", "accent7", false, "a:ST_SchemeColorVal"},
		{"a:CT_SystemColor", "val", "windowText", true, ""},
		{"a:CT_SystemColor", "val", "notASystemColour", false, "a:ST_SystemColorVal"},
		{"a:CT_SystemColor", "lastClr", "00FF00", true, ""},
		{"a:CT_SystemColor", "lastClr", "00FF0", false, "hex"},
		{"a:CT_PresetColor", "val", "red", true, ""},
		{"a:CT_PresetColor", "val", "reddish", false, "a:ST_PresetColorVal"},
		{"a:CT_SRgbColor", "val", "4472C4", true, ""},
		{"a:CT_SRgbColor", "val", "4472C", false, "6 hex digits"},
		{"a:CT_HslColor", "hue", "21599999", true, ""},
		{"a:CT_HslColor", "hue", "21600000", false, "from 0 to 21599999"},
		{"a:CT_HslColor", "sat", "-5", true, ""},
		{"a:CT_HslColor", "sat", "50%", true, ""},
		{"a:CT_HslColor", "sat", "x", false, "or want the pattern"},
		{"a:CT_PositiveFixedPercentage", "val", "100001", false, "from 0 to 100000"},
		{"a:CT_TextBodyProperties", "anchor", "ctr", true, ""},
		{"a:CT_TextBodyProperties", "anchor", "middle", false, "want one of t, ctr, b, just, dist"},
		{"a:CT_TextBodyProperties", "wrap", "none", true, ""},
		{"a:CT_TextBodyProperties", "lIns", "-91440", true, ""},
		{"a:CT_TextBodyProperties", "lIns", "2147483648", false, "a:ST_Coordinate32"},
		{"a:CT_TextCharacterProperties", "sz", "99", false, "from 100 to 400000"},
		{"a:CT_Connection", "idx", "-1", false, "xsd:unsignedInt"},
	} {
		err := CheckAttr(c.ctype, c.attr, c.v)
		switch {
		case c.ok && err != nil:
			t.Errorf("%s@%s=%q: %v", c.ctype, c.attr, c.v, err)
		case !c.ok && (!errors.Is(err, ErrSchema) || !strings.Contains(err.Error(), c.why)):
			t.Errorf("%s@%s=%q: err = %v, want ErrSchema naming %q", c.ctype, c.attr, c.v, err, c.why)
		}
	}
	if err := CheckAttr("a:CT_LineProperties", "nope", "x"); err == nil || errors.Is(err, ErrSchema) {
		t.Errorf("untabulated attribute: %v", err)
	}
}

func TestChildType(t *testing.T) {
	for _, ct := range []string{"a:CT_SRgbColor", "a:CT_SchemeColor", "a:CT_SystemColor", "a:CT_PresetColor", "a:CT_HslColor", "a:CT_ScRgbColor"} {
		if typ, ok := ChildType(ct, "a:lumMod"); !ok || typ != "a:CT_Percentage" {
			t.Errorf("%s lumMod: %q %v", ct, typ, ok)
		}
		if _, ok := ChildType(ct, "a:bogus"); ok {
			t.Errorf("%s bogus: tabulated", ct)
		}
	}
	if _, ok := AttrType("a:CT_InverseTransform", "val"); ok {
		t.Error("inv has a val")
	}
}
