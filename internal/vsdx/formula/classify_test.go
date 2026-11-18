package formula

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
)

// TestAllowed checks the FL2 rows and the CC1 split (gate ruling
// 2026-11-17): Allowed, the formulas a write may replace with a value,
// is "", Inh, No Formula, a constant (2.5, 1IN, 0.5DL, 25.4MM, 90DEG,
// -1IN), THEMEVAL() and THEMEGUARD(RGB(0,0,0)); Evaluable adds the
// closed-grammar dependents a write recomputes but never replaces:
// each evaluable 1-D form of the CC1 table with its corpus spelling,
// GUARD(1), Width*0.5 and 1FT+2IN. Both refuse the still-refused forms
// (GUARD(TEXTWIDTH(TheText)), User.DefaultWidth, Sheet.N!Width*k,
// IF(...), SETATREF(...)), the glue forms, a lone reference and an
// overflowing or unknown-unit constant.
func TestAllowed(t *testing.T) {
	rows := []struct {
		in          string
		allow, eval bool
	}{
		{"", true, true},
		{"Inh", true, true},
		{"No Formula", true, true},
		{"2.5", true, true},
		{"1IN", true, true},
		{"0.5DL", true, true},
		{"25.4MM", true, true},
		{"90DEG", true, true},
		{"THEMEVAL()", true, true},
		{"THEMEGUARD(RGB(0,0,0))", true, true},
		{"GUARD(1)", false, true},
		{"Width*0.5", false, true},
		{"Height*0.5", false, true},
		{"GUARD(EndX-BeginX)", false, true},
		{"GUARD(EndY-BeginY)", false, true},
		{"GUARD(0.19685039370079DL)", false, true},
		{"GUARD(0DA)", false, true},
		{"GUARD((BeginX+EndX)/2)", false, true},
		{"GUARD((BeginY+EndY)/2)", false, true},
		{"GUARD(Width*0.5)", false, true},
		{"GUARD(Height*0.5)", false, true},
		{"SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)", false, true},
		{"ATAN2(EndY-BeginY,EndX-BeginX)", false, true},
		{"(BeginX+EndX)/2", false, true},
		{"(BeginY+EndY)/2", false, true},
		{"Width^2", false, true},
		{"SQRT(4)", false, true},
		{"Sheet.10!Width*0.25", false, false},
		{"Sheet.5!Width*0.5", false, false},
		{"PAR(PNT(Sheet.135!Connections.X1,Sheet.135!Connections.Y1))", false, false},
		{"_WALKGLUE(BegTrigger,EndTrigger,WalkPreference)", false, false},
		{"_XFTRIGGER(Sheet.41!EventXFMod)", false, false},
		{"GUARD(TEXTWIDTH(TheText))", false, false},
		{"User.DefaultWidth", false, false},
		{"IF(Width,1,0)", false, false},
		{"SETATREF(Width)", false, false},
		{"1FT+2IN", false, true},
		{"1XX", false, false},
		{"-1IN", true, true},
		{"1in", true, true},
		{" ", true, true},
		{"Width", false, false},
		{`"abc"`, false, false},
		{"1E400", false, false},
		{`THEMEVAL("LineWeight",0DT)`, false, false},
		{"THEMEGUARD(1", false, false},
		{"THEMEGUARD(1))", false, false},
		{"InhX", false, false},
		{"1E308FT", false, false},
		{"100%", false, false},
	}
	for _, row := range rows {
		if got := Allowed(row.in); got != row.allow {
			t.Errorf("Allowed(%q) = %v, want %v", row.in, got, row.allow)
		}
		if got := Evaluable(row.in); got != row.eval {
			t.Errorf("Evaluable(%q) = %v, want %v", row.in, got, row.eval)
		}
	}
}

// TestConstant checks the FL2 rows: Constant("25.4MM") = 1,
// Constant("1FT") = 12, Constant("90DEG") = pi/2, and an unknown
// unit 1XX is not ok.
func TestConstant(t *testing.T) {
	rows := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"25.4MM", 1, true},
		{"1FT", 12, true},
		{"90DEG", math.Pi / 2, true},
		{"1IN", 1, true},
		{"2.5", 2.5, true},
		{"0.5DL", 0.5, true},
		{"-1IN", -1, true},
		{"1E-4", 0.0001, true},
		{"72PT", 1, true},
		{"36PT", 0.5, true},
		{"2.54CM", 1, true},
		{"1M", 1 / 0.0254, true},
		{"180DEG", math.Pi, true},
		{"1RAD", 1, true},
		{"1DA", 1, true},
		{"1DP", 1, true},
		{"1DT", 1, true},
		{"1XX", 0, false},
		{"1FT+2IN", 0, false},
		{"Width*0.5", 0, false},
		{"", 0, false},
		{"Inh", 0, false},
		{"No Formula", 0, false},
		{"THEMEVAL()", 0, false},
		{"THEMEGUARD(1)", 0, false},
		{"1E400", 0, false},
		{"1E308FT", 0, false},
	}
	for _, row := range rows {
		got, ok := Constant(row.in)
		if ok != row.ok {
			t.Errorf("Constant(%q) ok = %v, want %v", row.in, ok, row.ok)
			continue
		}
		if ok && got != row.want {
			t.Errorf("Constant(%q) = %v, want %v", row.in, got, row.want)
		}
	}
}

// TestAllowedScaling checks the scaling row: Allowed on a formula of
// laws.Small (1000) and laws.Large (4000) terms allocates at most
// 5.5 times as much (laws.Scales) and the laws.TimeScales time ratio
// is at most 8. The formula is still refused (Sheet.5! refs), so
// Allowed and Evaluable (which walks the grammar) must scan it and
// refuse it at every size.
func TestAllowedScaling(t *testing.T) {
	setup := func(n int) (func() error, error) {
		var b strings.Builder
		for range n {
			b.WriteString("Sheet.5!Width*0.5+")
		}
		b.WriteString("Sheet.5!Width")
		formula := b.String()
		return func() error {
			if Allowed(formula) || Evaluable(formula) {
				return fmt.Errorf("Allowed or Evaluable of %d terms: allowed, want refused", n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Allowed formula terms", setup)
	laws.TimeScales(t, "vsdx Allowed formula terms", setup)
}
