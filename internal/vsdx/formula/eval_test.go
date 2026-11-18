package formula

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
)

// evalCell is the cell callback of the FL3 rows: Width 2, Height 1,
// Geometry1.X1 2.5, everything else unknown.
func evalCell(ref string) (float64, bool) {
	switch ref {
	case "Width":
		return 2, true
	case "Height":
		return 1, true
	case "Geometry1.X1":
		return 2.5, true
	}
	return 0, false
}

// TestEval checks the FL3 rows: Width*0.5 with Width 2 is 1,
// Height*1-0.25IN with Height 1 is 0.75, (Width+Height)/2 is 1.5,
// -Width is -2, Geometry1.X1 (2.5) is 2.5, 25.4MM is 1, and
// Width*0.5 with Width unknown is an error. It also checks the
// CC1 rows in that grammar: GUARD, SQRT, ATAN2, ^ with a constant
// exponent and the DL and DA units.
func TestEval(t *testing.T) {
	rows := []struct {
		in   string
		want float64
	}{
		{"Width*0.5", 1},
		{"Height*1-0.25IN", 0.75},
		{"(Width+Height)/2", 1.5},
		{"-Width", -2},
		{"Geometry1.X1", 2.5},
		{"25.4MM", 1},
		{"2+3*4", 14},
		{"10-4/2", 8},
		{"(2+3)*4", 20},
		{"1FT", 12},
		{"72PT", 1},
		{"2.54CM", 1},
		{"1M", 1 / 0.0254},
		{"90DEG", math.Pi / 2},
		{"1RAD", 1},
		{"1IN", 1},
		{"  Width * 0.5 ", 1},
		{"--Width", 2},
		{"-(Width+Height)", -3},
		{"Width/Height", 2},
		{"1-2-3", -4},
		{"8/4/2", 1},
		{"GUARD(Width*0.5)", 1},
		{"GUARD(Width)", 2},
		{"GUARD(0.19685039370079DL)", 0.19685039370079},
		{"GUARD(0DA)", 0},
		{"SQRT(4)", 2},
		{"SQRT(Width^2)", 2},
		{"Width^2", 4},
		{"2^3", 8},
		{"(Width+Height)^2", 9},
		{"Width^0.5", math.Sqrt(2)},
		{"0.5DL", 0.5},
		{"0.25DL", 0.25},
		{"ATAN2(1,1)", math.Pi / 4},
	}
	for _, row := range rows {
		got, err := Eval(row.in, evalCell)
		if err != nil {
			t.Errorf("Eval(%q): %v, want %v", row.in, err, row.want)
			continue
		}
		if got != row.want {
			t.Errorf("Eval(%q) = %v, want %v", row.in, got, row.want)
		}
	}
}

// TestEvalOneD checks the CC1 Eval rows: GUARD(EndX-BeginX) with
// BeginX 3.7014 and EndX 4.8207 is 1.1193 (Connectors.vsdx
// page0/s42's stored Width), SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)
// from (2,2) to (2.75,2) is 0.75 (master30), ATAN2(EndY-BeginY,
// EndX-BeginX) from (0,0) to (1,1) is pi/4 to 1e-15, and
// GUARD((BeginX+EndX)/2) from 1 to 2 is 1.5.
func TestEvalOneD(t *testing.T) {
	rows := []struct {
		in    string
		cells map[string]float64
		want  float64
		tol   float64
	}{
		{"GUARD(EndX-BeginX)", map[string]float64{"BeginX": 3.7014, "EndX": 4.8207}, 1.1193, 1e-12},
		{"SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)", map[string]float64{"BeginX": 2, "BeginY": 2, "EndX": 2.75, "EndY": 2}, 0.75, 0},
		{"ATAN2(EndY-BeginY,EndX-BeginX)", map[string]float64{"BeginX": 0, "BeginY": 0, "EndX": 1, "EndY": 1}, math.Pi / 4, 1e-15},
		{"GUARD((BeginX+EndX)/2)", map[string]float64{"BeginX": 1, "EndX": 2}, 1.5, 0},
	}
	for _, row := range rows {
		cell := func(ref string) (float64, bool) {
			v, ok := row.cells[ref]
			return v, ok
		}
		got, err := Eval(row.in, cell)
		if err != nil {
			t.Errorf("Eval(%q): %v, want %v", row.in, err, row.want)
			continue
		}
		if row.tol == 0 {
			if got != row.want {
				t.Errorf("Eval(%q) = %v, want %v", row.in, got, row.want)
			}
		} else if math.Abs(got-row.want) > row.tol {
			t.Errorf("Eval(%q) = %v, want %v within %v", row.in, got, row.want, row.tol)
		}
	}
}

