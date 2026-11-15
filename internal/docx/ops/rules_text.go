package ops

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/math"
)

// canonicalLaTeX is replace_equation's LaTeX in the canonical form the
// plan's canonical JSON holds (math.CanonicalLaTeX), or why it is not an
// equation the engine can write. The docx CLI takes equations as LaTeX
// only (docxdriver's replace_equation took MathML).
//
// The grammar is display math unless display is false. With display
// unset the operation keeps the target equation's form, which is known
// only when the plan is applied, so the plan accepts what display math
// accepts (a superset: it adds lines separated by \\ at the top level);
// applying such LaTeX to an inline equation is refused then.
func canonicalLaTeX(e *ReplaceEquation) (string, error) {
	return math.CanonicalLaTeX(e.LaTeX, e.Display == nil || *e.Display)
}

// normalizeEquation canonicalises an equation's LaTeX when it parses.
func normalizeEquation(s Spec) {
	e := s.(*ReplaceEquation)
	if l, err := canonicalLaTeX(e); err == nil {
		e.LaTeX = l
	}
}

// normalizeAlignment lowercases a format-paragraph alignment value.
func normalizeAlignment(s Spec) {
	p := s.(*FormatParagraph)
	if p.Alignment != nil {
		a := asciiOnlyLower(*p.Alignment)
		p.Alignment = &a
	}
}

// asciiOnlyLower lowercases ASCII letters only, as Rust's
// to_ascii_lowercase does.
func asciiOnlyLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// zero32 reports whether an optional uint32 is present and zero.
func zero32(p *uint32) bool { return p != nil && *p == 0 }

var replaceEquationRules = []Rule{
	ruleX("LaTeX errors from math.CanonicalLaTeX (internal/docx/math), in display math unless display is false",
		func(e *ReplaceEquation) (string, string, bool) {
			if _, err := canonicalLaTeX(e); err != nil {
				return err.Error(), "latex", true
			}
			return "", "", false
		}, `{"op":"equation.replace","at":"0000000A","latex":"\\frac{a}"}`),
	rule("equation must be positive (1-based)", "at",
		func(e *ReplaceEquation) bool { return zero32(e.At.N) },
		`{"op":"equation.replace","at":"0000000A/equation:0","latex":"x^2"}`),
}

var deleteEquationRules = []Rule{
	rule("equation must be positive (1-based)", "at",
		func(e *DeleteEquation) bool { return zero32(e.At.N) },
		`{"op":"equation.delete","at":"0000000A/equation:0"}`),
}

var replaceTextRules = []Rule{
	rule("select must be non-empty", "select",
		func(r *ReplaceText) bool { return r.Select == "" },
		`{"op":"text.replace","at":"0000000A","select":"","with":"y"}`),
	rule("occurrence must be positive", "occurrence",
		func(r *ReplaceText) bool { return zero32(r.Occurrence) },
		`{"op":"text.replace","at":"0000000A","select":"x","with":"y","occurrence":0}`),
	rule("occurrence and all exclude each other", "all",
		func(r *ReplaceText) bool { return r.All && r.Occurrence != nil },
		`{"op":"text.replace","at":"0000000A","select":"x","with":"y","occurrence":1,"all":true}`),
}

var replaceParagraphRules = []Rule{
	deleteRule[ReplaceParagraph](true, func(r *ReplaceParagraph) []string { return r.Delete },
		`{"op":"paragraph.replace","at":"0000000A","html":"x","delete":["0000000A/table:1"]}`),
}

var insertTextRules = []Rule{
	rule("select must be non-empty; omit it to insert at the start or end of the paragraph", "select",
		func(i *InsertText) bool { return i.Select != nil && *i.Select == "" },
		`{"op":"text.insert","at":"0000000A","select":"","position":"after","with":"y"}`),
	rule("occurrence must be positive", "occurrence",
		func(i *InsertText) bool { return zero32(i.Occurrence) },
		`{"op":"text.insert","at":"0000000A","select":"x","occurrence":0,"position":"after","with":"y"}`),
	rule("occurrence requires select", "occurrence",
		func(i *InsertText) bool { return i.Occurrence != nil && i.Select == nil },
		`{"op":"text.insert","at":"0000000A","occurrence":1,"position":"after","with":"y"}`),
}

