package coverage

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Files under eval/coverage.
const (
	UniverseFile = "universe.json"
	CountsFile   = "corpus.json"
	ReportFile   = "coverage.json"
	MarkdownFile = "coverage.md"
)

// statusOrder ranks overall statuses, strongest first.
var statusOrder = []Status{RoundTripLaw, WriteCodec, ReadModelled, PreservedRaw, OmittedWithReason, NonGoal, Unknown}

// readOrder and writeOrder list each dimension's statuses, strongest
// first.
var (
	readOrder  = []Status{ReadModelled, PreservedRaw, OmittedWithReason, NonGoal, Unknown}
	writeOrder = []Status{RoundTripLaw, WriteCodec, PreservedRaw, NonGoal, Unknown}
)

// rankIn returns the rank of s in order, or len(order) when absent.
func rankIn(order []Status, s Status) int {
	for i, x := range order {
		if x == s {
			return i
		}
	}
	return len(order)
}

// ReportItem is one universe item with its statuses and corpus weight.
type ReportItem struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Read    Status `json:"read"`
	Write   Status `json:"write"`
	Status  Status `json:"status"`
	ReadBy  string `json:"read_by,omitempty"`
	WriteBy string `json:"write_by,omitempty"`
	// Derived says the statuses were raised from the item's content (a
	// root or object reference, not probed itself).
	Derived bool  `json:"derived,omitempty"`
	Count   int64 `json:"count"`
	InObj   int64 `json:"object_count"`
	// Facets are path-level capabilities (a facet claim without a
	// per-value Has), such as "validated".
	Facets []string      `json:"facets,omitempty"`
	Values []ReportValue `json:"values,omitempty"`
}

// ReportValue is one value of an enumerated attribute.
type ReportValue struct {
	Value  string   `json:"value"`
	Read   Status   `json:"read"`
	Write  Status   `json:"write"`
	Facets []string `json:"facets,omitempty"`
	Count  int64    `json:"count"`
}

// Tally counts items and their corpus weight.
type Tally struct {
	Items  int   `json:"items"`
	Weight int64 `json:"weight"`
	// ObjWeight is the weight found in objects only.
	ObjWeight int64 `json:"object_weight"`
}

// Summary tallies one dimension by status.
type Summary map[Status]*Tally

// add tallies one report item into the summary.
func (s Summary) add(st Status, it *ReportItem) {
	t := s[st]
	if t == nil {
		t = &Tally{}
		s[st] = t
	}
	t.Items++
	t.Weight += it.Count
	t.ObjWeight += it.InObj
}

// ReportOp is one cell of the operation matrix.
type ReportOp struct {
	Op     string   `json:"op"`
	Field  string   `json:"field,omitempty"`
	Kind   string   `json:"kind"`
	Status OpStatus `json:"status"`
	Note   string   `json:"note,omitempty"`
}

// Report is the joined coverage report written to coverage.json.
type Report struct {
	Universe struct {
		Schema     string `json:"schema"`
		SHA256     string `json:"sha256"`
		Rule       string `json:"recursion_rule"`
		Items      int    `json:"items"`
		Elements   int    `json:"elements"`
		Attributes int    `json:"attributes"`
		Any        int    `json:"any"`
		Enums      int    `json:"enumerations"`
		EnumValues int    `json:"enumeration_values"`
	} `json:"universe"`
	Corpus struct {
		Corpus     string `json:"corpus"`
		Decks      int    `json:"decks"`
		Missing    int    `json:"missing"`
		Unreadable int    `json:"unreadable"`
		Slides     int    `json:"slides"`
		Objects    int    `json:"objects"`
		Outside    int64  `json:"outside"`
	} `json:"corpus"`
	Read       Summary            `json:"read"`
	Write      Summary            `json:"write"`
	Overall    Summary            `json:"overall"`
	Kinds      map[string]Summary `json:"kinds"`
	Values     map[string]Summary `json:"values"`
	Operations []ReportOp         `json:"operations"`
	Claims     []ReportClaim      `json:"claims"`
	Items      []ReportItem       `json:"items"`
	Parity     *Parity            `json:"parity,omitempty"`
}

