package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// TestNoSchemaTextInTrackedFiles checks that no tracked file holds
// the XSD's license header or schema tags: the schema is fetched
// into a gitignored cache by eval/corpus/fetch-vsdx-schema.sh and is
// never committed. The forbidden strings are built by joining
// fragments so this test file does not itself contain them.
func TestNoSchemaTextInTrackedFiles(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	// An export (git archive) has no .git at the root and nothing is
	// tracked; a checkout or worktree has one (a directory, or a file
	// in a worktree), and there a git error fails.
	if _, err := os.Stat(filepath.Join(root, ".git")); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("no .git at %s: not a git checkout, so there are no tracked files to check", root)
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	forbidden := [][]byte{
		[]byte("xsd" + ":complexType"),
		[]byte("Visio VSDX" + " Schema"),
	}
	for _, rel := range bytes.Split(out, []byte{0}) {
		if len(rel) == 0 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, string(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		for _, pat := range forbidden {
			if bytes.Contains(b, pat) {
				t.Errorf("tracked file %s contains %q (the schema is never committed)", rel, pat)
			}
		}
	}
}
