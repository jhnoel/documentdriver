package sheet

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// TestColRuns: the inputs of shift/cols_chunk_test.go and
// ops/colskernel_chunk_test.go (S2c).
func TestColRuns(t *testing.T) {
	for _, tc := range []struct {
		in   string
		mins []int
		maxs []int
	}{
		{`<cols><col min="2" max="5" width="9"/></cols>`, []int{2}, []int{5}},
		{`<cols><col max='5' min='2'/></cols>`, []int{2}, []int{5}},
		{`<cols><col min="2" max="5"/></cols>`, []int{2}, []int{5}},
		{`<cols><!-- <col min="1" max="1"/> --><col min="2" max="2"/></cols>`, []int{2}, []int{2}},
		{`<cols><col min="1" max="1"></col><col min="3" max="3"/></cols>`, []int{1, 3}, []int{1, 3}},
		{`<x:cols><x:col min='1' max='4' hidden="1" style="3" extra="a&gt;b"/></x:cols>`, []int{1}, []int{4}},
		{`<cols/>`, nil, nil},
		{`<cols></cols>`, nil, nil},
		{`<cols> <col min="1" max="1"/> <?p?> <!--c--> <col min="2" max="2"/> </cols>`, []int{1, 2}, []int{1, 2}},
	} {
		root, runs, err := ColRuns(tc.in)
		if err != nil {
			t.Fatalf("ColRuns(%q): %v", tc.in, err)
		}
		if root.Local(tc.in) != "cols" {
			t.Fatalf("ColRuns(%q): root local = %q", tc.in, root.Local(tc.in))
		}
		if len(runs) != len(tc.mins) {
			t.Fatalf("ColRuns(%q): %d runs, want %d (%v)", tc.in, len(runs), len(tc.mins), runs)
		}
		for i, r := range runs {
			if r.Min != tc.mins[i] || r.Max != tc.maxs[i] {
				t.Fatalf("ColRuns(%q): run %d = %d..%d, want %d..%d", tc.in, i, r.Min, r.Max, tc.mins[i], tc.maxs[i])
			}
			if r.Tag.Local(tc.in) != "col" {
				t.Fatalf("ColRuns(%q): run %d tag local = %q", tc.in, i, r.Tag.Local(tc.in))
			}
			if r.Start < 0 || r.Start >= r.End || r.End > len(tc.in) || tc.in[r.Start] != '<' {
				t.Fatalf("ColRuns(%q): run %d span %d..%d invalid", tc.in, i, r.Start, r.End)
			}
			if r.MinAttr.Value(tc.in) != strings.TrimSpace(tc.in[r.MinAttr.ValueStart:r.MinAttr.ValueEnd]) || r.MaxAttr.Value(tc.in) == "" {
				t.Fatalf("ColRuns(%q): run %d attr span wrong (min %q max %q)", tc.in, i, r.MinAttr.Value(tc.in), r.MaxAttr.Value(tc.in))
			}
			if i > 0 && r.Min <= runs[i-1].Max {
				t.Fatalf("ColRuns(%q): runs overlap at %d", tc.in, i)
			}
		}
	}

	// Spans keep the written tag, including a separate end tag.
	in := `<cols><col min="1" max="1"></col><col min="3" max="3"/></cols>`
	_, runs, err := ColRuns(in)
	if err != nil {
		t.Fatal(err)
	}
	if in[runs[0].Start:runs[0].End] != `<col min="1" max="1"></col>` {
		t.Fatalf("first span = %q", in[runs[0].Start:runs[0].End])
	}
	if in[runs[1].Start:runs[1].End] != `<col min="3" max="3"/>` {
		t.Fatalf("second span = %q", in[runs[1].Start:runs[1].End])
	}

	for _, bad := range []string{
		`<cols><col min="x" max="2"/></cols>`,                       // non-numeric min (colskernel test)
		`<cols><col min="2" max="3"/><col min="3" max="4"/></cols>`, // overlapping (colskernel test)
		`<cols><col min="2"/></cols>`,                               // missing max
		`<cols><col max="2"/></cols>`,                               // missing min
		`<cols><col min="3" max="2"/></cols>`,                       // min > max
		`<cols><col min="0" max="1"/></cols>`,                       // min below grid
		`<cols><col min="1" max="16385"/></cols>`,                   // past XFD
		`<cols><foo/></cols>`,                                       // child other than col
		`<cols>text</cols>`,                                         // content
		`<cols><col min="1" max="2"</cols>`,                         // unlexable tag
		`<rows><col min="1" max="1"/></rows>`,                       // wrong root
		`<cols><col min="2" max="2"/><col min="1" max="1"/></cols>`, // out of order
		`<cols>t`, // unclosed
		``,        // empty
	} {
		if _, _, err := ColRuns(bad); err == nil {
			t.Errorf("ColRuns accepted %q", bad)
		}
	}
}

