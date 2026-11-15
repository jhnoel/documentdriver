// Command ctypesgen builds the content-type and relationship-type table of
// internal/ooxml/ctypes from Office-authored files.
//
// Two stages keep the table checkable without the (gitignored) corpus:
//
//	ctypesgen -extract -src label=dir ...   corpus -> provenance.json
//	ctypesgen                               provenance.json -> table_gen.go, tools/conformance/ctypes.json
//
// provenance.json names, for every entry, the corpus file it was first seen
// in (label-relative path and SHA-256) and how many Word, PowerPoint and
// Excel files use it. -check renders in memory and fails on drift from the
// committed outputs; -verify re-extracts from the corpus and fails if
// provenance.json no longer matches it. Only files whose docProps/app.xml
// names Word, PowerPoint or Excel count (officeApps).
//
// The committed provenance.json was extracted with these roots (labels are
// what the file records; the directories are the maintainer's checkouts):
//
//	-src pptx-repos=<documentdriver>/corpus/repos   (PowerPoint decks from public repos)
//	-src pptx-web=<documentdriver>/corpus/web       (PowerPoint decks from the web)
//	-src docx-lo=<docxdriver>/corpus/lo             (the LibreOffice test-doc corpus, Word-authored files)
//	-src docx-test-docs=<docxdriver>/test-docs
//	-src xlsx-repos=<documentdriver>/corpus/xlsx/repos (Excel workbooks from public repos, eval/corpus/fetch-xlsx.sh)
//
// where docxdriver was at git 1ffb0d7afed108a36a25c4a95b489d0fbf7a58bb.
// corpus/renders holds this tool's own outputs and is not scanned.
//
// The Excel names in officeApps are the Application values of the xlsx
// corpus's Excel-written files (accepted by the 2026-11-14 extraction:
// Microsoft Excel 1,106 files, Microsoft Macintosh Excel 70). Microsoft
// Excel Online (about 54 files) is left out, as the web editions of Word and PowerPoint are: desktop Office is
// the authority for what Office writes.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhnoel/documentdriver/internal/repo"
)

const (
	provenanceFile = "internal/ooxml/ctypes/provenance.json"
	goFile         = "internal/ooxml/ctypes/table_gen.go"
	jsonFile       = "tools/conformance/ctypes.json"
)

type srcFlags []Root

// String returns the flag value's string form, always empty.
func (s *srcFlags) String() string { return "" }

// Set appends a label=dir root parsed from v, erroring on malformed input.
func (s *srcFlags) Set(v string) error {
	label, dir, ok := strings.Cut(v, "=")
	if !ok || label == "" || dir == "" {
		return errors.New("want label=dir")
	}
	*s = append(*s, Root{Label: label, Dir: dir})
	return nil
}

// main runs ctypesgen and exits 1 on error.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ctypesgen:", err)
		os.Exit(1)
	}
}

// run extracts or renders the content-type table according to flags, returning any error.
func run() error {
	var srcs srcFlags
	extract := flag.Bool("extract", false, "scan the -src corpora and write provenance.json")
	verify := flag.Bool("verify", false, "re-extract from the -src corpora and fail if provenance.json differs")
	check := flag.Bool("check", false, "fail if the generated files differ from provenance.json")
	// Two files, not one: the corpora hold files whose docProps/app.xml
	// names Word but which a test tool rewrote, and one such file
	// (docx-lo/ooxmlexport/tdf165642_glossaryFootnote.docx) would admit
	// application/xml as the content type of settings, footnotes, endnotes
	// and glossary parts. Three, the earlier rule, dropped types Word writes
	// in few files, such as commentsExtensible (two files).
	minFiles := flag.Int("min", 2, "keep entries used by at least this many Office-authored files (with -extract)")
	flag.Var(&srcs, "src", "label=dir of a corpus to scan (repeatable)")
	flag.Parse()
	root, err := repo.Root()
	if err != nil {
		return err
	}
	prov := filepath.Join(root, filepath.FromSlash(provenanceFile))
	switch {
	case *extract || *verify:
		if len(srcs) == 0 {
			return errors.New("-extract and -verify need at least one -src label=dir")
		}
		keep := *minFiles
		if *verify {
			old, err := loadProvenance(prov)
			if err != nil {
				return err
			}
			keep = old.MinFiles
		}
		p, err := Extract(srcs, keep)
		if err != nil {
			return err
		}
		b, err := marshal(p)
		if err != nil {
			return err
		}
		if *verify {
			old, err := os.ReadFile(prov) //nolint:gosec // G304: a fixed repo path
			if err != nil {
				return err
			}
			if !bytes.Equal(old, b) {
				return errors.New("provenance.json does not match the corpus: re-run -extract and review the diff")
			}
			fmt.Println("provenance.json matches the corpus")
			return nil
		}
		return os.WriteFile(prov, b, 0o644) //nolint:gosec // G306: a generated tracked file
	default:
		p, err := loadProvenance(prov)
		if err != nil {
			return err
		}
		goSrc, err := RenderGo(p)
		if err != nil {
			return err
		}
		jsonSrc, err := RenderJSON(p)
		if err != nil {
			return err
		}
		for _, f := range []struct {
			path string
			data []byte
		}{{goFile, goSrc}, {jsonFile, jsonSrc}} {
			full := filepath.Join(root, filepath.FromSlash(f.path))
			if *check {
				have, err := os.ReadFile(full) //nolint:gosec // G304: a fixed repo path
				if err != nil {
					return err
				}
				if !bytes.Equal(have, f.data) {
					return fmt.Errorf("%s is stale: run go generate ./internal/ooxml/ctypes", f.path)
				}
				continue
			}
			if err := os.WriteFile(full, f.data, 0o644); err != nil { //nolint:gosec // G306: a generated tracked file
				return err
			}
		}
	}
	return nil
}