// ReportClaim lists a registry claim and how many items it decides.
type ReportClaim struct {
	By     string `json:"by"`
	Dim    Dim    `json:"dim,omitempty"`
	Status Status `json:"status,omitempty"`
	Facet  string `json:"facet,omitempty"`
	Items  int    `json:"items"`
	Note   string `json:"note,omitempty"`
}

// kindOf names the table an item is reported under.
func kindOf(it *Item, u *Universe) string {
	for _, r := range u.Roots {
		if r.Path == it.Root {
			if r.Kind == "factored" {
				return "shared"
			}
			return r.Kind
		}
	}
	return "slide"
}

// Build joins the universe, the registry, the operation matrix and the
// corpus counts.
func Build(u *Universe, c *Counts) (*Report, error) {
	r := &Report{Read: Summary{}, Write: Summary{}, Overall: Summary{}, Kinds: map[string]Summary{}, Values: map[string]Summary{}}
	r.Universe.Schema, r.Universe.SHA256, r.Universe.Rule = u.Schema, u.SHA256, u.Rule
	r.Universe.Items = len(u.Items)
	for _, it := range u.Items {
		switch it.Node {
		case "element":
			r.Universe.Elements++
		case "attribute":
			r.Universe.Attributes++
		case "any":
			r.Universe.Any++
		}
	}
	r.Universe.Enums = len(u.Enums)
	for _, v := range u.Enums {
		r.Universe.EnumValues += len(v)
	}
	r.Corpus.Corpus, r.Corpus.Decks, r.Corpus.Missing, r.Corpus.Unreadable = c.Corpus, c.Decks, c.Missing, c.Unreadable
	r.Corpus.Slides, r.Corpus.Objects = c.Slides, c.Objects
	for _, n := range c.Outside {
		r.Corpus.Outside += n
	}

	roots := map[string]bool{}
	for _, rt := range u.Roots {
		roots[rt.Path] = true
	}
	isProbedRoot := func(p string) bool { return Probed(&Item{Path: p, Node: "element"}, u) }
	items := make([]ReportItem, len(u.Items))
	idx := map[string]int{}
	for i := range u.Items {
		it := &u.Items[i]
		ri := ReportItem{Path: it.Path, Kind: kindOf(it, u), Read: Unknown, Write: Unknown, Count: c.Paths[it.Path], InObj: c.ObjectPaths[it.Path]}
		probed := Probed(it, u)
		if it.Node == "any" || probed {
			if cl := Decide(Read, it.Path); cl != nil && (probed || cl.Status == NonGoal) {
				ri.Read, ri.ReadBy = cl.Status, cl.By
			}
			if cl := Decide(Write, it.Path); cl != nil && (probed || cl.Status == NonGoal) {
				ri.Write, ri.WriteBy = cl.Status, cl.By
			}
		}
		idx[it.Path] = i
		items[i] = ri
	}
	// Roots and object references take the strongest status of their
	// content: they are not probed themselves.
	for i := len(u.Items) - 1; i >= 0; i-- {
		it := &u.Items[i]
		par := parentOf(it.Path)
		j, ok := idx[par]
		if !ok || !(roots[par] && !isProbedRoot(par)) {
			continue
		}
		pi := &items[j]
		pi.Derived = true
		if rankIn(readOrder, items[i].Read) < rankIn(readOrder, pi.Read) {
			pi.Read = items[i].Read
		}
		if rankIn(writeOrder, items[i].Write) < rankIn(writeOrder, pi.Write) {
			pi.Write = items[i].Write
		}
	}
	for i := range u.Items {
		it := &u.Items[i]
		if !it.Object {
			continue
		}
		if j, ok := idx[lastStepOf(it.Path)]; ok {
			items[i].Read, items[i].Write, items[i].Derived = items[j].Read, items[j].Write, true
		}
	}
	for i := range u.Items {
		it := &u.Items[i]
		ri := &items[i]
		ri.Status = overall(ri.Read, ri.Write)
		for _, f := range Facets(it.Path) {
			if f.Has == nil {
				ri.Facets = append(ri.Facets, f.Facet)
			}
		}
		if it.Enum != "" {
			ri.Values = values(u, it, ri, c)
		}
		r.Read.add(ri.Read, ri)
		r.Write.add(ri.Write, ri)
		r.Overall.add(ri.Status, ri)
		k := r.Kinds[ri.Kind]
		if k == nil {
			k = Summary{}
			r.Kinds[ri.Kind] = k
		}
		k.add(ri.Status, ri)
		for _, v := range ri.Values {
			vi := &ReportItem{Count: v.Count}
			for _, d := range []struct {
				name string
				st   Status
			}{{"read", v.Read}, {"write", v.Write}} {
				s := r.Values[d.name]
				if s == nil {
					s = Summary{}
					r.Values[d.name] = s
				}
				s.add(d.st, vi)
			}
		}
	}
	r.Items = items
	for _, row := range Operations {
		for _, k := range Kinds {
			if st, ok := row.Cells[k]; ok {
				r.Operations = append(r.Operations, ReportOp{Op: row.Op, Field: row.Field, Kind: k, Status: st, Note: row.Notes[k]})
			}
		}
	}
	for _, cl := range Claims {
		rc := ReportClaim{By: cl.By, Dim: cl.Dim, Status: cl.Status, Facet: cl.Facet, Note: cl.Note}
		for i := range items {
			if cl.Facet != "" {
				if cl.Matches(items[i].Path) {
					rc.Items++
				}
				continue
			}
			by := items[i].ReadBy
			if cl.Dim == Write {
				by = items[i].WriteBy
			}
			if by == cl.By && Decide(cl.Dim, items[i].Path) == cl {
				rc.Items++
			}
		}
		r.Claims = append(r.Claims, rc)
	}
	return r, nil
}

