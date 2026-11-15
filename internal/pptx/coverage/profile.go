package coverage

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProfileFile is the committed pdraw profile under eval/coverage.
const ProfileFile = "pdraw-profile.json"

// Profile is what a reference set of decks uses inside objects: every
// universe path and enumerated value with its occurrence count. The
// reference set is pdraw's output (eval/parity/build.sh); the decks
// themselves are not committed, only this file, which make check reads.
type Profile struct {
	Source string `json:"source"`
	// Commit is the git commit of the generator, "-dirty" when its tree
	// had uncommitted changes to tracked files.
	Commit string `json:"commit"`
	// Decks names the decks counted (file names).
	Decks   []string `json:"decks"`
	Slides  int      `json:"slides"`
	Objects int      `json:"objects"`
	// Paths counts each universe path inside objects.
	Paths map[string]int64 `json:"paths"`
	// Values counts the values of enumerated attributes inside objects.
	Values map[string]map[string]int64 `json:"values"`
	// Outside counts what objects hold that the universe has no item for
	// (extension lists, mc:AlternateContent).
	Outside map[string]int64 `json:"outside"`
}

// BuildProfile counts the decks in dir/decks, naming dir/COMMIT as the
// generator's commit (the layout eval/parity/build.sh writes).
func BuildProfile(u *Universe, dir string) (*Profile, error) {
	commit, err := os.ReadFile(filepath.Join(dir, "COMMIT")) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
	if err != nil {
		return nil, err
	}
	decks, err := filepath.Glob(filepath.Join(dir, "decks", "*.pptx"))
	if err != nil {
		return nil, err
	}
	if len(decks) == 0 {
		return nil, fmt.Errorf("no decks in %s", filepath.Join(dir, "decks"))
	}
	sort.Strings(decks)
	c, err := countDecks(u, decks, "", true)
	if err != nil {
		return nil, err
	}
	if c.Missing+c.Unreadable != 0 {
		return nil, fmt.Errorf("%d unreadable decks", c.Missing+c.Unreadable)
	}
	p := &Profile{
		Source:  "pdraw create over the examples of eval/parity/build.sh",
		Commit:  strings.TrimSpace(string(commit)),
		Slides:  c.Slides,
		Objects: c.Objects,
		Paths:   map[string]int64{},
		Values:  c.Values,
		Outside: c.Outside,
	}
	for _, d := range decks {
		p.Decks = append(p.Decks, filepath.Base(d))
	}
	maps.Copy(p.Paths, c.ObjectPaths)
	return p, nil
}

// LoadProfile reads a profile file.
func LoadProfile(path string) (*Profile, error) { return loadJSON[Profile](path) }
