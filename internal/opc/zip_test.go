package opc

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
)

func TestBuildZip(t *testing.T) {
	members := []ZipMember{{"a", "first"}, {"b", "second"}}
	a, err := BuildZip(members...)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildZip(members...)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("non-deterministic zip: %v", err)
	}
	z, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != len(members) {
		t.Fatal("wrong member count")
	}
	for i, f := range z.File {
		if f.Name != members[i].Name || f.Method != zip.Deflate || !f.Modified.Equal(ZipStamp) {
			t.Fatalf("bad header: %+v", f.FileHeader)
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
		if string(body) != members[i].Body {
			t.Fatalf("body %q", body)
		}
	}
}
func TestPartKey(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"XL/A.XML", "/xl/a.xml"}, {"/XL/A.XML", "/xl/a.xml"}, {"", "/"}} {
		if got := PartKey(tc.in); got != tc.want {
			t.Errorf("PartKey(%q) = %q", tc.in, got)
		}
	}
}