// Probed reports whether the tests probe an item, so that its statuses
// come from the registry: every element and attribute below a root,
// except xsd:any and object elements below a group or shape tree, plus
// the roots that sit below their part's document element outside the
// slide (layout and master colour maps, text styles, the slide size, the
// default text style, the theme's schemes), probed by removing them.
// Other roots, object references and xsd:any items are not probed: a
// root or object reference takes the strongest status of its content,
// and xsd:any only a non-goal.
func Probed(it *Item, u *Universe) bool {
	for _, r := range u.Roots {
		if r.Path == it.Path {
			return r.Kind == "slide" && strings.Contains(r.Path, "/")
		}
	}
	return it.Node != "any" && !it.Object && strings.Contains(it.Path, "/")
}

// parentOf returns the path above p, or empty for a root.
func parentOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// lastStepOf returns the final slash-separated step of p.
func lastStepOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// overall is the strongest of an item's read and write statuses.
func overall(read, write Status) Status {
	switch {
	case write == RoundTripLaw || write == WriteCodec:
		return write
	case read == ReadModelled:
		return ReadModelled
	case read == PreservedRaw || write == PreservedRaw:
		return PreservedRaw
	case read == OmittedWithReason:
		return OmittedWithReason
	case read == NonGoal || write == NonGoal:
		return NonGoal
	}
	return Unknown
}

// values gives each value of an enumerated item its own statuses: a
// claim with Values covers only those, and facets list the values that
// have them.
func values(u *Universe, it *Item, ri *ReportItem, c *Counts) []ReportValue {
	var out []ReportValue
	rc, wc := Decide(Read, it.Path), Decide(Write, it.Path)
	facets := Facets(it.Path)
	for _, v := range u.Enums[it.Enum] {
		rv := ReportValue{Value: v, Read: ri.Read, Write: ri.Write, Count: c.Values[it.Path][v]}
		if rc != nil && ri.Read == rc.Status && !rc.Covers(v) {
			rv.Read = Unknown
		}
		if wc != nil && ri.Write == wc.Status && !wc.Covers(v) {
			rv.Write = Unknown
		}
		for _, f := range facets {
			if f.Has != nil && f.Has(v) {
				rv.Facets = append(rv.Facets, f.Facet)
			}
		}
		out = append(out, rv)
	}
	return out
}

