package math

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// TestOracleLaTeX cross-checks OMML to LaTeX with docxdriver 0.5.2's
// omml_to_latex, whose output for the golden equations is in
// oracle-latex.json. Its LaTeX is a different surface, so the check is on
// meaning: where this grammar parses the oracle's LaTeX, the equation it
// describes is compared with the one read from the OMML. Counts are
// reported, not failed: the oracle drops structure this grammar keeps.
func TestOracleLaTeX(t *testing.T) {
	var golden []struct {
		File      string `json:"file"`
		Fragments []struct {
			Display bool   `json:"display"`
			LaTeX   string `json:"latex"`
		} `json:"fragments"`
	}
	b, err := os.ReadFile(filepath.Join("testdata", "omml", "oracle-latex.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	agree, differ, unparsed, total := 0, 0, 0, 0
	for _, g := range golden {
		data, err := os.ReadFile(filepath.Join("testdata", "omml", g.File))
		if err != nil {
			continue // a file since removed
		}
		doc, err := tree.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		frags := fragments(doc)
		for i, f := range g.Fragments {
			if i >= len(frags) {
				break
			}
			total++
			var rd reader
			ours := rd.readEquation(frags[i])
			theirs, err := parseEquation(f.LaTeX, ours.display)
			switch {
			case err != nil:
				unparsed++
			case keyOf(theirs) == keyOf(ours):
				agree++
			default:
				differ++
			}
		}
	}
	t.Logf("oracle LaTeX of %d equations: %d mean the same, %d differ, %d outside this grammar", total, agree, differ, unparsed)
}
