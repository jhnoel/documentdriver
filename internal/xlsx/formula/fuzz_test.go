package formula

import (
	"errors"
	"testing"
)

var fuzzSeeds = []string{
	"SUM(A1:B2)", "'It''s'!$A$1:B2+Jan:Mar!C3", "[1]Sheet1!A1+[1]!Rate", "Sales[[#This Row],[Qty]]",
	"{1,2;-3,\"x\"}", "_xlfn._xlws.SORT(_xlfn.ANCHORARRAY(A1))", "_xlfn.LET(_xlpm.x,1,_xlpm.x)",
	"A1 B1:C3", "(A1,B1)", "IF(A1,,#N/A)", "Sheet2!#REF!", "\"open", "'a':'b'!A1", "A:A+1:1",
	"A1048576:XFD1048576", "Sheet・1!A1", "1E+5%", "A1#", "@A1",
}

// FuzzLex: the tokens of any input join back to it, with offsets in
// order, and Validate returns nil or a *Refusal without panicking.
func FuzzLex(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		toks := Lex(s)
		if Join(toks) != s {
			t.Fatalf("Lex(%q) does not join back", s)
		}
		pos := 0
		for _, tk := range toks {
			if tk.Pos != pos || tk.Text == "" {
				t.Fatalf("Lex(%q): token %+v at %d", s, tk, pos)
			}
			pos += len(tk.Text)
		}
		if err := ValidateTokens(toks); err != nil {
			if _, ok := errors.AsType[*Refusal](err); !ok {
				t.Fatalf("Validate(%q) = %v, not a refusal", s, err)
			}
		}
		for _, tk := range toks {
			if tk.Kind == Ref || tk.Kind == Name || tk.Kind == Func {
				SplitQualified(tk.Text)
			}
		}
	})
}

// FuzzShift: Shift, Rename and Translate never panic; an insert that
// succeeds is undone by its inverse up to the canonical form of the
// references it moved (insert(delete(insert(s))) == insert(s)); and, for
// a valid formula, a rename there and back is stable the same way.
func FuzzShift(f *testing.F) {
	for i, s := range fuzzSeeds {
		f.Add(s, uint8(i), uint16(i+1), uint8(i%3+1))
	}
	f.Fuzz(func(t *testing.T, s string, kind uint8, at uint16, n uint8) {
		sh := Sheets{Own: "S", Order: []string{"Jan", "S", "Mar"}}
		e := Edit{Sheet: "S", Kind: EditKind(kind % 4), At: int(at), Count: int(n)}
		got, err := Shift(s, sh, e)
		if err != nil {
			if _, ok := errors.AsType[*Refusal](err); !ok {
				t.Fatalf("Shift error %v is not a refusal", err)
			}
		}
		if err == nil && e.insert() {
			back, err := Shift(got, sh, e.Inverse())
			if err != nil {
				t.Fatalf("Shift(%q) inverse refused: %v", got, err)
			}
			again, err := Shift(back, sh, e)
			if err != nil || again != got {
				t.Fatalf("insert/delete/insert of %q: %q then %q then %q, %v", s, got, back, again, err)
			}
		}
		r1 := Rename(s, "S", "My Sheet")
		r2 := Rename(r1, "My Sheet", "S")
		// Stable for formulas that validate: in a malformed one a renamed
		// prefix may run into the token before it ("0'S'!A" to "0S!A").
		if Validate(s) == nil && Rename(r2, "S", "My Sheet") != r1 {
			t.Fatalf("rename of %q is not stable", s)
		}
		if _, err := Translate(s, Cell{Row: 1, Col: 1}, Cell{Row: int(at)%MaxRows + 1, Col: int(n)%MaxCols + 1}); err != nil {
			if _, ok := errors.AsType[*Refusal](err); !ok {
				t.Fatalf("Translate error %v is not a refusal", err)
			}
		}
	})
}
