package formula

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

func TestStoreChunkRefusals(t *testing.T) {
	for _, input := range []string{"A1#", "(1", "\"unterminated"} {
		if _, err := Store(input); err == nil {
			t.Errorf("Store(%q) accepted invalid input", input)
		}
	}
}

func TestStoreChunkSpills(t *testing.T) {
	if name, ok := Spills(`"SEQUENCE("`); ok {
		t.Fatalf("literal spills: %s", name)
	}
}

func TestStoreChunkCorpus(t *testing.T) {
	_, files := corpusFiles(t)
	corpustest.Run(t, files, func(t *testing.T, f corpustest.File, count *int) {
		cf := readCorpusFile(f.Path)
		for _, fm := range cf.Formulas {
			if fm.Kind == "shared-child" || fm.Kind == "data-table" || strings.HasPrefix(fm.Kind, "empty ") {
				continue
			}
			text := fm.Text
			if fm.Kind == "external-name" {
				text = strings.TrimPrefix(text, "=")
			}
			xlsxtest.Skip(t, func() {
				got, err := Store(Display(text))
				// V: two designed classes. A formula with the spill
				// operator (ANCHORARRAY shown as A1#) is refused, as plans
				// that write one are. And Display is not injective on
				// SINGLE: SINGLE(-F5:F7) and SINGLE((-F5:F7)) both show
				// @(-F5:F7); Store writes Excel's form, with the
				// parentheses (the corpus has it 5 to 3, and SINGLE((F5,H5))
				// needs them), so the law there is that it shows the same.
				if r, ok := errors.AsType[*Refusal](err); ok && strings.Contains(r.Error(), "spill operator") {
					return
				}
				if err == nil && got != text && Display(got) == Display(text) && strings.Contains(text, "SINGLE(") {
					return
				}
				if err != nil || got != text {
					*count++
					t.Errorf("%s %s: Store(Display(%q)) = %q, %v", fm.Part, fm.Where, text, got, err)
				}
			})
		}
	})
}
