package read

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/render"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// fuzzFixtureMembers returns the three members FuzzRead mutates of
// fixture (V1, V2 or V5): its page part, its pages part and the
// shared document part.
func fuzzFixtureMembers(t testing.TB, fixture string) (page, pages, doc string) {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	page = readMemberFile(t, filepath.Join(root, "testdata", "vsdx", fixture, "visio", "pages", "page1.xml"))
	pages = readMemberFile(t, filepath.Join(root, "testdata", "vsdx", fixture, "visio", "pages", "pages.xml"))
	doc = readMemberFile(t, filepath.Join(root, "testdata", "vsdx", "common", "document.xml"))
	return page, pages, doc
}

// readMemberFile returns the content of the testdata file at path.
func readMemberFile(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: testdata path from the repo root, never from a document
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// packageWithMembers builds the v1-minimal package with its page,
// pages and document members replaced by page, pages and doc, and
// no file writes: the members are read from testdata/vsdx/
// v1-minimal and common, [Content_Types].xml first and the rest in
// name order, as vsdxtest builds them.
func packageWithMembers(t testing.TB, page, pages, doc string) []byte {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	v1 := filepath.Join(root, "testdata", "vsdx", "v1-minimal")
	common := filepath.Join(root, "testdata", "vsdx", "common")
	members := []opc.ZipMember{
		{Name: "[Content_Types].xml", Body: readMemberFile(t, filepath.Join(v1, "[Content_Types].xml"))},
		{Name: "_rels/.rels", Body: readMemberFile(t, filepath.Join(v1, "_rels", ".rels"))},
		{Name: "docProps/app.xml", Body: readMemberFile(t, filepath.Join(common, "app.xml"))},
		{Name: "visio/_rels/document.xml.rels", Body: readMemberFile(t, filepath.Join(v1, "visio", "_rels", "document.xml.rels"))},
		{Name: "visio/document.xml", Body: doc},
		{Name: "visio/pages/_rels/pages.xml.rels", Body: readMemberFile(t, filepath.Join(v1, "visio", "pages", "_rels", "pages.xml.rels"))},
		{Name: "visio/pages/page1.xml", Body: page},
		{Name: "visio/pages/pages.xml", Body: pages},
	}
	data, err := opc.BuildZip(members...)
	if err != nil {
		t.Fatalf("opc.BuildZip: %v", err)
	}
	return data
}

// readAllFuzz runs every read on input: Document, Page for the
// first two addresses Document names, Find and XML of the first
// object. Every result must encode as valid JSON. It never panics
// on any input (AGENTS.md: no panics on input). Page is capped at
// two addresses because Page reopens the package: a mutated pages
// part can name thousands of pages, and reading each is quadratic
// on that count in wall time.
func readAllFuzz(t *testing.T, input []byte) {
	t.Helper()
	check := func(what string, v any, err error) {
		if err != nil {
			return
		}
		b, err := cli.Marshal(v)
		if err != nil || !json.Valid(b) {
			t.Fatalf("%s: does not encode: %v", what, err)
		}
	}
	sum, err := Document(input, "B")
	check("summary", sum, err)
	if err != nil {
		return
	}
	jsonCheck := func(what string, b []byte, err error) {
		if err == nil && !json.Valid(b) {
			t.Fatalf("%s: does not encode", what)
		}
	}
	var xmlIDs []string
	addrs := make([]string, 0, len(sum.Pages)+len(sum.Masters))
	for _, p := range sum.Pages {
		addrs = append(addrs, p.ID)
	}
	for _, m := range sum.Masters {
		addrs = append(addrs, m.ID)
	}
	if len(addrs) > 2 {
		addrs = addrs[:2]
	}
	for _, addr := range addrs {
		b, err := Page(input, "B", addr, Options{})
		jsonCheck("page "+addr, b, err)
		if err != nil {
			continue
		}
		var p pageJSON
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatalf("page %s: does not parse: %v", addr, err)
		}
		if len(p.Objects) > 0 {
			xmlIDs = append(xmlIDs, p.Objects[0].ID)
		}
	}
	// Find walks every page: bound it to one page when the mutated
	// pages part names many (the same budget as Page above).
	if len(sum.Pages) <= 4 {
		res, err := Find(input, "B", FindRequest{Query: "a", IgnoreCase: true})
		check("find", res, err)
	} else {
		res, err := Find(input, "B", FindRequest{Query: "a", IgnoreCase: true, Page: sum.Pages[0].ID})
		check("find", res, err)
	}
	for _, id := range xmlIDs {
		if _, err := XML(input, id); err != nil {
			continue
		}
	}
}

