package coverage

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Parity measures pptxdriver against a reference producer: the user
// requires at least parity with pdraw for the styles pdraw uses. The
// profile (profile.go) lists every universe path and enumerated value
// the reference output uses inside objects; each is looked up in the
// registry. Parity needs two things of an item: the views show it
// (read-modelled) and a plan can set it (round-trip-law or write-codec).

// ParityExemption names profile items that are legitimately not
// settable, with the reason. Patterns are registry path patterns. The
// list is deliberately short and each entry is a different reason. An
// exemption removes the settable requirement only (the read requirement
// stays) unless Read is set; an exempt item that is settable anyway
// still counts as settable.
type ParityExemption struct {
	Patterns []string
	Reason   string
	// Read also exempts the item from being read-modelled.
	Read bool
}

// wrappers are the schema-required container elements that carry no
// value of their own: their attributes and children are separate items
// and are measured there. An empty element whose presence is the fact
// (a:noFill, a:spAutoFit, a:picLocks) is not a wrapper.
const wrappers = "{p:sp,p:cxnSp,p:pic,p:grpSp,p:graphicFrame}/{p:nvSpPr,p:nvCxnSpPr,p:nvPicPr,p:nvGrpSpPr,p:nvGraphicFramePr}"

// ParityExemptions is the explicit list of items measured for reading
// only. Xfrm children (a:off, a:ext, a:chOff, a:chExt and their
// attributes) need no exemption: the transform codec settles them at
// their parent, and the registry already marks them settable.
var ParityExemptions = []ParityExemption{
	{Patterns: []string{"{p:sp,p:cxnSp,p:pic,p:grpSp}/{p:nvSpPr,p:nvCxnSpPr,p:nvPicPr,p:nvGrpSpPr}/p:cNvPr/@id"},
		Reason: "shape id: allocated by the tool on insert and referenced by connector glue; renumbering is not an edit"},
	{Patterns: []string{"(a:CT_Blip)/@r:embed"},
		Reason: "relationship id: allocated with the media part; a picture is replaced by a new part, not by writing a rel id"},
	{Patterns: []string{"(a:CT_Blip)/a:extLst/a:ext/*"}, Read: true,
		Reason: "xsd:any content of an extension, which the tracker cannot probe (only a non-goal claim decides a wildcard): the SVG blip pdraw writes there is read and shown as svg and set by svg, verified at the a:ext and its @uri"},
	{Patterns: []string{"(a:CT_Blip)/a:extLst/**"},
		Reason: "extension list (the SVG blip): carried with the picture; the extension URI and its body are fixed by the extension, not authored"},
	{Patterns: []string{wrappers, wrappers + "/{p:nvPr,p:cNvPr,p:cNvSpPr,p:cNvCxnSpPr,p:cNvPicPr}", "{p:sp,p:cxnSp,p:pic}/p:spPr", "p:sp/p:txBody", "p:sp/p:txBody/a:lstStyle"},
		Reason: "wrapper element with no value of its own (non-visual property containers, shape properties, text body, empty list style): its attributes and children are separate items, measured there"},
	{Patterns: []string{"p:sp/p:txBody/a:p/a:r/a:rPr/@dirty"}, Read: true,
		Reason: "proofing state flag PowerPoint maintains while editing: no document meaning to show or set"},
}

// ParityRow is one profile item with its registry statuses.
type ParityRow struct {
	Path string `json:"path"`
	// Value is set for one value of an enumerated attribute.
	Value string `json:"value,omitempty"`
	Area  string `json:"area"`
	Count int64  `json:"count"`
	Read  Status `json:"read"`
	Write Status `json:"write"`
	// Derived items are roots and containers whose status is the
	// strongest of their content: no fact of their own, not counted.
	Derived bool `json:"derived,omitempty"`
	// Exempt gives the reason the settable requirement does not apply.
	Exempt string `json:"exempt,omitempty"`
	// ExemptRead says the read requirement is exempt too.
	ExemptRead bool `json:"exempt_read,omitempty"`
}

// ReadOK reports whether the item is read-modelled.
func (r *ParityRow) ReadOK() bool { return r.Read == ReadModelled }

// WriteOK reports whether the item is settable.
func (r *ParityRow) WriteOK() bool { return r.Write == RoundTripLaw || r.Write == WriteCodec }

// Counted reports whether the item is part of the headline.
func (r *ParityRow) Counted() bool { return !r.Derived }

// Fails reports whether the item fails parity: not read-modelled, or not
// settable and not exempt.
func (r *ParityRow) Fails() bool {
	return r.Counted() && ((!r.ReadOK() && !r.ExemptRead) || (!r.WriteOK() && r.Exempt == ""))
}

