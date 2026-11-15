package formula

import "testing"

// TestFutureFunctionsAgreeWithCorpus: where the corpus table and
// Microsoft's list both name a function, they agree on its prefix.
func TestFutureFunctionsAgreeWithCorpus(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range futureFunctions {
		if seen[f.name] {
			t.Errorf("%s listed twice", f.name)
		}
		seen[f.name] = true
		for _, c := range functionTable {
			if c.Name == f.name && c.Prefix != f.prefix {
				t.Errorf("%s: corpus prefix %q, list %q", f.name, c.Prefix, f.prefix)
			}
		}
	}
	if f, ok := LookupFunction("xlookup"); !ok || f.Prefix != PrefixFuture {
		t.Errorf("XLOOKUP: %+v %v", f, ok)
	}
}

// TestIsDynamicArrayNamed: Excel's array-returning functions spill even
// when the corpus never showed them spilling, prefixed or not.
func TestIsDynamicArrayNamed(t *testing.T) {
	for _, n := range []string{"FILTER", "_xlfn._xlws.SORT", "_xlfn.UNIQUE", "VSTACK", "TRANSPOSE", "SEQUENCE"} {
		if !IsDynamicArray(n) {
			t.Errorf("IsDynamicArray(%q) = false", n)
		}
	}
	for _, n := range []string{"SUM", "VLOOKUP", "IF", "XLOOKUP", "NOSUCH"} {
		if IsDynamicArray(n) {
			t.Errorf("IsDynamicArray(%q) = true", n)
		}
	}
}
