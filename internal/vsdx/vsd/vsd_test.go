package vsd

import (
	"math"
	"math/rand/v2"
	"testing"
)

// TestIsMain checks the Visio main namespace spellings (chunk V1 rows).
func TestIsMain(t *testing.T) {
	for _, tc := range []struct {
		uri  string
		want bool
	}{
		{NSMain2012, true},
		{NSCore2011, true},
		{NSMain2012 + "/", false},
		{"", false},
	} {
		if got := IsMain(tc.uri); got != tc.want {
			t.Errorf("IsMain(%q) = %v, want %v", tc.uri, got, tc.want)
		}
	}
}

// TestNumber checks stored number parsing (chunk V1 rows).
func TestNumber(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
		ok   bool
	}{
		{"2.5", 2.5, true},
		{"-1E-3", -0.001, true},
		{"0", 0, true},
		{"0.01041666666666667", 0.01041666666666667, true},
		{"1.#INF", 0, false},
		{"", 0, false},
		{" 1", 0, false},
		{"NaN", 0, false},
		{"1e400", 0, false},
		{"0x10", 0, false},
	} {
		got, ok := Number(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("Number(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestBool checks stored boolean parsing (chunk V1 rows).
func TestBool(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
		ok   bool
	}{
		{"0", false, true},
		{"1", true, true},
		{"TRUE", true, true},
		{"false", false, true},
		{"2", false, false},
		{"", false, false},
	} {
		got, ok := Bool(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("Bool(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestColor checks stored colour parsing (chunk V1 rows).
func TestColor(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"#4472c4", "#4472C4", true},
		{"#FF0000", "#FF0000", true},
		{"3", "", false},
		{"Themed", "", false},
		{"#FFF", "", false},
		{"#GG0000", "", false},
	} {
		got, ok := Color(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("Color(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestFormatInches checks inch formatting and its round trip (chunk V1 rows).
func TestFormatInches(t *testing.T) {
	parsed0104, ok := Number("0.01041666666666667")
	if !ok {
		t.Fatal("Number(0.01041666666666667) not ok")
	}
	parsed0166, ok := Number("0.1666666666666667")
	if !ok {
		t.Fatal("Number(0.1666666666666667) not ok")
	}
	parsed1169, ok := Number("11.69291338582677")
	if !ok {
		t.Fatal("Number(11.69291338582677) not ok")
	}
	a, b := 0.1, 0.2
	one, three := 1.0, 3.0
	negZero := math.Copysign(0, -1)
	runtimeThird := one / three
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{8.5, "8.5"},
		{parsed0104, "0.01041666666666667"},
		{parsed0166, "0.1666666666666667"},
		{parsed1169, "11.69291338582677"},
		{a + b, "0.30000000000000004"},
		{runtimeThird, "0.3333333333333333"},
		{1e-20, "0.00000000000000000001"},
		{negZero, "0"},
		{72, "72"},
	} {
		if got := FormatInches(tc.in); got != tc.want {
			t.Errorf("FormatInches(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Property: 10,000 random doubles in [-1e6, 1e6] round trip exactly.
	rng := rand.New(rand.NewPCG(1, 2))
	for range 10000 {
		x := rng.Float64()*2e6 - 1e6
		got, ok := Number(FormatInches(x))
		if !ok || got != x {
			t.Fatalf("Number(FormatInches(%v)) = %v, %v; want %v, true", x, got, ok, x)
		}
	}
}

// TestDegrees checks stored radians to view degrees (chunk V1 rows).
func TestDegrees(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want float64
	}{
		{0, 0},
		{math.Pi / 2, 270},
		{-math.Pi / 2, 90},
		{0.5235987755982988, 330},
		{math.Pi, 180},
		{2 * math.Pi, 0},
	} {
		got := Degrees(tc.in)
		if math.Abs(got-tc.want) > 1e-9 || got < 0 || got >= 360 {
			t.Errorf("Degrees(%v) = %v, want %v within 1e-9 in [0,360)", tc.in, got, tc.want)
		}
	}
}

// TestStyleCategory checks the style category table (chunk V1 rows).
func TestStyleCategory(t *testing.T) {
	for _, tc := range []struct {
		section string
		cell    string
		want    Category
	}{
		{"", "LineWeight", LineCategory},
		{"", "FillForegnd", FillCategory},
		{"", "ShdwForegnd", FillCategory},
		{"", "LeftMargin", TextCategory},
		{"Character", "Size", TextCategory},
		{"Paragraph", "HorzAlign", TextCategory},
		{"", "PinX", NoStyle},
		{"", "LockWidth", NoStyle},
		{"Property", "Value", NoStyle},
		{"Geometry", "X", NoStyle},
		{"", "PrintPageOrientation", PageCategory},
	} {
		if got := StyleCategory(tc.section, tc.cell); got != tc.want {
			t.Errorf("StyleCategory(%q, %q) = %v, want %v", tc.section, tc.cell, got, tc.want)
		}
	}
}

// numberSeeds are inputs that exercise Number's rules; they seed
// FuzzNumber and its committed seed corpus.
var numberSeeds = []string{
	"2.5", "-1E-3", "0", "0.01041666666666667", "1.#INF", "",
	" 1", "NaN", "1e400", "0x10",
}

// FuzzNumber: Number never panics and ok implies finite.
func FuzzNumber(f *testing.F) {
	for _, s := range numberSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, ok := Number(s)
		if ok && (math.IsNaN(v) || math.IsInf(v, 0)) {
			t.Fatalf("Number(%q) = %v, true; want finite", s, v)
		}
	})
}
