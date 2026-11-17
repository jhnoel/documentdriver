package main

import (
	"io"

	"github.com/jhnoel/documentdriver/internal/cli"
)

var renderCommand = &cli.Command{
	Name:    "render",
	Args:    []string{"drawing.vsdx"},
	JSON:    true,
	Summary: "rasterise a page to a PNG, with optional overlays",
	Flags: []cli.Flag{
		{Name: "page", Value: "pageN", Help: "the page (required)"},
		cli.OutFlag(),
		{Name: "overlay", Value: "ids,sites,glue", Help: "marks to draw"},
		{Name: "ids", Value: "ids", Help: "limit the marks to these objects and their descendants"},
		{Name: "scale", Value: "n", Help: "pixels per point (default 1.5)"},
	},
	Help: `Rasterises one page through headless LibreOffice to the PNG named by -o
(required), as pptx render does a slide. LibreOffice draws the values
the file caches and ignores what only Visio computes; the render is for
checking, not proof. Overlays come from the read model, in the page's
coordinates.`,
}

// runRender is the render handler (chunk RN2), on the shared render
// package promoted from internal/pptx/render (chunk RN1).
func runRender(p *cli.Parsed, stdout io.Writer) error {
	_, _ = p, stdout
	return cli.Errorf(cli.CodeOperationFailed, "", "vsdx render is not implemented yet")
}
