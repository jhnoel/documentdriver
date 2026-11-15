package opc

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"testing"
)

// rawEntry is a zip member as stored: central header fields, compressed
// bytes, and the local header bytes.
type rawEntry struct {
	hdr   zip.FileHeader
	raw   []byte
	local []byte // local file header including name and extra; nil if not located
}

// rawEntries reads every member of a zip without decompressing.
func rawEntries(t testing.TB, path string) ([]rawEntry, string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	locals := localHeaders(file)
	var out []rawEntry
	for i, zf := range zr.File {
		r, err := zf.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		e := rawEntry{hdr: zf.FileHeader, raw: b}
		if i < len(locals) {
			e.local = locals[i]
		}
		out = append(out, e)
	}
	return out, zr.Comment
}

// localHeaders returns the local header bytes of each central directory
// record, in central directory order, or nil for zip64 archives.
func localHeaders(file []byte) [][]byte {
	eocd := bytes.LastIndex(file, []byte("PK\x05\x06"))
	if eocd < 0 || eocd+22 > len(file) {
		return nil
	}
	n := int(binary.LittleEndian.Uint16(file[eocd+10:]))
	off := int(binary.LittleEndian.Uint32(file[eocd+16:]))
	if n == 0xFFFF || off == 0xFFFFFFFF {
		return nil
	}
	var out [][]byte
	for i := 0; i < n; i++ {
		if off+46 > len(file) || !bytes.Equal(file[off:off+4], []byte("PK\x01\x02")) {
			return nil
		}
		nameLen := int(binary.LittleEndian.Uint16(file[off+28:]))
		extraLen := int(binary.LittleEndian.Uint16(file[off+30:]))
		commentLen := int(binary.LittleEndian.Uint16(file[off+32:]))
		lo := int(binary.LittleEndian.Uint32(file[off+42:]))
		if lo+30 > len(file) {
			return nil
		}
		ln := int(binary.LittleEndian.Uint16(file[lo+26:]))
		le := int(binary.LittleEndian.Uint16(file[lo+28:]))
		if lo+30+ln+le > len(file) {
			return nil
		}
		out = append(out, file[lo:lo+30+ln+le])
		off += 46 + nameLen + extraLen + commentLen
	}
	return out
}

// sameEntry reports how two stored members differ, or "" if they are the
// same in every field archive/zip exposes plus the compressed bytes.
func sameEntry(a, b rawEntry) string {
	ha, hb := a.hdr, b.hdr
	switch {
	case ha.Name != hb.Name:
		return fmt.Sprintf("name %q vs %q", ha.Name, hb.Name)
	case ha.Method != hb.Method:
		return "method"
	case ha.Flags != hb.Flags:
		return fmt.Sprintf("flags %#x vs %#x", ha.Flags, hb.Flags)
	case ha.ModifiedTime != hb.ModifiedTime || ha.ModifiedDate != hb.ModifiedDate: //nolint:staticcheck // SA1019: compares the raw MS-DOS fields the writer must preserve
		return "timestamp"
	case ha.CRC32 != hb.CRC32:
		return "crc"
	case ha.CompressedSize64 != hb.CompressedSize64 || ha.UncompressedSize64 != hb.UncompressedSize64:
		return "sizes"
	case !bytes.Equal(ha.Extra, hb.Extra):
		return "central extra"
	case ha.ExternalAttrs != hb.ExternalAttrs:
		return "external attrs"
	case ha.CreatorVersion != hb.CreatorVersion:
		return "creator version"
	case ha.ReaderVersion != hb.ReaderVersion:
		return "reader version"
	case ha.Comment != hb.Comment:
		return "comment"
	case !bytes.Equal(a.raw, b.raw):
		return "compressed bytes"
	}
	return ""
}
