package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/span"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// The laws of the span family (text.replace, text.revise, text.insert,
// text.format and the bookmark operations): the change-mode law per plan
// on the law inputs and write fixtures, and the five value laws
// (laws.Suite) where a value reads back.

// spanSuite is the change-mode law of one span plan, of class class.
func spanSuite(name, opsJSON string, class laws.ModeClass) *laws.ModeSuite {
	return &laws.ModeSuite{
		Name:  name,
		Class: class,
		Apply: func(src, dst, mode string) error { return runPlan(src, dst, mode, opsJSON) },
		Refused: func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		},
		// The direct-mode guard (a selection overlapping a tracked change
		// or a comment range) refuses in direct mode only.
		Guarded: func(mode string, err error) bool {
			var r refusal
			return mode == laws.Direct && errors.As(err, &r) && strings.Contains(r.d.Message, "untracked replacement requires")
		},
		Project:        projectNoSource,
		Settle:         settleFile,
		AcceptAuthored: acceptAuthored,
	}
}

// plainParagraph is a body paragraph a value law may rewrite: its id and
// final-view text, when the text is non-empty and holds no object, and
// the paragraph has no revision, comment, field, content control or
// unknown content.
type plainParagraph struct{ id, text string }

// plainParagraphs lists the body's plain paragraphs (in tables too).
func plainParagraphs(path string) ([]plainParagraph, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		return nil, err
	}
	d, err := model.OpenDocument(b)
	if err != nil {
		return nil, err
	}
	if d.XML.Body() == nil {
		return nil, nil
	}
	idx := d.ParaIDs()
	if len(idx.Repaired) > 0 {
		// Saving stores the repaired ids and their namespace on the root:
		// the one change outside the target that a write makes.
		return nil, errors.New("paragraph ids need repair")
	}
	ids := idx.IDs
	var out []plainParagraph
	for _, p := range d.XML.Paragraphs() {
		id, ok := ids[p.Node]
		if !ok {
			continue
		}
		text := model.ParagraphText(p.Node, model.Current)
		if text == "" || strings.ContainsAny(text, "<￼") || text != model.ParagraphText(p.Node, model.All) || text != model.ParagraphText(p.Node, model.Baseline) {
			continue
		}
		if _, _, bad := span.Blocker([]*tree.Node{p.Node}); bad || len(span.Unknown([]*tree.Node{p.Node})) > 0 {
			continue
		}
		out = append(out, plainParagraph{id.ID, text})
	}
	return out, nil
}

// textSet is the value law's op: set paragraph at's final text to text.
type textSet struct{ at, text string }

// textReplaceSuite is text.replace's value suite: a paragraph's final
// text, set by replacing the whole text (select is the text as it
// stands when the plan runs, earlier ops of the plan included).
func textReplaceSuite(mode string) *laws.Suite[string] {
	read := func(path string) ([]laws.Value[string], error) {
		ps, err := plainParagraphs(path)
		if err != nil {
			return nil, err
		}
		var out []laws.Value[string]
		for _, p := range ps {
			out = append(out, laws.Value[string]{Target: laws.Target{Part: "word/document.xml", Addr: p.id}, V: p.text})
		}
		return out, nil
	}
	return &laws.Suite[string]{
		Name: "text.replace " + mode,
		Read: func(path string) ([]laws.Value[string], error) {
			vals, err := read(path)
			if err != nil {
				return nil, err
			}
			if mode == laws.Track {
				return finalTexts(path, vals)
			}
			return vals, nil
		},
		Show: func(v string) string { return v },
		Set:  func(t laws.Target, v string) laws.Op { return textSet{t.Addr, v} },
		Vary: func(rng *rand.Rand, _ laws.Target, cur string) string {
			words := strings.Fields(cur)
			if len(words) == 0 {
				return cur + " varied"
			}
			k := rng.IntN(len(words))
			return strings.Replace(cur, words[k], words[k]+"-v", 1)
		},
		Apply: func(src, dst string, opsIn []laws.Op) error {
			cur := map[string]string{}
			vals, err := read(src)
			if err != nil {
				return err
			}
			for _, v := range vals {
				cur[v.Target.Addr] = v.V
			}
			var list []map[string]any
			for _, o := range opsIn {
				s := o.(textSet)
				if s.at == "" {
					list = append(list, map[string]any{"op": "text.replace", "at": "0FFFFFF0", "select": "x", "with": "y"})
					continue
				}
				list = append(list, map[string]any{"op": "text.replace", "at": s.at, "select": cur[s.at], "with": s.text})
				cur[s.at] = s.text
			}
			b, err := json.Marshal(list)
			if err != nil {
				return err
			}
			return runInPlace(src, dst, mode, string(b))
		},
		Refused: func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		},
		Units: docxedit.Units(),
		Owns: func(t laws.Target, e *tree.Node) bool {
			if !model.IsW(e, "p") {
				return false
			}
			v, ok := model.Attr(e, "paraId")
			return !ok || strings.EqualFold(v, t.Addr)
		},
		Fail:   func() laws.Op { return textSet{} },
		Budget: 6,
	}
}