// Markdown renders the report's summary tables.
func (r *Report) Markdown() []byte {
	var b bytes.Buffer
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("# Coverage\n\n")
	w("Generated by `go run ./tools/coverage`; do not edit. The registry is `internal/pptx/coverage/registry.go`, ")
	w("checked against the read model and the plan operations by `go test ./internal/pptx/coverage`.\n\n")
	w("Universe: %d items (%d elements, %d attributes, %d xsd:any) from the ECMA-376 transitional schemas ", r.Universe.Items, r.Universe.Elements, r.Universe.Attributes, r.Universe.Any)
	w("(Apache POI %s, SHA-256 %s…); %d enumerations with %d values.\n\n", shortCommit(r.Universe.Schema), r.Universe.SHA256[:12], r.Universe.Enums, r.Universe.EnumValues)
	w("Corpus: %s: %d decks (%d missing, %d unreadable), %d slides, %d objects. ", r.Corpus.Corpus, r.Corpus.Decks, r.Corpus.Missing, r.Corpus.Unreadable, r.Corpus.Slides, r.Corpus.Objects)
	w("Weight is the number of occurrences of an item in the corpus (slides' objects and slide-level context, layouts' colour maps, masters' colour maps and text styles, the presentation, themes); ")
	w("object weight counts occurrences inside objects only. %d occurrences fell outside the universe (extensions, mc:AlternateContent).\n\n", r.Corpus.Outside)

	r.parityMarkdown(&b)
	w("## Headline\n\n")
	all, obj := r.totalWeight()
	share := func(s Summary, sts ...Status) (float64, float64, float64) {
		var n int
		var wt, ot int64
		for _, st := range sts {
			if t := s[st]; t != nil {
				n += t.Items
				wt += t.Weight
				ot += t.ObjWeight
			}
		}
		return pct(int64(n), int64(r.Universe.Items)), pct(wt, all), pct(ot, obj)
	}
	w("| | by item count | by corpus weight | by object weight |\n| --- | ---: | ---: | ---: |\n")
	row := func(name string, s Summary, sts ...Status) {
		a, b2, c := share(s, sts...)
		w("| %s | %.1f%% | %.1f%% | %.1f%% |\n", name, a, b2, c)
	}
	row("Read: shown in the views (read-modelled)", r.Read, ReadModelled)
	row("Read: shown, or shown as raw XML", r.Read, ReadModelled, PreservedRaw)
	row("Write: settable by plans (round-trip-law, write-codec)", r.Write, RoundTripLaw, WriteCodec)
	row("Write: settable, or carried by the codec rewriting it", r.Write, RoundTripLaw, WriteCodec, PreservedRaw)
	row("Unknown overall (the gap)", r.Overall, Unknown)
	w("\n")

	for _, d := range []struct {
		title string
		s     Summary
		order []Status
	}{{"Read status", r.Read, readOrder}, {"Write status", r.Write, writeOrder}, {"Overall status", r.Overall, statusOrder}} {
		w("## %s\n\n| status | items | %% items | weight | %% weight | object weight | %% object weight |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n", d.title)
		for _, st := range d.order {
			t := d.s[st]
			if t == nil {
				t = &Tally{}
			}
			w("| %s | %d | %.1f%% | %d | %.1f%% | %d | %.1f%% |\n", st, t.Items, pct(int64(t.Items), int64(r.Universe.Items)), t.Weight, pct(t.Weight, all), t.ObjWeight, pct(t.ObjWeight, obj))
		}
		w("\n")
	}

	w("## Enumerated values\n\n| dimension | status | values | weight |\n| --- | --- | ---: | ---: |\n")
	for _, d := range []struct {
		name  string
		order []Status
	}{{"read", readOrder}, {"write", writeOrder}} {
		for _, st := range d.order {
			if t := r.Values[d.name][st]; t != nil {
				w("| %s | %s | %d | %d |\n", d.name, st, t.Items, t.Weight)
			}
		}
	}
	w("\n")
	r.valueDetail(&b)

	w("## By object kind (overall status)\n\n")
	kinds := []string{"shape", "connector", "group", "picture", "other", "slide", "shared"}
	w("| kind | items | ")
	for _, st := range statusOrder {
		w("%s | ", st)
	}
	w("weight | %% weight settable | %% weight shown or settable |\n| --- | ---: |")
	for range statusOrder {
		w(" ---: |")
	}
	w(" ---: | ---: | ---: |\n")
	for _, k := range kinds {
		s := r.Kinds[k]
		var n int
		var wt, set, shown int64
		for st, t := range s {
			n += t.Items
			wt += t.Weight
			if st == RoundTripLaw || st == WriteCodec {
				set += t.Weight
			}
			if st == RoundTripLaw || st == WriteCodec || st == ReadModelled {
				shown += t.Weight
			}
		}
		w("| %s | %d | ", k, n)
		for _, st := range statusOrder {
			if t := s[st]; t != nil {
				w("%d | ", t.Items)
			} else {
				w("0 | ")
			}
		}
		w("%d | %.1f%% | %.1f%% |\n", wt, pct(set, wt), pct(shown, wt))
	}
	w("\n`shared` holds the factored value types (colours, fill bodies, effects, 3D, list-style levels, blips), whose weight sums every position.\n\n")

	w("## Operations\n\n| operation | ")
	for _, k := range Kinds {
		w("%s | ", k)
	}
	w("\n| --- |")
	for range Kinds {
		w(" --- |")
	}
	w("\n")
	for _, row := range Operations {
		name := "`" + row.Op + "`"
		if row.Field != "" {
			name += " " + row.Field
		}
		w("| %s | ", name)
		for _, k := range Kinds {
			st, ok := row.Cells[k]
			switch {
			case !ok:
				w("| ")
			case row.Notes[k] != "":
				w("%s (%s) | ", st, row.Notes[k])
			default:
				w("%s | ", st)
			}
		}
		w("\n")
	}
	w("\n")

	gaps := func(title string, n int, byObj bool, keep func(*ReportItem) bool) {
		weight := func(it *ReportItem) int64 {
			if byObj {
				return it.InObj
			}
			return it.Count
		}
		var list []*ReportItem
		for i := range r.Items {
			if keep(&r.Items[i]) && weight(&r.Items[i]) > 0 {
				list = append(list, &r.Items[i])
			}
		}
		sort.SliceStable(list, func(i, j int) bool { return weight(list[i]) > weight(list[j]) })
		if len(list) > n {
			list = list[:n]
		}
		w("## %s\n\n| # | path | weight | object weight | read | write |\n| ---: | --- | ---: | ---: | --- | --- |\n", title)
		for i, it := range list {
			w("| %d | `%s` | %d | %d | %s | %s |\n", i+1, it.Path, it.Count, it.InObj, it.Read, it.Write)
		}
		w("\n")
	}
	gap := func(it *ReportItem) bool { return it.Status == Unknown || it.Status == OmittedWithReason }
	gaps("Gaps: the 50 paths with overall status unknown or omitted most frequent in objects", 50, true, gap)
	w("This is the prioritised gap list: occurrences inside shapes, connectors, groups, pictures and graphic frames. ")
	w("Factored content (colours, list-style levels...) counts only where an object holds it.\n\n")
	gaps("Gaps by total weight (themes, masters and layouts included)", 50, false, gap)
	gaps("Not shown: the 30 paths most frequent in objects that the read model neither shows nor omits with a reason", 30, true, func(it *ReportItem) bool {
		return it.Read == Unknown
	})

	w("## Registry\n\n| reader or codec | dimension | status | items | note |\n| --- | --- | --- | ---: | --- |\n")
	for _, c := range r.Claims {
		dim, st := string(c.Dim), string(c.Status)
		if c.Facet != "" {
			dim, st = "facet", c.Facet
		}
		w("| %s | %s | %s | %d | %s |\n", c.By, dim, st, c.Items, c.Note)
	}
	return b.Bytes()
}

