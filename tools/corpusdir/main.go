// Command corpusdir prints the corpus directory (see repo.Corpus), so the
// Makefile and scripts find it from a linked worktree too.
package main

import (
	"fmt"
	"os"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// main prints the corpus directory, exiting 1 if it cannot be found.
func main() {
	dir, err := repo.Corpus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "corpusdir:", err)
		os.Exit(1)
	}
	fmt.Println(dir)
}
