package apply

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// commentSuite is comment.add's change-mode law: comments are not
// tracked, so both modes write the same.
func commentSuite(name, ops string) *laws.ModeSuite {
	return &laws.ModeSuite{
		Name:  "comment.add " + name,
		Class: laws.Same,
		Apply: func(src, dst, mode string) error { return runPlan(src, dst, mode, ops) },
		Refused: func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		},
		Project: projectNoSource,
		Settle:  settleFile,
	}
}

// uniqueWord returns a word of at least three letters that occurs once in
// the final text of one of path's body paragraphs, with the paragraph's
// address, or "" when there is none.
func uniqueWord(t *testing.T, path string) (id, word string) {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(b)
	if err != nil || d.XML.Body() == nil {
		return "", ""
	}
	ids := d.ParaIDs().IDs
	for _, p := range d.XML.Paragraphs() {
		text := model.ParagraphText(p.Node, model.Current)
		for _, w := range strings.Fields(text) {
			if len(w) >= 3 && strings.Count(text, w) == 1 && !strings.ContainsAny(w, `<"\`) {
				if pid, ok := ids[p.Node]; ok {
					return pid.ID, w
				}
			}
		}
	}
	return "", ""
}

// commentedText is the text between the markers of the newest comment
// in the document read's markup (tags removed).
func commentedText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the law's private copy
	if err != nil {
		t.Fatal(err)
	}
	res, err := read.Read(b, "document", "", read.Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	s, err := read.Encode(res.Object(), false)
	if err != nil {
		t.Fatal(err)
	}
	starts := regexp.MustCompile(`data-comment-start=\\"(\d+)\\"`).FindAllStringSubmatch(s, -1)
	top := -1
	for _, m := range starts {
		if n, err := strconv.Atoi(m[1]); err == nil && n > top {
			top = n
		}
	}
	id := strconv.Itoa(top)
	re := regexp.MustCompile(`data-comment-start=\\"` + id + `\\"[^>]*></span>(.*?)<span data-comment-end=\\"` + id + `\\"`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("%s: comment %s has no range in the markup", filepath.Base(path), id)
	}
	return regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m[1], "")
}

// TestCommentAddLaws runs comment.add's change-mode law (track equals
// direct) on the law inputs and the write fixtures, for a whole-paragraph
// comment on the first and last body paragraphs and a comment on a word,
// and checks that the word's comment covers exactly the word.
func TestCommentAddLaws(t *testing.T) {
	eachInput(t, deleteInputs(t), func(t *testing.T, in string, _ int) {
		ids := bodyParagraphIDs(t, in)
		if len(ids) == 0 {
			return
		}
		plans := map[string]string{
			"first": `[{"op":"comment.add","at":"` + ids[0] + `","text":"On the first."}]`,
			"last":  `[{"op":"comment.add","at":"` + ids[len(ids)-1] + `","text":" On the last "}]`,
		}
		pid, word := uniqueWord(t, in)
		if word != "" {
			plans["word"] = `[{"op":"comment.add","at":"` + pid + `","select":"` + word + `","text":"On a word & more <x>."}]`
		}
		for name, ops := range plans {
			commentSuite(name, ops).Check(t, in)
		}
		if word == "" {
			return
		}
		out := filepath.Join(t.TempDir(), "out.docx")
		err := runPlan(laws.Private(t, in), out, laws.Direct, plans["word"])
		var r refusal
		if errors.As(err, &r) {
			return // a package that cannot take comment parts (rich.docx has no content types stream)
		}
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(in), err)
			return
		}
		if got := commentedText(t, out); got != word {
			t.Errorf("%s: the comment covers %q, want %q", filepath.Base(in), got, word)
		}
	})
}

// TestCommentInsideRevision checks that a comment on part of a tracked
// insertion covers exactly the selection: its markers go inside the
// w:ins (docxdriver widens the range to the whole wrapper).
func TestCommentInsideRevision(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(root, "tools", "oracle", "testdata", "write", "mixed.docx")
	out := filepath.Join(t.TempDir(), "out.docx")
	for _, mode := range []string{laws.Track, laws.Direct} {
		if err := runPlan(laws.Private(t, in), out, mode, `[{"op":"comment.add","at":"10000004","select":"insertion","text":"On my text"}]`); err != nil {
			t.Fatal(err)
		}
		if got := commentedText(t, out); got != "insertion" {
			t.Errorf("%s: the comment covers %q, want %q", mode, got, "insertion")
		}
	}
}