// valueDetail lists the enumerated attributes a reader or codec covers
// only in part, and the facets.
func (r *Report) valueDetail(b *bytes.Buffer) {
	w := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	type agg struct {
		read, write map[string]bool
		facets      map[string][]string
		count       map[string]int64
		order       []string
		paths       int
		settable    int // positions where a plan sets it
		validated   int // of those, positions that refuse values outside the enumeration
	}
	byEnum := map[string]*agg{}
	var names []string
	for i := range r.Items {
		it := &r.Items[i]
		if len(it.Values) == 0 || it.Read != ReadModelled && it.Write != RoundTripLaw && it.Write != WriteCodec {
			continue
		}
		key := lastStepOf(parentOf(it.Path)) + "/" + lastStepOf(it.Path)
		a := byEnum[key]
		if a == nil {
			a = &agg{read: map[string]bool{}, write: map[string]bool{}, facets: map[string][]string{}, count: map[string]int64{}}
			byEnum[key] = a
			names = append(names, key)
			for _, v := range it.Values {
				a.order = append(a.order, v.Value)
			}
		}
		a.paths++
		if it.Write == RoundTripLaw || it.Write == WriteCodec {
			a.settable++
			if contains(it.Facets, "validated") {
				a.validated++
			}
		}
		for _, v := range it.Values {
			if v.Read == ReadModelled {
				a.read[v.Value] = true
			}
			if v.Write == RoundTripLaw || v.Write == WriteCodec {
				a.write[v.Value] = true
			}
			for _, f := range v.Facets {
				if !contains(a.facets[f], v.Value) {
					a.facets[f] = append(a.facets[f], v.Value)
				}
			}
			a.count[v.Value] += v.Count
		}
	}
	sort.Strings(names)
	w("Enumerated attributes the tool reads or writes (grouped by element/attribute over all positions): values shown, values settable, and facets.\n\n")
	w("A settable attribute is validated when a plan giving a value outside the enumeration fails; otherwise the value is written as given and the file no longer conforms to the schema.\n\n")
	w("| attribute | positions | values | shown | settable | validated | facets | values in corpus not settable |\n| --- | ---: | ---: | ---: | ---: | --- | --- | --- |\n")
	for _, n := range names {
		a := byEnum[n]
		var fs []string
		var fnames []string
		for f := range a.facets {
			fnames = append(fnames, f)
		}
		sort.Strings(fnames)
		for _, f := range fnames {
			fs = append(fs, fmt.Sprintf("%s: %d (%s)", f, len(a.facets[f]), strings.Join(a.facets[f], " ")))
		}
		var missing []string
		for _, v := range a.order {
			if a.count[v] > 0 && !a.write[v] {
				missing = append(missing, fmt.Sprintf("%s (%d)", v, a.count[v]))
			}
		}
		if len(missing) > 12 {
			missing = append(missing[:12], fmt.Sprintf("… %d more", len(missing)-12))
		}
		val := ""
		switch {
		case a.settable == 0:
		case a.validated == a.settable:
			val = "yes"
		case a.validated == 0:
			val = "**no**"
		default:
			val = fmt.Sprintf("%d of %d positions", a.validated, a.settable)
		}
		w("| `%s` | %d | %d | %d | %d | %s | %s | %s |\n", n, a.paths, len(a.order), len(a.read), len(a.write), val, strings.Join(fs, "; "), strings.Join(missing, ", "))
	}
	w("\n")
}

// contains reports whether s holds v.
func contains(s []string, v string) bool {
	return slices.Contains(s, v)
}

// totalWeight sums overall weight and object weight.
func (r *Report) totalWeight() (int64, int64) {
	var all, obj int64
	for _, t := range r.Overall {
		all += t.Weight
		obj += t.ObjWeight
	}
	return all, obj
}

// pct returns n as a percentage of d, or 0 when d is 0.
func pct(n, d int64) float64 {
	if d == 0 {
		return 0
	}
	return 100 * float64(n) / float64(d)
}

// shortCommit returns the 8-character commit from a POI URL, else the URL.
func shortCommit(url string) string {
	const marker = "/poi/"
	if _, after, ok := strings.Cut(url, marker); ok {
		rest := after
		if len(rest) >= 8 {
			return rest[:8]
		}
	}
	return url
}
