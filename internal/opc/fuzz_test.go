package opc

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// fuzzLimits keep one fuzz input from using much memory; they are what
// the fuzz target checks is enforced.
var fuzzLimits = Limits{MaxPartSize: 1 << 20, MaxTotalSize: 4 << 20, MaxEntries: 1000}

// FuzzOpen opens arbitrary bytes as a package and reads everything a
// command might: every part's bytes, every XML part's tree, the content
// types, and every part's relationships with their targets. None of it may
// panic or hang, and no part may decompress past the limits.
func FuzzOpen(f *testing.F) {
	for _, path := range []string{testdeck.Minimal(f), testdeck.Quirky(f)} {
		b, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	// One file per worker process, rewritten for each input (inputs run
	// one at a time in a process); a directory per input is slow.
	path := filepath.Join(f.TempDir(), "fuzz.pptx")
	f.Fuzz(func(t *testing.T, b []byte) {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		p, err := OpenWithLimits(path, fuzzLimits)
		if err != nil {
			return
		}
		defer p.Close()
		ct, err := p.ContentTypes()
		if err == nil {
			for _, name := range p.Names() {
				ct.Lookup(name)
			}
		}
		var total int
		for _, name := range p.Names() {
			pt, err := p.Part(name)
			if err != nil {
				t.Fatalf("listed part %s: %v", name, err)
			}
			orig, err := pt.Original()
			if err != nil {
				continue
			}
			if int64(len(orig)) > fuzzLimits.MaxPartSize {
				t.Fatalf("%s: read %d bytes past the part limit", name, len(orig))
			}
			total += len(orig)
			if p.IsXML(name) {
				if _, err := pt.Tree(); err != nil {
					continue
				}
			}
			if strings.HasSuffix(name, ".rels") {
				continue
			}
			rels, err := p.Rels(name)
			if err != nil {
				continue
			}
			for _, r := range rels.All() {
				if _, err := rels.TargetPart(r); err != nil {
					continue // an unresolvable target is a valid outcome; only panics and limit breaches fail
				}
			}
		}
		if int64(total) > fuzzLimits.MaxTotalSize {
			t.Fatalf("read %d bytes past the total limit", total)
		}
	})
}

// TestFuzzLimitsHold runs the fuzz target's limits on a member that
// decompresses past them, so the target's checks are known to bite.
func TestFuzzLimitsHold(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("big.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("<a/>"), 1<<19)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "big.pptx")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	p := openLimits(t, path, fuzzLimits)
	if err := readPart(t, p, "big.xml"); !errors.Is(err, ErrLimit) {
		t.Fatalf("err = %v, want ErrLimit", err)
	}
}