// FuzzRead runs every read on the v1-minimal package whose page,
// pages and document members are arbitrary, seeded with the members
// of V1, V2 and V5: it never panics. Inputs larger than 64KB in
// total are skipped: duplication mutations can grow the members far
// past the seeds (thousands of named pages), and Document walks every
// page, so one such input would cost seconds and stall the fuzzer
// instead of exercising the readers.
func FuzzRead(f *testing.F) {
	for _, fx := range lawFixtures {
		page, pages, doc := fuzzFixtureMembers(f, fx)
		f.Add(page, pages, doc)
	}
	f.Fuzz(func(t *testing.T, page, pages, doc string) {
		if len(page)+len(pages)+len(doc) > 64*1024 {
			return
		}
		readAllFuzz(t, packageWithMembers(t, page, pages, doc))
	})
}

// FuzzFind runs find with arbitrary queries over V2: it never
// panics, and for a valid UTF-8 query ignoring case finds at least
// what an exact search finds.
func FuzzFind(f *testing.F) {
	for _, q := range []string{"", "Process", "review", "12.5", "Box", "Left", "\u03a3"} {
		f.Add(q)
	}
	f.Fuzz(func(t *testing.T, q string) {
		input := fixtureBytes(t, "v2-master")
		exact, err := Find(input, "B", FindRequest{Query: q})
		if err != nil {
			t.Fatal(err)
		}
		folded, err := Find(input, "B", FindRequest{Query: q, IgnoreCase: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Find(input, "B", FindRequest{Query: q, Page: "page0"}); err != nil {
			t.Fatal(err)
		}
		// The superset law needs whole runes: a query that is a
		// fragment of a UTF-8 sequence can match the bytes of a rune
		// that folding changes.
		if utf8.ValidString(q) && len(folded.Matches) < len(exact.Matches) {
			t.Fatalf("%q: %d matches ignoring case, %d exact", q, len(folded.Matches), len(exact.Matches))
		}
	})
}

// FuzzPageSVG draws the first two pages of the v1-minimal package
// whose page, pages and document members are arbitrary (seeded with
// V1, V2 and V5, as FuzzRead): for every page the drawing view builds,
// the SVG is well formed with an svg root, and rasterising it at a
// small scale fails only with an error, never a panic. Inputs larger
// than 64KB are skipped, as in FuzzRead.
func FuzzPageSVG(f *testing.F) {
	for _, fx := range lawFixtures {
		page, pages, doc := fuzzFixtureMembers(f, fx)
		f.Add(page, pages, doc)
	}
	f.Fuzz(func(t *testing.T, page, pages, doc string) {
		if len(page)+len(pages)+len(doc) > 64*1024 {
			return
		}
		input := packageWithMembers(t, page, pages, doc)
		sum, err := Document(input, "B")
		if err != nil {
			return
		}
		for i, p := range sum.Pages {
			if i == 2 {
				break
			}
			scene, _, err := PageScene(input, "B", p.ID, Options{})
			if err != nil {
				continue
			}
			var buf bytes.Buffer
			if err := render.WriteSVG(&buf, scene); err != nil {
				t.Fatalf("%s: WriteSVG: %v", p.ID, err)
			}
			d, err := tree.Parse(buf.Bytes())
			if err != nil || d.Root() == nil || d.Root().Local() != "svg" {
				t.Fatalf("%s: SVG not well formed: %v", p.ID, err)
			}
			if _, _, err := render.Rasterise(scene, 0.25); err != nil {
				continue
			}
		}
	})
}
