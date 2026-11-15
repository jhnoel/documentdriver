package opc

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// zipMember is one member of a hand-built test zip.
type zipMember struct {
	name   string
	data   []byte
	method uint16
}

// buildZip writes members to a zip in a temporary file and returns its
// path. Deflated members use flate.BestCompression.
func buildZip(t testing.TB, ms ...zipMember) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	for _, m := range ms {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: m.name, Method: m.method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(m.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "limits.pptx")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readPart(t testing.TB, p *Package, name string) error {
	t.Helper()
	pt, err := p.Part(name)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pt.Bytes()
	return err
}

func wantLimit(t *testing.T, err error, substr string) {
	t.Helper()
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("err = %v, want ErrLimit", err)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("err = %v, want it to mention %q", err, substr)
	}
}

func openLimits(t *testing.T, path string, l Limits) *Package {
	t.Helper()
	p, err := OpenWithLimits(path, l)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func TestLimitPartSize(t *testing.T) {
	path := buildZip(t,
		zipMember{"small.xml", bytes.Repeat([]byte("a"), 100), zip.Deflate},
		zipMember{"big.xml", bytes.Repeat([]byte("b"), 101), zip.Deflate},
		zipMember{"big.bin", bytes.Repeat([]byte("c"), 101), zip.Store},
	)
	p := openLimits(t, path, Limits{MaxPartSize: 100})
	if err := readPart(t, p, "small.xml"); err != nil {
		t.Fatal(err)
	}
	wantLimit(t, readPart(t, p, "big.xml"), "more than 100 bytes")
	wantLimit(t, readPart(t, p, "big.bin"), "more than 100 bytes")
}

func TestLimitTotalSize(t *testing.T) {
	path := buildZip(t,
		zipMember{"a.xml", bytes.Repeat([]byte("a"), 60), zip.Deflate},
		zipMember{"b.xml", bytes.Repeat([]byte("b"), 60), zip.Deflate},
	)
	p := openLimits(t, path, Limits{MaxTotalSize: 100})
	if err := readPart(t, p, "a.xml"); err != nil {
		t.Fatal(err)
	}
	// A part is read once; reading it again costs nothing.
	if err := readPart(t, p, "a.xml"); err != nil {
		t.Fatal(err)
	}
	wantLimit(t, readPart(t, p, "b.xml"), "in total")
}

func TestLimitRatio(t *testing.T) {
	zeros := make([]byte, 4<<20)
	path := buildZip(t,
		zipMember{"zeros.bin", zeros, zip.Deflate},
		zipMember{"small.xml", make([]byte, 64<<10), zip.Deflate},
	)
	// The default ratio limit rejects 4 MiB of zeros (about 1030:1).
	p := openLimits(t, path, Limits{})
	wantLimit(t, readPart(t, p, "zeros.bin"), "1000:1")
	// Below RatioMinSize the ratio is not checked.
	if err := readPart(t, p, "small.xml"); err != nil {
		t.Fatal(err)
	}
	// A lower limit with a lower minimum size stops early.
	p = openLimits(t, path, Limits{MaxRatio: 10, RatioMinSize: 1024})
	wantLimit(t, readPart(t, p, "small.xml"), "10:1")
	// A generous limit reads it.
	p = openLimits(t, path, Limits{MaxRatio: 2000})
	pt, err := p.Part("zeros.bin")
	if err != nil {
		t.Fatal(err)
	}
	b, err := pt.Bytes()
	if err != nil || len(b) != len(zeros) {
		t.Fatalf("read %d bytes, %v", len(b), err)
	}
}

func TestLimitEntries(t *testing.T) {
	path := buildZip(t,
		zipMember{"a.xml", []byte("<a/>"), zip.Deflate},
		zipMember{"b.xml", []byte("<b/>"), zip.Deflate},
		zipMember{"c.xml", []byte("<c/>"), zip.Deflate},
	)
	if _, err := OpenWithLimits(path, Limits{MaxEntries: 2}); err == nil {
		t.Fatal("opened a zip with more entries than the limit")
	} else {
		wantLimit(t, err, "3 zip entries")
	}
	openLimits(t, path, Limits{MaxEntries: 3})
}

// TestLimitSharedData points the second central directory entry at the
// first entry's local header, so both members share compressed data.
func TestLimitSharedData(t *testing.T) {
	path := buildZip(t,
		zipMember{"a.xml", []byte("<a>first</a>"), zip.Deflate},
		zipMember{"b.xml", []byte("<b>second</b>"), zip.Deflate},
	)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Second central directory header: offset of local header at +42.
	sig := []byte{'P', 'K', 1, 2}
	first := bytes.Index(b, sig)
	second := first + 1 + bytes.Index(b[first+1:], sig)
	if first < 0 || second <= first {
		t.Fatal("central directory headers not found")
	}
	binary.LittleEndian.PutUint32(b[second+42:], 0)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("opened a zip whose members share compressed data")
	} else {
		wantLimit(t, err, "share compressed data")
	}
}

// TestLimitCorruptCRC checks that decompressing through the limits still
// verifies the CRC-32, as archive/zip does.
func TestLimitCorruptCRC(t *testing.T) {
	path := buildZip(t, zipMember{"a.bin", []byte("stored bytes"), zip.Store})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(b, []byte("stored bytes"))
	b[i] = 'S'
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	p := openLimits(t, path, Limits{})
	if err := readPart(t, p, "a.bin"); !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("err = %v, want zip.ErrChecksum", err)
	}
}

// TestLimitLazyCopy checks that a member over the part limit does not stop
// a package from opening, editing other parts and saving: untouched
// members are copied compressed, never decompressed.
func TestLimitLazyCopy(t *testing.T) {
	big := bytes.Repeat([]byte("0123456789abcdef"), 1<<16) // 1 MiB
	src := testdeck.Minimal(t)
	p := openLimits(t, src, Limits{})
	if _, err := p.AddPart("/ppt/media/big.bin", big, ""); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	p.Close()

	q := openLimits(t, src, Limits{MaxPartSize: 512 << 10})
	wantLimit(t, readPart(t, q, "/ppt/media/big.bin"), "more than 524288 bytes")
	setOff(t, q, slide1, "12345")
	dst := filepath.Join(t.TempDir(), "out.pptx")
	if err := q.SaveAs(dst); err != nil {
		t.Fatal(err)
	}
	r := openLimits(t, dst, Limits{})
	pt, err := r.Part("/ppt/media/big.bin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := pt.Bytes()
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("copied member differs (%d bytes, %v)", len(got), err)
	}
}
