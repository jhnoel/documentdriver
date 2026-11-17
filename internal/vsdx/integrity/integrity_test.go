package integrity

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// openFixture opens the vsdx fixture name and returns its package.
func openFixture(t *testing.T, name string) *opc.Package {
	t.Helper()
	return vsdxtest.OpenPackage(t, name)
}

// checkFixture checks the package of the fixture name.
func checkFixture(t *testing.T, name string) []Finding {
	t.Helper()
	findings, err := Check(openFixture(t, name))
	if err != nil {
		t.Fatalf("Check(%s): %v", name, err)
	}
	return findings
}

// kindsAt returns the kind and at of each finding, for table rows.
func kindsAt(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind+" "+f.Part+" "+f.At)
	}
	return out
}

// TestCheckClean checks the G1 rows: V1, V2 and V5 have no finding.
func TestCheckClean(t *testing.T) {
	for _, name := range []string{"v1-minimal", "v2-master", "v5-glue"} {
		if findings := checkFixture(t, name); len(findings) != 0 {
			t.Errorf("Check(%s) = %v, want no finding", name, findings)
		}
	}
}

// TestCheckV10 checks the V10 row: duplicate-shape-id at page0/s7,
// dangling-connect for FromSheet 0, and (per V10's PROVENANCE.md)
// duplicate-page-id, missing-part at page4 and unresolved-master at
// page0/s8, sorted by part then at.
func TestCheckV10(t *testing.T) {
	findings := checkFixture(t, "v10-defects")
	want := []string{
		"dangling-connect /visio/pages/page1.xml page0/s0",
		"duplicate-shape-id /visio/pages/page1.xml page0/s7",
		"unresolved-master /visio/pages/page1.xml page0/s8",
		"missing-part /visio/pages/pages.xml page4",
		"duplicate-page-id /visio/pages/pages.xml page5",
	}
	// Findings are sorted by part then at, so compare in that order.
	sorted := slices.Clone(findings)
	slices.SortFunc(sorted, func(a, b Finding) int {
		return compareFindings(a, b)
	})
	if got := kindsAt(findings); !slices.Equal(got, kindsAt(sorted)) {
		t.Errorf("Check(v10-defects) is not sorted by part then at: %v", got)
	}
	if got := kindsAt(findings); !slices.Equal(got, want) {
		t.Errorf("Check(v10-defects) = %v, want %v", got, want)
	}
}

// openBlank opens the create blank golden directory as a package.
func openBlank(t *testing.T) *opc.Package {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	dir := filepath.Join(root, "testdata", "vsdx", "create", "blank")
	var members []opc.ZipMember
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == "PROVENANCE.md" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path) //nolint:gosec // G304: testdata path from the repo root, never from a document
		if err != nil {
			return err
		}
		members = append(members, opc.ZipMember{Name: filepath.ToSlash(rel), Body: string(body)})
		return nil
	})
	if err != nil {
		t.Fatalf("walk blank: %v", err)
	}
	slices.SortFunc(members, func(a, b opc.ZipMember) int { return strings.Compare(a.Name, b.Name) })
	ordered := make([]opc.ZipMember, 0, len(members))
	for _, m := range members {
		if m.Name == "[Content_Types].xml" {
			ordered = append(ordered, m)
		}
	}
	for _, m := range members {
		if m.Name != "[Content_Types].xml" {
			ordered = append(ordered, m)
		}
	}
	data, err := opc.BuildZip(ordered...)
	if err != nil {
		t.Fatalf("BuildZip(blank): %v", err)
	}
	p, err := opc.OpenBytes(data, opc.Limits{})
	if err != nil {
		t.Fatalf("OpenBytes(blank): %v", err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close blank: %v", err)
		}
	})
	return p
}

