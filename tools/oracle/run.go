package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The input sets.
const (
	setFixtures = "fixtures"
	setCorpus   = "corpus-lo"
	setAuthored = "authored" // testdata/docx of this repository
)

// fixtureGlobs are the small docx files of the docxdriver repository, as
// paths relative to it. A trailing "/**" walks a directory.
var fixtureGlobs = []string{
	"test-docs/*.docx",
	"crates/docxdriver-core/assets/blank.docx",
	"crates/docxdriver-core/tests/fixtures/**",
	"crates/docxdriver-cli/tests/fixtures/**",
}

// runTimeout bounds one CLI run; a run that exceeds it is recorded with
// exit code -1 and "timeout" on stderr.
const runTimeout = 2 * time.Minute

// input is one docx file of a set.
type input struct {
	set  string
	dir  string // the set's root directory
	path string // relative to dir, slash-separated
}

// inputs lists the files of a set, sorted.
func (c *config) inputs(set string) ([]input, error) {
	var dir string
	var rels []string
	switch set {
	case setFixtures:
		dir = c.docxdriver
		for _, g := range fixtureGlobs {
			found, err := globDocx(dir, g)
			if err != nil {
				return nil, err
			}
			rels = append(rels, found...)
		}
	case setAuthored:
		dir = filepath.Join(c.root, "testdata", "docx")
		found, err := globDocx(dir, "*.docx")
		if err != nil {
			return nil, err
		}
		rels = found
	case setCorpus:
		dir = c.corpus
		found, err := globDocx(dir, "**")
		if err != nil {
			return nil, err
		}
		rels = found
	default:
		return nil, fmt.Errorf("unknown set %q", set)
	}
	slices.Sort(rels)
	rels = slices.Compact(rels)
	var out []input
	for _, r := range rels {
		if c.only != "" && !strings.Contains(r, c.only) {
			continue
		}
		out = append(out, input{set: set, dir: dir, path: r})
	}
	if len(out) == 0 && c.only == "" {
		return nil, fmt.Errorf("set %s: no docx files under %s", set, dir)
	}
	return out, nil
}

// globDocx expands one pattern below dir into slash-separated relative
// paths of .docx files.
func globDocx(dir, pattern string) ([]string, error) {
	if sub, ok := strings.CutSuffix(pattern, "**"); ok {
		base := filepath.Join(dir, filepath.FromSlash(sub))
		var out []string
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() && strings.HasSuffix(strings.ToLower(p), ".docx") {
				rel, err := filepath.Rel(dir, p)
				if err != nil {
					return err
				}
				out = append(out, filepath.ToSlash(rel))
			}
			return nil
		})
		return out, err
	}
	matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pattern)))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range matches {
		rel, err := filepath.Rel(dir, m)
		if err != nil {
			return nil, err
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}

// result is what one CLI run produced.
type result struct {
	exit   int
	stdout []byte
	stderr []byte
}

// hashLen is how many hex digits of SHA-256 a manifest keeps: 64 bits is
// ample to tell outputs apart and keeps the corpus manifest small.
const hashLen = 16

// digest returns '-' for empty input or the first 16 hex digits of its SHA-256.
func digest(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])[:hashLen]
}

// entry is one manifest line: the outcome of one case on one file.
type entry struct {
	file, kase   string
	exit         int
	stdout, errh string
}

// line returns the manifest line for this entry.
func (e entry) line() string {
	return fmt.Sprintf("%s\t%s\t%d\t%s\t%s", e.file, e.kase, e.exit, e.stdout, e.errh)
}

// entryOf builds a manifest entry from a run result's exit code and output digests.
func entryOf(file, kase string, r result) entry {
	return entry{file: file, kase: kase, exit: r.exit, stdout: digest(r.stdout), errh: digest(r.stderr)}
}

// runCLI runs bin with the case's arguments in the input's directory.
func runCLI(bin string, in input, k Case) (result, error) {
	full := filepath.Join(in.dir, filepath.FromSlash(in.path))
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, k.Args(filepath.Base(full))...)
	cmd.Dir = filepath.Dir(full)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return result{exit: -1, stderr: []byte("timeout")}, nil
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return result{stdout: stdout.Bytes(), stderr: stderr.Bytes()}, nil
	case errors.As(err, &exitErr):
		return result{exit: exitErr.ExitCode(), stdout: stdout.Bytes(), stderr: stderr.Bytes()}, nil
	default:
		return result{}, fmt.Errorf("run %s: %w", bin, err)
	}
}

// job is one input and case to run.
type job struct {
	in input
	k  Case
}

// runAll runs fn over every job with n workers and returns the first error.
func runAll(jobs []job, n int, fn func(job) error) error {
	ch := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	for range n {
		wg.Go(func() {
			for j := range ch {
				if err := fn(j); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}
		})
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	return first
}

// writeOutputs stores a run's stdout and stderr (when not empty) under dir
// as <file>/<case>.stdout and .stderr.
func writeOutputs(dir, file, kase string, r result) error {
	base := filepath.Join(dir, filepath.FromSlash(file))
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(base, kase+".stdout"), r.stdout, 0o644); err != nil {
		return err
	}
	errPath := filepath.Join(base, kase+".stderr")
	if len(r.stderr) == 0 {
		if err := os.Remove(errPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(errPath, r.stderr, 0o644)
}

// readOutputs loads what writeOutputs stored, if it is there.
func readOutputs(dir, file, kase string) (stdout, stderr []byte, ok bool) {
	base := filepath.Join(dir, filepath.FromSlash(file))
	out, err := os.ReadFile(filepath.Join(base, kase+".stdout"))
	if err != nil {
		return nil, nil, false
	}
	errb, err := os.ReadFile(filepath.Join(base, kase+".stderr"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, false
	}
	return out, errb, true
}
