// Package repo finds the documentdriver repository root at run time, so
// the dev tools under eval/ and tools/ keep their defaults repo-relative
// instead of hard-coding absolute paths.
package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const modulePath = "github.com/jhnoel/documentdriver"

// ErrNotFound is returned when no enclosing directory declares the module.
var ErrNotFound = errors.New("not inside the documentdriver repository (no go.mod declaring " + modulePath + " in any parent directory)")

// Root returns the nearest directory, starting at the working directory
// and walking up, whose go.mod declares module github.com/jhnoel/documentdriver.
func Root() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return RootFrom(wd)
}

// RootFrom is Root starting at dir.
func RootFrom(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && declaresModule(string(b)) { //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

// declaresModule reports whether go.mod text declares the documentdriver module.
func declaresModule(gomod string) bool {
	for _, line := range strings.Split(gomod, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "module" && strings.Trim(f[1], `"`) == modulePath {
			return true
		}
	}
	return false
}

// Corpus returns the corpus directory: <root>/corpus, or, when root is a
// linked git worktree without its own corpus (the corpus is gitignored, so
// worktrees never get one), <main checkout>/corpus.
func Corpus() (string, error) {
	r, err := Root()
	if err != nil {
		return "", err
	}
	return corpusFrom(r), nil
}

// corpusFrom returns root's corpus directory, falling back to the main checkout's.
func corpusFrom(root string) string {
	own := filepath.Join(root, "corpus")
	if _, err := os.Stat(own); err == nil {
		return own
	}
	if main, ok := MainCheckout(root); ok {
		return filepath.Join(main, "corpus")
	}
	return own
}

// MainCheckout returns the main checkout of the repository that root (a
// linked git worktree) belongs to. It reads the worktree's .git file
// ("gitdir: <main>/.git/worktrees/<name>") and that directory's commondir
// file, so it needs no git binary. ok is false when root is not a linked
// worktree.
func MainCheckout(root string) (main string, ok bool) {
	b, err := os.ReadFile(filepath.Join(root, ".git")) //nolint:gosec // G304: path derived from the repository root, not from a document
	if err != nil {
		return "", false // a directory (main checkout) or no git at all
	}
	line := strings.TrimSpace(string(b))
	gitdir, found := strings.CutPrefix(line, "gitdir:")
	if !found {
		return "", false
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(root, gitdir)
	}
	common := gitdir
	if c, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil { //nolint:gosec // G304: path inside the repository's git directory
		common = strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitdir, common)
		}
	}
	common = filepath.Clean(common)
	if filepath.Base(common) != ".git" {
		return "", false // bare repository or unusual layout
	}
	return filepath.Dir(common), true
}
