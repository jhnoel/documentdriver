package main

import (
	"io"

	"github.com/jhnoel/documentdriver/internal/cli"
)

var readCommand = &cli.Command{
	Name:    "read",
	Args:    []string{"drawing.vsdx"},
	JSON:    true,
	Summary: "list the pages and masters, or show one page's objects",
	Flags: []cli.Flag{
		{Name: "page", Value: "pageN", Help: "show this page's objects (page0: the page's stored ID)"},
		{Name: "master", Value: "masterN", Help: "show this master's objects (read only)"},
		{Name: "detail", Value: "sites|full", Enum: []string{"sites", "full"}, Help: "add connection sites, or origins and stored cells"},
		{Name: "ids", Value: "ids", Help: "only these objects and their descendants (page0/s12,page0/s13)"},
		{Name: "region", Value: "x,y,w,h", Help: "only objects whose box intersects this one"},
		{Name: "units", Value: "pt|in", Enum: []string{"pt", "in"}, Help: "units of lengths (default pt)"},
	},
	Help: `Without --page or --master, lists the pages in stored order (ID, name,
background, size, orientation, object count) and the masters. With
--page, prints the page's objects in paint order (first is bottom) with
their effective values after master and style inheritance, the glue
between them (connections), its layers and hazards. Lengths are points
on a page whose origin is the top-left corner, y down, unless --units
in. Every result starts with base, the file's hash, and has notes.`,
}

// runRead is the read handler (chunk C1): the file read once, cli.Hash
// for base, read.Document or read.Page; --page and --master exclusive
// (usage); the per-page flags only with one of them (usage, as pptx);
// --region parsed as pptx parses it; errors mapped as pptx's openDeck
// and readSlide map them.
func runRead(p *cli.Parsed, stdout io.Writer) error {
	_, _ = p, stdout
	return cli.Errorf(cli.CodeOperationFailed, "", "vsdx read is not implemented yet")
}