var reviseTextRules = []Rule{
	rule("within must be non-empty when given", "within",
		func(r *ReviseText) bool { return r.Within != nil && *r.Within == "" },
		`{"op":"text.revise","within":"","edits":[{"select":"a","with":"b"}]}`),
	rule("occurrence must be positive", "occurrence",
		func(r *ReviseText) bool { return zero32(r.Occurrence) },
		`{"op":"text.revise","within":"w","occurrence":0,"edits":[{"select":"a","with":"b"}]}`),
	rule("occurrence picks a within match; it requires within", "occurrence",
		func(r *ReviseText) bool { return r.Occurrence != nil && r.Within == nil },
		`{"op":"text.revise","occurrence":1,"edits":[{"select":"a","with":"b"}]}`),
	rule("edits must list at least one replacement", "edits",
		func(r *ReviseText) bool { return len(r.Edits) == 0 },
		`{"op":"text.revise","edits":[]}`),
	ruleX("edits[N].select must be non-empty | edits[N].occurrence must be positive | edits[N] sets both all and occurrence; choose one",
		func(r *ReviseText) (string, string, bool) {
			for i, e := range r.Edits {
				at := fmt.Sprintf("edits[%d]", i)
				switch {
				case e.Select == "":
					return at + ".select must be non-empty", at + ".select", true
				case zero32(e.Occurrence):
					return at + ".occurrence must be positive", at + ".occurrence", true
				case e.All && e.Occurrence != nil:
					return at + " sets both all and occurrence; choose one", at, true
				}
			}
			return "", "", false
		}, `{"op":"text.revise","edits":[{"select":"a","with":"b"},{"select":"c","with":"d","all":true,"occurrence":2}]}`),
}

// formatTextClear are the properties format_text's clear may name.
var formatTextClear = []string{"bold", "italic", "underline", "strike", "superscript", "subscript", "highlight", "color", "font_size"}

var formatTextRules = []Rule{
	rule("select must be non-empty", "select",
		func(f *FormatText) bool { return f.Select == "" },
		`{"op":"text.format","at":"0000000A","select":"","bold":true}`),
	rule("occurrence must be positive", "occurrence",
		func(f *FormatText) bool { return zero32(f.Occurrence) },
		`{"op":"text.format","at":"0000000A","select":"x","occurrence":0,"bold":true}`),
	rule("superscript and subscript cannot both be enabled", "superscript",
		func(f *FormatText) bool { return isTrue(f.Superscript) && isTrue(f.Subscript) },
		`{"op":"text.format","at":"0000000A","select":"x","superscript":true,"subscript":true}`),
	rule("color must be #RRGGBB", "color",
		func(f *FormatText) bool { return f.Color != nil && !textColor(*f.Color) },
		`{"op":"text.format","at":"0000000A","select":"x","color":"#abcdef"}`),
	rule("text.format requires a property", "",
		func(f *FormatText) bool {
			return f.Bold == nil && f.Italic == nil && f.Underline == nil && f.Strike == nil &&
				f.Superscript == nil && f.Subscript == nil && f.Highlight == nil && f.Color == nil &&
				f.FontSize == nil && len(f.Clear) == 0
		}, `{"op":"text.format","at":"0000000A","select":"x"}`),
	ruleX("text.format clear contains an unknown property",
		func(f *FormatText) (string, string, bool) {
			for i, c := range f.Clear {
				if !slices.Contains(formatTextClear, c) {
					return "text.format clear contains an unknown property", fmt.Sprintf("clear[%d]", i), true
				}
			}
			return "", "", false
		}, `{"op":"text.format","at":"0000000A","select":"x","clear":["bold","shadow"]}`),
}

