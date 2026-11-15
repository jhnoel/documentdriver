package tree

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCorpusRoundTrip parses every XML member of every .pptx under
// $PPTX_CORPUS and requires a byte-identical serialisation, both untouched
// and with every start tag rebuilt from its parts. Parts that fail to parse
// are logged and counted, not failed: real decks contain parts that are not
// XML at all, and those are never edited.
func TestCorpusRoundTrip(t *testing.T) {
	dir := os.Getenv("PPTX_CORPUS")
	if dir == "" {
		t.Skip("PPTX_CORPUS not set")
	}
	var files []string
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".pptx") {
			files = append(files, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var nFiles, nParts, nBytes int
	failures := map[string]int{}
	for _, f := range files {
		zr, err := zip.OpenReader(f)
		if err != nil {
			t.Logf("skip %s: %v", f, err)
			failures["unreadable zip"]++
			continue
		}
		nFiles++
		for _, zf := range zr.File {
			ext := strings.ToLower(filepath.Ext(zf.Name))
			if ext != ".xml" && ext != ".rels" {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				t.Logf("%s: %s: %v", f, zf.Name, err)
				failures["unreadable member"]++
				continue
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Logf("%s: %s: %v", f, zf.Name, err)
				failures["unreadable member"]++
				continue
			}
			nParts++
			nBytes += len(b)
			doc, err := Parse(b)
			if err != nil {
				// Not well formed (or not XML at all): such a part can never be
				// edited, and the package layer copies it verbatim.
				t.Logf("%s: %s: %v", f, zf.Name, err)
				failures["not well formed"]++
				continue
			}
			if !bytes.Equal(doc.Bytes(), b) {
				t.Errorf("%s: %s: untouched serialisation differs", f, zf.Name)
				failures["untouched differs"]++
				continue
			}
			rebuildAll(doc)
			if got := doc.Bytes(); !bytes.Equal(got, b) {
				t.Errorf("%s: %s: rebuilt serialisation differs at byte %d", f, zf.Name, firstDiff(got, b))
				failures["rebuilt differs"]++
			}
		}
		zr.Close()
	}
	t.Logf("corpus: %d files, %d XML parts, %d bytes; failures: %v", nFiles, nParts, nBytes, failures)
}

func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