// finalTexts replaces each value with its paragraph's text after
// accepting every revision: what a tracked replacement reads back as.
func finalTexts(path string, vals []laws.Value[string]) ([]laws.Value[string], error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		return nil, err
	}
	d, err := model.OpenDocument(b)
	if err != nil {
		return nil, err
	}
	ids := d.ParaIDs().IDs
	byID := map[string]string{}
	for _, p := range d.XML.Paragraphs() {
		if id, ok := ids[p.Node]; ok {
			byID[id.ID] = model.ParagraphText(p.Node, model.Current)
		}
	}
	var out []laws.Value[string]
	for _, v := range vals {
		out = append(out, laws.Value[string]{Target: v.Target, V: byID[v.Target.Addr]})
	}
	return out, nil
}

// spanInputs are the law inputs and the authored write fixtures.
func spanInputs(t *testing.T) []string { return deleteInputs(t) }

// TestTextReplaceLaws runs text.replace's value laws (direct mode) and
// its change-mode law on the law inputs: a unique word, every match of a
// repeated word, a deleted word, a word turned into two, and a
// replacement across a tab.
func TestTextReplaceLaws(t *testing.T) {
	rep := reportInputs(t, spanInputs(t), func(t *testing.T, in string, i int, rep *laws.Report) {
		in = storedIDs(t, in)
		textReplaceSuite(laws.Direct).Check(t, in, uint64(i), rep)
		pid, word := uniqueWord(t, in)
		if word == "" {
			return
		}
		plans := map[string]string{
			"unique word": `[{"op":"text.replace","at":"` + pid + `","select":"` + word + `","with":"Replaced"}]`,
			"delete word": `[{"op":"text.replace","at":"` + pid + `","select":"` + word + `","with":""}]`,
			"two words":   `[{"op":"text.replace","at":"` + pid + `","select":"` + word + `","with":"` + word + ` and more"}]`,
		}
		if id, w := repeatedWord(t, in); w != "" {
			plans["all"] = `[{"op":"text.replace","at":"` + id + `","select":"` + w + `","with":"ALL","all":true}]`
		}
		for name, ops := range plans {
			spanSuite("text.replace "+name, ops, laws.AcceptAuthored).Check(t, in)
		}
	})
	t.Logf("text.replace: %s", rep)
}

