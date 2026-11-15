package math

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// FuzzOMMLToLaTeX converts every equation of a mutated part to LaTeX: it
// must not panic, the LaTeX must parse to the same equation, and the
// OMML compiled from it must read back the same.
func FuzzOMMLToLaTeX(f *testing.F) {
	files, err := filepath.Glob(filepath.Join("testdata", "omml", "*.xml"))
	if err != nil {
		f.Fatal(err)
	}
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, part []byte) {
		doc, err := tree.Parse(part)
		if err != nil {
			return
		}
		for i, n := range fragments(doc) {
			checkRoundTrip(t, "fragment "+string(rune('0'+i%10)), n, lossTally{})
		}
	})
}

// FuzzLaTeXToOMML parses LaTeX: it must not panic, every error must be
// invalid LaTeX, and accepted LaTeX must have a stable canonical form
// and compile to well-formed OMML that reads back the same.
func FuzzLaTeXToOMML(f *testing.F) {
	for _, c := range canonicalCases {
		f.Add(c, false)
		f.Add(c, true)
	}
	var golden []string
	b, err := os.ReadFile(filepath.Join("testdata", "latex-inputs.json"))
	if err != nil {
		f.Fatal(err)
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		f.Fatal(err)
	}
	for _, g := range golden {
		f.Add(g, false)
	}
	f.Fuzz(func(t *testing.T, latex string, display bool) {
		eq, err := parseEquation(latex, display)
		if err != nil {
			if !errors.Is(err, ErrInvalidLaTeX) {
				t.Fatalf("error not ErrInvalidLaTeX: %v", err)
			}
			return
		}
		checkCanonical(t, latex, display, eq)
	})
}
