package formula

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
)

// TestGlueForm checks the CC1 GlueForm rows:
// PAR(PNT(Sheet.40!Connections.X2,Sheet.40!Connections.Y2)) is
// point 40:2, _WALKGLUE(BegTrigger,EndTrigger,WalkPreference) is
// whole, _XFTRIGGER(Sheet.41!EventXFMod) is trigger 41,
// _XFTRIGGER(EventXFMod) is trigger 0, and PNT(1,2) and
// PAR(PNT(Sheet.40!Width,1)) are not glue forms.
func TestGlueForm(t *testing.T) {
	rows := []struct {
		in   string
		want Glue
		ok   bool
	}{
		{"PAR(PNT(Sheet.40!Connections.X2,Sheet.40!Connections.Y2))", Glue{Kind: GluePoint, Sheet: 40, Point: 2}, true},
		{"PAR(PNT(Sheet.135!Connections.X1,Sheet.135!Connections.Y1))", Glue{Kind: GluePoint, Sheet: 135, Point: 1}, true},
		{"_WALKGLUE(BegTrigger,EndTrigger,WalkPreference)", Glue{Kind: GlueWhole}, true},
		{"_WALKGLUE(EndTrigger,BegTrigger,WalkPreference)", Glue{Kind: GlueWhole}, true},
		{"_XFTRIGGER(Sheet.41!EventXFMod)", Glue{Kind: GlueTrigger, Sheet: 41}, true},
		{"_XFTRIGGER(EventXFMod)", Glue{Kind: GlueTrigger, Sheet: 0}, true},
		{"PNT(1,2)", Glue{}, false},
		{"PAR(PNT(Sheet.40!Width,1))", Glue{}, false},
		{"PAR(PNT(Sheet.40!Connections.X2,Sheet.41!Connections.Y2))", Glue{}, false},
		{"PAR(PNT(Sheet.40!Connections.X2,Sheet.40!Connections.Y3))", Glue{}, false},
		{"PAR(PNT(Sheet.40!Connections.X2,Sheet.40!Connections.Y2)", Glue{}, false},
		{"PAR(PNT(Sheet.40!Connections.X2,Sheet.40!Connections.Y2))x", Glue{}, false},
		{"_WALKGLUE(BegTrigger,EndTrigger)", Glue{}, false},
		{"_WALKGLUE(BegTrigger,EndTrigger,Width)", Glue{}, false},
		{"_XFTRIGGER(Sheet.41!Width)", Glue{}, false},
		{"_XFTRIGGER(Width)", Glue{}, false},
		{"GUARD(EndX-BeginX)", Glue{}, false},
		{"Width*0.5", Glue{}, false},
		{"", Glue{}, false},
		{"Inh", Glue{}, false},
	}
	for _, row := range rows {
		got, ok := GlueForm(row.in)
		if ok != row.ok {
			t.Errorf("GlueForm(%q) ok = %v, want %v", row.in, ok, row.ok)
			continue
		}
		if ok && got != row.want {
			t.Errorf("GlueForm(%q) = %+v, want %+v", row.in, got, row.want)
		}
	}
}

// TestGlueFormScaling checks the scaling row: GlueForm on a formula
// of laws.Small (1000) and laws.Large (4000) terms allocates at most
// 5.5 times as much (laws.Scales) and the laws.TimeScales time ratio
// is at most 8.
func TestGlueFormScaling(t *testing.T) {
	setup := func(n int) (func() error, error) {
		var b strings.Builder
		for range n {
			b.WriteString("Width*0.5+")
		}
		b.WriteString("Width")
		formula := b.String()
		return func() error {
			if _, ok := GlueForm(formula); ok {
				return fmt.Errorf("GlueForm of %d terms: glue, want not glue", n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx GlueForm formula terms", setup)
	laws.TimeScales(t, "vsdx GlueForm formula terms", setup)
}
