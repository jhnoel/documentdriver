package read

import (
	"slices"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/view"
)

// paragraphEntries returns the read/find entry of every body paragraph:
// its index, id (the canonical w14:paraId address), text in view,
// selectable (current-view) text, equations (as LaTeX, with the content
// the LaTeX leaves out), style, list label (number)
// and list membership. Labels and lists are counted as the markup view
// shows them, whatever view the text is in. keep, when not nil, selects
// the entries by their text first, so equations are converted only for
// the entries returned.
func paragraphEntries(st *model.Document, v model.View, opt Options, keep func(text string) bool) ([]Object, error) {
	ids := st.ParaIDs()
	markers := st.ListMarkers(model.All)
	lists := st.Lists(model.All)
	paras := st.XML.Paragraphs()
	out := make([]Object, 0, len(paras))
	for _, p := range paras {
		e := Object{
			"index":           p.Index,
			"text":            model.ParagraphText(p.Node, v),
			"selectable_text": model.ParagraphText(p.Node, model.Current),
		}
		if keep != nil && !keep(e["text"].(string)) {
			continue
		}
		if id, ok := ids.IDs[p.Node]; ok {
			e["id"] = id.ID
		}
		if eqs := model.CurrentEquations(p.Node); len(eqs) > 0 {
			if opt.Equation == nil {
				return nil, ErrNoEquation
			}
			list := make([]any, len(eqs))
			for i, eq := range eqs {
				latex, _, losses := opt.Equation(eq)
				entry := Object{
					"index":    i + 1,
					"latex":    latex,
					"display":  model.IsLocal(eq, "oMathPara"),
					"editable": eq.Parent() == p.Node,
				}
				if lost := view.ContentLosses(losses); len(lost) > 0 {
					l := make([]any, len(lost))
					for j, w := range lost {
						l[j] = w
					}
					entry["loss"] = l
				}
				if id, ok := e["id"].(string); ok {
					entry["id"] = EquationAddress(id, i+1)
				}
				list[i] = entry
			}
			e["equations"] = list
		}
		if s, ok := model.ParagraphStyle(p.Node); ok {
			e["style"] = s
		}
		if m, ok := markers[p.Node]; ok {
			e["number"] = m
		}
		if l, ok := lists[p.Node]; ok {
			e["list"] = Object{
				"num_id":                 l.ID,
				"level":                  l.Level,
				"format":                 l.Format,
				"start":                  l.Start,
				"value":                  l.Value,
				"continued":              l.Continued,
				"continuation_paragraph": l.Continuation,
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// containsString reports whether haystack contains needle.
func containsString(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// rustLower lowercases s as Rust's str::to_lowercase does: per character
// by the Unicode lowercase mapping, with İ (U+0130) becoming "i̇" and a
// capital sigma becoming final ς at the end of a word (preceded by a cased
// letter and not followed by one, case-ignorable characters skipped).
func rustLower(s string) string {
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range rs {
		switch r {
		case 'İ':
			b.WriteString("i̇")
		case 'Σ':
			if casedBefore(rs[:i]) && !casedAfter(rs[i+1:]) {
				b.WriteRune('ς')
			} else {
				b.WriteRune('σ')
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// casedBefore reports whether the preceding significant character is cased.
func casedBefore(rs []rune) bool {
	for _, r := range slices.Backward(rs) {
		if !caseIgnorable(r) {
			return cased(r)
		}
	}
	return false
}

// casedAfter reports whether the following significant character is cased.
func casedAfter(rs []rune) bool {
	for _, r := range rs {
		if !caseIgnorable(r) {
			return cased(r)
		}
	}
	return false
}

// cased is the Unicode Cased property.
func cased(r rune) bool {
	return unicode.In(r, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Other_Lowercase, unicode.Other_Uppercase)
}

// caseIgnorable is the Unicode Case_Ignorable property: marks, format
// characters, modifier letters and symbols, and the word-break MidLetter,
// MidNumLet and Single_Quote characters.
func caseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', '^', '`', '¨', '­', '¯', '´', '·', '¸',
		'·', '՟', '״', '‘', '’', '․', '‧',
		'︓', '﹒', '﹕', '＇', '．', '：':
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}