// TestMerges: the inputs of ops/mergekernel_chunk_test.go (S2c).
func TestMerges(t *testing.T) {
	src := `<x:mergeCells count='9'><x:mergeCell ref="$B$2:$A$1"/> <x:mergeCell ref="D4:E5"/></x:mergeCells>`
	m, err := Merges(src)
	if err != nil {
		t.Fatal(err)
	}
	if m.Root.Local(src) != "mergeCells" {
		t.Fatalf("root local = %q", m.Root.Local(src))
	}
	if !m.HasCount || m.Count.Raw(src) != "9" {
		t.Fatalf("count = %q has=%v", m.Count.Raw(src), m.HasCount)
	}
	if src[m.ContentEnd:] != `</x:mergeCells>` {
		t.Fatalf("content end at %d: %q", m.ContentEnd, src[m.ContentEnd:])
	}
	if len(m.Cells) != 2 {
		t.Fatalf("%d cells", len(m.Cells))
	}
	if m.Cells[0].Ref != "$B$2:$A$1" || m.Cells[1].Ref != "D4:E5" {
		t.Fatalf("refs = %q %q", m.Cells[0].Ref, m.Cells[1].Ref)
	}
	if got := m.Cells[0].Area.String(); got != "A1:B2" {
		t.Fatalf("first area = %q, want A1:B2 (normalised)", got)
	}
	if got := m.Cells[1].Area.String(); got != "D4:E5" {
		t.Fatalf("second area = %q", got)
	}
	for _, c := range m.Cells {
		if c.Start < m.Root.End || c.End > m.ContentEnd || src[c.Start] != '<' {
			t.Fatalf("cell span %d..%d outside content", c.Start, c.End)
		}
		area, ok := formula.ParseArea(c.Ref)
		if !ok || area != c.Area {
			t.Fatalf("cell area = %v, ParseArea = %v %v", c.Area, area, ok)
		}
	}

	m, err = Merges(`<mergeCells/>`)
	if err != nil || len(m.Cells) != 0 || m.HasCount || m.ContentEnd != len(`<mergeCells/>`) {
		t.Fatalf("empty: %+v %v", m, err)
	}
	m, err = Merges(`<mergeCells><mergeCell ref="A1:B1"></mergeCell></mergeCells>`)
	if err != nil || len(m.Cells) != 1 {
		t.Fatalf("start-tag cell: %+v %v", m, err)
	}
	if `<mergeCells><mergeCell ref="A1:B1"></mergeCell></mergeCells>`[m.Cells[0].Start:m.Cells[0].End] != `<mergeCell ref="A1:B1"></mergeCell>` {
		t.Fatalf("cell span = %q", `<mergeCells><mergeCell ref="A1:B1"></mergeCell></mergeCells>`[m.Cells[0].Start:m.Cells[0].End])
	}
	m, err = Merges(`<mergeCells> <!--c--> <mergeCell ref="A1"/> </mergeCells>`)
	if err != nil || len(m.Cells) != 1 || m.Cells[0].Ref != "A1" {
		t.Fatalf("whitespace/comment: %+v %v", m, err)
	}

	for _, bad := range []string{
		`<mergeCells><mergeCell/></mergeCells>`,             // mergekernel test: no ref
		`<mergeCells><mergeCell ref="bogus"/></mergeCells>`, // mergekernel test: bad ref
		`<mergeCells>`, // mergekernel test: unclosed
		`<mergeCells><mergeCell ref="1:2"/></mergeCells>`,     // whole rows not a cell area
		`<mergeCells><foo/></mergeCells>`,                     // unexpected child
		`<mergeCells>text</mergeCells>`,                       // content
		`<mergeCells><?p?><mergeCell ref="A1"/></mergeCells>`, // PI not allowed here
		`<other/>`, // wrong root
		``,
	} {
		if _, err := Merges(bad); err == nil {
			t.Errorf("Merges accepted %q", bad)
		}
	}
}

