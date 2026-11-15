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
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// chromeSuite is a header or footer op's change-mode law: on an existing
// story, direct equals the authored revisions of track accepted and
// rejecting the tracked output gives the input back (rejected); the
// guards are track mode's documented refusals.
func chromeSuite(name, ops string) *laws.ModeSuite {
	s := replaceSuite(name, ops)
	s.Name = name
	s.Guarded = func(mode string, err error) bool {
		var r refusal
		return mode == laws.Track && errors.As(err, &r) && r.d.Code == "refused_in_mode"
	}
	return s
}

// storyRefs returns, per section number, the story kinds that section
// references explicitly ("hdr default", ...), and the story-wide
// addresses of each story's equations and placeholders, from read.
func storyRefs(t *testing.T, path string) (map[int][]string, map[string][]string) {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(b)
	if err != nil {
		return nil, nil
	}
	out := map[int][]string{}
	for i, s := range view.SectPrNodes(d.XML) {
		for _, c := range s.Elements() {
			kind := model.AttrOr(c, "type")
			if kind == "" {
				kind = "default"
			}
			switch {
			case model.IsW(c, "headerReference"):
				out[i+1] = append(out[i+1], "header "+kind)
			case model.IsW(c, "footerReference"):
				out[i+1] = append(out[i+1], "footer "+kind)
			}
		}
	}
	res, err := read.Read(b, "document", "", read.Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	markup, _ := res.Object()["markup"].(string)
	refs := map[string][]string{}
	for _, tag := range []string{"header", "footer"} {
		for _, seg := range strings.Split(markup, "<"+tag)[1:] {
			end := strings.Index(seg, "</"+tag+">")
			if end < 0 {
				continue
			}
			for _, m := range contentStoryRef.FindAllStringSubmatch(seg[:end], -1) {
				refs[tag] = append(refs[tag], m[1])
			}
		}
	}
	return out, refs
}

// TestChromeLaws runs header.set, footer.set, header.delete and
// footer.delete's change-mode law on every explicit story of the write
// fixtures and the law inputs: a replacement that deletes the story's
// equations and placeholders, and the deletion.
func TestChromeLaws(t *testing.T) {
	eachInput(t, deleteInputs(t), func(t *testing.T, in string, _ int) {
		sections, _ := storyRefs(t, in)
		for n, stories := range sections {
			for _, st := range stories {
				word, kind, _ := strings.Cut(st, " ")
				at := `"at":"section:` + strconv.Itoa(n) + `","kind":"` + kind + `"`
				// The story's own refs: read the story alone through a set
				// that deletes everything it holds, found by trying.
				set := `[{"op":"` + word + `.set",` + at + `,"html":"<p>New <b>` + word + `</b> <page-number/></p><p>two</p>"}]`
				out := filepath.Join(t.TempDir(), "probe.docx")
				err := runPlan(laws.Private(t, in), out, laws.Direct, set)
				var r refusal
				if errors.As(err, &r) && strings.HasPrefix(r.d.Message, "html leaves out ") {
					left := strings.TrimSuffix(strings.SplitN(strings.TrimPrefix(r.d.Message, "html leaves out "), ":", 2)[0], "")
					set = `[{"op":"` + word + `.set",` + at + `,"html":"<p>New <b>` + word + `</b> <page-number/></p><p>two</p>","delete":["` + strings.ReplaceAll(left, ", ", `","`) + `"]}]`
				}
				chromeSuite(filepath.Base(in)+" "+st+" set", set).Check(t, in)
				if n == 1 {
					del := `[{"op":"` + word + `.delete",` + at + `}]`
					s := chromeSuite(filepath.Base(in)+" "+st+" delete", del)
					s.Class = laws.OwnPath // EmptiesStory: accepted, the story stays empty
					s.Check(t, in)
				}
			}
		}
	})
}

var contentStoryRef = regexp.MustCompile(`data-ref="((?:opaque|equation):\d+)"`)
