package ops

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/span"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ReviseText is text.revise (docxdriver's revise_text).
type ReviseText struct {
	At         *ParagraphAddress `json:"at" default:"null"`
	Within     *string           `json:"within" default:"null"`
	Occurrence *uint32           `json:"occurrence" default:"null"`
	Edits      []TextEdit        `json:"edits"`
}

// opTextRevise is text.revise's entry in the operation table.
var opTextRevise = OpDef{
	Name: "text.revise", Oracle: "revise_text", New: func() Spec { return &ReviseText{} },
	Rules: reviseTextRules, DirectGuards: textGuard,
}

// reviseRegion is one paragraph region the scope covers, in final-view
// characters of the paragraph as the operation found it.
type reviseRegion struct {
	part       string
	p          *tree.Node
	id         string
	text       []rune
	start, end int
}

// reviseSpan is one edit's match: region, final-view range, edit index.
type reviseSpan struct{ region, start, end, edit int }

// preview quotes text, cut at 240 characters (typed.rs preview).
func preview(text string) string {
	if utf8.RuneCountInString(text) <= 240 {
		return strconv.Quote(text)
	}
	return strconv.Quote(string([]rune(text)[:240])) + "…"
}

// charMatches are the non-overlapping matches of needle in text, in
// characters.
func charMatches(text []rune, needle string) [][2]int {
	if needle == "" {
		return nil
	}
	s, n := string(text), utf8.RuneCountInString(needle)
	var out [][2]int
	from, chars := 0, 0
	for {
		i := strings.Index(s[from:], needle)
		if i < 0 {
			return out
		}
		chars += utf8.RuneCountInString(s[from : from+i])
		out = append(out, [2]int{chars, chars + n})
		chars += n
		from += i + len(needle)
	}
}

// candidates are the paragraphs the operation may edit: at's, or every
// paragraph of the main document part with its address (typed.rs
// addressable_paragraphs).
func (o *ReviseText) candidates(env *Env) ([]reviseRegion, error) {
	if o.At != nil {
		part, p, id, err := env.textParagraph(*o.At)
		if err != nil {
			return nil, err
		}
		return []reviseRegion{{part: part, p: p, id: id}}, nil
	}
	main, x, err := env.Main()
	if err != nil {
		return nil, err
	}
	var out []reviseRegion
	for _, p := range x.AllParagraphs() {
		if id, ok := env.WS.ParaID(p); ok {
			out = append(out, reviseRegion{part: main, p: p, id: id.ID})
		} else if v, ok := model.Attr(p, "paraId"); ok && model.IsStoredParaID(v) {
			// A paragraph an earlier operation created.
			out = append(out, reviseRegion{part: main, p: p, id: strings.ToUpper(v)})
		}
	}
	return out, nil
}

