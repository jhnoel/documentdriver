package apply

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// The law suites run input by input in parallel (internal/corpustest):
// each input is a subtest of its own, and a suite that sums a report
// gives each input a report of its own and adds them up in input order.
// The suites also run side by side (t.Parallel), sharing corpustest's
// file slots, so that one suite's slowest input overlaps the others'
// instead of leaving the slots idle. Nothing they share is written:
// fault hooks are scoped to a file (opc.SetFaultHookFor), and the
// package's sequential tests (the scaling and memory gates among them)
// finish before any parallel test starts.

// inputFiles names law inputs relative to DOCX_CORPUS or the repository
// (generated fixtures by their base name), so that the names are the
// same on every machine.
func inputFiles(t *testing.T, ins []string) []corpustest.File {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	return corpustest.Files(ins, os.Getenv("DOCX_CORPUS"), root)
}

// eachInput runs fn on every input in parallel, beside the other
// parallel suites (it calls t.Parallel); i is the input's place in ins
// (whatever the shard).
func eachInput(t *testing.T, ins []string, fn func(t *testing.T, in string, i int)) {
	t.Helper()
	t.Parallel()
	corpustest.Each(t, inputFiles(t, ins), func(t *testing.T, f corpustest.File) { fn(t, f.Path, f.Index) })
}

// reportInputs is eachInput for suites that sum a laws.Report: fn adds
// to the input's own report, and the sum comes back.
func reportInputs(t *testing.T, ins []string, fn func(t *testing.T, in string, i int, rep *laws.Report)) laws.Report {
	t.Helper()
	t.Parallel()
	var rep laws.Report
	for _, r := range corpustest.Run(t, inputFiles(t, ins), func(t *testing.T, f corpustest.File, rep *laws.Report) { fn(t, f.Path, f.Index, rep) }) {
		rep.Add(r.Value)
	}
	return rep
}

// scans caches scanCorpus: every law suite filters the same corpus, so
// each filter reads it once per test binary.
var scans sync.Map // key -> *scan

type scan struct {
	once  sync.Once
	paths []string
	err   error
}

// scanCorpus returns the .docx files under dir, in walk order, for
// which keep reports true, reading and checking them in parallel
// (CORPUS_PARALLEL). key names keep; the result is cached by key and dir.
func scanCorpus(t *testing.T, key, dir string, keep func(b []byte) bool) []string {
	t.Helper()
	v, _ := scans.LoadOrStore(key+"\x00"+dir, &scan{})
	sc := v.(*scan)
	sc.once.Do(func() {
		var all []string
		sc.err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".docx") {
				return err
			}
			all = append(all, p)
			return nil
		})
		if sc.err != nil {
			return
		}
		par, err := corpustest.Parallel()
		if err != nil {
			sc.err = err
			return
		}
		ok := make([]bool, len(all))
		errs := make([]error, len(all))
		next := make(chan int)
		var wg sync.WaitGroup
		for range par {
			wg.Go(func() {
				for i := range next {
					b, err := os.ReadFile(all[i]) //nolint:gosec // G304: the corpus the caller named
					if err != nil {
						errs[i] = err
						continue
					}
					ok[i] = keep(b)
				}
			})
		}
		for i := range all {
			next <- i
		}
		close(next)
		wg.Wait()
		for i, p := range all {
			if errs[i] != nil {
				sc.err = errs[i]
				return
			}
			if ok[i] {
				sc.paths = append(sc.paths, p)
			}
		}
	})
	if sc.err != nil {
		t.Fatal(sc.err)
	}
	return sc.paths
}
