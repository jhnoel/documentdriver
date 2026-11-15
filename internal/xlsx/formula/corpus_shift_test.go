package formula

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
)

// corpusSheets is the context the corpus law tests use: each formula is
// taken to live on sheet "Own", the sheet every edit targets, so its
// unqualified references move. Named sheets are not in Order, so a 3-D
// span the edit would move refuses (counted).
var corpusSheets = Sheets{Own: "Own", Order: []string{"Own"}}

// TestCorpusShiftLaws runs row and column inserts over every cell formula
// of the corpus that validates and checks: the result still validates;
// the inverse delete gives back the input up to the canonical form of the
// references that moved (insert(delete(insert(s))) == insert(s)), and
// exactly when the input's references are canonical. Refusals (3-D spans,
// references pushed off the grid) are counted.
func TestCorpusShiftLaws(t *testing.T) {
	_, files := corpusFiles(t)
	type counts map[string]int
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *counts) {
		*r = counts{}
		cf := readCorpusFile(f.Path)
		for _, fm := range cf.Formulas {
			if fm.Kind != "cell" || Validate(fm.Text) != nil {
				continue
			}
			c, ok := ParseCell(fm.Where)
			if !ok {
				continue
			}
			for _, e := range []Edit{
				{Sheet: "Own", Kind: InsertRows, At: 1, Count: 1},
				{Sheet: "Own", Kind: InsertRows, At: c.Row, Count: 3},
				{Sheet: "Own", Kind: InsertCols, At: 2, Count: 2},
				{Sheet: "Own", Kind: InsertCols, At: c.Col, Count: 1},
			} {
				(*r)["shifts"]++
				got, err := Shift(fm.Text, corpusSheets, e)
				if err != nil {
					(*r)["refused"]++
					continue
				}
				if got != fm.Text {
					(*r)["changed"]++
				}
				if err := Validate(got); err != nil {
					t.Errorf("%s %s: Shift(%q) = %q does not validate: %v", fm.Part, fm.Where, fm.Text, got, err)
				}
				back, err := Shift(got, corpusSheets, e.Inverse())
				if err != nil {
					t.Errorf("%s %s: inverse of %q refused: %v", fm.Part, fm.Where, got, err)
					continue
				}
				if back == fm.Text {
					(*r)["exact inverse"]++
				} else if again, err := Shift(back, corpusSheets, e); err != nil || again != got {
					t.Errorf("%s %s: %q: insert %q, delete %q, insert %q, %v", fm.Part, fm.Where, fm.Text, got, back, again, err)
				} else {
					(*r)["inverse up to canonical form"]++
				}
			}
			r2 := Rename(fm.Text, "Own", "My Sheet")
			if Rename(r2, "My Sheet", "Own") != fm.Text && !strings.Contains(strings.ToLower(fm.Text), "own") {
				t.Errorf("%s %s: rename there and back changed %q", fm.Part, fm.Where, fm.Text)
			}
		}
	})
	total := map[string]int{}
	for _, r := range res {
		for k, n := range r.Value {
			total[k] += n
		}
	}
	t.Logf("corpus shift laws: %s", formatCounts(total))
}

// TestCorpusSharedLaw decides every corpus shared group under row and
// column edits through and around it, and checks the shared law: each
// surviving member reads the same before and after, modulo the shift.
func TestCorpusSharedLaw(t *testing.T) {
	_, files := corpusFiles(t)
	type counts map[string]int
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *counts) {
		*r = counts{}
		cf := readCorpusFile(f.Path)
		type key struct{ part, si string }
		groups := map[key]*Group{}
		var keys []key
		for _, fm := range cf.Formulas {
			if fm.T != "shared" {
				continue
			}
			c, ok := ParseCell(fm.Where)
			if !ok {
				continue
			}
			k := key{fm.Part, fm.SI}
			g := groups[k]
			if g == nil {
				g = &Group{Members: []Cell{}}
				groups[k] = g
				keys = append(keys, k)
			}
			g.Members = append(g.Members, c)
			if fm.Kind == "cell" {
				ref, ok := ParseRange(fm.Ref)
				if !ok {
					continue
				}
				g.Ref, g.Master, g.Text = ref, c, fm.Text
			}
		}
		for _, k := range keys {
			g := *groups[k]
			if g.Text == "" || Validate(g.Text) != nil {
				(*r)["groups without a valid master"]++
				continue
			}
			(*r)["groups"]++
			for _, m := range g.Members {
				ct, err := Translate(g.Text, g.Master, m)
				if err != nil {
					t.Errorf("%s si %s: child %s: %v", k.part, k.si, m, err)
					continue
				}
				if err := Validate(ct); err != nil {
					t.Errorf("%s si %s: child %s %q does not validate: %v", k.part, k.si, m, ct, err)
				}
				(*r)["children translated"]++
			}
			r1, c1, r2, c2 := g.Ref.Bounds()
			for _, e := range []Edit{
				{Sheet: "Own", Kind: InsertRows, At: r1, Count: 1},
				{Sheet: "Own", Kind: InsertRows, At: (r1 + r2 + 1) / 2, Count: 2},
				{Sheet: "Own", Kind: DeleteRows, At: r1, Count: 1},
				{Sheet: "Own", Kind: DeleteRows, At: (r1 + r2 + 1) / 2, Count: 1},
				{Sheet: "Own", Kind: InsertCols, At: (c1 + c2 + 1) / 2, Count: 1},
				{Sheet: "Own", Kind: DeleteCols, At: c1, Count: 1},
			} {
				d, err := DecideShared(g, corpusSheets, e)
				if err != nil {
					(*r)["decisions refused"]++
					continue
				}
				(*r)[[...]string{"keep", "split", "unshare"}[d.Kind]]++
				checkSharedLaw(t, g, corpusSheets, e, d)
			}
		}
	})
	total := map[string]int{}
	for _, r := range res {
		for k, n := range r.Value {
			total[k] += n
		}
	}
	t.Logf("corpus shared law: %s", formatCounts(total))
}
