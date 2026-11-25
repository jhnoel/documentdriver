package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/render"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

var renderCommand = &cli.Command{
	Name:    "render",
	Args:    []string{"drawing.vsdx"},
	JSON:    true,
	Summary: "rasterise a page to a PNG, with optional overlays",
	Flags: []cli.Flag{
		{Name: "page", Value: "pageN", Help: "the page (required)", Required: true},
		cli.RequiredOutFlag(),
		{Name: "overlay", Value: "ids,sites,glue", Help: "marks to draw"},
		{Name: "ids", Value: "ids", Help: "limit the marks to these objects and their descendants"},
		{Name: "scale", Value: "n", Help: "pixels per point (default 1.5)"},
		{Name: "renderer", Value: "auto|libreoffice|svg", Enum: []string{"auto", "libreoffice", "svg"}, Help: "who draws the page: auto (default; LibreOffice when installed, else svg), libreoffice, or svg (the read --format svg drawing, rasterised in Go)"},
	},
	Help: `Rasterises one page to the PNG named by -o (required). Two renderers
draw it, and the result's renderer says which one did:

  libreoffice  headless LibreOffice, as pptx render draws a slide; soffice
               comes from DD_SOFFICE or PATH, with pdftoppm, mutool or gs.
               LibreOffice draws the values the file caches. Measured on
               LibreOffice 26.2: it draws a shape's whole text in the
               formatting of its last run (Visio-written text too), so a
               label with mixed sizes or bold words shows one style; it
               honours text_block box, angle, valign and margins, but
               draws a background only behind the letters (no margins)
               and opaque (no transparency).
  svg          the drawing read --format svg writes, rasterised in Go with
               no other program: the same paths, hatches, dashes, arrowheads
               and pictures, and text drawn with embedded open fonts
               (Carlito, Liberation Sans and Serif: the metrics of Calibri,
               Arial and Times New Roman; DejaVu Sans for characters they
               lack), each run in its face, size, bold and italic. Its
               notes list what it left out or approximated. The fonts and
               their licences: vsdx help fonts.

--renderer auto (the default) uses libreoffice when it is installed (DD_SOFFICE
names a file that exists, or soffice is on PATH or in its usual place, and a
PDF rasteriser is on PATH) and svg otherwise; libreoffice without LibreOffice is operation_failed and writes
nothing. Either picture is for checking, not proof of what Visio draws.
Overlays come from the read model, in the page's coordinates.`}

// runRender rasterises one page to a PNG and writes the JSON result.
// The page is located before LibreOffice runs, so an unknown page is
// not_found and an unreadable page is unreadable_target even without
// it.
func runRender(p *cli.Parsed, stdout io.Writer) error {
	page, out := p.Get("page"), p.Get("out")
	if page == "" {
		return cli.Errorf(cli.CodeUsage, "--page", "render needs --page")
	}
	if err := checkPageValue(page, "--page", vsd.KindPage); err != nil {
		return err
	}
	if out == "" {
		return cli.Errorf(cli.CodeUsage, "--out", "render needs -o, the PNG to write")
	}
	scale := render.DefaultScale
	if p.Has("scale") {
		v, err := render.ParseScale(p.Get("scale"))
		if err != nil {
			return cli.Wrap(cli.CodeUsage, "--scale", err)
		}
		scale = v
	}
	overlay, err := render.ParseOverlay(p.Get("overlay"))
	if err != nil {
		return cli.Wrap(cli.CodeUsage, "--overlay", err)
	}
	input, err := os.ReadFile(p.Args[0]) //nolint:gosec // G304: the drawing is the caller's argument
	if err != nil {
		return cli.Wrap(cli.CodeIO, "", err)
	}
	info, err := read.Locate(input, page)
	if err != nil {
		return cli.Wrap(cli.CodeOperationFailed, "", err)
	}
	img, used, notes, err := drawPage(p, input, info, scale)
	if err != nil {
		return err
	}
	result := render.Result{Saved: out, Size: [2]json.Number{json.Number(geom.FormatNum(info.W)), json.Number(geom.FormatNum(info.H))}, Scale: scale, Key: []render.KeyEntry{}}
	if overlay.IDs || overlay.Sites || overlay.Glue {
		marks, err := pageMarks(input, info.Page, idList(p.Get("ids")), overlay.Sites)
		if err != nil {
			return cli.Wrap(cli.CodeOperationFailed, "", err)
		}
		for _, k := range render.Draw(img, info.W, info.H, marks, overlay) {
			result.Key = append(result.Key, render.KeyEntry{N: k.N, ID: k.ID})
		}
	}
	var buf bytes.Buffer
	if err := render.EncodePNG(&buf, img); err != nil {
		return err
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil { //nolint:gosec // G306: a picture for the caller, not a secret
		return cli.Errorf(cli.CodeIO, "--out", "write the picture: %v", err)
	}
	result.Px = [2]int{img.Bounds().Dx(), img.Bounds().Dy()}
	j, err := cli.Marshal(renderResult{Result: result, Renderer: used, Notes: notes})
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, j, p.Has("pretty"))
}

// renderResult is vsdx render's result: pptx render's fields, then
// the renderer that drew the picture ("libreoffice" or "svg") and the
// notes on what it left out (always empty for libreoffice, whose
// omissions are not known).
type renderResult struct {
	render.Result
	Renderer string      `json:"renderer"`
	Notes    []read.Note `json:"notes"`
}

