package formula

import "testing"

func cell(s string) Cell {
	c, ok := ParseCell(s)
	if !ok {
		panic(s)
	}
	return c
}

func TestTranslate(t *testing.T) {
	for _, tc := range []struct{ text, from, to, want string }{
		{"A1+$B$1+B$1+$C1", "D1", "D3", "A3+$B$1+B$1+$C3"},
		{"SUM(A1:A3)", "B1", "C2", "SUM(B2:B4)"},
		{"A:A+1:1", "B1", "C2", "B:B+2:2"},
		{"Sheet2!A1+[1]S!A1", "A1", "A2", "Sheet2!A2+[1]S!A2"},
		{"\"A1\"&Rate", "A1", "A9", "\"A1\"&Rate"},
	} {
		got, err := Translate(tc.text, cell(tc.from), cell(tc.to))
		if err != nil || got != tc.want {
			t.Errorf("Translate(%q, %s, %s) = %q, %v; want %q", tc.text, tc.from, tc.to, got, err, tc.want)
		}
	}
	if _, err := Translate("A1048576", cell("A1"), cell("A2")); err == nil {
		t.Error("translate off the grid accepted")
	}
}

// checkSharedLaw: every surviving member reads, after the decision,
// Shift(Translate(master, member)).
func checkSharedLaw(t *testing.T, g Group, sh Sheets, e Edit, d Decision) {
	t.Helper()
	members, err := groupMembers(g)
	if err != nil {
		t.Fatal(err)
	}
	got := map[Cell]string{}
	for _, p := range d.Parts {
		for _, m := range p.Members {
			s, err := Translate(p.Text, p.Master, m)
			if err != nil {
				t.Fatalf("part translate: %v", err)
			}
			if !p.Ref.Contains(m.Row, m.Col) {
				t.Errorf("member %s outside part ref %s", m, p.Ref)
			}
			got[m] = s
		}
		if len(p.Members) == 0 || p.Members[0] != p.Master {
			t.Errorf("master %s is not the first member of %v", p.Master, p.Members)
		}
	}
	for _, c := range d.Children {
		got[c.Cell] = c.Text
	}
	onSheet := sh.Own == e.Sheet
	for _, m := range members {
		after := m
		if onSheet {
			r, gone, err := shiftRange(Range{Kind: Cells, From: m, To: m, Single: true}, e)
			if err != nil {
				t.Fatal(err)
			}
			if gone {
				if _, ok := got[m]; ok && !onSheet {
					t.Errorf("deleted member %s kept", m)
				}
				continue
			}
			after = r.From
		}
		ct, err := Translate(g.Text, g.Master, m)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Shift(ct, sh, e)
		if err != nil {
			t.Fatal(err)
		}
		if got[after] != want {
			t.Errorf("member %s (now %s) reads %q, want %q (decision %d)", m, after, got[after], want, d.Kind)
		}
	}
}

func TestDecideShared(t *testing.T) {
	sh := Sheets{Own: "S", Order: []string{"S", "T"}}
	col := Group{Ref: Range{Kind: Cells, From: cell("B2"), To: cell("B10")}, Master: cell("B2"), Text: "A2*2"}
	far := Group{Ref: Range{Kind: Cells, From: cell("B2"), To: cell("B10")}, Master: cell("B2"), Text: "A10+$C$1"}
	for _, tc := range []struct {
		name string
		g    Group
		sh   Sheets
		e    Edit
		want DecisionKind
	}{
		{"insert above", col, sh, Edit{Sheet: "S", Kind: InsertRows, At: 1, Count: 2}, Keep},
		{"insert inside, same-row refs", col, sh, Edit{Sheet: "S", Kind: InsertRows, At: 5, Count: 2}, Keep},
		{"insert below", col, sh, Edit{Sheet: "S", Kind: InsertRows, At: 20, Count: 2}, Keep},
		{"insert inside, refs cross", far, sh, Edit{Sheet: "S", Kind: InsertRows, At: 5, Count: 2}, Split},
		{"delete master row", col, sh, Edit{Sheet: "S", Kind: DeleteRows, At: 2, Count: 1}, Split},
		{"delete inside", col, sh, Edit{Sheet: "S", Kind: DeleteRows, At: 5, Count: 2}, Keep},
		{"other sheet, extent not cut", col, Sheets{Own: "T", Order: sh.Order}, Edit{Sheet: "S", Kind: InsertRows, At: 5, Count: 1}, Keep},
		{"insert column before", col, sh, Edit{Sheet: "S", Kind: InsertCols, At: 1, Count: 1}, Keep},
		{"insert column between ref and group", col, sh, Edit{Sheet: "S", Kind: InsertCols, At: 2, Count: 1}, Keep},
	} {
		d, err := DecideShared(tc.g, tc.sh, tc.e)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if d.Kind != tc.want {
			t.Errorf("%s: decision %d, want %d (%+v)", tc.name, d.Kind, tc.want, d)
		}
		checkSharedLaw(t, tc.g, tc.sh, tc.e, d)
	}
	// A group on another sheet whose referenced extent the edit cuts.
	x := Group{Ref: Range{Kind: Cells, From: cell("B1"), To: cell("B6")}, Master: cell("B1"), Text: "S!A1"}
	osh := Sheets{Own: "T", Order: sh.Order}
	e := Edit{Sheet: "S", Kind: InsertRows, At: 3, Count: 1}
	d, err := DecideShared(x, osh, e)
	if err != nil || d.Kind != Unshare {
		t.Errorf("cut extent: %+v, %v", d, err)
	}
	checkSharedLaw(t, x, osh, e, d)
}

// TestDecideSharedLaw sweeps edits over a few groups and checks the law
// for each decision.
func TestDecideSharedLaw(t *testing.T) {
	sh := Sheets{Own: "S", Order: []string{"S", "T"}}
	groups := []Group{
		{Ref: Range{Kind: Cells, From: cell("C3"), To: cell("C12")}, Master: cell("C3"), Text: "A3+B$1+SUM($A$1:A3)"},
		{Ref: Range{Kind: Cells, From: cell("C3"), To: cell("F3")}, Master: cell("C3"), Text: "C2*2+T!C1"},
		{Ref: Range{Kind: Cells, From: cell("C3"), To: cell("E6")}, Master: cell("C3"), Text: "A1+B10"},
		{Ref: Range{Kind: Cells, From: cell("C3"), To: cell("C9")}, Master: cell("C3"), Text: "C9", Members: []Cell{cell("C3"), cell("C5"), cell("C9")}},
	}
	for _, g := range groups {
		for _, k := range []EditKind{InsertRows, DeleteRows, InsertCols, DeleteCols} {
			for at := 1; at <= 13; at++ {
				for _, n := range []int{1, 2, 5} {
					e := Edit{Sheet: "S", Kind: k, At: at, Count: n}
					d, err := DecideShared(g, sh, e)
					if err != nil {
						t.Fatalf("%+v %+v: %v", g, e, err)
					}
					checkSharedLaw(t, g, sh, e, d)
				}
			}
		}
	}
}
