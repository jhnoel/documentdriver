package apply

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// rawEntry is a zip member as stored: its header fields and compressed
// bytes.
type rawEntry struct {
	method          uint16
	crc             uint32
	csize, usize    uint64
	modified, extra string
	raw             []byte
}

func rawEntryOf(t *testing.T, path, name string) rawEntry {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		r, err := f.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return rawEntry{f.Method, f.CRC32, f.CompressedSize64, f.UncompressedSize64, f.Modified.String(), string(f.Extra), raw}
	}
	t.Fatalf("%s has no %s", path, name)
	return rawEntry{}
}

// TestLazyDamagedMember: lazy.docx holds a member nothing reads whose CRC
// is wrong. Plans that edit document.xml, including ops that read the
// working copy as a package (a style check, settling revisions), save,
// and the damaged member comes out as the same raw entry: header, CRC
// and compressed bytes, never decompressed.
func TestLazyDamagedMember(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "tools", "oracle", "testdata", "write", "lazy.docx")
	want := rawEntryOf(t, src, "word/unused.xml")
	for _, ops := range []string{
		`[{"op":"paragraph.insert","at":"60000001","position":"after","html":"x"}]`,
		`[{"op":"paragraph.format","at":"60000002","style":"Normal","alignment":"right"}]`,
		`[{"op":"paragraph.insert","at":"60000001","position":"after","html":"x"},{"op":"revision.accept","all":true}]`,
	} {
		for _, mode := range []string{"track", "direct"} {
			dst := filepath.Join(t.TempDir(), "lazy.docx")
			if err := laws.CopyFile(src, dst); err != nil {
				t.Fatal(err)
			}
			c := commit(t, dst, opsText(t, dst, mode, ops))
			if c.Outcome != Committed || len(c.Parts) == 0 {
				t.Fatalf("%s %s: %s %+v", ops, mode, c.Outcome, c.Diagnostic)
			}
			if got := rawEntryOf(t, dst, "word/unused.xml"); !bytes.Equal(got.raw, want.raw) || got.crc != want.crc || got.method != want.method ||
				got.csize != want.csize || got.usize != want.usize || got.modified != want.modified || got.extra != want.extra {
				t.Errorf("%s %s: the damaged member changed: %+v, want %+v", ops, mode, got, want)
			}
		}
	}
}
