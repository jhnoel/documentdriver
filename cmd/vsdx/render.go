package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/render"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
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

// renderResult is the JSON result of one render.
type renderResult struct {
	Saved string      `json:"saved"`
	Px    [2]int      `json:"px"`
	Size  [2]float64  `json:"size"`
	Scale float64     `json:"scale"`
	Key   []renderKey `json:"key"`
}

// renderKey is one numbered mark in a render result's key.
type renderKey struct {
	N  int    `json:"n"`
	ID string `json:"id"`
}

// runRender rasterises one page to a PNG and writes the JSON result.
// The page is located before LibreOffice runs, so an unknown page is
// not_found even without it.
func runRender(p *cli.Parsed, stdout io.Writer) error {
	page, out := p.Get("page"), p.Get("out")
	if page == "" {
		return cli.Errorf(cli.CodeUsage, "--page", "render needs --page")
	}
	if out == "" {
		return cli.Errorf(cli.CodeUsage, "--out", "render needs -o, the PNG to write")
	}
	scale, err := scaleFlag(p)
	if err != nil {
		return err
	}
	overlay, err := overlayFlag(p)
	if err != nil {
		return err
	}
	input, err := os.ReadFile(p.Args[0]) //nolint:gosec // G304: the drawing is the caller's argument
	if err != nil {
		return cli.Wrap(cli.CodeIO, "", err)
	}
	info, err := read.Locate(input, page)
	if err != nil {
		return cli.Wrap(cli.CodeOperationFailed, "", err)
	}
	renderer, err := render.New(os.Getenv("VSDX_SOFFICE"))
	if err != nil {
		return cli.Wrap(cli.CodeIO, "", err)
	}
	defer renderer.Close()
	img, err := renderPage(renderer, p.Args[0], info, scale)
	if err != nil {
		return cli.Wrap(cli.CodeOperationFailed, "", err)
	}
	result := renderResult{Saved: out, Size: [2]float64{info.W, info.H}, Scale: scale, Key: []renderKey{}}
	if overlay.IDs || overlay.Sites || overlay.Glue {
		marks, err := pageMarks(input, info.Page, idList(p.Get("ids")), overlay.Sites)
		if err != nil {
			return cli.Wrap(cli.CodeOperationFailed, "", err)
		}
		for _, k := range render.Draw(img, info.W, info.H, marks, overlay) {
			result.Key = append(result.Key, renderKey{N: k.N, ID: k.ID})
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
	j, err := cli.Marshal(result)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, j, p.Has("pretty"))
}

// scaleFlag returns --scale, the shared default when unset, and a
// usage error when it is not a positive number.
func scaleFlag(p *cli.Parsed) (float64, error) {
	if !p.Has("scale") {
		return render.DefaultScale, nil
	}
	n, err := strconv.ParseFloat(p.Get("scale"), 64)
	if err != nil || n <= 0 {
		return 0, cli.Errorf(cli.CodeUsage, "--scale", "--scale is a positive number")
	}
	return n, nil
}

// overlayFlag returns the overlay --overlay names, and a usage error
// for a word that is not ids, sites or glue.
func overlayFlag(p *cli.Parsed) (render.Overlay, error) {
	var overlay render.Overlay
	for word := range strings.SplitSeq(p.Get("overlay"), ",") {
		switch strings.TrimSpace(word) {
		case "":
		case "ids":
			overlay.IDs = true
		case "sites":
			overlay.Sites = true
		case "glue":
			overlay.Glue = true
		default:
			return overlay, cli.Errorf(cli.CodeUsage, "--overlay", "unknown overlay %q: ids, sites or glue", word)
		}
	}
	return overlay, nil
}

// idList splits a comma-separated --ids value into trimmed addresses.
func idList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
}

// renderPage converts the drawing at file and rasterises the PDF page
// info.Index+1 at scale pixels per point, refusing when the PDF page
// count is known and differs from the drawing's page count.
func renderPage(renderer *render.Renderer, file string, info read.PageInfo, scale float64) (*image.RGBA, error) {
	pdf, err := renderer.Convert(file, "drawing.vsdx", "", render.DefaultTimeout)
	if err != nil {
		return nil, err
	}
	if n := render.CountPages(pdf); n > 0 && n != info.Count {
		return nil, fmt.Errorf("render: LibreOffice exported %d pages for a %d-page drawing; cannot map %s to a page", n, info.Count, info.Page)
	}
	return renderer.Rasterise(pdf, info.Index+1, 72*scale, render.DefaultTimeout)
}

// pageMarks reads page of input through read.Page and turns its objects
// into drawing marks in the page's points: a connector by its ends, a
// shape by its box and rotation, with its sites when sites and a glue
// end for each glued connector end. An object with no box, path, site
// or glue end has nothing to mark and is left out.
func pageMarks(input []byte, page string, ids []string, sites bool) ([]render.Mark, error) {
	detail := ""
	if sites {
		detail = "sites"
	}
	b, err := read.Page(input, cli.Hash(input), page, read.Options{Detail: detail, IDs: ids})
	if err != nil {
		return nil, err
	}
	var shown struct {
		Objects []struct {
			ID       string      `json:"id"`
			Box      []float64   `json:"box"`
			Rot      float64     `json:"rot"`
			From     []float64   `json:"from"`
			To       []float64   `json:"to"`
			FromGlue string      `json:"from_glue"`
			ToGlue   string      `json:"to_glue"`
			Sites    [][]float64 `json:"sites"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(b, &shown); err != nil {
		return nil, fmt.Errorf("render: read the page for marks: %w", err)
	}
	point := func(v []float64) ([2]float64, bool) {
		if len(v) != 2 {
			return [2]float64{}, false
		}
		return [2]float64{v[0], v[1]}, true
	}
	var marks []render.Mark
	for _, o := range shown.Objects {
		mark := render.Mark{ID: o.ID}
		from, hasFrom := point(o.From)
		to, hasTo := point(o.To)
		switch {
		case hasFrom && hasTo:
			mark.Path = [][2]float64{from, to}
		case len(o.Box) == 4:
			mark.Box = [4]float64{o.Box[0], o.Box[1], o.Box[2], o.Box[3]}
			mark.Rot = o.Rot
		}
		for _, site := range o.Sites {
			if s, ok := point(site); ok {
				mark.Sites = append(mark.Sites, s)
			}
		}
		if o.FromGlue != "" && hasFrom {
			mark.Ends = append(mark.Ends, from)
		}
		if o.ToGlue != "" && hasTo {
			mark.Ends = append(mark.Ends, to)
		}
		if len(mark.Path) == 0 && mark.Box == [4]float64{} && len(mark.Sites) == 0 && len(mark.Ends) == 0 {
			continue
		}
		marks = append(marks, mark)
	}
	return marks, nil
}
