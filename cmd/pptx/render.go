package main

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/render"
)

var renderCommand = &cli.Command{
	Name:    "render",
	Args:    []string{"deck.pptx"},
	JSON:    true,
	Summary: "rasterise a slide to a PNG, with optional overlays",
	Flags: []cli.Flag{
		{Name: "slide", Value: "slideN", Help: "the slide (required)"},
		cli.OutFlag(),
		{Name: "overlay", Value: "ids,sites,glue", Help: "marks to draw"},
		{Name: "ids", Value: "ids", Help: "limit the marks to these objects and their descendants"},
		{Name: "scale", Value: "n", Help: "pixels per point (default 1.5)"},
	},
	Help: `Rasterises one slide through headless LibreOffice to the PNG named by -o
(required). Overlays are drawn in slide coordinates: ids tags each object
(dense slides get numbers, listed in the result's key), sites dots each
connection site with its index, glue rings each glued connector end. Marks
come from the read model, so site labels equal the indices pptx read
--detail sites reports. The result says what was written: saved, px, size
(points), scale, hidden, key.`,
}

// runRender rasterises one slide to a PNG and writes the JSON result.
func runRender(p *cli.Parsed, stdout io.Writer) error {
	deck, slide, out := p.Args[0], p.Get("slide"), p.Get("out")
	switch {
	case slide == "":
		return cli.Errorf(cli.CodeUsage, "--slide", "render needs --slide")
	case out == "":
		return cli.Errorf(cli.CodeUsage, "--out", "render needs -o, the PNG to write")
	}
	scale := render.DefaultScale
	if p.Has("scale") {
		v, err := strconv.ParseFloat(p.Get("scale"), 64)
		if err != nil || v <= 0 {
			return cli.Errorf(cli.CodeUsage, "--scale", "--scale is a positive number")
		}
		scale = v
	}
	var ids []string
	for id := range strings.SplitSeq(p.Get("ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			if _, short, ok := strings.Cut(id, "/"); ok {
				id = short
			}
			ids = append(ids, id)
		}
	}
	var ov render.Overlay
	for k := range strings.SplitSeq(p.Get("overlay"), ",") {
		switch strings.TrimSpace(k) {
		case "":
		case "ids":
			ov.IDs = true
		case "sites":
			ov.Sites = true
		case "glue":
			ov.Glue = true
		default:
			return cli.Errorf(cli.CodeUsage, "--overlay", "unknown overlay %q: ids, sites or glue", k)
		}
	}
	r, err := render.New()
	if err != nil {
		return cli.Wrap(cli.CodeIO, "", err)
	}
	defer r.Close()
	res, err := r.Slide(deck, slide, render.Options{Scale: scale})
	if err != nil {
		return cli.Wrap(cli.CodeOperationFailed, "", err)
	}
	var key []render.KeyEntry
	if ov.IDs || ov.Sites || ov.Glue {
		marks, err := slideMarks(deck, res.Info.Part, ids)
		if err != nil {
			return cli.Wrap(cli.CodeOperationFailed, "", err)
		}
		key = render.Draw(res.Image, res.Info.W, res.Info.H, marks, ov)
	}
	var buf bytes.Buffer
	if err := render.EncodePNG(&buf, res.Image); err != nil {
		return err
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil { //nolint:gosec // G306: a picture for the caller, not a secret
		return cli.Errorf(cli.CodeIO, "--out", "write the picture: %v", err)
	}
	type keyEntry struct {
		N  int    `json:"n"`
		ID string `json:"id"`
	}
	b := res.Image.Bounds()
	result := struct {
		Saved  string     `json:"saved"`
		Px     [2]int     `json:"px"`
		Size   [2]float64 `json:"size"`
		Scale  float64    `json:"scale"`
		Hidden bool       `json:"hidden"`
		Key    []keyEntry `json:"key"`
	}{Saved: out, Px: [2]int{b.Dx(), b.Dy()}, Size: [2]float64{res.Info.W, res.Info.H}, Scale: scale, Hidden: res.Info.Hidden, Key: []keyEntry{}}
	for _, k := range key {
		result.Key = append(result.Key, keyEntry{N: k.N, ID: slide + "/" + k.ID})
	}
	j, err := cli.Marshal(result)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, j, p.Has("pretty"))
}
