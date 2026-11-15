package main

import (
	"io"
	"os"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
)

var findCommand = &cli.Command{
	Name:    "find",
	Args:    []string{"file.xlsx", "query"},
	JSON:    true,
	Summary: "list the cells whose value or formula contains the query",
	Flags: []cli.Flag{
		{Name: "ignore-case", Help: "match letters of either case (simple Unicode case folding)"},
		{Name: "sheet", Value: "sheet:Name", Help: "search one sheet (default every worksheet, in tab order)"},
		{Name: "view", Value: "view", Enum: []string{"values", "formulas"}, Help: "search the values (default) or the formulas"},
		{Name: "limit", Value: "n", Help: "at most n matches (default 10000; 0 is no limit)"},
	},
	Help: `Prints {"base", "matches", "notes"}: one entry per cell whose value
(--view values, the default) or formula (--view formulas) contains the
query, as {"cell":"Budget!B2","value":...}, the value as read shows it
(a formula as "=..."). Values match as read prints them: numbers in their
shortest form (1200, 0.5), dates and times as their ISO text
(2024-03-01), booleans as true and false, errors as #N/A. The empty
query matches every cell with a value (or formula). At most --limit
matches (default 10000; 0 is no limit); a cut search has
truncated.after, the last cell matched.`,
}

// runFind is the find handler (chunk 7): the input read once; --limit as
// parseLimit; read.Find with --view values (default) or formulas; the
// result printed as runRead prints.
func runFind(p *cli.Parsed, stdout io.Writer) error {
	limit, err := parseLimit(p)
	if err != nil {
		return err
	}
	input, err := os.ReadFile(p.Args[0]) //nolint:gosec // G304: the workbook is the file the caller named
	if err != nil {
		return cli.Errorf(cli.CodeIO, "", "read the workbook: %v", err)
	}
	view := model.ViewValues
	if p.Has("view") {
		view, _ = model.ParseView(p.Get("view"))
	}
	res, err := read.Find(input, read.FindRequest{
		Query:      p.Args[1],
		IgnoreCase: p.Has("ignore-case"),
		Sheet:      p.Get("sheet"),
		View:       view,
		Limit:      limit,
	}, read.Options{})
	if err != nil {
		return err
	}
	b, err := cli.Marshal(res)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, b, p.Has("pretty"))
}
