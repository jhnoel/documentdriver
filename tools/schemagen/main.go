// Command schemagen generates the docx plan's JSON Schema and operation
// reference from the operation table in internal/docx/ops:
//
//	docs/docx-plan.schema.json   the plan JSON Schema (draft-07), which
//	                             package docs embeds for docx help schema
//	docs/docx-plan.md            the operation reference, rules, change-mode
//	                             classes and intended differences
//
// Run it from the repository root after changing internal/docx/ops. With
// -check it writes nothing and fails if the committed files are stale
// (its test runs that check too).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// outputs renders the generated files, by repository-relative path.
func outputs() map[string][]byte {
	var b strings.Builder
	writePretty(&b, planSchema(), "")
	b.WriteByte('\n')
	return map[string][]byte{
		"docs/docx-plan.schema.json": []byte(b.String()),
		"docs/docx-plan.md":          []byte(reference()),
	}
}

// main writes generated docs or checks them when -check is set.
func main() {
	check := flag.Bool("check", false, "fail if the committed files differ from the generated ones; write nothing")
	flag.Parse()
	root, err := repo.Root()
	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
	stale := false
	for name, want := range outputs() {
		path := filepath.Join(root, name)
		if *check {
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				fmt.Fprintf(os.Stderr, "schemagen: %s is stale; run go run ./tools/schemagen\n", name)
				stale = true
			}
			continue
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "schemagen:", err)
			os.Exit(1)
		}
	}
	if stale {
		os.Exit(1)
	}
}
