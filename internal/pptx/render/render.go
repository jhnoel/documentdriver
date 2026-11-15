package render

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/soffice"
)

// DefaultScale is the default resolution in pixels per point.
const DefaultScale = 1.5

// DefaultTimeout bounds each external process (soffice, rasteriser).
const DefaultTimeout = 120 * time.Second

// ErrNoSoffice is returned when LibreOffice cannot be found.
var ErrNoSoffice = errors.New("render: soffice (LibreOffice) not found; set PPTX_SOFFICE or add it to PATH")

// ErrNoRasteriser is returned when no PDF rasteriser can be found.
var ErrNoRasteriser = errors.New("render: no PDF rasteriser found (need pdftoppm, mutool or gs on PATH)")

// Options controls one render.
type Options struct {
	Scale   float64       // pixels per point; DefaultScale when zero
	Timeout time.Duration // per external process; DefaultTimeout when zero
}

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
	dir             string
	pdf             map[string]string // deck sha256 -> pdf path
	soffice, raster string
}

// New returns a Renderer. It fails if LibreOffice or a rasteriser is
// missing, with ErrNoSoffice or ErrNoRasteriser.
func New() (*Renderer, error) {
	so := soffice.Find(os.Getenv("PPTX_SOFFICE"), "soffice", soffice.DefaultPaths...)
	if so == "" {
		return nil, ErrNoSoffice
	}
	ras := ""
	for _, n := range []string{"pdftoppm", "mutool", "gs"} {
		if p := soffice.Find("", n, "/opt/homebrew/bin/"+n, "/usr/local/bin/"+n); p != "" {
			ras = p
			break
		}
	}
	if ras == "" {
		return nil, ErrNoRasteriser
	}
	dir, err := os.MkdirTemp("", "pptx-render-")
	if err != nil {
		return nil, err
	}
	return &Renderer{dir: dir, pdf: map[string]string{}, soffice: so, raster: ras}, nil
}

// Close removes the Renderer's temporary files.
func (r *Renderer) Close() error { return os.RemoveAll(r.dir) }

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
	pdf, err := r.convert(deck, opt.Timeout)
	if err != nil {
		return nil, err
	}
	if n := countPages(pdf); n > 0 && n != info.Count {
		return nil, fmt.Errorf("render: LibreOffice exported %d pages for a %d-slide deck (%d hidden); cannot map %s to a page",
			n, info.Count, info.Hiddens, slide)
	}
	img, err := r.rasterise(pdf, info.Index+1, 72*opt.Scale, opt.Timeout)
	if err != nil {
		return nil, err
	}
	return &Result{Image: img, Info: info, Scale: opt.Scale}, nil
}

// convert returns the PDF of the deck, converting it at most once per
// distinct deck content.
func (r *Renderer) convert(deck string, timeout time.Duration) (string, error) {
	b, err := os.ReadFile(deck) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	key := hex.EncodeToString(sum[:])
	if p, ok := r.pdf[key]; ok {
		return p, nil
	}
	work := filepath.Join(r.dir, key[:16])
	if err := os.MkdirAll(work, 0o755); err != nil { //nolint:gosec // G301: render scratch directory under the renderer's own temp dir
		return "", err
	}
	// Copy the deck to a fixed name so odd file names cannot confuse
	// soffice, and so the output name is known.
	src := filepath.Join(work, "deck.pptx")
	if err := os.WriteFile(src, b, 0o644); err != nil { //nolint:gosec // G306: scratch file under the renderer's own temp dir, or a fixture the caller reads back
		return "", err
	}
	profile := filepath.Join(work, "profile")
	// ExportHiddenSlides keeps one PDF page per slide, in sldIdLst order.
	filter := `pdf:impress_pdf_Export:{"ExportHiddenSlides":{"type":"boolean","value":"true"}}`
	out, err := soffice.Run(timeout, work, r.soffice,
		"-env:UserInstallation=file://"+profile,
		"--headless", "--norestore", "--nolockcheck", "--nodefault", "--nologo",
		"--convert-to", filter, "--outdir", work, src)
	pdf := filepath.Join(work, "deck.pdf")
	if err != nil {
		return "", fmt.Errorf("render: LibreOffice could not convert %s: %w%s", deck, err, tail(out))
	}
	if st, err := os.Stat(pdf); err != nil || st.Size() == 0 { //nolint:gosec // G703: path built from the renderer's own temp dir
		return "", fmt.Errorf("render: LibreOffice produced no PDF for %s%s", deck, tail(out))
	}
	r.pdf[key] = pdf
	return pdf, nil
}

// rasterise renders page of pdf at dpi, returning the decoded image or an error.
func (r *Renderer) rasterise(pdf string, page int, dpi float64, timeout time.Duration) (*image.RGBA, error) {
	dir := filepath.Dir(pdf)
	pg := strconv.Itoa(page)
	d := strconv.FormatFloat(dpi, 'f', 3, 64)
	var args []string
	var png1 string
	switch filepath.Base(r.raster) {
	case "pdftoppm":
		png1 = filepath.Join(dir, "page-"+pg+".png")
		args = []string{"-png", "-singlefile", "-r", d, "-f", pg, "-l", pg, pdf, strings.TrimSuffix(png1, ".png")}
	case "mutool":
		png1 = filepath.Join(dir, "page-"+pg+".png")
		args = []string{"draw", "-q", "-r", d, "-o", png1, pdf, pg}
	default: // gs
		png1 = filepath.Join(dir, "page-"+pg+".png")
		args = []string{"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER", "-sDEVICE=png16m", "-dTextAlphaBits=4", "-dGraphicsAlphaBits=4",
			"-r" + d, "-dFirstPage=" + pg, "-dLastPage=" + pg, "-sOutputFile=" + png1, pdf}
	}
	_ = os.Remove(png1)
	out, err := soffice.Run(timeout, dir, r.raster, args...)
	if err != nil {
		return nil, fmt.Errorf("render: %s failed for page %d: %w%s", filepath.Base(r.raster), page, err, tail(out))
	}
	f, err := os.Open(png1) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
	if err != nil {
		return nil, fmt.Errorf("render: %s produced no image for page %d%s", filepath.Base(r.raster), page, tail(out))
	}
	defer f.Close()
	return decodeRGBA(f)
}

// decodeRGBA decodes a PNG from rd into a new RGBA image.
func decodeRGBA(rd io.Reader) (*image.RGBA, error) {
	im, err := png.Decode(rd)
	if err != nil {
		return nil, err
	}
	b := im.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.Set(x, y, im.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out, nil
}

// EncodePNG writes img as a PNG.
func EncodePNG(w io.Writer, img image.Image) error { return png.Encode(w, img) }

// tail returns the last 400 bytes of output as a colon-prefixed suffix, or empty.
func tail(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return ""
	}
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return ": " + s
}

var pageRE = regexp.MustCompile(`/Type\s*/Page[^s]`)

// countPages counts page objects in the PDF; 0 means it could not tell
// (for example page objects inside compressed object streams).
func countPages(pdf string) int {
	b, err := os.ReadFile(pdf) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
	if err != nil {
		return 0
	}
	return len(pageRE.FindAll(b, -1))
}