// Run makes several exact replacements inside one scope (typed.rs
// revise_text_typed): a paragraph (at), a match of within (in at, or in
// the document), or the whole document; every edit's select is matched
// in the scope as it stands when the operation starts, so earlier edits
// never shift later ones, and edits may not overlap. Each replacement
// goes through the text engine as text.replace's does.
func (o *ReviseText) Run(env *Env) error {
	at := ""
	if o.At != nil {
		at = strings.ToUpper(string(*o.At))
	}
	fail := func(code, field, format string, args ...any) error {
		msg := fmt.Sprintf(format, args...)
		if at != "" {
			msg = "blocked in paragraph " + at + ": " + msg
		}
		return &Rejection{Code: code, Field: field, Msg: msg}
	}
	for i, e := range o.Edits {
		if strings.Contains(e.Select, "<") {
			return fail("operation_failed", fmt.Sprintf("edits[%d].select", i), "edits[%d].select must be plain text without HTML markup — it addresses visible text, never markup", i)
		}
		if err := checkWith(fmt.Sprintf("edits[%d].with", i), e.With); err != nil {
			return err
		}
	}
	cands, err := o.candidates(env)
	if err != nil {
		return err
	}
	if o.At != nil {
		at = cands[0].id
	}
	for i := range cands {
		cands[i].text = []rune(span.Read(cands[i].p).Text(model.Current))
	}
	regions, err := o.regions(cands, at, fail)
	if err != nil {
		return err
	}
	scope := "the document"
	switch {
	case o.Within != nil:
		scope = "within " + strconv.Quote(*o.Within)
	case o.At != nil:
		scope = "this paragraph (current visible text: " + preview(string(regions[0].text)) + ")"
	}
	var spans []reviseSpan
	for i, e := range o.Edits {
		var ms []reviseSpan
		for r, reg := range regions {
			for _, m := range charMatches(reg.text[reg.start:reg.end], e.Select) {
				ms = append(ms, reviseSpan{r, reg.start + m[0], reg.start + m[1], i})
			}
		}
		field := fmt.Sprintf("edits[%d]", i)
		switch n := len(ms); {
		case n == 0:
			return fail("not_found", field+".select", "edits[%d].select %q matched nothing in %s", i, e.Select, scope)
		case e.All:
			spans = append(spans, ms...)
		case e.Occurrence != nil && int(*e.Occurrence) <= n:
			spans = append(spans, ms[*e.Occurrence-1])
		case e.Occurrence != nil:
			return fail("not_found", field+".occurrence", "edits[%d].occurrence %d is out of range: select %q matched %d times in %s", i, *e.Occurrence, e.Select, n, scope)
		case n == 1:
			spans = append(spans, ms[0])
		default:
			return fail("ambiguous", field+".select", "edits[%d].select %q matched %d times in %s — narrow the scope with within, lengthen select, or pass occurrence or all", i, e.Select, n, scope)
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		a, b := spans[i], spans[j]
		if a.region != b.region {
			return a.region < b.region
		}
		if a.start != b.start {
			return a.start < b.start
		}
		return a.end < b.end
	})
	for k := 1; k < len(spans); k++ {
		a, b := spans[k-1], spans[k]
		if a.region == b.region && b.start < a.end {
			msg := fmt.Sprintf("edits[%d] and edits[%d] select overlapping text; combine them into one edit", min(a.edit, b.edit), max(a.edit, b.edit))
			return &Rejection{Code: "operation_failed", Field: "edits", Msg: "blocked in paragraph " + regions[a.region].id + ": " + msg}
		}
	}
	var story *core.Handle
	for r, reg := range regions {
		var edits []textEdit
		for _, s := range spans {
			if s.region == r {
				edits = append(edits, textEdit{s.start, s.end, o.Edits[s.edit].With, fmt.Sprintf("edits[%d].select", s.edit), fmt.Sprintf("edits[%d].with", s.edit)})
			}
		}
		if len(edits) == 0 {
			continue
		}
		// One handle for the operation: the Reviser finishes every
		// revision it authored through the handle of the first.
		if story == nil {
			if o.At != nil {
				story, err = env.WS.Core().Range(reg.part, "paragraph "+reg.id, reg.p, reg.p)
			} else {
				story, err = env.bodyStory()
			}
			if err != nil {
				return err
			}
		}
		h := story
		part := reg.part
		t := &textTarget{part: part, p: reg.p, id: reg.id, h: h, ids: func() (string, error) { return env.WS.RevisionID(part) }}
		if err := env.editText(t, edits); err != nil {
			return err
		}
	}
	return nil
}

// regions resolves the scope: within's one match (occurrence picks one of
// several), or each candidate paragraph whole.
func (o *ReviseText) regions(cands []reviseRegion, at string, fail func(code, field, format string, args ...any) error) ([]reviseRegion, error) {
	if o.Within == nil {
		for i := range cands {
			cands[i].start, cands[i].end = 0, len(cands[i].text)
		}
		return cands, nil
	}
	within := *o.Within
	var found []reviseRegion
	for _, c := range cands {
		for _, m := range charMatches(c.text, within) {
			r := c
			r.start, r.end = m[0], m[1]
			found = append(found, r)
		}
	}
	place := "the document"
	if at != "" {
		place = "this paragraph"
	}
	switch n := len(found); {
	case n == 0:
		detail := ""
		if at != "" {
			detail = " (current visible text: " + preview(string(cands[0].text)) + ")"
		}
		clause := ""
		for _, c := range cands {
			if clause = hiddenClause(span.Read(c.p), within, false); clause != "" {
				break
			}
		}
		return nil, fail("not_found", "within", "within %q matched nothing in %s%s%s", within, place, detail, clause)
	case o.Occurrence != nil && int(*o.Occurrence) <= n:
		return found[*o.Occurrence-1 : *o.Occurrence], nil
	case o.Occurrence != nil:
		return nil, fail("not_found", "occurrence", "occurrence %d is out of range: within matched %d times in %s", *o.Occurrence, n, place)
	case n == 1:
		return found, nil
	default:
		var ids []string
		for _, f := range found {
			if len(ids) == 0 || ids[len(ids)-1] != f.id {
				ids = append(ids, f.id)
			}
		}
		ids = ids[:min(len(ids), 8)]
		return nil, fail("ambiguous", "within", "within %q matched %d times in %s (paragraphs %s) — lengthen it, pass at, or pass occurrence (1-based)", within, n, place, strings.Join(slices.Clip(ids), ", "))
	}
}

// bodyStory opens a region over the main document's body (its root when
// it has none).
func (env *Env) bodyStory() (*core.Handle, error) {
	main, x, err := env.Main()
	if err != nil {
		return nil, err
	}
	root := x.Body()
	if root == nil {
		root = x.Root()
	}
	return env.Story(main, root), nil
}
