package html

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/htmlx"
)

// The differential check against docxdriver 0.5.2. DOCX_HTMLDIFF names a
// results file from the oracle harness (testdata/README.md); TestGolden
// checks the committed subset. Each case compares three aspects — the
// parsed tree, the fragment items and the document sections — and
// classifies each:
//
//   - match: equal results (errors compared by message);
//   - an intended difference, by rule (intendedClass): Go does the right
//     thing where docxdriver drops, moves or rewrites content, or where
//     docx changed the surface (equations);
//   - mismatch: anything else, which fails the test.

type oracleCase struct {
	Input  string          `json:"input"`
	Source string          `json:"source"`
	Tree   json.RawMessage `json:"tree"`
	Frag   json.RawMessage `json:"frag"`
	Doc    json.RawMessage `json:"doc"`
}

func optJSON(o Opt[string]) any {
	if !o.OK {
		return nil
	}
	return o.V
}

func wrapJSON(w WrapID) any {
	if !w.OK {
		return nil
	}
	return map[string]any{"id": optJSON(w.V)}
}

func fmtJSON(f Fmt) any {
	m := map[string]any{"resets": f.Resets, "b": f.Bold, "i": f.Italic, "u": f.Underline, "s": f.Strike,
		"va": nil, "hl": nil, "color": nil, "size": nil, "font": nil}
	if f.VertAlign.OK {
		m["va"] = f.VertAlign.V.DialectTag()
	}
	if f.Highlight.OK {
		m["hl"] = f.Highlight.V.String()
	}
	if f.Color.OK {
		m["color"] = f.Color.V
	}
	if f.FontSize.OK {
		m["size"] = f.FontSize.V
	}
	if f.FontFamily.OK {
		m["font"] = string(f.FontFamily.V)
	}
	return m
}

func itemJSON(it Item) any {
	switch it.Kind {
	case ItemParaOpen:
		p := it.Para
		var list any
		if p.List.OK {
			l := p.List.V
			list = map[string]any{"cont": l.Continuation, "id": l.ID, "level": l.Level, "format": l.Format, "start": l.Start}
		}
		return map[string]any{"k": "open", "tag": p.Tag, "class": optJSON(p.Class), "bi": wrapJSON(p.BreakIns),
			"bd": wrapJSON(p.BreakDel), "num": optJSON(p.Num), "css": optJSON(p.CSS), "list": list}
	case ItemParaClose:
		return map[string]any{"k": "close"}
	case ItemStruct:
		return map[string]any{"k": "struct", "s": it.Struct}
	}
	a := it.Atom
	common := func(m map[string]any) map[string]any {
		m["ins"], m["del"] = wrapJSON(a.Ins), wrapJSON(a.Del)
		return m
	}
	linked := func(m map[string]any) map[string]any {
		m["link"], m["field"] = optJSON(a.Link), optJSON(a.Field)
		return common(m)
	}
	switch a.Kind {
	case AtomChar:
		var fp any
		if a.FmtPending.OK {
			fp = map[string]any{"id": optJSON(a.FmtPending.V.ID), "author": optJSON(a.FmtPending.V.Author)}
		}
		return linked(map[string]any{"k": "char", "ch": []rune(a.Text)[0], "fmt": fmtJSON(a.Fmt), "fp": fp})
	case AtomBr:
		kind := map[BrKind]string{BrLine: "line", BrPage: "page", BrColumn: "column"}[a.Br]
		return linked(map[string]any{"k": "br", "kind": kind})
	case AtomTab:
		return linked(map[string]any{"k": "tab"})
	case AtomMilestone:
		return map[string]any{"k": "milestone", "start": a.Start, "id": a.ID}
	case AtomNote:
		return common(map[string]any{"k": "note", "endnote": a.Endnote, "id": optJSON(a.NoteID), "inner": a.Inner})
	case AtomEquation:
		return common(map[string]any{"k": "equation", "text": a.Text, "display": a.Display})
	}
	var w, h any
	if a.Width.OK {
		w = a.Width.V
	}
	if a.Height.OK {
		h = a.Height.V
	}
	return linked(map[string]any{"k": "image", "src": a.Src, "rid": a.RID, "w": w, "h": h, "alt": optJSON(a.Alt)})
}

func itemsJSON(items []Item, err error) any {
	if err != nil {
		return map[string]any{"err": err.Error()}
	}
	out := []any{}
	for _, it := range items {
		if it.Kind == ItemAtom && it.Atom.Kind == AtomChar {
			// docxdriver has one atom per character.
			for _, ch := range it.Atom.Text {
				one := it
				one.Atom.Text = string(ch)
				out = append(out, itemJSON(one))
			}
			continue
		}
		out = append(out, itemJSON(it))
	}
	return map[string]any{"ok": out}
}

