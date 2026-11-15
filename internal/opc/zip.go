package opc

import (
	"archive/zip"
	"bytes"
	"fmt"
	"time"
)

// ZipStamp is the modification time of every member BuildZip writes:
// 1980-01-01, the zip format's earliest, so a built package is the same
// bytes every time.
var ZipStamp = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// ZipMember is one member of a zip BuildZip writes: its name in the zip
// (no leading slash) and its content.
type ZipMember struct {
	Name, Body string
}

// BuildZip returns the bytes of a new zip holding members in order, each
// deflated and stamped ZipStamp: a template package or a test fixture.
// It is the one zip writer outside the package's own Save (archtest
// TestZipThroughOPC).
func BuildZip(members ...ZipMember) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, m := range members {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: m.Name, Method: zip.Deflate, Modified: ZipStamp})
		if err != nil {
			return nil, fmt.Errorf("opc: zip member %s: %w", m.Name, err)
		}
		if _, err := w.Write([]byte(m.Body)); err != nil {
			return nil, fmt.Errorf("opc: zip member %s: %w", m.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("opc: finish zip: %w", err)
	}
	return buf.Bytes(), nil
}
