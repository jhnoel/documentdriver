package integrity

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// TestGateMatchesFullCheckCorpus is the corpus half of
// TestGateMatchesFullCheck (verification chunk V): on every corpus
// workbook, CheckScoped with every part in scope has exactly Check's
// findings and counts, and CheckScoped of one worksheet or one table
// part has, in each part it reads, Check's findings and counts there.
// A table part's duplicate id and name findings depend on the tables
// read before it, so they are compared only in the full scope (the gate
// checks input and output with one scope, so they cancel there).
func TestGateMatchesFullCheckCorpus(t *testing.T) {
	dir := xlsxCorpus(t)
	if dir == "" {
		t.Skip("no xlsx corpus (set XLSX_CORPUS)")
	}
	files := corpustest.Files(walkAll(dir), dir)
	corpustest.Each(t, files, func(t *testing.T, f corpustest.File) {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		full := Check(data)
		if full.HasKind(PackageUnreadable) {
			return
		}
		parts, err := xlsxtest.Parts(data)
		if err != nil {
			return // not a zip the test helper reads; Check reported it
		}
		var all Scope
		var one []string
		for name := range parts {
			all.Parts = append(all.Parts, "/"+name)
			if strings.HasPrefix(name, "xl/worksheets/") && strings.HasSuffix(name, ".xml") ||
				strings.HasPrefix(name, "xl/tables/") && strings.HasSuffix(name, ".xml") {
				one = append(one, "/"+name)
			}
		}
		slices.Sort(all.Parts)
		slices.Sort(one)
		scoped := CheckScoped(data, all)
		if !slices.Equal(full.Kinds(), scoped.Kinds()) {
			t.Fatalf("every part in scope: findings differ:\nfull   %v\nscoped %v", full.Kinds(), scoped.Kinds())
		}
		for _, k := range full.Kinds() {
			if full.Count(k) != scoped.Count(k) {
				t.Errorf("every part in scope: %v: full %d, scoped %d", k, full.Count(k), scoped.Count(k))
			}
		}
		if len(one) > 40 {
			one = one[:40] // bound the per-file cost; the order is by name
		}
		for _, p := range one {
			s := CheckScoped(data, Scope{Parts: []string{p}})
			for _, k := range s.Kinds() {
				if k.Kind == TableIDDuplicate || k.Kind == TableNameDuplicate {
					continue
				}
				if full.Count(k) != s.Count(k) {
					t.Errorf("scope %s: %v: full %d, scoped %d", p, k, full.Count(k), s.Count(k))
				}
			}
			// Nothing Check finds in the part itself is missed.
			for _, k := range full.Kinds() {
				if opc.PartKey(k.Part) != opc.PartKey(p) || k.Kind == TableIDDuplicate || k.Kind == TableNameDuplicate {
					continue
				}
				if s.Count(k) != full.Count(k) {
					t.Errorf("scope %s: %v: full %d, scoped %d", p, k, full.Count(k), s.Count(k))
				}
			}
		}
	})
}
