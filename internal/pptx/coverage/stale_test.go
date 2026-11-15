package coverage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestCommittedReportIsCurrent fails when eval/coverage/coverage.json or
// coverage.md differ from what the registry, the operation matrix, the
// committed universe and the committed corpus counts give. It needs
// neither the schemas nor the corpus. Regenerate with
// `go run ./tools/coverage`.
func TestCommittedReportIsCurrent(t *testing.T) {
	dir := filepath.Join(repoRoot(), "eval", "coverage")
	u, err := LoadUniverse(filepath.Join(dir, UniverseFile))
	if err != nil {
		t.Fatal(err)
	}
	c, err := LoadCounts(filepath.Join(dir, CountsFile))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Build(u, c)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := LoadProfile(filepath.Join(dir, ProfileFile))
	if err != nil {
		t.Fatal(err)
	}
	r.AddParity(u, prof)
	want, err := MarshalStable(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		name string
		want []byte
	}{{ReportFile, want}, {MarkdownFile, r.Markdown()}} {
		got, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, f.want) {
			t.Errorf("eval/coverage/%s is stale: run go run ./tools/coverage", f.name)
		}
	}
}

// TestProfileMatchesUniverse fails when the committed pdraw profile names
// paths or values the committed universe does not have: the universe was
// regenerated without rebuilding the profile (eval/parity/build.sh, then
// go run ./tools/coverage --profile). Every path must also fall in a named
// parity area, so that a new kind of item is placed on purpose.
func TestProfileMatchesUniverse(t *testing.T) {
	u := loadUniverse(t)
	p, err := LoadProfile(filepath.Join(repoRoot(), "eval", "coverage", ProfileFile))
	if err != nil {
		t.Fatal(err)
	}
	if p.Commit == "" || len(p.Decks) == 0 || len(p.Paths) == 0 {
		t.Fatalf("%s is empty: build it with eval/parity/build.sh and go run ./tools/coverage --profile", ProfileFile)
	}
	by := u.ByPath()
	for path := range p.Paths {
		it := by[path]
		if it == nil {
			t.Errorf("%s names %s, which is not in universe.json", ProfileFile, path)
			continue
		}
		if parityArea(path) == "other" {
			t.Errorf("%s: no parity area for %s: extend parityArea", ProfileFile, path)
		}
	}
	for path, vals := range p.Values {
		it := by[path]
		if it == nil || it.Enum == "" {
			t.Errorf("%s counts values of %s, which is not an enumerated universe item", ProfileFile, path)
			continue
		}
		for v := range vals {
			ok := false
			for _, e := range u.Enums[it.Enum] {
				ok = ok || e == v
			}
			if !ok {
				t.Errorf("%s: %s = %q is not in enumeration %s", ProfileFile, path, v, it.Enum)
			}
		}
	}
}

// TestParityExemptionsAreUsed fails on an exemption that matches no
// profile item: the list stays small by dropping what no longer applies.
func TestParityExemptionsAreUsed(t *testing.T) {
	p, err := LoadProfile(filepath.Join(repoRoot(), "eval", "coverage", ProfileFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ParityExemptions {
		c := &Claim{Paths: e.Patterns}
		used := false
		for path := range p.Paths {
			used = used || c.Matches(path)
		}
		if !used {
			t.Errorf("parity exemption %q matches no profile path: remove it", e.Patterns)
		}
	}
}

// TestCountsMatchUniverse fails when the committed corpus counts name
// paths the committed universe does not have: the universe was
// regenerated without recounting.
func TestCountsMatchUniverse(t *testing.T) {
	dir := filepath.Join(repoRoot(), "eval", "coverage")
	u := loadUniverse(t)
	c, err := LoadCounts(filepath.Join(dir, CountsFile))
	if err != nil {
		t.Fatal(err)
	}
	by := u.ByPath()
	for p := range c.Paths {
		if by[p] == nil {
			t.Errorf("corpus.json counts %s, which is not in universe.json: recount with --corpus", p)
		}
	}
}

// TestProbedAgreesWithBuild checks that the report and the probes agree
// on which items are probed: Build takes statuses from claims only for
// those.
func TestProbedAgreesWithBuild(t *testing.T) {
	u := loadUniverse(t)
	for i := range u.Items {
		it := &u.Items[i]
		if got, want := Probed(it, u), probeable(it); got != want {
			t.Errorf("%s: Build says probed=%v, the probes %v", it.Path, got, want)
		}
	}
}
