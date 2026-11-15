package main

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/view"
)

var readCommand = &cli.Command{
	Name:    "read",
	Args:    []string{"deck.pptx"},
	JSON:    true,
	Summary: "list the slides, or show one slide's objects",
	Flags: []cli.Flag{
		{Name: "slide", Value: "slideN", Help: "show this slide's objects (a part name such as slide3)"},
		{Name: "format", Value: "json|svg", Enum: []string{"json", "svg"}, Help: "the view: JSON (default) or SVG"},
		{Name: "detail", Value: "sites|full", Enum: []string{"sites", "full"}, Help: "add connection sites, or every modelled property with its origin"},
		{Name: "ids", Value: "ids", Help: "only these objects and their descendants (slide3/s12,slide3/s13)"},
		{Name: "region", Value: "x,y,w,h", Help: "only objects whose box intersects this one"},
		{Name: "units", Value: "pt|emu", Enum: []string{"pt", "emu"}, Help: "units of lengths (default pt)"},
	},
	Help: `Without --slide, lists the slides in presentation order with part names,
titles and object counts. With --slide, prints the slide's objects in paint
order (first is bottom). Every result starts with base, the deck's hash,
and has notes: what the tool left out or could not read. Read never writes.`,
}

// openDeck reads the deck file and opens it; base is the hash of its
// bytes.
func openDeck(path string) (p *opc.Package, d *model.Deck, base string, err error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the deck is the file the caller named
	if err != nil {
		return nil, nil, "", cli.Errorf(cli.CodeIO, "", "read the deck: %v", err)
	}
	if p, err = opc.Open(path); err != nil {
		return nil, nil, "", cli.Wrap(cli.CodeInvalidDocument, "", err)
	}
	if d, err = model.OpenDeck(p); err != nil {
		p.Close()
		return nil, nil, "", cli.Wrap(cli.CodeInvalidDocument, "", err)
	}
	return p, d, cli.Hash(data), nil
}

// readSlide resolves and reads one slide of an open deck.
func readSlide(d *model.Deck, slide, flag string) (*model.Slide, error) {
	part, err := d.ResolveSlide(slide)
	if err != nil {
		return nil, cli.Wrap(cli.CodeNotFound, flag, err)
	}
	s, err := d.ReadSlide(part)
	if err != nil {
		return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Path: flag, Message: "the slide cannot be read", Detail: err.Error(), Cause: err}
	}
	return s, nil
}

// runRead reads slides or one slide from the deck and writes JSON or SVG.
func runRead(p *cli.Parsed, stdout io.Writer) error {
	opt := view.Options{Detail: p.Get("detail"), Units: p.Get("units")}
	for id := range strings.SplitSeq(p.Get("ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			opt.IDs = append(opt.IDs, id)
		}
	}
	if region := p.Get("region"); region != "" {
		f := strings.Split(region, ",")
		if len(f) != 4 {
			return cli.Errorf(cli.CodeUsage, "--region", "--region is x,y,w,h")
		}
		var r [4]float64
		for i, s := range f {
			v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return cli.Errorf(cli.CodeUsage, "--region", "--region is four numbers: %q is not one", s)
			}
			r[i] = v
		}
		opt.Region = &r
	}
	pkg, d, base, err := openDeck(p.Args[0])
	if err != nil {
		return err
	}
	defer pkg.Close()
	opt.Base = base
	if !p.Has("slide") {
		for _, f := range []string{"format", "detail", "ids", "region", "units"} {
			if p.Has(f) && !(f == "format" && p.Get(f) == "json") {
				return cli.Errorf(cli.CodeUsage, "--"+f, "--%s goes with --slide", f)
			}
		}
		list, err := d.List()
		if err != nil {
			return cli.Wrap(cli.CodeInvalidDocument, "", err)
		}
		out, err := view.SlideList(list, base)
		if err != nil {
			return err
		}
		return cli.WriteJSON(stdout, out, p.Has("pretty"))
	}
	s, err := readSlide(d, p.Get("slide"), "--slide")
	if err != nil {
		return err
	}
	if p.Get("format") == "svg" {
		out, err := view.SVG(s, opt)
		if err != nil {
			return err
		}
		_, err = stdout.Write(out)
		return err
	}
	out, err := view.JSON(s, opt)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, out, p.Has("pretty"))
}

var xmlCommand = &cli.Command{
	Name:    "xml",
	Args:    []string{"deck.pptx", "address"},
	Summary: "print the source XML of one object",
	Help: `Prints the exact source bytes of one object, addressed as pptx read shows
it (slide3/s17, or slide3/s17~2 for the second object with id 17). An
object inside mc:AlternateContent prints the whole AlternateContent
element. The output is XML, not JSON.`,
}

// runXML prints the source XML of the addressed object.
func runXML(p *cli.Parsed, stdout io.Writer) error {
	slide, id, ok := strings.Cut(p.Args[1], "/")
	if !ok || id == "" {
		return cli.Errorf(cli.CodeUsage, "", "%q is not an object address; give one as pptx read shows it, such as slide3/s17", p.Args[1])
	}
	pkg, d, _, err := openDeck(p.Args[0])
	if err != nil {
		return err
	}
	defer pkg.Close()
	s, err := readSlide(d, slide, "")
	if err != nil {
		return err
	}
	for _, o := range s.Objects {
		if o.ID == id {
			if _, err := stdout.Write(append(o.XML(), '\n')); err != nil {
				return err
			}
			return nil
		}
	}
	return cli.Errorf(cli.CodeNotFound, "", "no object %s on %s", id, s.Name)
}
