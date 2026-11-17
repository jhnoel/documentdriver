package main

import (
	"io"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
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

// parseLimit reads --limit: read.DefaultLimit when absent; a usage error
// (path --limit) unless it is a decimal integer from 0 to 1<<31-1 without
// sign or spaces, as xlsx parses it (through ascii.Digits and strconv,
// not a copy of xlsx's loop).
func parseLimit(p *cli.Parsed) (int, error) {
	if !p.Has("limit") {
		return read.DefaultLimit, nil
	}
	const maxLimit = 1<<31 - 1
	s := p.Get("limit")
	if !ascii.Digits(s) {
		return 0, cli.Errorf(cli.CodeUsage, "--limit", "--limit must be a decimal integer from 0 to %d", maxLimit)
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n > maxLimit {
		return 0, cli.Errorf(cli.CodeUsage, "--limit", "--limit must be a decimal integer from 0 to %d", maxLimit)
	}
	return int(n), nil
}

// runFind is the find handler (chunk C1): --limit as xlsx parses it,
// read.Find, the result through cli.Marshal.
func runFind(p *cli.Parsed, stdout io.Writer) error {
	limit, err := parseLimit(p)
	if err != nil {
		return err
	}
	input, err := readInput(p.Args[0])
	if err != nil {
		return err
	}
	res, err := read.Find(input, cli.Hash(input), read.FindRequest{
		Query:      p.Args[1],
		IgnoreCase: p.Has("ignore-case"),
		Page:       p.Get("page"),
		Limit:      limit,
	})
	if err != nil {
		return cli.Wrap(cli.CodeInvalidDocument, "", err)
	}
	out, err := cli.Marshal(res)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, out, p.Has("pretty"))
}