// ParityTally counts items for the headline.
type ParityTally struct {
	Items int `json:"items"`
	// Read is the items read-modelled; Settable the items settable.
	Read     int `json:"read_modelled"`
	Settable int `json:"settable"`
	// Both is the items passing parity.
	Both int `json:"parity"`
}

// Parity is the pdraw parity section of the report.
type Parity struct {
	Source     string            `json:"source"`
	Commit     string            `json:"commit"`
	Decks      []string          `json:"decks"`
	Objects    int               `json:"objects"`
	Exemptions []ParityExemption `json:"exemptions"`
	// Paths, Values and All tally the paths, the enumerated values and
	// both.
	Paths   ParityTally `json:"paths"`
	Values  ParityTally `json:"values"`
	All     ParityTally `json:"all"`
	Derived int         `json:"derived_not_counted"`
	Rows    []ParityRow `json:"rows"`
}

// parityAreas lists the areas in report order.
var parityAreas = []string{
	"geometry", "line", "fill", "connector glue", "text body", "paragraph", "run",
	"picture", "group", "non-visual properties", "colour (shared type)", "containers", "other",
}

// parityArea names the area of a profile path.
func parityArea(path string) string {
	steps := splitSteps(path)
	has := func(s string) bool {
		return slices.Contains(steps, s)
	}
	step := func(i int) string {
		if i < len(steps) {
			return steps[i]
		}
		return ""
	}
	switch {
	case has("a:stCxn") || has("a:endCxn") || has("p:cNvCxnSpPr"):
		return "connector glue"
	case strings.HasPrefix(path, "(a:CT_Blip)") || step(0) == "p:pic" && (step(1) == "p:blipFill" || step(2) == "p:cNvPicPr" || len(steps) == 1):
		return "picture"
	case step(0) == "p:grpSp" && (step(1) == "p:grpSpPr" || len(steps) == 1):
		return "group"
	case len(steps) == 1 || len(steps) == 2 && (step(1) == "p:spPr" || step(1) == "p:txBody"):
		return "containers"
	case strings.HasPrefix(step(1), "p:nv"):
		return "non-visual properties"
	case step(1) == "p:spPr":
		switch step(2) {
		case "a:xfrm", "a:prstGeom", "a:custGeom":
			return "geometry"
		case "a:ln":
			return "line"
		case "a:noFill", "a:solidFill", "a:gradFill", "a:pattFill", "a:blipFill", "a:grpFill":
			return "fill"
		}
	case step(1) == "p:txBody":
		switch step(2) {
		case "a:bodyPr", "a:lstStyle":
			return "text body"
		case "a:p":
			switch step(3) {
			case "a:pPr", "":
				return "paragraph"
			}
			return "run"
		}
	case strings.HasPrefix(path, "(a:CT_TextListStyle)"):
		return "text body"
	case strings.HasPrefix(path, "(a:CT_") && strings.Contains(path, "Color)"):
		return "colour (shared type)"
	}
	return "other"
}

// AddParity joins the profile to the report's registry statuses and
// stores the result as the report's parity section.
func (r *Report) AddParity(_ *Universe, p *Profile) {
	by := map[string]*ReportItem{}
	for i := range r.Items {
		by[r.Items[i].Path] = &r.Items[i]
	}
	var exempt []*Claim
	for _, e := range ParityExemptions {
		exempt = append(exempt, &Claim{Paths: e.Patterns, Note: e.Reason, Facet: fmt.Sprint(e.Read)})
	}
	reasonFor := func(path string) (string, bool) {
		for _, c := range exempt {
			if c.Matches(path) {
				return c.Note, c.Facet == "true"
			}
		}
		return "", false
	}
	par := &Parity{Source: p.Source, Commit: p.Commit, Decks: p.Decks, Objects: p.Objects, Exemptions: ParityExemptions}
	var paths []string
	for path := range p.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		it := by[path]
		row := ParityRow{Path: path, Area: parityArea(path), Count: p.Paths[path], Read: Unknown, Write: Unknown}
		if it != nil {
			row.Read, row.Write, row.Derived = it.Read, it.Write, it.Derived
			row.Exempt, row.ExemptRead = reasonFor(path)
		}
		par.Rows = append(par.Rows, row)
		if it == nil {
			continue
		}
		var vals []string
		for v := range p.Values[path] {
			vals = append(vals, v)
		}
		sort.Strings(vals)
		for _, v := range vals {
			vr := ParityRow{Path: path, Value: v, Area: row.Area, Count: p.Values[path][v], Read: Unknown, Write: Unknown, Exempt: row.Exempt, ExemptRead: row.ExemptRead}
			for _, rv := range it.Values {
				if rv.Value == v {
					vr.Read, vr.Write = rv.Read, rv.Write
				}
			}
			par.Rows = append(par.Rows, vr)
		}
	}
	for i := range par.Rows {
		row := &par.Rows[i]
		if !row.Counted() {
			par.Derived++
			continue
		}
		for _, t := range []*ParityTally{&par.All, map[bool]*ParityTally{false: &par.Paths, true: &par.Values}[row.Value != ""]} {
			t.Items++
			if row.ReadOK() {
				t.Read++
			}
			if row.WriteOK() {
				t.Settable++
			}
			if !row.Fails() {
				t.Both++
			}
		}
	}
	r.Parity = par
}

