package integrity

import (
	"archive/zip"
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
)

func TestCP437Table(t *testing.T) {
	if len(cp437) != 128 {
		t.Fatalf("%d entries", len(cp437))
	}
	if cp437[0] != 'Ç' || cp437[0x7F] != 0xA0 {
		t.Fatalf("ends %q %q", cp437[0], cp437[0x7F])
	}
}

func TestEntryNames(t *testing.T) {
	for _, c := range []struct {
		name  string
		flags uint16
		want  string
	}{
		{"word/document.xml", 0, "word/document.xml"},
		{"caf\x82.xml", 0, "café.xml"},
		{"café.xml", 0x800, "café.xml"},
	} {
		if got := entryName(&zip.File{FileHeader: zip.FileHeader{Name: c.name, Flags: c.flags}}); got != c.want {
			t.Errorf("%q: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestReadingIsBounded(t *testing.T) {
	big := testPackage(t, `<w:p><w:r><w:t>`+strings.Repeat("a", 4<<10)+`</w:t></w:r></w:p>`, "")
	got := CheckWithLimits(big, opc.Limits{MaxPartSize: 1 << 10})
	want := Finding{"word/document.xml", "package: entry exceeds a safety limit"}
	if !got.Has(want) {
		t.Fatalf("%q", got.Kinds())
	}
	if f := CheckWithLimits(big, opc.Limits{MaxEntries: 2}); !slices.Equal(f.Messages(), []string{"package: exceeds a safety limit (too many entries)"}) {
		t.Fatalf("%q", f.Kinds())
	}
}

func TestContainerFaults(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"[Content_Types].xml", "a/../b.xml", "A.xml", "a.xml"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`)); err != nil {
			t.Fatal(err)
		}
	}
	h := &zip.FileHeader{Name: "enc.xml", Method: zip.Store, Flags: 0x1}
	if _, err := zw.CreateHeader(h); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	got := Check(buf.Bytes())
	for _, want := range []Finding{
		{"a/../b.xml", "package: invalid part name"},
		{"a.xml", "integrity: duplicate part name (part names are case-insensitive)"},
		{"enc.xml", "package: entry is encrypted"},
		{"_rels/.rels", "integrity: no officeDocument relationship"},
	} {
		if !got.Has(want) {
			t.Errorf("%v not among %q", want, got.Kinds())
		}
	}
}
