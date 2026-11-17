package read

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDocumentMF6NonZip checks the MF6 row: Document of a non-zip input
// is an error whose text contains "vsdx:" exactly once.
func TestDocumentMF6NonZip(t *testing.T) {
	for _, in := range [][]byte{[]byte("not a zip"), {}} {
		sum, err := Document(in, "B")
		if err == nil || sum != nil {
			t.Fatalf("Document(%q) = %v, %v, want nil, an error", in, sum, err)
		}
		if got := strings.Count(err.Error(), "vsdx:"); got != 1 {
			t.Errorf("Document(%q) error = %q, want \"vsdx:\" exactly once", in, err)
		}
	}
}

// TestDocumentMF6RecursionCycle checks the MF6 row: Document of
// libvisio's recursion-cycle.vsdx (skipped without VSDX_CORPUS,
// failed when VSDX_CORPUS is set but the file is missing) is an
// error or notes naming the cycle's part (/visio/document.xml) once,
// with no repeated "vsdx:" prefix.
func TestDocumentMF6RecursionCycle(t *testing.T) {
	dir := os.Getenv("VSDX_CORPUS")
	if dir == "" {
		t.Skip("VSDX_CORPUS is not set")
	}
	rel := filepath.Join("libvisio", "src", "test", "data", "recursion-cycle.vsdx")
	var path string
	for _, cand := range []string{
		filepath.Join(dir, "repos", rel),
		filepath.Join(dir, "corpus", "vsdx", "repos", rel),
	} {
		if _, err := os.Stat(cand); err == nil {
			path = cand
			break
		}
	}
	if path == "" {
		t.Fatalf("corpus file %s not found under VSDX_CORPUS %s", filepath.Join(dir, "repos", rel), dir)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: corpus path from VSDX_CORPUS, never from a document
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum, err := Document(data, "B")
	if err != nil {
		if got := strings.Count(err.Error(), "vsdx:"); got != 1 {
			t.Errorf("error = %q, want \"vsdx:\" exactly once", err)
		}
		if got := strings.Count(err.Error(), "/visio/document.xml"); got != 1 {
			t.Errorf("error = %q, want /visio/document.xml exactly once", err)
		}
		return
	}
	texts := []string{}
	for _, n := range sum.Notes {
		texts = append(texts, n.At, n.Reason)
	}
	joined := strings.Join(texts, " ")
	if got := strings.Count(joined, "vsdx:"); got > 1 {
		t.Errorf("notes = %q, want at most one \"vsdx:\"", joined)
	}
	if strings.Count(joined, "/visio/document.xml") != 1 {
		t.Errorf("notes = %q, want /visio/document.xml exactly once", joined)
	}
}
