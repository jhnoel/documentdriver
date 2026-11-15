package apply

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
)

// partText returns a part of the package at path.
func partText(t *testing.T, path, part string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == part {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close() //nolint:errcheck // read-only
			s, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			return string(s)
		}
	}
	t.Fatalf("no part %s", part)
	return ""
}

// TestStoryParagraphs: a paragraph of a header (or another story) is
// addressed by its stored w14:paraId, as read lists bookmark anchors
// there, and text operations edit it in its own part: revisions are
// numbered in that part, and the body is untouched. A body paragraph
// with the same id would win.
func TestStoryParagraphs(t *testing.T) {
	in := writeFixture(t, "sections.docx")
	for _, mode := range []string{laws.Track, laws.Direct} {
		out := filepath.Join(t.TempDir(), "out.docx")
		if err := runPlan(laws.Private(t, in), out, mode, `[{"op":"text.replace","at":"40000001","select":"one","with":"ONE"}]`); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		hdr := partText(t, out, "word/header1.xml")
		want := `<w:ins w:id="52" w:author="Law Author" w:date="2026-01-01T00:00:00Z"><w:r><w:t>ONE</w:t></w:r></w:ins>`
		if mode == laws.Direct {
			want = `<w:r><w:t>ONE</w:t></w:r>`
		}
		if !strings.Contains(hdr, want) {
			t.Errorf("%s: no %s in %s", mode, want, hdr)
		}
		if partText(t, out, "word/document.xml") != partText(t, in, "word/document.xml") && mode == laws.Track {
			// Saving stores the body's missing paragraph ids; nothing else.
			if strings.Contains(partText(t, out, "word/document.xml"), "ONE") {
				t.Errorf("%s: the body changed", mode)
			}
		}
	}
	err := runPlan(laws.Private(t, in), filepath.Join(t.TempDir(), "out.docx"), laws.Direct, `[{"op":"text.replace","at":"4000000F","select":"one","with":"ONE"}]`)
	var r refusal
	if !errors.As(err, &r) || r.d.Code != "not_found" {
		t.Errorf("an id no story holds: %v", err)
	}
}