type formulaHit struct {
	rule, start, end int
	text             string
}

func collectFormula(local, xml string) ([]formulaHit, error) {
	var hits []formulaHit
	err := FormulaTexts(local, xml, func(rule, start, end int, text string) error {
		hits = append(hits, formulaHit{rule, start, end, text})
		return nil
	})
	return hits, err
}

// TestFormulaTexts: the inputs of shift/sqref_chunk_test.go and
// ops/renameelement_chunk_test.go (S2c).
func TestFormulaTexts(t *testing.T) {
	cf := `<conditionalFormatting sqref="A1:A5"><cfRule><formula>A4&gt;0</formula></cfRule></conditionalFormatting>`
	hits, err := collectFormula("conditionalFormatting", cf)
	if err != nil || len(hits) != 1 || hits[0].rule != 0 || hits[0].text != "A4>0" || cf[hits[0].start:hits[0].end] != "A4&gt;0" {
		t.Fatalf("cf: %v %v", hits, err)
	}

	dv := `<dataValidations count="2"><dataValidation sqref="A3"><formula1><nested/></formula1></dataValidation><dataValidation sqref="B5"><formula1>A4&gt;0</formula1></dataValidation></dataValidations>`
	hits, err = collectFormula("dataValidations", dv)
	if err != nil || len(hits) != 1 || hits[0].rule != 1 || hits[0].text != "A4>0" {
		t.Fatalf("dv: %v %v", hits, err)
	}

	dv2 := `<dataValidations><dataValidation><formula1>Budget!A1</formula1><formula2>Budget!B1</formula2></dataValidation></dataValidations>`
	hits, err = collectFormula("dataValidations", dv2)
	if err != nil || len(hits) != 2 || hits[0].text != "Budget!A1" || hits[1].text != "Budget!B1" || hits[0].rule != 0 || hits[1].rule != 0 {
		t.Fatalf("dv2: %v %v", hits, err)
	}

	cf2 := `<conditionalFormatting><cfRule><formula>Budget!A1&gt;0</formula></cfRule></conditionalFormatting>`
	hits, err = collectFormula("conditionalFormatting", cf2)
	if err != nil || len(hits) != 1 || hits[0].text != "Budget!A1>0" {
		t.Fatalf("cf2: %v %v", hits, err)
	}

	cdata := `<conditionalFormatting><cfRule><formula><![CDATA[Budget!A1>0]]></formula></cfRule></conditionalFormatting>`
	hits, err = collectFormula("conditionalFormatting", cdata)
	if err != nil || len(hits) != 1 || hits[0].text != "Budget!A1>0" || cdata[hits[0].start:hits[0].end] != `<![CDATA[Budget!A1>0]]>` {
		t.Fatalf("cdata: %v %v", hits, err)
	}

	entities := `<dataValidations><dataValidation><formula1>A&amp;B&#60;C</formula1></dataValidation></dataValidations>`
	hits, err = collectFormula("dataValidations", entities)
	if err != nil || len(hits) != 1 || hits[0].text != "A&B<C" {
		t.Fatalf("entities: %v %v", hits, err)
	}

	prefixed := `<x:dataValidations><x:dataValidation><x:formula1>A1</x:formula1></x:dataValidation></x:dataValidations>`
	hits, err = collectFormula("dataValidations", prefixed)
	if err != nil || len(hits) != 1 || hits[0].text != "A1" {
		t.Fatalf("prefixed: %v %v", hits, err)
	}

	emptyRule := `<dataValidations><dataValidation/><dataValidation><formula1>A1</formula1></dataValidation></dataValidations>`
	hits, err = collectFormula("dataValidations", emptyRule)
	if err != nil || len(hits) != 1 || hits[0].rule != 1 {
		t.Fatalf("empty rule counts: %v %v", hits, err)
	}

	hits, err = collectFormula("other", "not xml")
	if err != nil || len(hits) != 0 {
		t.Fatalf("other local: %v %v", hits, err)
	}

	sentinel := errors.New("stop")
	err = FormulaTexts("conditionalFormatting", cf, func(_, _, _ int, _ string) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("yield error = %v, want sentinel", err)
	}

	if _, err := collectFormula("conditionalFormatting", `<conditionalFormatting><cfRule>`); err == nil {
		t.Error("FormulaTexts accepted unclosed conditionalFormatting")
	}
	if _, err := collectFormula("dataValidations", `<dataValidations><dataValidation><formula1>A1</dataValidation></dataValidations>`); err == nil {
		t.Error("FormulaTexts accepted mismatched dataValidations")
	}
	if _, err := collectFormula("dataValidations", ""); err == nil {
		t.Error("FormulaTexts accepted empty input")
	}
	if _, err := collectFormula("conditionalFormatting", `<dataValidations/>`); err == nil {
		t.Error("FormulaTexts accepted dataValidations root as conditionalFormatting")
	}
}

