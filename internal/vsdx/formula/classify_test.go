package formula

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
)

// TestAllowed checks the FL2 rows: "", Inh, No Formula, 2.5, 1IN,
// 0.5DL, 25.4MM, 90DEG, THEMEVAL() and THEMEGUARD(RGB(0,0,0)) are
// allowed; GUARD(1), Width*0.5, Sheet.10!Width*0.25,
// PAR(PNT(Sheet.135!Connections.X1,Sheet.135!Connections.Y1)) and
// 1FT+2IN are refused.
func TestAllowed(t *testing.T) {
	rows := []struct {
		in    string
		allow bool
	}{
		{"", true},
		{"Inh", true},
		{"No Formula", true},
		{"2.5", true},
		{"1IN", true},
		{"0.5DL", true},
		{"25.4MM", true},
		{"90DEG", true},
		{"THEMEVAL()", true},
		{"THEMEGUARD(RGB(0,0,0))", true},
		{"GUARD(1)", false},
		{"Width*0.5", false},
		{"Sheet.10!Width*0.25", false},
		{"PAR(PNT(Sheet.135!Connections.X1,Sheet.135!Connections.Y1))", false},
		{"1FT+2IN", false},
		{"1XX", false},
		{"-1IN", true},
		{"1in", true},
		{" ", true},
		{"Width", false},
		{`"abc"`, false},
		{"1E400", false},
		{`THEMEVAL("LineWeight",0DT)`, false},
		{"THEMEGUARD(1", false},
		{"THEMEGUARD(1))", false},
		{"InhX", false},
		{"1E308FT", false},
		{"100%", false},
	}
	for _, row := range rows {
		if got := Allowed(row.in); got != row.allow {
			t.Errorf("Allowed(%q) = %v, want %v", row.in, got, row.allow)
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
// is at most 8.
func TestAllowedScaling(t *testing.T) {
	setup := func(n int) (func() error, error) {
		var b strings.Builder
		for range n {
			b.WriteString("Width*0.5+")
		}
		b.WriteString("Width")
		formula := b.String()
		return func() error {
			if Allowed(formula) {
				return fmt.Errorf("Allowed of %d terms: allowed, want refused", n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Allowed formula terms", setup)
	laws.TimeScales(t, "vsdx Allowed formula terms", setup)
}