// TestEvalOutside checks the FL3 outside rows: Sheet.5!Width,
// ThePage!PageHeight and 1XX are ErrOutsideGrammar, as are the
// still-refused CC1 forms GUARD(TEXTWIDTH(TheText)), IF(...),
// SETATREF(...) and a ^ with a non-constant exponent, and 65
// nested parentheses are an error, never a panic. Width unknown
// is ErrUnknownCell, not outside the grammar.
func TestEvalOutside(t *testing.T) {
	rows := []string{
		"Sheet.5!Width", "ThePage!PageHeight", "1XX",
		"Width%Height", "Width&Height",
		"Width=Height", "Width<>Height", `"abc"`, "Width,Height",
		"Width;Height", "Width*", "*Width", "+Width", "()",
		"(Width", "Width)", "Width Height", "1IN*",
		"Width[", "1E400", "",
		"GUARD(TEXTWIDTH(TheText))", "IF(Width,1,0)", "SETATREF(Width)",
		"Width^Height", "Width^(Height)", "GUARD()", "GUARD(Width,Height)",
		"SQRT()", "SQRT(Width,Height)", "ATAN2(Width)", "ATAN2(Width,Height,Width)",
		"THEMEVAL()", "THEMEGUARD(Width)", "PAR(PNT(Sheet.40!Connections.X2,Sheet.40!Connections.Y2))",
		"_WALKGLUE(BegTrigger,EndTrigger,WalkPreference)", "_XFTRIGGER(EventXFMod)", "PNT(1,2)", "TEXTWIDTH(TheText)",
	}
	for _, in := range rows {
		_, err := Eval(in, evalCell)
		if !errors.Is(err, ErrOutsideGrammar) {
			t.Errorf("Eval(%q) error = %v, want ErrOutsideGrammar", in, err)
		}
	}
	deep := strings.Repeat("(", 65) + "1" + strings.Repeat(")", 65)
	if _, err := Eval(deep, evalCell); !errors.Is(err, ErrOutsideGrammar) {
		t.Errorf("Eval(65 nested parentheses) error = %v, want ErrOutsideGrammar", err)
	}
	// 64 nested parentheses are within the limit.
	within := strings.Repeat("(", 64) + "1" + strings.Repeat(")", 64)
	if got, err := Eval(within, evalCell); err != nil || got != 1 {
		t.Errorf("Eval(64 nested parentheses) = %v, %v; want 1, nil", got, err)
	}
	// 65 nested unary minuses are equally beyond the limit.
	unary := strings.Repeat("-", 65) + "Width"
	if _, err := Eval(unary, evalCell); !errors.Is(err, ErrOutsideGrammar) {
		t.Errorf("Eval(65 unary minuses) error = %v, want ErrOutsideGrammar", err)
	}
	// 65 nested GUARD calls are equally beyond the limit.
	guardDeep := strings.Repeat("GUARD(", 65) + "1" + strings.Repeat(")", 65)
	if _, err := Eval(guardDeep, evalCell); !errors.Is(err, ErrOutsideGrammar) {
		t.Errorf("Eval(65 nested GUARD calls) error = %v, want ErrOutsideGrammar", err)
	}
	// A negative square root and a power that overflows are
	// non-finite errors, not outside the grammar.
	if _, err := Eval("SQRT(-1)", evalCell); err == nil || errors.Is(err, ErrOutsideGrammar) {
		t.Errorf("Eval(SQRT(-1)) error = %v, want a non-outside error", err)
	}
	// Width unknown is in the grammar, so not ErrOutsideGrammar.
	_, err := Eval("Width*0.5", func(string) (float64, bool) { return 0, false })
	if !errors.Is(err, ErrUnknownCell) || errors.Is(err, ErrOutsideGrammar) {
		t.Errorf("Eval with Width unknown error = %v, want ErrUnknownCell only", err)
	}
	// A nil cell knows no reference but still evaluates constants.
	if _, err := Eval("Width", nil); !errors.Is(err, ErrUnknownCell) {
		t.Errorf("Eval(Width, nil) error = %v, want ErrUnknownCell", err)
	}
	if got, err := Eval("25.4MM", nil); err != nil || got != 1 {
		t.Errorf("Eval(25.4MM, nil) = %v, %v; want 1, nil", got, err)
	}
	// A non-finite result is an error, not a value and not outside.
	if _, err := Eval("1/0", evalCell); err == nil || errors.Is(err, ErrOutsideGrammar) {
		t.Errorf("Eval(1/0) error = %v, want a non-outside error", err)
	}
}

