package opc

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// damagedZip writes a package whose member word/unused.xml has a wrong
// CRC: reading it fails, copying it raw does not.
func damagedZip(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, m := range []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`},
		{"word/document.xml", `<doc a="1"/>`},
	} {
		w, err := zw.Create(m.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	body := []byte(`<unused/>`)
	w, err := zw.CreateRaw(&zip.FileHeader{Name: "word/unused.xml", Method: zip.Store, CRC32: 0xdeadbeef,
		CompressedSize64: uint64(len(body)), UncompressedSize64: uint64(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "damaged.docx")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestDamagedMemberCopiedRaw: a member nothing reads is copied through as
// its original raw entry by Save and by WriteZip, bad CRC and all, while
// another member is edited.
func TestDamagedMemberCopiedRaw(t *testing.T) {
	path := damagedZip(t)
	before, _ := rawEntries(t, path)
	p := open(t, path)
	pt, err := p.Part("/word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	pt.SetBytes([]byte(`<doc a="2"/>`))
	var snap bytes.Buffer
	if err := p.WriteZip(&snap); err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	snapPath := filepath.Join(t.TempDir(), "snap.docx")
	if err := os.WriteFile(snapPath, snap.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveAndClose(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, out := range []string{path, snapPath} {
		after, _ := rawEntries(t, out)
		if len(after) != len(before) {
			t.Fatalf("%s: %d members, want %d", out, len(after), len(before))
		}
		for i := range before {
			if before[i].hdr.Name != "word/unused.xml" {
				continue
			}
			if d := sameEntry(before[i], after[i]); d != "" || !bytes.Equal(before[i].raw, after[i].raw) || !bytes.Equal(before[i].local, after[i].local) {
				t.Errorf("%s: the damaged member changed: %s", out, d)
			}
		}
	}
}

// defectsZip writes a package with the member defects the corpus has
// (apache-poi crash-57308ca3..., libreoffice fail/ofz35597-1 and
// ofz46160-1, shapecrawler 031): a wrong CRC, a deflate stream that is
// corrupt, one cut short, and an XML part that is not well formed. Its
// zip directory is sound, so it opens.
func defectsZip(t *testing.T) (string, []string) {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, m := range []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`},
		{"word/document.xml", `<doc a="1"/>`},
		{"customXml/item1.xml", "\x00junk<a/>"},
	} {
		w, err := zw.Create(m.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	var deflated bytes.Buffer
	fw, err := flate.NewWriter(&deflated, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	full := []byte(strings.Repeat(`<slide n="1"/>`, 200))
	if _, err := fw.Write(full); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Repeat([]byte{0xff}, 64)
	for _, m := range []struct {
		name   string
		method uint16
		body   []byte
		crc    uint32
		size   int
	}{
		{"word/badcrc.xml", zip.Store, []byte(`<unused/>`), 0xdeadbeef, 9},
		{"word/corrupt.xml", zip.Deflate, corrupt, crc32.ChecksumIEEE(full), len(full)},
		{"word/short.xml", zip.Deflate, deflated.Bytes()[:deflated.Len()/2], crc32.ChecksumIEEE(full), len(full)},
	} {
		w, err := zw.CreateRaw(&zip.FileHeader{Name: m.name, Method: m.method, CRC32: m.crc,
			CompressedSize64: uint64(len(m.body)), UncompressedSize64: uint64(m.size)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(m.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "defects.docx")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, []string{"/customXml/item1.xml", "/word/badcrc.xml", "/word/corrupt.xml", "/word/short.xml"}
}

// TestDefectsNeverBlockASave: a package whose members are damaged (bad
// CRC, corrupt or truncated deflate data, XML that is not well formed)
// opens; reading those parts fails at the part; a no-op save, after
// every part was read or tried, writes nothing, and the in-memory seal
// is the file's bytes; an edit elsewhere saves, the damaged members
// copied through as their raw entries.
func TestDefectsNeverBlockASave(t *testing.T) {
	path, damaged := defectsZip(t)
	ageFile(t, path)
	s := snap(t, path)
	p := open(t, path)
	for _, name := range p.Names() {
		pt, err := p.Part(name)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pt.Tree()
		if want := slices.Contains(damaged, name); (err != nil) != want {
			t.Errorf("%s: Tree error %v, want an error: %v", name, err, want)
		}
	}
	if changed, err := p.ChangedParts(); err != nil || len(changed) != 0 {
		t.Fatalf("ChangedParts = %v, %v", changed, err)
	}
	sealed, err := p.Seal()
	if err != nil || sealed.Changed() || !bytes.Equal(sealed.Bytes(), s.data) {
		t.Fatalf("Seal of an unchanged package: %v", err)
	}
	if err := p.Save(); err != nil {
		t.Fatalf("no-op Save: %v", err)
	}
	assertUntouched(t, path, s)

	before, _ := rawEntries(t, path)
	p = open(t, path)
	pt, err := p.Part("/word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	pt.SetBytes([]byte(`<doc a="2"/>`))
	if err := p.SaveAndClose(); err != nil {
		t.Fatalf("Save after an edit elsewhere: %v", err)
	}
	after, _ := rawEntries(t, path)
	if len(after) != len(before) {
		t.Fatalf("%d members, want %d", len(after), len(before))
	}
	for i := range before {
		if !slices.Contains(damaged, "/"+before[i].hdr.Name) {
			continue
		}
		if d := sameEntry(before[i], after[i]); d != "" || !bytes.Equal(before[i].raw, after[i].raw) {
			t.Errorf("%s changed: %s", before[i].hdr.Name, d)
		}
	}
}
