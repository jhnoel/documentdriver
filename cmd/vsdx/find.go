package main

import (
	"io"

	"github.com/jhnoel/documentdriver/internal/cli"
)

var findCommand = &cli.Command{
	Name:    "find",
	Args:    []string{"drawing.vsdx", "query"},
	JSON:    true,
	Summary: "list the shapes and shape data whose text contains the query",
	Flags: []cli.Flag{
		{Name: "ignore-case", Help: "match letters of either case (simple Unicode case folding)"},
		{Name: "page", Value: "pageN", Help: "search one page (default every page, in stored order)"},
		{Name: "limit", Value: "n", Help: "at most n matches (default 10000; 0 is no limit)"},
	},
	Help: `Prints {"base", "matches", "notes"}: one entry per shape whose text, or
shape data row whose value, contains the query, as {"at", "text"}. A
shape that shows its master's text matches on it. A cut search has
truncated.after, the last match returned.`,
}

// runFind is the find handler (chunk C1): --limit as xlsx parses it,
// read.Find, the result through cli.Marshal.
func runFind(p *cli.Parsed, stdout io.Writer) error {
	_, _ = p, stdout
	return cli.Errorf(cli.CodeOperationFailed, "", "vsdx find is not implemented yet")
}