// drawPage draws page info of input with the renderer --renderer
// selects, returning the picture, the renderer used and its notes.
func drawPage(p *cli.Parsed, input []byte, info read.PageInfo, scale float64) (*image.RGBA, string, []read.Note, error) {
	choice := p.Get("renderer")
	if choice == "" {
		choice = "auto"
	}
	if choice != "svg" {
		env := os.Getenv("DD_SOFFICE")
		renderer, err := render.New(env)
		if err == nil && choice == "auto" && env != "" {
			// auto takes LibreOffice only when DD_SOFFICE names a file
			// that exists; libreoffice tries it and reports the failure.
			if st, statErr := os.Stat(env); statErr != nil || st.IsDir() {
				if closeErr := renderer.Close(); closeErr != nil {
					return nil, "", nil, cli.Errorf(cli.CodeIO, "", "remove the render's temporary files: %v", closeErr)
				}
				err = render.ErrNoSoffice
			}
		}
		switch {
		case err == nil:
			defer renderer.Close()
			img, err := renderPage(renderer, p.Args[0], info, scale)
			if err != nil {
				return nil, "", nil, cli.Wrap(cli.CodeOperationFailed, "-o", err)
			}
			return img, "libreoffice", []read.Note{}, nil
		case choice == "libreoffice":
			return nil, "", nil, rendererError(err)
		}
	}
	scene, notes, err := read.PageScene(input, "", info.Page, read.Options{})
	if err != nil {
		return nil, "", nil, cli.Wrap(cli.CodeOperationFailed, "", err)
	}
	img, rnotes, err := render.Rasterise(scene, scale)
	if err != nil {
		return nil, "", nil, cli.Wrap(cli.CodeOperationFailed, "--scale", err)
	}
	for _, n := range rnotes {
		at, reason, _ := strings.Cut(n, ": ")
		notes = append(notes, read.Note{At: at, Reason: reason})
	}
	return img, "svg", notes, nil
}

// idList splits a comma-separated --ids value into trimmed addresses.
func idList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
}

// renderPage converts the drawing at file and rasterises the PDF page
// info.Index+1 at scale pixels per point, returning only that page's
// own size: LibreOffice sizes every PDF page to the union of all pages,
// so the raster of a smaller page is cut to info.W by info.H (see
// render.CropToPage). It refuses when the PDF page count is known and
// differs from the drawing's page count.
func renderPage(renderer *render.Renderer, file string, info read.PageInfo, scale float64) (*image.RGBA, error) {
	pdf, err := renderer.Convert(file, "drawing.vsdx", "", render.DefaultTimeout)
	if err != nil {
		return nil, err
	}
	if n := render.CountPages(pdf); n > 0 && n != info.Count {
		return nil, fmt.Errorf("render: LibreOffice exported %d pages for a %d-page drawing; cannot map %s to a page", n, info.Count, info.Page)
	}
	img, err := renderer.Rasterise(pdf, info.Index+1, 72*scale, render.DefaultTimeout)
	if err != nil {
		return nil, err
	}
	return render.CropToPage(img, info.W, info.H, scale), nil
}

// pageMarks reads page of input through read.PageModel and turns its
// objects into drawing marks in the page's points: a connector by
// its ends, a shape by its box and rotation, with its sites when
// sites and a glue end for each glued connector end. An object with
// no box, path, site or glue end has nothing to mark and is left out.
func pageMarks(input []byte, page string, ids []string, sites bool) ([]render.Mark, error) {
	detail := ""
	if sites {
		detail = "sites"
	}
	shown, err := read.PageModel(input, page, read.Options{Detail: detail, IDs: ids})
	if err != nil {
		return nil, err
	}
	var marks []render.Mark
	for _, o := range shown.Objects {
		mark := render.Mark{ID: o.ID}
		if o.From != nil && o.To != nil {
			mark.Path = [][2]float64{{o.From.X, o.From.Y}, {o.To.X, o.To.Y}}
		} else if o.Box != nil {
			mark.Box = [4]float64{o.Box.X, o.Box.Y, o.Box.W, o.Box.H}
			mark.Rot = o.Box.Rot
		}
		for _, site := range o.Sites {
			mark.Sites = append(mark.Sites, [2]float64{site.Pt.X, site.Pt.Y})
		}
		if o.FromGlue != "" && o.From != nil {
			mark.Ends = append(mark.Ends, [2]float64{o.From.X, o.From.Y})
		}
		if o.ToGlue != "" && o.To != nil {
			mark.Ends = append(mark.Ends, [2]float64{o.To.X, o.To.Y})
		}
		if len(mark.Path) == 0 && mark.Box == [4]float64{} && len(mark.Sites) == 0 && len(mark.Ends) == 0 {
			continue
		}
		marks = append(marks, mark)
	}
	return marks, nil
}

// rendererError is the error of a renderer that cannot start.
// LibreOffice or a PDF rasteriser that is missing makes render
// operation_failed at -o (the picture that cannot be made), as xlsx
// --recalc is at --recalc without LibreOffice.
func rendererError(err error) *cli.Error {
	e := &cli.Error{Code: cli.CodeOperationFailed, Path: "-o", Message: err.Error(), Cause: err}
	e.Hint = "install LibreOffice (and pdftoppm, mutool or gs) or set DD_SOFFICE, or use --renderer svg"
	return e
}