// isTrue reports whether an optional boolean is present and true.
func isTrue(b *bool) bool { return b != nil && *b }

// textColor is format_text's color check: "#" and six uppercase hex
// digits.
func textColor(c string) bool {
	x, ok := strings.CutPrefix(c, "#")
	if !ok || len(x) != 6 {
		return false
	}
	for i := 0; i < len(x); i++ {
		if !isHexDigit(x[i]) {
			return false
		}
	}
	return c == strings.ToUpper(c)
}

// indentOutOfRange is docxdriver's 2,000,000-twip indentation bound,
// which SignedPoints decoding already enforces.
func indentOutOfRange(ps ...*SignedPoints) bool {
	for _, p := range ps {
		if p != nil && (*p > 2_000_000 || *p < -2_000_000) {
			return true
		}
	}
	return false
}

var formatParagraphRules = []Rule{
	ruleX("paragraph indentation must be between -100000 and 100000 points",
		func(f *FormatParagraph) (string, string, bool) {
			for _, x := range []struct {
				p    *SignedPoints
				name string
			}{{f.IndentLeft, "indent_left"}, {f.IndentRight, "indent_right"}, {f.FirstLineIndent, "first_line_indent"}} {
				if indentOutOfRange(x.p) {
					return "paragraph indentation must be between -100000 and 100000 points", x.name, true
				}
			}
			return "", "", false
		}, ""),
	rule("style must be non-empty", "style",
		func(f *FormatParagraph) bool { return f.Style != nil && *f.Style == "" },
		`{"op":"paragraph.format","at":"0000000A","style":""}`),
	rule("alignment must be justify, not both", "alignment",
		func(f *FormatParagraph) bool { return f.Alignment != nil && asciiOnlyLower(*f.Alignment) == "both" },
		`{"op":"paragraph.format","at":"0000000A","alignment":"Both"}`),
	rule("invalid alignment", "alignment",
		func(f *FormatParagraph) bool {
			return f.Alignment != nil && !slices.Contains([]string{"left", "center", "right", "justify"}, asciiOnlyLower(*f.Alignment))
		}, `{"op":"paragraph.format","at":"0000000A","alignment":"start"}`),
	rule("paragraph.format requires a property", "",
		func(f *FormatParagraph) bool {
			return f.IndentLeft == nil && f.IndentRight == nil && f.FirstLineIndent == nil &&
				f.SpaceBefore == nil && f.SpaceAfter == nil && f.LineSpacing == nil &&
				f.Style == nil && f.Alignment == nil && len(f.Clear) == 0
		}, `{"op":"paragraph.format","at":"0000000A"}`),
}

var insertParagraphRules = []Rule{
	rule("invalid alias", "as",
		func(p *InsertParagraph) bool { return p.Alias != nil && !cli.ValidAlias(*p.Alias) },
		`{"op":"paragraph.insert","at":"0000000A","position":"after","as":"1st"}`),
	rule("an html list must not be empty", "html",
		func(p *InsertParagraph) bool { return p.HTML.List && len(p.HTML.Items) == 0 },
		`{"op":"paragraph.insert","at":"0000000A","position":"after","html":[]}`),
}

var deleteParagraphsRules = []Rule{
	rule("at must not be empty", "at",
		func(d *DeleteParagraphs) bool { return len(d.At) == 0 },
		`{"op":"paragraph.delete","at":[]}`),
}

// listFormats are the numbering formats set_list writes.
var listFormats = []string{"bullet", "decimal", "lowerLetter", "upperLetter", "lowerRoman", "upperRoman"}

const maxI32 = 1<<31 - 1

// badID reports whether an optional id is zero or above signed 32-bit max.
func badID(p *uint32) bool { return p != nil && (*p == 0 || *p > maxI32) }

