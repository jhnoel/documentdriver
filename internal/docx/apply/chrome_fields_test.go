package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// TestStoryFieldsNotDirty: footer.set and header.set with page fields,
// in both modes, write no w:dirty field and no w:updateFields setting
// (either makes Word ask on open to update fields that may refer to
// other files; docxdriver writes w:dirty="true", fixed here).
func TestStoryFieldsNotDirty(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(root, "tools", "oracle", "testdata", "write", "sections.docx")
	for _, op := range []string{
		`[{"op":"footer.set","at":"section:1","html":"<p>Page <page-number/> of <page-count/></p>","delete":["opaque:1"]}]`,
		`[{"op":"header.set","at":"section:2","html":"<p>Page <page-number/></p>"}]`,
	} {
		for _, mode := range []string{laws.Track, laws.Direct} {
			out := filepath.Join(t.TempDir(), "out.docx")
			if err := runPlan(laws.Private(t, in), out, mode, op); err != nil {
				t.Fatalf("%s %s: %v", mode, op, err)
			}
			b, err := os.ReadFile(out) //nolint:gosec // G304: the test's own output
			if err != nil {
				t.Fatal(err)
			}
			for name, part := range zipEntries(t, b) {
				s := string(part)
				if strings.Contains(s, "updateFields") || strings.Contains(s, `w:dirty="true"`) && !strings.Contains(partOf(t, in, name), `w:dirty="true"`) {
					t.Errorf("%s %s: %s writes a dirty field or updateFields", mode, op, name)
				}
			}
		}
	}
}

// partOf returns part name of the package at path, "" when absent.
func partOf(t *testing.T, path, name string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: a test input
	if err != nil {
		t.Fatal(err)
	}
	return string(zipEntries(t, b)[name])
}
