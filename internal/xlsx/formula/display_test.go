package formula

import "testing"

func TestDisplay(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		// Function prefixes.
		{"SUM(A1:B2)", "SUM(A1:B2)"},
		{"_xlfn.XLOOKUP(A1)", "XLOOKUP(A1)"},
		{"_xlfn._xlws.SORT(A1:A3)", "SORT(A1:A3)"},
		{"_xlws.FOO(A1)", "FOO(A1)"},
		{"_XLFN.XLOOKUP(A1)", "XLOOKUP(A1)"},
		{"_xlfn.xlookup(A1)", "xlookup(A1)"},
		{"_xlfn.IF(_xlfn.CONCAT(A1),1,2)", "IF(CONCAT(A1),1,2)"},

		// LAMBDA/LET parameters.
		{"_xlfn.LET(_xlpm.x,1,_xlpm.x)", "LET(x,1,x)"},
		{"_xlpm.x+_xlpm.y", "x+y"},

		// ANCHORARRAY.
		{"_xlfn.ANCHORARRAY(A1)", "A1#"},
		{"ANCHORARRAY(A1)", "A1#"},
		{"_xlfn.ANCHORARRAY(A1:B2)", "A1:B2#"},
		{"_xlfn.ANCHORARRAY(Sheet1!A1)", "Sheet1!A1#"},
		{"_xlfn.ANCHORARRAY(OFFSET(G4,1,1))", "ANCHORARRAY(OFFSET(G4,1,1))"},
		{"_xlfn.ANCHORARRAY(A1,B1)", "ANCHORARRAY(A1,B1)"},
		{"_xlfn.ANCHORARRAY(Sheet2!#REF!)", "ANCHORARRAY(Sheet2!#REF!)"},

		// SINGLE.
		{"_xlfn.SINGLE(A1)", "@A1"},
		{"_xlfn.SINGLE($A$1)", "@$A$1"},
		{"_xlfn.SINGLE(Sheet1!A1)", "@Sheet1!A1"},
		{"_xlfn.SINGLE(Rate)", "@Rate"},
		{"_xlfn.SINGLE(A1:B2)", "@A1:B2"},
		{"_xlfn.SINGLE(SUM(A1:A3))", "@SUM(A1:A3)"},
		{"_xlfn.SINGLE((A1:B2))", "@(A1:B2)"},
		{"_xlfn.SINGLE(5)", "@(5)"},
		{"_xlfn.SINGLE(\"x\")", "@(\"x\")"},
		{"_xlfn.SINGLE(-F5:F7)", "@(-F5:F7)"},
		{"_xlfn.SINGLE(A1+B1)", "@(A1+B1)"},
		{"_xlfn.SINGLE(A1,B1)", "SINGLE(A1,B1)"},

		// Nesting.
		{"_xlfn._xlws.SORT(_xlfn.ANCHORARRAY(A1))", "SORT(A1#)"},
		{"_xlfn.SINGLE(_xlfn.ANCHORARRAY(A1))", "@A1#"},
		{"_xlfn.SINGLE((_xlfn.ANCHORARRAY(A1)))", "@(A1#)"},
		{"_xlfn.SINGLE(SUM(_xlfn.ANCHORARRAY(F5)))", "@SUM(F5#)"},
		{"_xlfn.SINGLE((_xlfn._xlws.SORT(_xlfn.ANCHORARRAY(F58))))", "@(SORT(F58#))"},

		// Structured references.
		{"Tbl[[#This Row],[Col]]", "Tbl[@Col]"},
		{"[[#This Row],[Col 2]]", "[@[Col 2]]"},
		{"Tbl[[#This Row], [Amount]]", "Tbl[@Amount]"},
		{"Tbl[[#This Row], [Col 2]]", "Tbl[@[Col 2]]"},
		{"Tbl[#This Row]", "Tbl[@]"},
		{"Sales[[#This Row],[Qty]]*2", "Sales[@Qty]*2"},
		{"Tabelle1[[#This Row],[Belegdatum]]", "Tabelle1[@Belegdatum]"},
		{"MyTable1[[#This Row],[This is the,second column]]", "MyTable1[@[This is the,second column]]"},
		{"Tbl[[#This Row],[Field 2]:[Field 3]]", "Tbl[@[Field 2]:[Field 3]]"},
		{"Tbl[[#this row],[Col]]", "Tbl[@Col]"},
		{"Tbl[#All]", "Tbl[#All]"},
		{"Tbl[[#Headers],[A]:[B]]", "Tbl[[#Headers],[A]:[B]]"},

		// Strings and names hold their text.
		{"\"a _xlfn.b string\"", "\"a _xlfn.b string\""},
		{"\"x\"&_xlfn.XLOOKUP(A1)", "\"x\"&XLOOKUP(A1)"},
		{"'_xlfn.Sheet'!A1", "'_xlfn.Sheet'!A1"},
		{"_xlfn.FOO", "_xlfn.FOO"},
		{"'S'!_xlfn.XLOOKUP(A1)", "'S'!XLOOKUP(A1)"},
	} {
		got := Display(tc.in)
		if got != tc.want {
			t.Errorf("Display(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if again := Display(got); again != got {
			t.Errorf("Display(%q) = %q is not idempotent: Display = %q", tc.in, got, again)
		}
	}
}

// TestDisplayLexFailure: text the lexer cannot classify is returned
// unchanged, so a malformed formula is never mangled.
func TestDisplayLexFailure(t *testing.T) {
	for _, s := range []string{
		"\"open _xlfn.FOO", // unterminated string
		"[open",            // unbalanced bracket
		"'no bang'",        // quoted name without a reference
		"\xff_xlfn.FOO",    // stray byte then a prefix
	} {
		if got := Display(s); got != s {
			t.Errorf("Display(%q) = %q, want the input unchanged", s, got)
		}
	}
}

func TestDisplayCounts(t *testing.T) {
	in := "_xlfn.SINGLE(_xlfn.ANCHORARRAY(A1))+_xlfn.LET(_xlpm.x,1,Tbl[[#This Row],[Col]])+_xlfn.XLOOKUP(B1)"
	out, counts := displayWithCounts(in)
	want := "@A1#+LET(x,1,Tbl[@Col])+XLOOKUP(B1)"
	if out != want {
		t.Fatalf("displayWithCounts(%q) = %q, want %q", in, out, want)
	}
	for k, n := range map[string]int{
		"single": 1, "anchorarray": 1, "param prefix": 1, "struct ref": 1, "func prefix": 2,
	} {
		if counts[k] != n {
			t.Errorf("counts[%q] = %d, want %d (all counts %v)", k, counts[k], n, counts)
		}
	}
}

func FuzzDisplay(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	for _, s := range []string{
		"_xlfn.SINGLE(A1:B2)", "_xlfn.ANCHORARRAY(A1)", "Tbl[[#This Row],[Col]]",
		"_xlfn._xlws.SORT(_xlfn.ANCHORARRAY(A1))", "_xlfn.LET(_xlpm.x,1,_xlpm.x+1)",
		"_xlws.FOO(A1)", "_xlfn.SINGLE(SUM(A1:A3))", "[[#This Row],[Col 2]]",
		"Tbl[#This Row]", "_xlfn.SINGLE(-F5:F7)",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := Display(s)
		if again := Display(out); again != out {
			t.Fatalf("Display(%q) = %q is not idempotent: Display = %q", s, out, again)
		}
	})
}
