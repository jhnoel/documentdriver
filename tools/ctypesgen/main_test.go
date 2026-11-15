package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/repo"
)

func readRepo(t *testing.T, rel string) []byte {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadCommitted(t *testing.T) *Provenance {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	p, err := loadProvenance(filepath.Join(root, filepath.FromSlash(provenanceFile)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTableDrift fails when the committed Go table or the JSON the Python
// integrity check reads no longer render from provenance.json.
func TestTableDrift(t *testing.T) {
	p := loadCommitted(t)
	goSrc, err := RenderGo(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(goSrc, readRepo(t, goFile)) {
		t.Errorf("%s is stale: run go generate ./internal/ooxml/ctypes", goFile)
	}
	jsonSrc, err := RenderJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(jsonSrc, readRepo(t, jsonFile)) {
		t.Errorf("%s is stale: run go generate ./internal/ooxml/ctypes", jsonFile)
	}
}

// TestProvenanceComplete requires every entry to name its source file and
// digest, so the table can be checked against the files it came from.
func TestProvenanceComplete(t *testing.T) {
	p := loadCommitted(t)
	check := func(kind, v string, s Source, files int) {
		if s.Label == "" || s.Path == "" || len(s.SHA256) != 64 {
			t.Errorf("%s %s: incomplete source %+v", kind, v, s)
		}
		if strings.HasPrefix(s.Path, "/") || strings.Contains(s.Path, "..") {
			t.Errorf("%s %s: source path %q is not corpus-relative", kind, v, s.Path)
		}
		if files < p.MinFiles {
			t.Errorf("%s %s: %d files, below the minimum %d", kind, v, files, p.MinFiles)
		}
	}
	for _, e := range p.ContentTypes {
		check("content type", e.Value, e.Source, e.Files)
	}
	for _, e := range p.RelTypes {
		check("relationship type", e.Value, e.Source, e.Files)
	}
	for _, r := range p.RelTargets {
		check("relationship target", r.Rel+" -> "+r.ContentType, r.Source, r.Files)
	}
}

// TestProvenanceMatchesCorpus re-extracts from the corpora named in
// CTYPES_SRC (label=dir pairs separated by ';') and requires the committed
// provenance.json to match. It skips without the corpora, which are not in
// git.
func TestProvenanceMatchesCorpus(t *testing.T) {
	env := os.Getenv("CTYPES_SRC")
	if env == "" {
		t.Skip("CTYPES_SRC not set (label=dir;label=dir)")
	}
	var roots srcFlags
	for _, kv := range strings.Split(env, ";") {
		if err := roots.Set(kv); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Extract(roots, loadCommitted(t).MinFiles)
	if err != nil {
		t.Fatal(err)
	}
	b, err := marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, readRepo(t, provenanceFile)) {
		t.Error("provenance.json does not match the corpus: re-run ctypesgen -extract")
	}
}

// TestProvenanceCanonical requires provenance.json to be exactly what
// -extract writes for its records, so a field added to Entry (Excel)
// is present in the committed file.
func TestProvenanceCanonical(t *testing.T) {
	b, err := marshal(loadCommitted(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, readRepo(t, provenanceFile)) {
		t.Errorf("%s is not in the form -extract writes", provenanceFile)
	}
}
