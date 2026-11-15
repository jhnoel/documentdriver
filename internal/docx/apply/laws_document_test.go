package apply

import (
	"errors"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// evenOddSuite is document.set_even_odd_headers's five laws: one target,
// the document's setting in its settings part (a document without one
// has no target: adding the part is not an untargeted-law change).
func evenOddSuite() *laws.Suite[bool] {
	op := func(on bool) laws.Op {
		return sectionOp(`{"op":"document.set_even_odd_headers","even_and_odd":` + strconv.FormatBool(on) + `}`)
	}
	s := marginLaw.suite(laws.Direct)
	return &laws.Suite[bool]{
		Name: "document.set_even_odd_headers",
		Read: func(path string) ([]laws.Value[bool], error) {
			b, err := os.ReadFile(path) //nolint:gosec // G304: the law's private copy
			if err != nil {
				return nil, err
			}
			d, err := model.OpenDocument(b)
			if err != nil {
				return nil, err
			}
			name := d.Pkg.SettingsPartName()
			x, ok := d.Pkg.Part(name)
			if !ok || x.Root() == nil || !model.IsW(x.Root(), "settings") {
				return nil, nil
			}
			on := false
			if n := model.ChildW(x.Root(), "evenAndOddHeaders"); n != nil {
				v, _ := model.Attr(n, "val")
				on = v != "0" && v != "false" && v != "off"
			}
			return []laws.Value[bool]{{Target: laws.Target{Part: name, Addr: "document"}, V: on}}, nil
		},
		Show:    strconv.FormatBool,
		Set:     func(_ laws.Target, v bool) laws.Op { return op(v) },
		Vary:    func(_ *rand.Rand, _ laws.Target, v bool) bool { return !v },
		Apply:   s.Apply,
		Refused: s.Refused,
		Units:   docxedit.Units(),
		Owns:    func(laws.Target, *tree.Node) bool { return true },
		Fail:    func() laws.Op { return sectionOp(`{"op":"section.set_margins","at":"section:999","top":1}`) },
	}
}

// TestEvenOddHeadersLaws runs document.set_even_odd_headers's five laws
// and its change-mode law (OnlyDirect: track mode refuses it) on every
// law input.
func TestEvenOddHeadersLaws(t *testing.T) {
	rep := reportInputs(t, sectionLawInputs(t), func(t *testing.T, in string, _ int, rep *laws.Report) {
		evenOddSuite().Check(t, storedSectionIDs(t, in), 4, rep)
		for _, on := range []string{"true", "false"} {
			s := sectionModeSuite("document.set_even_odd_headers "+on, `[{"op":"document.set_even_odd_headers","even_and_odd":`+on+`}]`)
			s.Class = laws.OnlyDirect
			s.Guarded = func(mode string, err error) bool {
				var r refusal
				return mode == laws.Track && errors.As(err, &r) && r.d.Code == "refused_in_mode"
			}
			s.Check(t, in)
		}
	})
	t.Log(rep)
}