// FuzzColRuns: no panic; runs sorted and disjoint whenever no error (S2c).
func FuzzColRuns(f *testing.F) {
	for _, s := range []string{
		`<cols><col min="2" max="5" width="9"/></cols>`,
		`<cols><!-- <col min="1" max="1"/> --><col min="2" max="2"/></cols>`,
		`<cols><col min="1" max="1"></col><col min="3" max="3"/></cols>`,
		`<x:cols><x:col min='1' max='4'/></x:cols>`,
		`<cols/>`, `<cols></cols>`, `<cols><col min="x" max="2"/></cols>`,
		`<cols><col min="2" max="3"/><col min="3" max="4"/></cols>`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		root, runs, err := ColRuns(s)
		if err != nil {
			return
		}
		if root.Local(s) != "cols" {
			t.Fatalf("root local = %q for %q", root.Local(s), s)
		}
		prev := 0
		for _, r := range runs {
			if r.Start < 0 || r.Start >= r.End || r.End > len(s) || s[r.Start] != '<' {
				t.Fatalf("run span %d..%d of %q", r.Start, r.End, s)
			}
			if r.Min < 1 || r.Max > formula.MaxCols || r.Min > r.Max {
				t.Fatalf("run %d..%d of %q", r.Min, r.Max, s)
			}
			if r.Min <= prev {
				t.Fatalf("runs overlap at %d..%d of %q", r.Min, r.Max, s)
			}
			prev = r.Max
			if r.MinAttr.Value(s) == "" || r.MaxAttr.Value(s) == "" {
				t.Fatalf("run attr span wrong in %q", s)
			}
		}
	})
}

// FuzzMerges: no panic; every cell span inside the content (S2c).
func FuzzMerges(f *testing.F) {
	for _, s := range []string{
		`<x:mergeCells count='9'><x:mergeCell ref="$B$2:$A$1"/> <x:mergeCell ref="D4:E5"/></x:mergeCells>`,
		`<mergeCells/>`, `<mergeCells><mergeCell ref="A1:B1"></mergeCell></mergeCells>`,
		`<mergeCells><mergeCell/></mergeCells>`, `<mergeCells><mergeCell ref="bogus"/></mergeCells>`,
		`<mergeCells>`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		m, err := Merges(s)
		if err != nil {
			return
		}
		if m.Root.Local(s) != "mergeCells" {
			t.Fatalf("root local = %q for %q", m.Root.Local(s), s)
		}
		if m.ContentEnd < m.Root.End || m.ContentEnd > len(s) {
			t.Fatalf("content end %d of %q", m.ContentEnd, s)
		}
		prevStart := -1
		for _, c := range m.Cells {
			if c.Start < m.Root.End || c.End > m.ContentEnd || c.Start >= c.End || s[c.Start] != '<' {
				t.Fatalf("cell span %d..%d of %q", c.Start, c.End, s)
			}
			if c.Start <= prevStart {
				t.Fatalf("cells out of order in %q", s)
			}
			prevStart = c.Start
			if c.Ref == "" {
				t.Fatalf("cell ref empty in %q", s)
			}
			if _, ok := formula.ParseArea(c.Ref); !ok || c.Area.String() == "" {
				t.Fatalf("cell area %v ref %q in %q", c.Area, c.Ref, s)
			}
		}
	})
}
