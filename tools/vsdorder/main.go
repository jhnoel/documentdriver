// Command vsdorder generates the Visio child-order table of
// internal/ooxml/schema (v_table.go) and its provenance record
// (v_provenance.json) from the MS-VSDX XSD and the Visio-authored
// corpus files. The XSD is read from the gitignored schema cache
// (default: corpus/vsdx-schema of the main checkout, or $VSDX_SCHEMA)
// and is never committed; only element names, their order, counts,
// file names and the XSD's SHA-256 are.
//
//	vsdorder [-xsd path] [-corpus dir] [-check] [-out dir]
//
// The corpus directory holds the corpus layout of
// eval/corpus/fetch-vsdx.sh (files under repos/<source>/), the
// vsdx-corpus worktree root (corpus/vsdx/repos), or a repos/
// directory directly; -corpus, $VSDX_CORPUS and the default
// (corpus/vsdx of the main checkout) are resolved the same way. With
// -check the generator writes nothing and fails on drift from the
// committed files. Without -out (default: internal/ooxml/schema of
// the repository root) the output directory is replaced; tests
// generate in memory instead.
package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// tableFile and provenanceFile are the generated files, relative to
// the repository root.
const (
	tableFile      = "internal/ooxml/schema/v_table.go"
	provenanceFile = "internal/ooxml/schema/v_provenance.json"
)

// main runs vsdorder, exiting 1 on error.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vsdorder:", err)
		os.Exit(1)
	}
}

// run parses the flags and generates or checks the table.
func run() error {
	xsdPath := flag.String("xsd", "", "path to the MS-VSDX visio.xsd (default: $VSDX_SCHEMA/visio.xsd or the schema cache)")
	corpusDir := flag.String("corpus", "", "corpus directory (default: $VSDX_CORPUS or the vsdx corpus of the main checkout)")
	check := flag.Bool("check", false, "fail if the committed files differ from the generated ones; write nothing")
	outDir := flag.String("out", "", "output directory (default: internal/ooxml/schema of the repository root)")
	flag.Parse()
	root, err := repo.Root()
	if err != nil {
		return err
	}
	out := *outDir
	if out == "" {
		out = filepath.Join(root, "internal", "ooxml", "schema")
	} else if !filepath.IsAbs(out) {
		out = filepath.Join(root, filepath.FromSlash(out))
	}
	table, prov, err := generateFromPaths(*xsdPath, *corpusDir)
	if err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{{filepath.Join(out, "v_table.go"), table}, {filepath.Join(out, "v_provenance.json"), prov}} {
		if *check {
			got, err := os.ReadFile(f.name)
			if err != nil || !bytes.Equal(got, f.data) {
				return fmt.Errorf("%s is stale: run go run ./tools/vsdorder", f.name)
			}
			continue
		}
		if err := os.WriteFile(f.name, f.data, 0o644); err != nil {
			return fmt.Errorf("vsdorder: write %s: %w", f.name, err)
		}
	}
	return nil
}

// generateFromPaths resolves the default XSD and corpus paths and
// generates the table and provenance in memory.
func generateFromPaths(xsdFlag, corpusFlag string) ([]byte, []byte, error) {
	xsdPath, err := resolveXSD(xsdFlag)
	if err != nil {
		return nil, nil, err
	}
	corpusRoot, err := resolveCorpus(corpusFlag)
	if err != nil {
		return nil, nil, err
	}
	files, err := listCorpusFiles(corpusRoot)
	if err != nil {
		return nil, nil, err
	}
	var paths, ids []string
	for _, rel := range files {
		paths = append(paths, filepath.Join(corpusRoot, filepath.FromSlash(rel)))
		ids = append(ids, rel)
	}
	return generate(xsdPath, paths, ids)
}

// generate reads the schema at xsdPath, scans the corpus files at
// paths (identified for the provenance by ids, in order), and
// returns the table and provenance bytes.
func generate(xsdPath string, paths, ids []string) ([]byte, []byte, error) {
	src, err := os.ReadFile(xsdPath)
	if err != nil {
		return nil, nil, fmt.Errorf("vsdorder: read schema %s: %w", xsdPath, err)
	}
	sum := sha256.Sum256(src)
	x, err := parseXSD(src, fmt.Sprintf("%x", sum))
	if err != nil {
		return nil, nil, err
	}
	st := newCorpusStats()
	var accepted []string
	for i, p := range paths {
		ok, err := scanCorpusFile(p, ids[i], st)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			accepted = append(accepted, ids[i])
		}
	}
	res, err := refine(x, st, slicesClone(ids), accepted)
	if err != nil {
		return nil, nil, err
	}
	table, err := renderTable(res)
	if err != nil {
		return nil, nil, err
	}
	prov, err := renderProvenance(res)
	if err != nil {
		return nil, nil, err
	}
	return table, prov, nil
}

// slicesClone returns a copy of ss.
func slicesClone(ss []string) []string {
	out := make([]string, len(ss))
	copy(out, ss)
	return out
}

// resolveXSD returns the path to the XSD: the flag, else
// $VSDX_SCHEMA/visio.xsd (or the path itself when it ends in .xsd),
// else the schema cache of the main checkout.
func resolveXSD(flagPath string) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	if env := os.Getenv("VSDX_SCHEMA"); env != "" {
		if strings.HasSuffix(env, ".xsd") {
			return env, nil
		}
		return filepath.Join(env, "visio.xsd"), nil
	}
	corpus, err := repo.Corpus()
	if err != nil {
		return "", err
	}
	return filepath.Join(corpus, "vsdx-schema", "visio.xsd"), nil
}

// resolveCorpus returns the directory holding the corpus drawing
// files, resolving the flag, $VSDX_CORPUS and the default layout
// (a vsdx/ directory holding repos/<source>/, a worktree root
// holding corpus/vsdx/repos, or a repos/ directory directly).
func resolveCorpus(flagDir string) (string, error) {
	dir := flagDir
	if dir == "" {
		dir = os.Getenv("VSDX_CORPUS")
	}
	if dir == "" {
		corpus, err := repo.Corpus()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(corpus, "vsdx")
	}
	for _, cand := range []string{
		filepath.Join(dir, "repos"),
		filepath.Join(dir, "corpus", "vsdx", "repos"),
		dir,
	} {
		if files, err := listCorpusFilesSafe(cand); err == nil && len(files) > 0 {
			return cand, nil
		}
	}
	return dir, fmt.Errorf("vsdorder: no Visio corpus files under %s (set -corpus or VSDX_CORPUS)", dir)
}

// listCorpusFilesSafe is listCorpusFiles that reports a missing
// directory as empty rather than an error.
func listCorpusFilesSafe(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	return listCorpusFiles(root)
}
