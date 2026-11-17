package render

import (
	"errors"
	"fmt"
	"image"
	"io"
	"os"

	"github.com/jhnoel/documentdriver/internal/opc"
	sharedrender "github.com/jhnoel/documentdriver/internal/render"
)

// DefaultScale is the default resolution in pixels per point.
const DefaultScale = sharedrender.DefaultScale

// DefaultTimeout bounds each external process (soffice, rasteriser).
const DefaultTimeout = sharedrender.DefaultTimeout

// ErrNoSoffice is returned when LibreOffice cannot be found.
var ErrNoSoffice = errors.New("render: soffice (LibreOffice) not found; set PPTX_SOFFICE or add it to PATH")

// ErrNoRasteriser is returned when no PDF rasteriser can be found.
var ErrNoRasteriser = sharedrender.ErrNoRasteriser

// impressFilter exports one PDF page per slide, in sldIdLst order, hidden
// slides included.
const impressFilter = `pdf:impress_pdf_Export:{"ExportHiddenSlides":{"type":"boolean","value":"true"}}`

// Options controls one render.
type Options = sharedrender.Options

// Mark is one object to annotate; it forwards to internal/render.
type Mark = sharedrender.Mark

// Overlay selects what Draw paints; it forwards to internal/render.
type Overlay = sharedrender.Overlay

// KeyEntry maps a number drawn on the image to an object id; it forwards to
// internal/render.
type KeyEntry = sharedrender.KeyEntry

// Draw paints the overlays onto img; it forwards to internal/render.
func Draw(img *image.RGBA, slideW, slideH float64, marks []Mark, ov Overlay) []KeyEntry {
	return sharedrender.Draw(img, slideW, slideH, marks, ov)
}

// EncodePNG writes img as a PNG; it forwards to internal/render.
func EncodePNG(w io.Writer, img image.Image) error { return sharedrender.EncodePNG(w, img) }

// Result is one rendered slide.
type Result struct {
	Image *image.RGBA // the slide, Scale pixels per point (rounded up by the rasteriser)
	Info  SlideInfo   // where the slide sits in the deck, and its size in points
	Scale float64     // requested pixels per point
}

// Renderer converts decks with headless LibreOffice and caches the PDF of
// each deck (by sha256 of its bytes) for the lifetime of the Renderer. It
// is not safe for concurrent use. Call Close to remove the temporary files.
type Renderer struct {
	*sharedrender.Renderer
	pdf map[string]string // deck sha256 -> pdf path (the shared cache, mirrored)
}

// New returns a Renderer. It reads PPTX_SOFFICE for the soffice path and
// passes it to the shared package. It fails if LibreOffice or a rasteriser
// is missing, with ErrNoSoffice or ErrNoRasteriser.
func New() (*Renderer, error) {
	r, err := sharedrender.New(os.Getenv("PPTX_SOFFICE"))
	if errors.Is(err, sharedrender.ErrNoSoffice) {
		return nil, ErrNoSoffice
	}
	if err != nil {
		return nil, err
	}
	return &Renderer{Renderer: r, pdf: map[string]string{}}, nil
}

// Slide renders one slide of the deck at path. slide is a part name such as
// "slide3" or "/ppt/slides/slide3.xml". Hidden slides are exported too
// (LibreOffice is asked to include them), so the page is always the slide's
// position in sldIdLst.
func (r *Renderer) Slide(deck, slide string, opt Options) (*Result, error) {
	if opt.Scale <= 0 {
		opt.Scale = DefaultScale
	}
	if opt.Timeout <= 0 {
		opt.Timeout = DefaultTimeout
	}
	pkg, err := opc.Open(deck)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	info, err := Locate(pkg, slide)
	if err != nil {
		return nil, err
	}
	pdf, err := r.Convert(deck, impressFilter, opt.Timeout)
	if err != nil {
		return nil, err
	}
	r.pdf = r.PDFs()
	if n := sharedrender.CountPages(pdf); n > 0 && n != info.Count {
		return nil, fmt.Errorf("render: LibreOffice exported %d pages for a %d-slide deck (%d hidden); cannot map %s to a page",
			n, info.Count, info.Hiddens, slide)
	}
	img, err := r.Rasterise(pdf, info.Index+1, 72*opt.Scale, opt.Timeout)
	if err != nil {
		return nil, err
	}
	return &Result{Image: img, Info: info, Scale: opt.Scale}, nil
}
