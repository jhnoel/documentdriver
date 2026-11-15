package repo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRootFrom(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/jhnoel/documentdriver\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := RootFrom(sub)
	if err != nil || got != root {
		t.Fatalf("RootFrom = %q, %v; want %q", got, err, root)
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RootFrom(other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other module: err = %v, want ErrNotFound", err)
	}
}

// TestCorpusFromWorktree builds the on-disk layout git uses for a linked
// worktree and checks that the corpus resolves to the main checkout's.
func TestCorpusFromWorktree(t *testing.T) {
	main := t.TempDir()
	mustMkdir(t, filepath.Join(main, ".git", "worktrees", "wt1"))
	mustMkdir(t, filepath.Join(main, "corpus"))
	mustWrite(t, filepath.Join(main, ".git", "worktrees", "wt1", "commondir"), "../..\n")
	wt := filepath.Join(t.TempDir(), "wt1")
	mustMkdir(t, wt)
	mustWrite(t, filepath.Join(wt, ".git"), "gitdir: "+filepath.Join(main, ".git", "worktrees", "wt1")+"\n")

	got, ok := MainCheckout(wt)
	if !ok || got != main {
		t.Fatalf("MainCheckout = %q, %v; want %q", got, ok, main)
	}
	if c := corpusFrom(wt); c != filepath.Join(main, "corpus") {
		t.Fatalf("corpusFrom(worktree) = %q, want the main checkout's corpus", c)
	}
	// A worktree with its own corpus keeps it.
	mustMkdir(t, filepath.Join(wt, "corpus"))
	if c := corpusFrom(wt); c != filepath.Join(wt, "corpus") {
		t.Fatalf("corpusFrom(worktree with corpus) = %q", c)
	}
	// The main checkout (.git is a directory) is not a linked worktree.
	if _, ok := MainCheckout(main); ok {
		t.Fatal("MainCheckout(main) reported a linked worktree")
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