// levelProblem is set_list's check of one new list level, at index i.
func levelProblem(i int, l ListLevel) (string, string, bool) {
	at := fmt.Sprintf("levels[%d]", i)
	switch {
	case !slices.Contains(listFormats, l.Format):
		return "invalid list level format, label, start or restart ancestor", at + ".format", true
	case badID(l.Start):
		return "invalid list level format, label, start or restart ancestor", at + ".start", true
	case l.Label == "" || len(l.Label) > 255:
		return "invalid list level format, label, start or restart ancestor", at + ".label", true
	case l.RestartAfter != nil && int(*l.RestartAfter) >= i:
		return "invalid list level format, label, start or restart ancestor", at + ".restart_after", true
	case indentOutOfRange(l.IndentLeft, l.IndentRight, l.FirstLineIndent):
		return "list indentation is out of range", at, true
	}
	b := l.Label
	for j := 0; j < len(b); j++ {
		if b[j] == '%' && (j+1 == len(b) || b[j+1] < '1' || int(b[j+1]) > '1'+i) {
			return "list labels may only reference this level or ancestor %1..%9 counters", at + ".label", true
		}
	}
	if l.MarkerFont != nil && *l.MarkerFont == "" {
		return "invalid list marker font or six-digit RGB color", at + ".marker_font", true
	}
	if l.MarkerColor != nil && !hexDigits(*l.MarkerColor, 6) {
		return "invalid list marker font or six-digit RGB color", at + ".marker_color", true
	}
	return "", "", false
}

// hexDigits reports whether s is exactly n hexadecimal digits.
func hexDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return false
		}
	}
	return true
}

var setListRules = []Rule{
	rule("levels requires a new list with 1..9 definitions covering the selected level", "levels",
		func(l *SetList) bool {
			if l.Levels == nil {
				return false
			}
			n := len(*l.Levels)
			return n == 0 || n > 9 || l.NumID != nil || l.Remove || l.Format != nil || int(l.Level) >= n
		}, `{"op":"list.set","at":["0000000A"],"level":1,"levels":[{"format":"decimal","label":"%1."}]}`),
	ruleX("invalid list level format, label, start or restart ancestor | list indentation is out of range | list labels may only reference this level or ancestor %1..%9 counters | invalid list marker font or six-digit RGB color",
		func(l *SetList) (string, string, bool) {
			if l.Levels == nil {
				return "", "", false
			}
			for i, lv := range *l.Levels {
				if msg, path, bad := levelProblem(i, lv); bad {
					return msg, path, true
				}
			}
			return "", "", false
		}, `{"op":"list.set","at":["0000000A"],"levels":[{"format":"decimal","label":"%1.","marker_color":"red"},{"format":"roman","label":"%2"}]}`),
	ruleX("list.set requires paragraphs, level 0..8 and IDs/starts in 1..2147483647",
		func(l *SetList) (string, string, bool) {
			const msg = "list.set requires paragraphs, level 0..8 and IDs/starts in 1..2147483647"
			switch {
			case len(l.At) == 0:
				return msg, "at", true
			case l.Level > 8:
				return msg, "level", true
			case badID(l.Start):
				return msg, "start", true
			case badID(l.NumID):
				return msg, "num_id", true
			}
			return "", "", false
		}, `{"op":"list.set","at":["0000000A"],"num_id":0}`),
	rule("list.set remove cannot be combined with list settings", "remove",
		func(l *SetList) bool {
			return l.Remove && (l.NumID != nil || l.Format != nil || l.Start != nil || l.Level != 0)
		}, `{"op":"list.set","at":["0000000A"],"remove":true,"level":1}`),
	rule("unsupported list.set format", "format",
		func(l *SetList) bool { return l.Format != nil && !slices.Contains(listFormats, *l.Format) },
		`{"op":"list.set","at":["0000000A"],"format":"roman"}`),
	rule("format cannot restyle an existing list; create a new list", "format",
		func(l *SetList) bool { return l.Format != nil && l.NumID != nil },
		`{"op":"list.set","at":["0000000A"],"format":"bullet","num_id":3}`),
}
