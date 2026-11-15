package corpustest

import (
	"fmt"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
)

func TestFilesNames(t *testing.T) {
	root := filepath.FromSlash("/corpus/lo")
	repo := filepath.FromSlash("/repo")
	got := Files([]string{
		filepath.Join(root, "a", "x.docx"),
		filepath.Join(repo, "testdata", "y.docx"),
		filepath.FromSlash("/tmp/1/in.docx"),
		filepath.FromSlash("/tmp/2/in.docx"),
	}, "", root, repo)
	want := []string{"a/x.docx", "testdata/y.docx", "in.docx", "in.docx#1"}
	for i, f := range got {
		if f.Name != want[i] || f.Index != i {
			t.Errorf("file %d: %+v, want name %s", i, f, want[i])
		}
	}
}

func TestShardSpec(t *testing.T) {
	// Each case sets CORPUS_SHARD itself.
	for _, tc := range []struct {
		v    string
		i, n int
		bad  bool
	}{
		{"", 1, 1, false}, {"1/1", 1, 1, false}, {"2/3", 2, 3, false},
		{"0/3", 0, 0, true}, {"4/3", 0, 0, true}, {"1", 0, 0, true}, {"a/b", 0, 0, true}, {"1/0", 0, 0, true},
	} {
		t.Setenv("CORPUS_SHARD", tc.v)
		i, n, err := ShardSpec()
		if (err != nil) != tc.bad || i != tc.i || n != tc.n {
			t.Errorf("%q: %d/%d %v", tc.v, i, n, err)
		}
	}
}

// The shards of any n partition the files: each file in exactly one.
func TestShardsPartition(t *testing.T) {
	var paths []string
	for i := range 500 {
		paths = append(paths, filepath.Join("/c", fmt.Sprintf("d%d", i%7), fmt.Sprintf("f%d.docx", i)))
	}
	files := Files(paths, "/c")
	for _, n := range []int{1, 2, 3, 8} {
		seen := map[string]int{}
		for i := 1; i <= n; i++ {
			t.Setenv("CORPUS_SHARD", fmt.Sprintf("%d/%d", i, n))
			for _, f := range Shard(t, files) {
				seen[f.Name]++
			}
		}
		if len(seen) != len(files) {
			t.Errorf("n=%d: %d of %d files in a shard", n, len(seen), len(files))
		}
		for name, k := range seen {
			if k != 1 {
				t.Errorf("n=%d: %s in %d shards", n, name, k)
			}
		}
	}
}

// Run returns the results in file order, at most CORPUS_PARALLEL at once,
// and keeps what a file recorded before it skipped.
func TestRun(t *testing.T) {
	t.Setenv("CORPUS_SHARD", "") // the whole list, whatever shard the run is
	t.Setenv("CORPUS_PARALLEL", "3")
	var paths []string
	for i := range 40 {
		paths = append(paths, fmt.Sprintf("/c/f%02d", i))
	}
	var running, peak atomic.Int32
	res := Run(t, Files(paths, "/c"), func(t *testing.T, f File, n *int) {
		now := running.Add(1)
		defer running.Add(-1)
		for {
			p := peak.Load()
			if now <= p || peak.CompareAndSwap(p, now) {
				break
			}
		}
		*n = f.Index * 10
		if f.Index%5 == 0 {
			t.Skip("recorded, then skipped")
		}
	})
	if len(res) != len(paths) {
		t.Fatalf("%d results", len(res))
	}
	for i, r := range res {
		if r.Path != paths[i] || r.Value != i*10 || r.Skipped != (i%5 == 0) || r.Ran == (i%5 == 0) {
			t.Errorf("result %d: %+v", i, r)
		}
	}
	if p := peak.Load(); p > 3 {
		t.Errorf("%d files at once, want at most 3", p)
	}
	t.Setenv("CORPUS_SHARD", "2/3")
	var names []string
	for _, r := range Each(t, Files(paths, "/c"), func(*testing.T, File) {}) {
		names = append(names, r.Name)
	}
	var want []string
	for _, f := range Files(paths, "/c") {
		if InShard(f.Name, 2, 3) {
			want = append(want, f.Name)
		}
	}
	if !slices.Equal(names, want) {
		t.Errorf("shard 2/3 ran %v, want %v", names, want)
	}
}