// TestCheckBlank checks the G1 row: the create blank has no finding.
func TestCheckBlank(t *testing.T) {
	findings, err := Check(openBlank(t))
	if err != nil {
		t.Fatalf("Check(blank): %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("Check(blank) = %v, want no finding", findings)
	}
}

// memberMap returns the members of the fixture name as a map, for
// building a mutated copy.
func memberMap(t *testing.T, name string) map[string]string {
	t.Helper()
	p := openFixture(t, name)
	members := make(map[string]string)
	for _, part := range p.Names() {
		pt, err := p.Part(part)
		if err != nil {
			t.Fatalf("Part(%s): %v", part, err)
		}
		body, err := pt.Original()
		if err != nil {
			t.Fatalf("Original(%s): %v", part, err)
		}
		members[strings.TrimPrefix(part, "/")] = string(body)
	}
	return members
}

// checkMutated builds a copy of the fixture name with one member
// replaced by the result of edit, opens it, and checks it.
func checkMutated(t *testing.T, name, member string, edit func(string) string) []Finding {
	t.Helper()
	members := memberMap(t, name)
	before, ok := members[member]
	if !ok {
		t.Fatalf("fixture %s has no member %s", name, member)
	}
	members[member] = edit(before)
	names := make([]string, 0, len(members))
	for n := range members {
		names = append(names, n)
	}
	slices.Sort(names)
	ordered := make([]opc.ZipMember, 0, len(names))
	for _, n := range names {
		if n == "[Content_Types].xml" {
			ordered = append(ordered, opc.ZipMember{Name: n, Body: members[n]})
		}
	}
	for _, n := range names {
		if n != "[Content_Types].xml" {
			ordered = append(ordered, opc.ZipMember{Name: n, Body: members[n]})
		}
	}
	data, err := opc.BuildZip(ordered...)
	if err != nil {
		t.Fatalf("BuildZip(mutated %s): %v", name, err)
	}
	p, err := opc.OpenBytes(data, opc.Limits{})
	if err != nil {
		t.Fatalf("OpenBytes(mutated %s): %v", name, err)
	}
	defer func() {
		if err := p.Close(); err != nil {
			t.Errorf("close mutated %s: %v", name, err)
		}
	}()
	findings, err := Check(p)
	if err != nil {
		t.Fatalf("Check(mutated %s): %v", name, err)
	}
	return findings
}

// mustReplace returns edit replacing the one occurrence of old in a
// member with new, failing the test when old is absent.
func mustReplace(t *testing.T, member, old, repl string) func(string) string {
	t.Helper()
	return func(s string) string {
		if !strings.Contains(s, old) {
			t.Fatalf("member %s does not contain %q", member, old)
		}
		return strings.Replace(s, old, repl, 1)
	}
}

// TestCheckMutations checks the G1 mutation rows on copies of V1: a
// second Override for page1.xml, the page Rel removed, Master 99 and
// BackPage 9, each with exactly its finding.
func TestCheckMutations(t *testing.T) {
	// duplicate-override: a second Override for /visio/pages/page1.xml.
	findings := checkMutated(t, "v1-minimal", "[Content_Types].xml", func(s string) string {
		return strings.Replace(s, "</Types>",
			`<Override PartName="/visio/pages/page1.xml" ContentType="application/vnd.ms-visio.page+xml"/></Types>`, 1)
	})
	if got := kindsAt(findings); !slices.Equal(got, []string{"duplicate-override /[Content_Types].xml /visio/pages/page1.xml"}) {
		t.Errorf("duplicate-override mutation = %v, want one duplicate-override finding", got)
	}

	// missing-part: the page relationship in pages.xml.rels removed.
	findings = checkMutated(t, "v1-minimal", "visio/pages/_rels/pages.xml.rels", func(s string) string {
		return strings.Replace(s,
			`<Relationship Id="rId1" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page1.xml"/>`, "", 1)
	})
	if got := kindsAt(findings); !slices.Equal(got, []string{"missing-part /visio/pages/pages.xml page0"}) {
		t.Errorf("page Rel removed mutation = %v, want one missing-part finding at page0", got)
	}

	// unresolved-master: s1 gains Master 99.
	findings = checkMutated(t, "v1-minimal", "visio/pages/page1.xml",
		mustReplace(t, "visio/pages/page1.xml", "<Shape ID='1'", "<Shape ID='1' Master='99'"))
	if got := kindsAt(findings); !slices.Equal(got, []string{"unresolved-master /visio/pages/page1.xml page0/s1"}) {
		t.Errorf("Master 99 mutation = %v, want one unresolved-master finding at page0/s1", got)
	}

	// unresolved-backpage: page0 gains BackPage 9.
	findings = checkMutated(t, "v1-minimal", "visio/pages/pages.xml",
		mustReplace(t, "visio/pages/pages.xml", "<Page ID='0'", "<Page ID='0' BackPage='9'"))
	if got := kindsAt(findings); !slices.Equal(got, []string{"unresolved-backpage /visio/pages/pages.xml page0"}) {
		t.Errorf("BackPage 9 mutation = %v, want one unresolved-backpage finding at page0", got)
	}
}

// TestCheckExtraKinds checks the remaining finding kinds on mutated
// copies: unresolved-style, duplicate-rel, kind-mismatch and a
// MasterShape naming no shape of its master.
func TestCheckExtraKinds(t *testing.T) {
	findings := checkMutated(t, "v1-minimal", "visio/pages/page1.xml",
		mustReplace(t, "visio/pages/page1.xml", "LineStyle='3'", "LineStyle='99'"))
	if got := kindsAt(findings); !slices.Equal(got, []string{"unresolved-style /visio/pages/page1.xml page0/s1"}) {
		t.Errorf("LineStyle 99 mutation = %v, want one unresolved-style finding", got)
	}

	findings = checkMutated(t, "v1-minimal", "visio/pages/_rels/pages.xml.rels", func(s string) string {
		return strings.Replace(s, "</Relationships>",
			`<Relationship Id="rId99" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page1.xml"/></Relationships>`, 1)
	})
	if got := kindsAt(findings); !slices.Equal(got, []string{"duplicate-rel /visio/pages/_rels/pages.xml.rels /visio/pages/page1.xml"}) {
		t.Errorf("duplicate rel mutation = %v, want one duplicate-rel finding", got)
	}

	findings = checkMutated(t, "v1-minimal", "[Content_Types].xml",
		mustReplace(t, "[Content_Types].xml",
			`<Override PartName="/visio/pages/page1.xml" ContentType="application/vnd.ms-visio.page+xml"/>`,
			`<Override PartName="/visio/pages/page1.xml" ContentType="application/vnd.ms-visio.master+xml"/>`))
	if got := kindsAt(findings); !slices.Equal(got, []string{"kind-mismatch /visio/pages/page1.xml page0"}) {
		t.Errorf("page content type mutation = %v, want one kind-mismatch finding", got)
	}

	findings = checkMutated(t, "v2-master", "visio/pages/page1.xml",
		mustReplace(t, "visio/pages/page1.xml", "<Shape ID='4' Type='Shape' MasterShape='6'/>",
			"<Shape ID='4' Type='Shape' MasterShape='99'/>"))
	if got := kindsAt(findings); !slices.Equal(got, []string{"unresolved-master /visio/pages/page1.xml page0/s4"}) {
		t.Errorf("MasterShape 99 mutation = %v, want one unresolved-master finding at page0/s4", got)
	}
}

// TestCheckNil checks that Check(nil) is an error, not a panic.
func TestCheckNil(t *testing.T) {
	if _, err := Check(nil); err == nil {
		t.Error("Check(nil) succeeded, want an error")
	}
}

// TestScalingCheck checks the scaling row: Check on Generated(1000)
// and Generated(4000) (N = 1000 shapes, 4N = 4000) allocates at most
// 5.5 times as much (laws.Scales) and the laws.TimeScales time ratio is at
// most 8 (linear gives about 4, quadratic 16).
func TestScalingCheck(t *testing.T) {
	setup := func(n int) (func() error, error) {
		path := vsdxtest.Generated(t, n)
		return func() error {
			p, err := opc.Open(path)
			if err != nil {
				return err
			}
			defer func() {
				if cerr := p.Close(); cerr != nil {
					t.Errorf("close Generated(%d): %v", n, cerr)
				}
			}()
			findings, err := Check(p)
			if err != nil {
				return err
			}
			if len(findings) != 0 {
				return fmt.Errorf("Generated(%d): findings %v", n, findings)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx integrity.Check", setup)
	laws.TimeScales(t, "vsdx integrity.Check", setup)
}