// TestRefs checks Refs: the same-shape references of a formula in
// the grammar, in order of occurrence, and nil for a formula
// outside it.
func TestRefs(t *testing.T) {
	rows := []struct {
		in   string
		want []string
	}{
		{"Width*0.5", []string{"Width"}},
		{"(Width+Height)/2", []string{"Width", "Height"}},
		{"Geometry1.X1*Width+Height", []string{"Geometry1.X1", "Width", "Height"}},
		{"Width+Width", []string{"Width", "Width"}},
		{"GUARD(Width*0.5)", []string{"Width"}},
		{"SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)", []string{"EndX", "BeginX", "EndY", "BeginY"}},
		{"ATAN2(EndY-BeginY,EndX-BeginX)", []string{"EndY", "BeginY", "EndX", "BeginX"}},
		{"Width^2", []string{"Width"}},
		{"25.4MM", nil},
		{"SQRT(4)", nil},
		{"GUARD(TEXTWIDTH(TheText))", nil},
		{"Width^Height", nil},
		{"IF(Width,1,0)", nil},
		{"Sheet.5!Width", nil},
		{"ThePage!PageHeight", nil},
		{"1XX", nil},
		{"Width*", nil},
		{"", nil},
	}
	for _, row := range rows {
		if got := Refs(row.in); !slices.Equal(got, row.want) {
			t.Errorf("Refs(%q) = %v, want %v", row.in, got, row.want)
		}
	}
}

// TestEvalScaling checks the scaling row: Eval and Refs on a
// formula of laws.Small (1000) and laws.Large (4000) terms allocate
// at most 5.5 times as much (laws.Scales) and the laws.TimeScales
// time ratio is at most 8.
func TestEvalScaling(t *testing.T) {
	setup := func(n int) (func() error, error) {
		var b strings.Builder
		for range n {
			b.WriteString("Width*0.5+")
		}
		b.WriteString("Width")
		formula := b.String()
		want := float64(n) + 2
		return func() error {
			got, err := Eval(formula, evalCell)
			if err != nil {
				return fmt.Errorf("Eval of %d terms: %w", n, err)
			}
			if got != want {
				return fmt.Errorf("Eval of %d terms = %v, want %v", n, got, want)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Eval formula terms", setup)
	laws.TimeScales(t, "vsdx Eval formula terms", setup)
}

// TestRefsScaling checks the scaling row for Refs: Refs on a
// formula of laws.Small (1000) and laws.Large (4000) terms
// allocates at most 5.5 times as much (laws.Scales) and the
// laws.TimeScales time ratio is at most 8.
func TestRefsScaling(t *testing.T) {
	setup := func(n int) (func() error, error) {
		var b strings.Builder
		for range n {
			b.WriteString("Width*0.5+")
		}
		b.WriteString("Width")
		formula := b.String()
		return func() error {
			refs := Refs(formula)
			if len(refs) != n+1 {
				return fmt.Errorf("Refs of %d terms: %d references, want %d", n, len(refs), n+1)
			}
			for _, ref := range refs {
				if ref != "Width" {
					return fmt.Errorf("Refs of %d terms: reference %q, want Width", n, ref)
				}
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Refs formula terms", setup)
	laws.TimeScales(t, "vsdx Refs formula terms", setup)
}

// FuzzEval feeds arbitrary strings to Eval and Refs: neither must
// ever panic (chunk FL3). A formula Eval reports as outside the
// grammar has no references, and a successful Eval is finite.
func FuzzEval(f *testing.F) {
	for _, s := range []string{
		"", "Width*0.5", "Height*1-0.25IN", "(Width+Height)/2", "-Width",
		"Geometry1.X1", "25.4MM", "SQRT(4)", "Sheet.5!Width",
		"ThePage!PageHeight", "1XX", "1/0", "Width/0", "1E400",
		"((((Width))))", "---Width", "Width**Height", "Width*", "+Width",
		"0.5DL", "90DEG", "1FT+2IN", "\xff\xfe", "Width<>Height",
		"GUARD(Width*0.5)", "SQRT(Width^2)", "ATAN2(Height,Width)", "Width^2",
		"Width^Height", "GUARD(TEXTWIDTH(TheText))", "GUARD(EndX-BeginX)",
		"SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)", "ATAN2(EndY-BeginY,EndX-BeginX)",
	} {
		f.Add(s)
	}
	f.Add(strings.Repeat("(", 65) + "1" + strings.Repeat(")", 65))
	f.Fuzz(func(t *testing.T, s string) {
		got, err := Eval(s, evalCell)
		if err == nil && (math.IsNaN(got) || math.IsInf(got, 0)) {
			t.Errorf("Eval(%q) = %v, want a finite value or an error", s, got)
		}
		if errors.Is(err, ErrOutsideGrammar) && Refs(s) != nil {
			t.Errorf("Eval(%q) is outside the grammar but Refs(%q) = %v, want nil", s, s, Refs(s))
		}
		refs := Refs(s)
		for _, ref := range refs {
			if strings.Contains(ref, "!") {
				t.Errorf("Refs(%q) = %v, want only same-shape references", s, refs)
				break
			}
		}
		if err == nil {
			if _, gerr := Eval(s, evalCell); gerr != nil {
				t.Errorf("Eval(%q) second call error = %v, want deterministic success", s, gerr)
			}
		}
	})
}
