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
// Width*0.5 with Width unknown is an error.
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

// TestEvalOutside checks the FL3 outside rows: SQRT(4),
// Sheet.5!Width, ThePage!PageHeight and 1XX are ErrOutsideGrammar,
// and 65 nested parentheses are an error, never a panic. Width
// unknown is ErrUnknownCell, not outside the grammar.
func TestEvalOutside(t *testing.T) {
	rows := []string{
		"SQRT(4)", "Sheet.5!Width", "ThePage!PageHeight", "1XX",
		"0.5DL", "Width^2", "Width%Height", "Width&Height",
		"Width=Height", "Width<>Height", `"abc"`, "Width,Height",
		"Width;Height", "Width*", "*Width", "+Width", "()",
		"(Width", "Width)", "Width Height", "1IN*",
		"GUARD(Width)", "Width[", "1E400", "",
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
		{"25.4MM", nil},
		{"SQRT(4)", nil},
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
