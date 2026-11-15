package apply

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// replaceSuite is paragraph.replace's change-mode law for one plan.
func replaceSuite(name, ops string) *laws.ModeSuite {
	return &laws.ModeSuite{
		Name:  "paragraph.replace " + name,
		Class: laws.AcceptAuthored,
		Apply: func(src, dst, mode string) error { return runPlan(src, dst, mode, ops) },
		Refused: func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		},
		Guarded: func(mode string, err error) bool {
			var r refusal
			return mode == laws.Track && errors.As(err, &r) && strings.Contains(r.d.Message, "direct only")
		},
		Project:        projectNoSource,
		Settle:         settleFile,
		AcceptAuthored: acceptAuthoredJoined,
	}
}

var contentRef = regexp.MustCompile(`data-ref="([0-9A-F]{8}(?:~\d+)?/(?:opaque|equation):\d+)"`)

// paragraphMarkup is the markup read shows for each body paragraph of
// path, by address: its <p> or heading element.
func paragraphMarkup(t *testing.T, path string, ids []string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		t.Fatal(err)
	}
	res, err := read.Read(b, "document", "", read.Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	markup, _ := res.Object()["markup"].(string)
	out := map[string]string{}
	for _, id := range ids {
		re := regexp.MustCompile(`<(p|h[1-6]) id="` + regexp.QuoteMeta(id) + `"[^>]*>.*?</(?:p|h[1-6])>`)
		if m := re.FindString(markup); m != "" {
			out[id] = m
		}
	}
	return out
}

// TestReplaceParagraphLaws runs paragraph.replace's change-mode law on
// the law inputs and the write fixtures: every body paragraph sent back
// as read shows it (what read writes, apply accepts: refused only for
// its target, a comment, a tracked change or a field that is no one
// placeholder), and replaced by new text with all its equations and
// placeholders deleted.
func TestReplaceParagraphLaws(t *testing.T) {
	eachInput(t, deleteInputs(t), func(t *testing.T, in string, _ int) {
		ids := bodyParagraphIDs(t, in)
		if len(ids) > 12 {
			ids = append(ids[:6], ids[len(ids)-6:]...)
		}
		markup := paragraphMarkup(t, in, ids)
		for _, id := range ids {
			m, ok := markup[id]
			if !ok {
				continue
			}
			same := replaceOp(id, m)
			var refs []string
			for _, r := range contentRef.FindAllStringSubmatch(m, -1) {
				refs = append(refs, r[1])
			}
			fresh := replaceOp(id, "New <b>text</b>", refs...)
			replaceSuite(id+" as read", same).Check(t, in)
			replaceSuite(id+" new", fresh).Check(t, in)
			out := filepath.Join(t.TempDir(), "out.docx")
			err := runPlan(laws.Private(t, in), out, laws.Direct, same)
			var r refusal
			if errors.As(err, &r) {
				if r.d.Code != "operation_failed" || r.d.Path != "ops[0].at" {
					t.Errorf("%s %s: read's markup refused: %s %s %s\n%s", filepath.Base(in), id, r.d.Code, r.d.Path, r.d.Message, m)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s %s: %v", filepath.Base(in), id, err)
				continue
			}
			if got := paragraphMarkup(t, out, []string{id})[id]; stripStyle(got) != stripStyle(m) {
				t.Errorf("%s %s: read's markup sent back reads\n%s\nwant\n%s", filepath.Base(in), id, got, m)
			}
		}
	})
}

// stripStyle drops the paragraph CSS a round trip may write directly
// that came from the style.
func stripStyle(s string) string {
	return regexp.MustCompile(` style="[^"]*"`).ReplaceAllString(strings.TrimSpace(s), "")
}
