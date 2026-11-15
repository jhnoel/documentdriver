// Command docxintegrity prints the integrity findings of .docx files, one
// line per error kind, in the format docxdriver's examples/integrity.rs
// prints (path, part, message and count, tab-separated), so the two
// outputs compare with diff:
//
//	go run ./tools/docxintegrity FILE...
//
// It exits 1 when any file has a finding and 2 when a file cannot be read.
// docxdriver's --round-trip flag is not ported: the Go engine needs no
// round-trip check (see integrity.Gate).
package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/jhnoel/documentdriver/internal/docx/integrity"
)

// main prints integrity findings and exits with run's status.
func main() {
	os.Exit(run(os.Args[1:]))
}

// run prints one line per finding kind for each path and returns the exit status.
func run(paths []string) int {
	out := bufio.NewWriter(os.Stdout)
	status := 0
	for _, path := range paths {
		data, err := os.ReadFile(path) //nolint:gosec // G304: the paths are the caller's arguments
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			status = 2
			continue
		}
		f := integrity.Check(data)
		for _, k := range f.Kinds() {
			fmt.Fprintf(out, "%s\t%s\t%s\t%d\n", path, k.Part, k.Message, f.Count(k))
		}
		if !f.Empty() {
			status = max(status, 1)
		}
	}
	if err := out.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "docxintegrity:", err)
		return 2
	}
	return status
}
