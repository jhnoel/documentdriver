package main

import (
	"io"
	"os"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
)

// kinds are the --kind values of the draft contract; A1 implements only
// workbook and refuses the others as not yet supported (A2).
var kinds = []string{"workbook", "names", "tables", "styles", "comments"}

var readCommand = &cli.Command{
	Name:    "read",
	Args:    []string{"file.xlsx"},
	JSON:    true,
	Summary: "show the workbook, or the cells of a sheet or range",
	Flags: []cli.Flag{
		{Name: "sheet", Value: "sheet:Name", Help: "read a sheet's used range"},
		{Name: "range", Value: "address", Help: "read a range: Budget!A1:D10, Budget!5:7, Budget!C:E, name:X, table:T, table:T/column:C"},
		{Name: "view", Value: "view", Enum: model.Views, Help: "cells (default): values and formulas; values: stored values only; formulas: formulas only"},
		{Name: "group", Help: "with --view formulas: list each shared formula filling a rectangle once, in groups (cells are then not in row order)"},
		{Name: "limit", Value: "n", Help: "at most n cells, cut at a whole row (default 10000; 0 is no limit)"},
		{Name: "kind", Value: "kind", Enum: kinds, Help: "what to list (default workbook; names, tables, styles and comments are not yet supported)"},
		{Name: "detail", Value: "detail", Enum: []string{"styles"}, Help: "add cell formats (not yet supported)"},
	},
	Help: `Prints JSON. With neither --sheet nor --range: the workbook summary,
sheets (id, name, state, stored dimension, tables), defined names, tables
and notes. With --sheet sheet:Budget (its used range) or --range: the
cells, keyed by address ("B2": 1200), with the range read, the sheet's
used range, merges, and hidden rows and columns ("Budget!5:7",
"Budget!C:C"). A row of height zero is not hidden; only rows and columns
marked hidden are listed.

--view cells (the default) shows a formula cell as {"f":"=B2+C2","v":2400}:
v is the value the file stored, left out when it stored none. --view
values shows the stored values only, --view formulas the formulas only.
With --view formulas, --group lists each shared formula that fills a
rectangle once, in groups ({"at":"Budget!D2:D100","f":"=B2+C2"}, f
written for the top-left cell), and leaves its cells out; the other
shared-formula cells then come after the rest, so under --group cells
are not in row-major order.
Formulas are shown as Excel shows them (A1#, @, Table[@Col], no _xlfn.);
a cell that shares a formula shows its own formula written out. A cell
filled by a dynamic-array formula is {"v":3,"spill":"Budget!A1"}, naming
the formula's cell; the cells of a legacy array formula (entered with
Ctrl+Shift+Enter), its own cell included, also have "array":"cse", and
those of a data table "array":"data_table".

At most --limit cells (default 10000; 0 is no limit), cut at a whole
row: a cut read has truncated.next, the range to read next. values_stale
says the workbook asks Excel to recalculate when it opens, so stored
values may be out of date; read never calculates. ` + "`xlsx help values`" + ` lists
the value forms. Every result starts with base, the file's hash, and has
notes: what could not be read or shown. Read never writes.`,
}

// runRead is the read handler (chunk 7).
//
//   - The input is read once (os.ReadFile; an error is cli.CodeIO), and
//     passed to internal/xlsx/read.
//   - --limit: absent is read.DefaultLimit; a decimal integer >= 0
//     (0 is no limit); anything else is usage, path --limit.
//   - --kind is checked first. Other than workbook: invalid_operation
//     "not yet supported" (path --kind), listing the kinds A1 has
//     (workbook), whatever else is given. --kind workbook with --sheet or
//     --range: usage, path --kind.
//   - --detail: invalid_operation "not yet supported", path --detail.
//   - --group without --view formulas: usage, path --group. --view or
//     --limit without --sheet or --range: usage, path --view or --limit
//     ("goes with --sheet or --range").
//   - Neither --sheet nor --range: read.Workbook. Otherwise read.Cells
//     with model.ParseView of --view (default cells).
//   - The result is encoded with cli.Marshal and printed with
//     cli.WriteJSON (honouring --pretty); a write error is cli.CodeIO.
func runRead(p *cli.Parsed, stdout io.Writer) error {
	// --kind is checked first: a kind other than workbook is
	// invalid_operation whatever else is given.
	if p.Has("kind") && p.Get("kind") != "workbook" {
		return cli.Errorf(cli.CodeInvalidOperation, "--kind", "not yet supported: --kind %s (A1 implements workbook)", p.Get("kind"))
	}
	if p.Has("kind") && (p.Has("sheet") || p.Has("range")) {
		return cli.Errorf(cli.CodeUsage, "--kind", "--kind workbook goes with neither --sheet nor --range")
	}
	if p.Has("detail") {
		return cli.Errorf(cli.CodeInvalidOperation, "--detail", "not yet supported: --detail %s", p.Get("detail"))
	}
	if p.Has("group") && !(p.Has("view") && p.Get("view") == "formulas") {
		return cli.Errorf(cli.CodeUsage, "--group", "--group goes with --view formulas")
	}
	if !p.Has("sheet") && !p.Has("range") {
		if p.Has("view") {
			return cli.Errorf(cli.CodeUsage, "--view", "--view goes with --sheet or --range")
		}
		if p.Has("limit") {
			return cli.Errorf(cli.CodeUsage, "--limit", "--limit goes with --sheet or --range")
		}
	}
	limit, err := parseLimit(p)
	if err != nil {
		return err
	}
	input, err := os.ReadFile(p.Args[0]) //nolint:gosec // G304: the workbook is the file the caller named
	if err != nil {
		return cli.Errorf(cli.CodeIO, "", "read the workbook: %v", err)
	}
	if !p.Has("sheet") && !p.Has("range") {
		res, err := read.Workbook(input, read.Options{})
		if err != nil {
			return err
		}
		b, err := cli.Marshal(res)
		if err != nil {
			return err
		}
		return cli.WriteJSON(stdout, b, p.Has("pretty"))
	}
	view := model.ViewCells
	if p.Has("view") {
		view, _ = model.ParseView(p.Get("view"))
	}
	res, err := read.Cells(input, read.CellsRequest{
		Range: p.Get("range"),
		Sheet: p.Get("sheet"),
		View:  view,
		Limit: limit,
		Group: p.Has("group"),
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

// parseLimit reads --limit: read.DefaultLimit when absent; a usage error
// (path --limit) unless it is a decimal integer from 0 to 1<<31-1 without
// sign or spaces.
func parseLimit(p *cli.Parsed) (int, error) {
	if !p.Has("limit") {
		return read.DefaultLimit, nil
	}
	s := p.Get("limit")
	const maxLimit = 1<<31 - 1
	if s == "" {
		return 0, cli.Errorf(cli.CodeUsage, "--limit", "--limit must be a decimal integer from 0 to %d", maxLimit)
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, cli.Errorf(cli.CodeUsage, "--limit", "--limit must be a decimal integer from 0 to %d", maxLimit)
		}
		d := int(c - '0')
		if n > (maxLimit-d)/10 {
			return 0, cli.Errorf(cli.CodeUsage, "--limit", "--limit must be a decimal integer from 0 to %d", maxLimit)
		}
		n = n*10 + d
	}
	return n, nil
}
