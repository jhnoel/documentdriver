package ascii

import "testing"

func TestLower(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"abc xyz 09", "abc xyz 09"},
		{"ABC XYZ 09", "abc xyz 09"},
		{"AbC", "abc"},
		{"A\xffB\x80C", "a\xffb\x80c"},
		{"İΣΑΣ", "İΣΑΣ"},
		{"\u212a", "\u212a"},
		{"KELVIN", "kelvin"},
	} {
		if got := Lower(tc.in); got != tc.want {
			t.Errorf("Lower(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if n := testing.AllocsPerRun(10, func() { _ = Lower("already lower 09") }); n != 0 {
		t.Errorf("Lower of lower-case text allocates %v", n)
	}
	if n := testing.AllocsPerRun(10, func() { _ = Lower("UPPER CASE") }); n != 1 {
		t.Errorf("Lower of upper-case text allocates %v, want 1", n)
	}
}

func TestEqualFold(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"abc", "ABC", true},
		{"AbC", "aBc", true},
		{"a\xffb", "A\xffb", true},
		{"abc", "abd", false},
		{"abc", "ab", false},
		{"\u212a", "k", false},
		{"İ", "i", false},
	} {
		if got := EqualFold(tc.a, tc.b); got != tc.want {
			t.Errorf("EqualFold(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
	if n := testing.AllocsPerRun(10, func() { _ = EqualFold("AbC", "aBc") }); n != 0 {
		t.Errorf("EqualFold allocates %v", n)
	}
}

func TestClasses(t *testing.T) {
	type cls struct {
		r                        rune
		alpha, digit, alnum, hex bool
	}
	for _, tc := range []cls{
		{'a', true, false, true, true},
		{'F', true, false, true, true},
		{'G', true, false, true, false},
		{'5', false, true, true, true},
		{'/', false, false, false, false},
		{':', false, false, false, false},
		{'İ', false, false, false, false},
		{'\u212a', false, false, false, false},
		{0x7f, false, false, false, false},
		{0x80, false, false, false, false},
	} {
		if got := IsAlpha(tc.r); got != tc.alpha {
			t.Errorf("IsAlpha(%q) = %v, want %v", tc.r, got, tc.alpha)
		}
		if got := IsDigit(tc.r); got != tc.digit {
			t.Errorf("IsDigit(%q) = %v, want %v", tc.r, got, tc.digit)
		}
		if got := IsAlnum(tc.r); got != tc.alnum {
			t.Errorf("IsAlnum(%q) = %v, want %v", tc.r, got, tc.alnum)
		}
		if got := IsHex(tc.r); got != tc.hex {
			t.Errorf("IsHex(%q) = %v, want %v", tc.r, got, tc.hex)
		}
	}
}

func TestDigitsValid(t *testing.T) {
	for _, tc := range []struct {
		in  string
		dig bool
		val bool
	}{
		{"", false, true},
		{"0", true, true},
		{"12345", true, true},
		{"12a", false, true},
		{" 12", false, true},
		{"²", false, false},
		{"\x7f", false, true},
		{"\x80", false, false},
		{"\xff", false, false},
	} {
		if got := Digits(tc.in); got != tc.dig {
			t.Errorf("Digits(%q) = %v, want %v", tc.in, got, tc.dig)
		}
		if got := Valid(tc.in); got != tc.val {
			t.Errorf("Valid(%q) = %v, want %v", tc.in, got, tc.val)
		}
	}
}

func FuzzLower(f *testing.F) {
	for _, s := range []string{"", "ABC", "abc", "İ", "A\xffB", "\u212a"} {
		f.Add(s, s)
		f.Add(s, "ABC")
	}
	f.Add("AbC", "aBc")
	f.Fuzz(func(t *testing.T, a, b string) {
		want := make([]byte, len(a))
		for i := range len(a) {
			if c := a[i]; c >= 'A' && c <= 'Z' {
				want[i] = c + 32
			} else {
				want[i] = a[i]
			}
		}
		if got := Lower(a); got != string(want) {
			t.Fatalf("Lower(%q) = %q, want %q", a, got, want)
		}
		if EqualFold(a, b) != (Lower(a) == Lower(b)) {
			t.Fatalf("EqualFold(%q, %q) disagrees with Lower equality", a, b)
		}
	})
}