// repeatedWord returns a word of at least two letters that occurs more
// than once in the final text of a body paragraph, with the paragraph's
// address, or "".
func repeatedWord(t *testing.T, path string) (id, word string) {
	t.Helper()
	ps, err := plainParagraphs(path)
	if err != nil {
		return "", ""
	}
	for _, p := range ps {
		for _, w := range strings.Fields(p.text) {
			if len(w) >= 2 && strings.Count(p.text, w) > 1 && !strings.ContainsAny(w, `<"\`) {
				return p.id, w
			}
		}
	}
	return "", ""
}

// writeFixture is the path of an authored write fixture.
func writeFixture(t *testing.T, name string) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "tools", "oracle", "testdata", "write", name)
}

// runInPlace is runPlan for the value laws, which also apply a plan to
// a file in place (dst == src): the file is not copied onto itself.
func runInPlace(src, dst, mode, opsJSON string) error {
	if src != dst {
		return runPlan(src, dst, mode, opsJSON)
	}
	b, err := os.ReadFile(dst) //nolint:gosec // G304: the law's private copy
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	text := []byte(`{"base":"sha256:` + hex.EncodeToString(sum[:]) + `","author":"` + lawAuthor + `","change_mode":"` + mode + `","ops":` + opsJSON + `}`)
	r, err := runText(dst, text, Options{View: viewOpt})
	if err != nil {
		return err
	}
	if r.Outcome == Rejected {
		return refusal{r.Diagnostic}
	}
	c, err := runText(dst, text, Options{Commit: r.PreviewKey, Now: "2026-01-01T00:00:00Z", View: viewOpt})
	if err != nil {
		return err
	}
	if c.Outcome == Rejected {
		return refusal{c.Diagnostic}
	}
	return sameHazards(c)
}

// storedIDs returns in, or when some of its paragraph ids are not stored
// a copy of it whose ids are (a direct paragraph.format of its first body
// paragraph saves them): the value laws' untargeted check holds a save to
// the target's own bytes, and storing the ids is the one change a save
// makes beyond them.
func storedIDs(t *testing.T, in string) string {
	t.Helper()
	b, err := os.ReadFile(in) //nolint:gosec // G304: a law input
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(b)
	if err != nil || len(d.ParaIDs().Repaired) == 0 {
		return in
	}
	ids := bodyParagraphIDs(t, in)
	if len(ids) == 0 {
		return in
	}
	out := filepath.Join(t.TempDir(), filepath.Base(in))
	if err := runPlan(in, out, laws.Direct, `[{"op":"paragraph.format","at":"`+ids[0]+`","alignment":"center"}]`); err != nil {
		t.Logf("%s: ids not stored: %v", filepath.Base(in), err)
		return in
	}
	return out
}

// TestTextReviseLaws runs text.revise's change-mode law on the law
// inputs: two edits in one paragraph, every match of a repeated word in
// the whole document, and one inside a within span.
func TestTextReviseLaws(t *testing.T) {
	eachInput(t, spanInputs(t), func(t *testing.T, in string, _ int) {
		in = storedIDs(t, in)
		plans := map[string]string{}
		if id, w := repeatedWord(t, in); w != "" {
			plans["document all"] = `[{"op":"text.revise","edits":[{"select":"` + w + `","with":"` + strings.ToUpper(w) + `x","all":true}]}]`
			plans["within"] = `[{"op":"text.revise","at":"` + id + `","within":"` + w + `","occurrence":1,"edits":[{"select":"` + w + `","with":"W"}]}]`
		}
		if ps, err := plainParagraphs(in); err == nil {
			for _, p := range ps {
				words := strings.Fields(p.text)
				if len(words) >= 3 && strings.Count(p.text, words[0]) == 1 && strings.Count(p.text, words[len(words)-1]) == 1 && words[0] != words[len(words)-1] && !strings.ContainsAny(p.text, `"\`) {
					plans["two edits"] = `[{"op":"text.revise","at":"` + p.id + `","edits":[{"select":"` + words[0] + `","with":"First"},{"select":"` + words[len(words)-1] + `","with":"last."}]}]`
					break
				}
			}
		}
		for name, ops := range plans {
			spanSuite("text.revise "+name, ops, laws.AcceptAuthored).Check(t, in)
		}
	})
}

// TestTextInsertLaws runs text.insert's change-mode law on the law
// inputs: before and after a word, and at the start and end of a
// paragraph.
func TestTextInsertLaws(t *testing.T) {
	eachInput(t, spanInputs(t), func(t *testing.T, in string, _ int) {
		in = storedIDs(t, in)
		pid, word := uniqueWord(t, in)
		if word == "" {
			return
		}
		plans := map[string]string{
			"before": `[{"op":"text.insert","at":"` + pid + `","select":"` + word + `","position":"before","with":"new "}]`,
			"after":  `[{"op":"text.insert","at":"` + pid + `","select":"` + word + `","position":"after","with":", inserted\tthere"}]`,
			"start":  `[{"op":"text.insert","at":"` + pid + `","position":"before","with":"Start: "}]`,
			"end":    `[{"op":"text.insert","at":"` + pid + `","position":"after","with":" (end)\nline"}]`,
			"twice":  `[{"op":"text.insert","at":"` + pid + `","position":"after","with":" one"},{"op":"text.insert","at":"` + pid + `","position":"after","with":" two"}]`,
		}
		for name, ops := range plans {
			spanSuite("text.insert "+name, ops, laws.AcceptAuthored).Check(t, in)
		}
	})
}

// boldOf is the direct bold of word's runs in paragraph p: "on", "off"
// or "absent", or "" when the runs disagree.
func boldOf(p *tree.Node, word string) string {
	para := span.Read(p)
	ms := para.Find(model.Current, word)
	if len(ms) != 1 {
		return ""
	}
	state := ""
	for _, g := range para.Segs {
		if g.Start >= ms[0].Span.End || g.End <= ms[0].Span.Start || g.Run == nil {
			continue
		}
		v := "absent"
		if rpr := model.ChildW(g.Run, "rPr"); rpr != nil {
			if b := model.ChildW(rpr, "b"); b != nil {
				switch val, ok := model.Attr(b, "val"); {
				case !ok || val == "1" || val == "true" || val == "on":
					v = "on"
				case val == "0" || val == "false" || val == "off":
					v = "off"
				default:
					return ""
				}
			}
		}
		if state != "" && state != v {
			return ""
		}
		state = v
	}
	return state
}

