package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
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

// readInput reads the drawing the caller named, once, and opens it
// as a package so a file that is not a zip (or trips a package
// limit) is reported in pptx read's form, opc: <path>: <cause> (the
// path the caller named), as invalid_document, before the bytes are
// handed to the read layer (which opens them in memory and would
// name "(in memory)"). Chunk NW.
func readInput(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the drawing is the file the caller named
	if err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "read the drawing: %v", err)
	}
	pkg, err := opc.Open(path)
	if err != nil {
		return nil, cli.Wrap(cli.CodeInvalidDocument, "", err)
	}
	if err := pkg.Close(); err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "close the drawing: %v", err)
	}
	return data, nil
}

// checkPageValue reports a usage error at flag when value is not
// an address of kind want, with the hint naming the form read
// lists. A well-formed address of that kind that the drawing lacks
// stays not_found downstream.
func checkPageValue(value, flag string, want vsd.Kind) error {
	if a, err := vsd.Parse(value); err != nil || a.Kind != want {
		hint := "want page<id>, as read lists them"
		if want == vsd.KindMaster {
			hint = "want master<id>, as read lists them"
		}
		return cli.Errorf(cli.CodeUsage, flag, "%q is not a %s address", value, strings.TrimPrefix(flag, "--")).WithHint(hint)
	}
	return nil
}

// runRead is the read handler (chunk C1): the file read once, cli.Hash
// for base, read.Document or read.WritePage; --page and --master exclusive
// (usage); the per-page flags only with one of them (usage, as pptx);
// --region parsed as pptx parses it; errors mapped as pptx's openDeck
// and readSlide map them.
func runRead(p *cli.Parsed, stdout io.Writer) error {
	if p.Has("page") && p.Has("master") {
		return cli.Errorf(cli.CodeUsage, "--master", "--page and --master are exclusive")
	}
	if p.Has("page") {
		if err := checkPageValue(p.Get("page"), "--page", vsd.KindPage); err != nil {
			return err
		}
	}
	if p.Has("master") {
		if err := checkPageValue(p.Get("master"), "--master", vsd.KindMaster); err != nil {
			return err
		}
	}
	if !p.Has("page") && !p.Has("master") {
		for _, f := range []string{"detail", "ids", "region", "units"} {
			if p.Has(f) {
				return cli.Errorf(cli.CodeUsage, "--"+f, "--%s goes with --page or --master", f)
			}
		}
	}
	var region *[4]float64
	if p.Has("region") {
		box, err := cli.ParseRegion(p.Get("region"))
		if err != nil {
			return cli.Wrap(cli.CodeUsage, "--region", err)
		}
		region = &box
	}
	var ids []string
	for id := range strings.SplitSeq(p.Get("ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	input, err := readInput(p.Args[0])
	if err != nil {
		return err
	}
	base := cli.Hash(input)
	if !p.Has("page") && !p.Has("master") {
		sum, err := read.Document(input, base)
		if err != nil {
			return cli.Wrap(cli.CodeInvalidDocument, "", err)
		}
		out, err := cli.Marshal(sum)
		if err != nil {
			return err
		}
		return cli.WriteJSON(stdout, out, p.Has("pretty"))
	}
	address := p.Get("page")
	if p.Has("master") {
		address = p.Get("master")
	}
	opts := read.Options{
		Units:  p.Get("units"),
		Detail: p.Get("detail"),
		IDs:    ids,
		Region: region,
	}
	// Without --pretty the page streams to stdout through a bufio
	// writer (chunk RM3); with it the buffered path keeps the bytes
	// cli.WriteJSON indents exactly as before.
	if !p.Has("pretty") {
		bw := bufio.NewWriter(stdout)
		if err := read.WritePage(bw, input, base, address, opts); err != nil {
			return cli.Wrap(cli.CodeInvalidDocument, "", err)
		}
		if err := bw.Flush(); err != nil {
			return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
		}
		return nil
	}
	var buf bytes.Buffer
	if err := read.WritePage(&buf, input, base, address, opts); err != nil {
		return cli.Wrap(cli.CodeInvalidDocument, "", err)
	}
	return cli.WriteJSON(stdout, buf.Bytes(), true)
}