func docJSON(secs []Section, err error) any {
	if err != nil {
		return map[string]any{"err": err.Error()}
	}
	out := []any{}
	for _, s := range secs {
		slots := []any{}
		for _, c := range s.Slots {
			slots = append(slots, map[string]any{"scope": c.Scope, "kind": c.Kind, "items": itemsJSON(c.Items, c.Err)})
		}
		out = append(out, map[string]any{"title": s.TitlePage, "slots": slots, "body": itemsJSON(s.Body, s.BodyErr), "blank": s.BodyBlank})
	}
	return map[string]any{"ok": out}
}

func treeJSON(input string) any {
	root, err := parseHTML(input)
	if err != nil {
		return map[string]any{"err": strings.TrimPrefix(err.Error(), "invalid HTML: ")}
	}
	return map[string]any{"ok": htmlx.Dump(root)}
}

// normalise round-trips v through JSON so Go and oracle values compare.
func normalise(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func decodeRaw(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func hasErr(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", false
	}
	s, ok := m["err"].(string)
	return s, ok
}

var aspectNames = []string{"tree", "frag", "doc"}

// aspectGo computes one aspect of an input on the Go side.
func aspectGo(name, input string) any {
	input = neutralEquations(input)
	switch name {
	case "tree":
		return treeJSON(input)
	case "frag":
		return itemsJSON(ParseFragment(input, math.LaTeXToOMML))
	}
	return docJSON(ParseDocument(input, math.LaTeXToOMML))
}

// emptyMarkerInValue finds docxdriver's self-closing rewrite applied
// inside a quoted attribute value.
var emptyMarkerInValue = regexp.MustCompile(`["'][^"'>]*<(image|comment-start|comment-end|header|footer|section|page-number|page-count)\b[^<>]*/>`)

// emptyMarkerSelfClosing finds an input docxdriver's pre-pass rewrites.
var emptyMarkerSelfClosing = regexp.MustCompile(`<(image|comment-start|comment-end|header|footer|section|page-number|page-count)\b[^<>]*?/>`)

// intendedClass names the intended difference that explains a Go result,
// or returns "" when none does. Each rule is a docxdriver defect Go does
// not inherit:
//
//   - latex-equations: an error that comes from an equation: docxdriver's
//     MathML validator or LaTeX converter, or docx's rejection of MathML
//     and of equations outside the <equation display="…">LaTeX</equation>
//     surface. (Equations themselves are compared masked, see
//     neutralEquations and maskEquations.)
//   - strict: docxdriver's parser repairs the structure silently (moving
//     text out of tables, reopening formatting after a block, closing an
//     element around content a start tag adds); Go rejects the input with
//     an htmlx.UnsupportedError naming the construct.
//   - strict-input: the agent contract's input policy (docs/contract.md,
//     section 7: HTML5 syntax, strict meaning; htmlx.Options.Strict).
//     docxdriver accepts, by ignoring or inventing, an end tag with
//     nothing to close (</p> becomes an empty paragraph, </br> a line
//     break), an element left open at the end or closed by its parent's
//     end tag, and comments; Go refuses each, naming it. An agent that
//     writes one of these made a mistake the document would silently
//     absorb. (Where docxdriver also refused the input, for another
//     reason, the refusal now comes earlier and reads differently.)
//   - unknown-attribute: docxdriver ignores an attribute it does not
//     lower (title on a span, a misspelt data- name); Go refuses it by
//     name, so that nothing an agent writes is dropped silently. (The
//     inputs' own data-docx-* names are rewritten to the contract's
//     before the comparison: view.FromOracleMarkup.)
//   - attribute-names: an input that spells docxdriver's data-docx-*
//     names where the rewrite to the contract's names changes more than
//     the names: a tokenizer error's offset (the input is shorter), or
//     text that itself spells an old attribute name. The result is Go's
//     on the rewritten input.
//   - duplicate-attribute: docxdriver keeps the first of repeated
//     attributes and drops the rest without a word; Go rejects.
//   - list-continuation-text: docxdriver marks later paragraphs of a list
//     item by splitting its serialised dialect on ` list-id="N"`, which also
//     matches that text in content (and rewrites it); Go marks paragraphs.
//   - error-position-prepass: a tokenizer error in input that holds an
//     empty marker (<section/>, <page-count/>, …): docxdriver reports the
//     offset into its rewritten copy of the input, Go into the input. The
//     error code is the same (checked by the full differential).
//   - error-position-crlf: a tokenizer error after a CR: docxdriver
//     reports the offset into its newline-normalised copy (and a line and
//     column that mix both); Go reports the position in the input.
//   - document-table-style: docxdriver refuses a style on a table, row
//     or cell of document HTML; Go's create stores the style read writes
//     for it (build's table lowering checks the value), so read's own
//     markup is accepted.
//   - document-image-source: an image of document HTML whose src is not
//     a data: URL; both refuse it, Go's create with a message that names
//     read's asset address and where its bytes are (ImageSourceError).
//   - empty-marker-prepass: docxdriver rewrites <section/>, <image/> and
//     the other empty markers with a regular expression over the raw input,
//     including inside attribute values; Go applies the rule to tags only.
//
// strictInput matches the refusals of strict input (htmlx.Options.Strict).
var strictInput = regexp.MustCompile(`^unsupported HTML structure: (</[^>]*> has no open <[^>]*> to close|<[^>]*> is not closed|a comment; nothing stores comments|</[^>]*> while <[^>]*> is still open)`)

func intendedClass(input string, goV, rustV any) string {
	goErr, isErr := hasErr(goV)
	rustErr, rustIsErr := hasErr(rustV)
	switch {
	case isErr && goEquationError.MatchString(goErr), rustIsErr && rustEquationError.MatchString(rustErr):
		return "latex-equations"
	case isErr && strings.HasPrefix(goErr, "duplicate HTML attribute "):
		return "duplicate-attribute"
	case isErr && rustIsErr && (strings.Contains(goErr, "is read's address of an image") || strings.Contains(goErr, "create fetches nothing")):
		return "document-image-source"
	case !isErr && rustIsErr && strings.Contains(rustErr, "unsupported table formatting attribute style"):
		return "document-table-style"
	case isErr && strictInput.MatchString(goErr):
		return "strict-input"
	case isErr && strings.HasPrefix(goErr, "unknown HTML attribute "):
		return "unknown-attribute"
	case isErr && (strings.HasPrefix(goErr, "unsupported HTML structure: ") || strings.HasPrefix(goErr, "unsupported HTML element <")):
		return "strict"
	case strings.Contains(input, `list-id="`):
		return "list-continuation-text"
	case isErr && strings.Contains(goErr, "ParseDiagnostic") && emptyMarkerSelfClosing.MatchString(input):
		return "error-position-prepass"
	case isErr && strings.Contains(goErr, "ParseDiagnostic") && strings.Contains(input, "\r"):
		return "error-position-crlf"
	case emptyMarkerInValue.MatchString(input):
		return "empty-marker-prepass"
	}
	return ""
}

// classify compares one aspect of a case.
func classify(input string, goV, rustV any) string {
	if fmtJSON2(goV) == fmtJSON2(rustV) {
		return "match"
	}
	if neutralEquations(input) != input {
		if class := equationClass(input, goV, rustV); class != "" {
			return class
		}
	}
	if class := intendedClass(input, goV, rustV); class != "" {
		if strings.HasPrefix(class, "error-position-") && diagnosticCode(goV) != diagnosticCode(rustV) {
			return "mismatch"
		}
		return class
	}
	return "mismatch"
}

func runDiff(t *testing.T, path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []oracleCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	var bad []string
	var golden []goldenCase
	codes := map[string]bool{}
	for _, c := range cases {
		raws := map[string]json.RawMessage{"tree": c.Tree, "frag": c.Frag, "doc": c.Doc}
		g := goldenCase{Input: c.Input, Source: c.Source, Aspects: map[string]goldenAspect{}}
		for _, name := range aspectNames {
			goV := maskEquations(name, normalise(t, aspectGo(name, c.Input)))
			rustV := maskEquations(name, decodeRaw(t, raws[name]))
			class := classify(c.Input, goV, rustV)
			counts[name+" "+class]++
			a := goldenAspect{Class: class}
			if class == "match" {
				a.Hash = hashJSON(t, rustV)
			} else {
				a.Hash = hashJSON(t, goV)
			}
			g.Aspects[name] = a
			if class == "mismatch" && len(bad) < 40 {
				bad = append(bad, fmt.Sprintf("%s %q (%s):\n  go:   %s\n  rust: %s", name, c.Input, c.Source,
					short(fmtJSON2(goV)), short(fmtJSON2(rustV))))
			}
		}
		if goldenWorthy(c, codes) {
			golden = append(golden, g)
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%-28s %d", k, counts[k])
	}
	for _, m := range bad {
		t.Error(m)
	}
	if os.Getenv("DOCX_HTMLDIFF_WRITE") != "" && len(bad) == 0 {
		writeGolden(t, golden)
		t.Logf("wrote %d golden cases", len(golden))
	}
}

func short(s string) string {
	if len(s) > 600 {
		return s[:600] + "…"
	}
	return s
}

// TestDifferential runs the full oracle results named by DOCX_HTMLDIFF.
func TestDifferential(t *testing.T) {
	path := os.Getenv("DOCX_HTMLDIFF")
	if path == "" {
		t.Skip("DOCX_HTMLDIFF not set")
	}
	runDiff(t, path)
}

// diagnosticCode extracts the tokenizer error code from an error result.
func diagnosticCode(v any) string {
	msg, _ := hasErr(v)
	_, code, _ := strings.Cut(msg, `message: "`)
	code, _, _ = strings.Cut(code, `"`)
	return code
}

// Equations are compared masked: docx changed their surface (LaTeX in
// <equation display="…">, no MathML), so the input fed to Go has each
// equation replaced by a neutral inline one, and both results have their
// equation atoms reduced to their revision wrapping and their equation
// subtrees (in parse dumps and note bodies) to a marker. Everything around
// an equation is still compared exactly.

var (
	equationElement = regexp.MustCompile(`(?is)(^|[^a-z0-9])<equation(?:[\s/][^>]*)?>.*?</equation>`)
	mathElement     = regexp.MustCompile(`(?is)(^|[^a-z0-9])<math(?:[\s/][^>]*)?>.*?</math>`)
	// goEquationError and rustEquationError match errors that come from an
	// equation on each side.
	goEquationError   = regexp.MustCompile(`^(MathML is not supported|<equation> |invalid LaTeX equation)`)
	rustEquationError = regexp.MustCompile(`^(legacy equation:|invalid MathML|MathML |unsupported MathML|math requires a paragraph)`)
)

const neutralEquation = `<equation display="inline">x</equation>`

// neutralEquations replaces each equation (docx or 0.5.2 form) in input.
func neutralEquations(input string) string {
	input = equationElement.ReplaceAllString(input, "${1}"+neutralEquation)
	return mathElement.ReplaceAllString(input, "${1}"+neutralEquation)
}

// maskEquations reduces the equations in a result of aspect name.
func maskEquations(name string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		if x["k"] == "equation" {
			return map[string]any{"k": "equation", "ins": x["ins"], "del": x["del"]}
		}
		out := make(map[string]any, len(x))
		for k, e := range x {
			if s, ok := e.(string); ok && name == "tree" && k == "ok" {
				out[k] = maskDump(s)
				continue
			}
			out[k] = maskEquations(name, e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = maskEquations(name, e)
		}
		return out
	case string:
		return equationElement.ReplaceAllString(x, "${1}<equation/>")
	}
	return v
}

// maskDump replaces each <math> or <equation> subtree of a parse dump
// with one <eq> line.
func maskDump(s string) string {
	var out []string
	skip := -1
	for line := range strings.SplitSeq(s, "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if skip >= 0 && indent > skip && line != "" {
			continue
		}
		skip = -1
		body := strings.TrimLeft(line, " ")
		if strings.HasPrefix(body, "<math") || strings.HasPrefix(body, "<equation>") {
			out = append(out, line[:indent]+"<eq>")
			skip = indent
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// mathBreakout finds HTML start tags that end MathML content in HTML5
// parsing: docxdriver moves what follows out of the equation.
var mathBreakout = regexp.MustCompile(`(?i)<(b|big|blockquote|body|br|center|code|dd|div|dl|dt|em|embed|h[1-6]|head|hr|i|img|li|listing|menu|meta|nobr|ol|p|pre|ruby|s|small|span|strong|strike|sub|sup|table|tt|u|ul|var)\b`)

// equationClass explains a difference in an input whose equations the
// test replaced (neutralEquations):
//
//   - match: the same tokenizer error, at an offset the replacement moved;
//   - latex-equations: docxdriver fails on a tokenizer error inside the
//     equation markup, or its MathML holds HTML that the HTML5 parser moves
//     out of the equation. docx rejects that markup (it takes no MathML),
//     so there is nothing to compare.
func equationClass(input string, goV, rustV any) string {
	goCode, rustCode := diagnosticCode(goV), diagnosticCode(rustV)
	if rustCode != "" && goCode == rustCode {
		return "match"
	}
	spans := append(equationElement.FindAllString(input, -1), mathElement.FindAllString(input, -1)...)
	for _, s := range spans {
		if mathBreakout.MatchString(strings.TrimPrefix(s, s[:min(len(s), 5)])) {
			return "latex-equations"
		}
		if rustCode == "" {
			continue
		}
		// Tokenize the span with <math> renamed, so the rejection of
		// MathML does not stop the tokenizer before the error.
		renamed := strings.NewReplacer("<math", "<x-math", "</math", "</x-math").Replace(s)
		if _, err := htmlx.ParseHTML(renamed, htmlx.Options{}); err != nil && strings.Contains(err.Error(), `message: "`+rustCode+`"`) {
			return "latex-equations"
		}
	}
	return ""
}