// textFormatSuite is text.format's value suite: the direct bold of the
// first unique word of each plain paragraph (absent, on or off), set with
// bold true, bold false or clear.
func textFormatSuite() *laws.Suite[string] {
	read := func(path string) ([]laws.Value[string], error) {
		ps, err := plainParagraphs(path)
		if err != nil {
			return nil, err
		}
		b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
		if err != nil {
			return nil, err
		}
		d, err := model.OpenDocument(b)
		if err != nil {
			return nil, err
		}
		nodes := map[string]*tree.Node{}
		for p, id := range d.ParaIDs().IDs {
			nodes[id.ID] = p
		}
		var out []laws.Value[string]
		for _, p := range ps {
			for _, w := range strings.Fields(p.text) {
				if len(w) >= 3 && strings.Count(p.text, w) == 1 && !strings.ContainsAny(w, `<"\|`) {
					if v := boldOf(nodes[p.id], w); v != "" {
						out = append(out, laws.Value[string]{Target: laws.Target{Part: "word/document.xml", Addr: p.id + "|" + w}, V: v})
					}
					break
				}
			}
		}
		return out, nil
	}
	return &laws.Suite[string]{
		Name: "text.format bold",
		Read: read,
		Show: func(v string) string { return v },
		Set:  func(t laws.Target, v string) laws.Op { return textSet{t.Addr, v} },
		Vary: func(rng *rand.Rand, _ laws.Target, cur string) string {
			others := slices.DeleteFunc([]string{"absent", "on", "off"}, func(s string) bool { return s == cur })
			return others[rng.IntN(len(others))]
		},
		Apply: func(src, dst string, opsIn []laws.Op) error {
			var list []map[string]any
			for _, o := range opsIn {
				s := o.(textSet)
				id, w, _ := strings.Cut(s.at, "|")
				op := map[string]any{"op": "text.format", "at": id, "select": w}
				switch s.text {
				case "on":
					op["bold"] = true
				case "off":
					op["bold"] = false
				case "absent":
					op["clear"] = []string{"bold"}
				default: // the failing op
					op["at"], op["select"], op["bold"] = "0FFFFFF0", "x", true
				}
				list = append(list, op)
			}
			b, err := json.Marshal(list)
			if err != nil {
				return err
			}
			return runInPlace(src, dst, laws.Direct, string(b))
		},
		Refused: func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		},
		Units: docxedit.Units(),
		Owns: func(t laws.Target, e *tree.Node) bool {
			if !model.IsW(e, "p") {
				return false
			}
			id, _, _ := strings.Cut(t.Addr, "|")
			v, ok := model.Attr(e, "paraId")
			return !ok || strings.EqualFold(v, id)
		},
		Fail:   func() laws.Op { return textSet{"x|y", "fail"} },
		Budget: 6,
	}
}

// TestTextFormatLaws runs text.format's value laws and its change-mode
// law (bold, colour and size on a unique word; clearing them) on the law
// inputs.
func TestTextFormatLaws(t *testing.T) {
	rep := reportInputs(t, spanInputs(t), func(t *testing.T, in string, i int, rep *laws.Report) {
		in = storedIDs(t, in)
		textFormatSuite().Check(t, in, uint64(i), rep)
		pid, word := uniqueWord(t, in)
		if word == "" {
			return
		}
		plans := map[string]string{
			"set":   `[{"op":"text.format","at":"` + pid + `","select":"` + word + `","bold":true,"color":"#C00000","font_size":14.5,"highlight":"yellow"}]`,
			"clear": `[{"op":"text.format","at":"` + pid + `","select":"` + word + `","clear":["bold","italic","color","font_size"],"underline":false}]`,
			"twice": `[{"op":"text.format","at":"` + pid + `","select":"` + word + `","italic":true},{"op":"text.format","at":"` + pid + `","select":"` + word + `","superscript":true}]`,
		}
		for name, ops := range plans {
			spanSuite("text.format "+name, ops, laws.AcceptAuthored).Check(t, in)
		}
	})
	t.Logf("text.format: %s", rep)
}