// parityMarkdown renders the "pdraw parity" section.
func (r *Report) parityMarkdown(b *bytes.Buffer) {
	par := r.Parity
	if par == nil {
		return
	}
	w := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	w("## pdraw parity\n\n")
	w("The requirement: reach at least parity with pdraw for the styles pdraw uses. The profile `pdraw-profile.json` lists every universe path and enumerated value that pdraw's output uses inside objects (%d objects in %d decks, pdraw commit `%s`; built by `eval/parity/build.sh`, counted by `go run ./tools/coverage --profile`). ", par.Objects, len(par.Decks), par.Commit)
	w("An item has parity when the views show it (read-modelled) and a plan can set it (round-trip-law or write-codec). Items that are legitimately not settable are measured for reading only (exemptions, below); %d derived items (object roots and shared-type roots, whose status is only the strongest of their content) are listed but not counted.\n\n", par.Derived)
	w("| | items | read-modelled | settable | parity |\n| --- | ---: | ---: | ---: | ---: |\n")
	line := func(name string, t ParityTally) {
		w("| %s | %d | %d (%.1f%%) | %d (%.1f%%) | %d (%.1f%%) |\n", name, t.Items,
			t.Read, pct(int64(t.Read), int64(t.Items)), t.Settable, pct(int64(t.Settable), int64(t.Items)),
			t.Both, pct(int64(t.Both), int64(t.Items)))
	}
	line("**all items**", par.All)
	line("paths (elements and attributes)", par.Paths)
	line("enumerated values", par.Values)
	w("\nRead-modelled and settable count every item; parity requires settable only of the items that are not exempt.\n\n")

	w("### Exemptions from the settable requirement\n\n| path patterns | reason |\n| --- | --- |\n")
	for _, e := range par.Exemptions {
		w("| `%s` | %s |\n", strings.Join(e.Patterns, "`, `"), e.Reason)
	}
	w("\n")

	w("### Items that fail parity, by area\n\n")
	fail := 0
	for _, area := range parityAreas {
		var rows []ParityRow
		for _, row := range par.Rows {
			if row.Area == area && row.Fails() {
				rows = append(rows, row)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fail += len(rows)
		w("**%s** (%d)\n\n| item | pdraw count | read | write |\n| --- | ---: | --- | --- |\n", area, len(rows))
		for _, row := range rows {
			w("| %s | %d | %s | %s |\n", rowName(row), row.Count, row.Read, row.Write)
		}
		w("\n")
	}
	if fail == 0 {
		w("None.\n\n")
	}

	w("### Every profile item\n\n")
	for _, area := range parityAreas {
		var rows []ParityRow
		for _, row := range par.Rows {
			if row.Area == area {
				rows = append(rows, row)
			}
		}
		if len(rows) == 0 {
			continue
		}
		w("**%s** (%d)\n\n| item | pdraw count | read | write | parity |\n| --- | ---: | --- | --- | --- |\n", area, len(rows))
		for _, row := range rows {
			verdict := "ok"
			switch {
			case row.Derived:
				verdict = "derived, not counted"
			case row.Fails():
				verdict = "fails"
			case row.Exempt != "" && !row.WriteOK():
				verdict = "ok (exempt)"
			}
			w("| %s | %d | %s | %s | %s |\n", rowName(row), row.Count, row.Read, row.Write, verdict)
		}
		w("\n")
	}
}

// rowName names a parity row by path and value.
func rowName(row ParityRow) string {
	if row.Value != "" {
		return fmt.Sprintf("`%s` = `%s`", row.Path, row.Value)
	}
	return fmt.Sprintf("`%s`", row.Path)
}
