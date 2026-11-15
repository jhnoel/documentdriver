// Command coverage writes the coverage tracker under eval/coverage:
//
//	go run ./tools/coverage [--xsd zip] [--corpus dir] [--profile dir] [--out dir]
//
// universe.json is the enumerated universe, generated from the pinned
// ECMA-376 transitional schemas only when --xsd names the schema zip
// (OfficeOpenXML-XMLSchema-Transitional.zip from Apache POI at commit
// 1243387d; its SHA-256 is checked). corpus.json holds occurrence counts
// of every universe path in the PowerPoint-native decks of
// the corpus manifest (<corpus>/manifest.jsonl unless --manifest says
// otherwise), refreshed only when --corpus names the corpus directory
// (PPTX_CORPUS). coverage.json and coverage.md are
// pdraw-profile.json holds what pdraw's output uses inside objects (every
// universe path and enumerated value, with counts), refreshed only when
// --profile names the output directory of eval/parity/build.sh.
// coverage.json and coverage.md, with their "pdraw parity" section, are
// always rebuilt from those files, the registry of what each reader
// and codec honours (internal/pptx/coverage) and the operation matrix.
// internal/pptx/coverage's tests fail when the committed coverage.json is
// stale.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jhnoel/documentdriver/internal/pptx/coverage"
)

// main parses coverage flags and exits 1 if run fails.
func main() {
	xsd := flag.String("xsd", "", "schema zip to regenerate universe.json from ("+coverage.SchemaURL+")")
	corpus := flag.String("corpus", "", "corpus directory to refresh corpus.json from (PPTX_CORPUS)")
	out := flag.String("out", filepath.Join("eval", "coverage"), "output directory")
	profile := flag.String("profile", "", "pdraw reference set (the output directory of eval/parity/build.sh) to refresh "+coverage.ProfileFile+" from")
	manifest := flag.String("manifest", "", "corpus manifest (default <corpus>/manifest.jsonl)")
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*xsd, *corpus, *profile, *out, *manifest); err != nil {
		fmt.Fprintln(os.Stderr, "coverage:", err)
		os.Exit(1)
	}
}

// run regenerates the requested coverage files under out and returns any error.
func run(xsd, corpus, profile, out, manifest string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	uPath := filepath.Join(out, coverage.UniverseFile)
	if xsd != "" {
		b, err := os.ReadFile(xsd)
		if err != nil {
			return err
		}
		u, err := coverage.GenerateUniverse(b)
		if err != nil {
			return err
		}
		if err := write(uPath, u); err != nil {
			return err
		}
	}
	u, err := coverage.LoadUniverse(uPath)
	if err != nil {
		return fmt.Errorf("%w (generate it with --xsd)", err)
	}
	cPath := filepath.Join(out, coverage.CountsFile)
	if corpus != "" {
		if manifest == "" {
			manifest = filepath.Join(corpus, "manifest.jsonl")
		}
		decks, err := coverage.NativeDecks(manifest, corpus)
		if err != nil {
			return err
		}
		c, err := coverage.CountCorpus(u, decks, "PowerPoint-native decks of the corpus manifest")
		if err != nil {
			return err
		}
		if err := write(cPath, c); err != nil {
			return err
		}
	}
	pPath := filepath.Join(out, coverage.ProfileFile)
	if profile != "" {
		p, err := coverage.BuildProfile(u, profile)
		if err != nil {
			return err
		}
		if err := write(pPath, p); err != nil {
			return err
		}
	}
	prof, err := coverage.LoadProfile(pPath)
	if err != nil {
		return fmt.Errorf("%w (generate it with --profile)", err)
	}
	counts, err := coverage.LoadCounts(cPath)
	if err != nil {
		return err
	}
	rep, err := coverage.Build(u, counts)
	if err != nil {
		return err
	}
	rep.AddParity(u, prof)
	if err := write(filepath.Join(out, coverage.ReportFile), rep); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, coverage.MarkdownFile), rep.Markdown(), 0o644)
}

// write marshals v stably to path.
func write(path string, v any) error {
	b, err := coverage.MarshalStable(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
