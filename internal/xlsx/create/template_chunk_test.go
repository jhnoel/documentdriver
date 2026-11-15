package create

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestTemplateChunkParts(t *testing.T) {
	data, err := Template()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"}
	if len(zr.File) != len(names) {
		t.Fatalf("got %d parts", len(zr.File))
	}
	for i, f := range zr.File {
		if f.Name != names[i] || f.Method != zip.Deflate || f.Modified.Year() != 1980 {
			t.Errorf("unexpected member: %s, method %d, time %v", f.Name, f.Method, f.Modified)
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		if f.Name == "xl/workbook.xml" && !strings.Contains(string(body), `<calcPr calcId="191029"/>`) {
			t.Error("missing calculation properties")
		}
	}
}
