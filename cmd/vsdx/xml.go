package main

import (
	"io"

	"github.com/jhnoel/documentdriver/internal/cli"
)

var xmlCommand = &cli.Command{
	Name:    "xml",
	Args:    []string{"drawing.vsdx", "address"},
	Summary: "print the source XML of one shape",
	Help: `Prints the exact source bytes of one shape, addressed as vsdx read shows
it (page0/s17, master2/s5), its nested shapes included. The output is
XML, not JSON.`,
}

// runXML is the xml handler (chunk C1): read.XML, bytes and a newline.
func runXML(p *cli.Parsed, stdout io.Writer) error {
	_, _ = p, stdout
	return cli.Errorf(cli.CodeOperationFailed, "", "vsdx xml is not implemented yet")
}

// htmlHelp is `vsdx help html` (chunk C1 writes it from the grammar of
// text.Render; docs/vsdx-contract-draft.md, section 7).
func htmlHelp() string {
	return "HTML in vsdx results: the pptx text grammar's subset (docs/vsdx-contract-draft.md, section 7).\n"
}
