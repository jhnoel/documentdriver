// Package corpustest runs a test's per-file work over a corpus in
// parallel, one subtest per file, and selects a deterministic shard of the
// files. It is imported only by tests (enforced by internal/archtest).
//
// Two variables steer it:
//
//	CORPUS_PARALLEL  files checked at once (default GOMAXPROCS); 1 runs
//	                 them one after another, as before this package
//	CORPUS_SHARD     i/n (1 <= i <= n): only the files whose stable hash
//	                 (FNV-1a of the file's name) is i-1 mod n; the n shards
//	                 together are exactly the full set
//
// Results come back in file order whatever the scheduling, so a test that
// sums counts or logs a report does so after Run returns, from the
// results, with no state shared between files.
package corpustest

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// File is one corpus file.
type File struct {
	// Path is the file's path on disk.
	Path string
	// Name is the file's stable name: its slash-separated path relative
	// to the first root that holds it (see Files), else its base name,
	// with "#k" added to the k-th repeat. It names the subtest and is the
	// shard key, so it must not depend on the machine or the run.
	Name string
	// Index is the file's position in the full list, before sharding.
	Index int
}

// Files names paths relative to the first of roots that holds each one,
// else by base name (for generated fixtures in temporary directories), in
// the given order.
func Files(paths []string, roots ...string) []File {
	out := make([]File, len(paths))
	seen := map[string]int{}
	for i, p := range paths {
		name := ""
		for _, r := range roots {
			if r == "" {
				continue
			}
			if rel, err := filepath.Rel(r, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				name = filepath.ToSlash(rel)
				break
			}
		}
		if name == "" {
			name = filepath.Base(p)
		}
		if k := seen[name]; k > 0 {
			seen[name]++
			name += "#" + strconv.Itoa(k)
		} else {
			seen[name] = 1
		}
		out[i] = File{Path: p, Name: name, Index: i}
	}
	return out
}

// Parallel returns how many files run at once: CORPUS_PARALLEL, else
// GOMAXPROCS.
func Parallel() (int, error) {
	v := os.Getenv("CORPUS_PARALLEL")
	if v == "" {
		return runtime.GOMAXPROCS(0), nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("CORPUS_PARALLEL=%q: want a positive integer", v)
	}
	return n, nil
}

// ShardSpec returns the shard CORPUS_SHARD selects as (i, n), 1 <= i <=
// n; (1, 1) when it is unset.
func ShardSpec() (i, n int, err error) {
	v := os.Getenv("CORPUS_SHARD")
	if v == "" {
		return 1, 1, nil
	}
	a, b, ok := strings.Cut(v, "/")
	i, erri := strconv.Atoi(a)
	n, errn := strconv.Atoi(b)
	if !ok || erri != nil || errn != nil || n < 1 || i < 1 || i > n {
		return 0, 0, fmt.Errorf("CORPUS_SHARD=%q: want i/n with 1 <= i <= n", v)
	}
	return i, n, nil
}

// Sharded reports whether CORPUS_SHARD selects a part of the corpus, so
// that a test asserting totals over the whole corpus can relax them.
func Sharded() bool {
	_, n, err := ShardSpec()
	return err == nil && n > 1
}

// InShard reports whether a file named name belongs to shard i of n
// (1 <= i <= n).
func InShard(name string, i, n int) bool {
	if n < 1 || i < 1 || i > n {
		return false
	}
	h := fnv.New64a()
	h.Write([]byte(name))                     //nolint:errcheck // hash writes never fail
	return h.Sum64()%uint64(n) == uint64(i-1) //nolint:gosec // G115: n and i-1 are positive, checked above
}

// Shard returns the files of the shard CORPUS_SHARD selects, in order
// (all of them when it is unset). Sharding twice gives the same files.
func Shard(t testing.TB, files []File) []File {
	t.Helper()
	i, n, err := ShardSpec()
	if err != nil {
		t.Fatal(err)
	}
	if n == 1 {
		return files
	}
	var out []File
	for _, f := range files {
		if InShard(f.Name, i, n) {
			out = append(out, f)
		}
	}
	return out
}

// Option adjusts Run.
type Option func(*config)

type config struct{ max int }

// MaxParallel caps the files run at once below CORPUS_PARALLEL, for a
// suite whose documents are large in memory.
func MaxParallel(n int) Option { return func(c *config) { c.max = n } }

// Result is what fn recorded for one file.
type Result[R any] struct {
	File
	// Value is what fn wrote to its *R, kept when fn skipped or failed
	// fatally part way (as counts summed into a shared tally were).
	Value R
	// Ran is false when fn did not return normally (t.Skip, t.Fatal).
	Ran     bool
	Skipped bool
	Failed  bool
}

// slots holds the test binary's file slots: Parallel() of them (as
// CORPUS_PARALLEL was at the first Run), shared
// by every Run, so that suites running side by side (top-level tests
// that call t.Parallel) still check CORPUS_PARALLEL files at once in all.
var (
	slotsOnce sync.Once
	slots     chan struct{}
)

// acquire takes one slot from the shared semaphore, blocking until one is free.
func acquire(n int) {
	slotsOnce.Do(func() { slots = make(chan struct{}, n) })
	slots <- struct{}{}
}

// release frees one slot acquired by acquire.
func release() { <-slots }

// Run runs fn on every file of the shard (Shard), each in its own
// subtest named by File.Name, up to Parallel() at once (in the whole
// test binary: suites that run side by side share the limit), and
// returns the results in file order. The largest files start first, so
// that a slow one does not start last. fn records what it finds in its
// own *R (zero on entry); it must not call t.Parallel or Run, and must
// keep its state to itself (its own t.TempDir, no package variables it
// writes).
func Run[R any](t *testing.T, files []File, fn func(t *testing.T, f File, r *R), opts ...Option) []Result[R] {
	t.Helper()
	files = Shard(t, files)
	par, err := Parallel()
	if err != nil {
		t.Fatal(err)
	}
	var c config
	for _, o := range opts {
		o(&c)
	}
	total := par
	if c.max > 0 {
		par = min(par, c.max)
	}
	par = max(1, min(par, len(files)))
	out := make([]Result[R], len(files))
	next := make(chan int)
	var wg sync.WaitGroup
	for range par {
		wg.Go(func() {
			for k := range next {
				acquire(total)
				r := &out[k]
				r.File = files[k]
				t.Run(files[k].Name, func(t *testing.T) {
					defer func() { r.Skipped, r.Failed = t.Skipped(), t.Failed() }()
					fn(t, files[k], &r.Value)
					r.Ran = true
				})
				release()
			}
		})
	}
	for _, k := range largestFirst(files) {
		next <- k
	}
	close(next)
	wg.Wait()
	return out
}

// Each is Run for fn that returns nothing.
func Each(t *testing.T, files []File, fn func(t *testing.T, f File), opts ...Option) []Result[struct{}] {
	t.Helper()
	return Run(t, files, func(t *testing.T, f File, _ *struct{}) { fn(t, f) }, opts...)
}

// largestFirst returns the indices of files by size, largest first (ties
// and files that cannot be statted keep their order).
func largestFirst(files []File) []int {
	size := make([]int64, len(files))
	order := make([]int, len(files))
	for i, f := range files {
		order[i] = i
		if info, err := os.Stat(f.Path); err == nil {
			size[i] = info.Size()
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return size[order[a]] > size[order[b]] })
	return order
}
