package render

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jhnoel/documentdriver/internal/soffice"
)

// DefaultScale is the default resolution in pixels per point.
const DefaultScale = 1.5

// DefaultTimeout bounds each external process (soffice, rasteriser).
const DefaultTimeout = 120 * time.Second

// ErrNoSoffice is returned when LibreOffice cannot be found.
var ErrNoSoffice = errors.New("render: soffice (LibreOffice) not found; add it to PATH")

// ErrNoRasteriser is returned when no PDF rasteriser can be found.
var ErrNoRasteriser = errors.New("render: no PDF rasteriser found (need pdftoppm, mutool or gs on PATH)")

// Options controls one render.
type Options struct {
	Scale   float64       // pixels per point; DefaultScale when zero
	Timeout time.Duration // per external process; DefaultTimeout when zero
}

// Renderer converts files with headless LibreOffice and caches the PDF of
// each file (by sha256 of its bytes) for the lifetime of the Renderer. It
// is not safe for concurrent use. Call Close to remove the temporary files.
type Renderer struct {
	dir             string
	pdf             map[string]string // file sha256 -> pdf path
	soffice, raster string
}

// New returns a Renderer using sofficePath as the LibreOffice binary; when
// it is empty the binary is searched for on PATH and in the default
// locations. The caller reads its own environment variable and passes the
// value, so the shared package names no format's variable. It fails if
// LibreOffice or a rasteriser is missing, with ErrNoSoffice or
// ErrNoRasteriser.
func New(sofficePath string) (*Renderer, error) {
	so := soffice.Find(sofficePath, "soffice", soffice.DefaultPaths...)
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
	dir, err := os.MkdirTemp("", "render-")
	if err != nil {
		return nil, err
	}
	return &Renderer{dir: dir, pdf: map[string]string{}, soffice: so, raster: ras}, nil
}

// Close removes the Renderer's temporary files.
func (r *Renderer) Close() error { return os.RemoveAll(r.dir) }

// Conversions returns the number of distinct files converted so far: the
// size of the PDF cache.
func (r *Renderer) Conversions() int { return len(r.pdf) }

// Convert returns the PDF of file, converting it at most once per distinct
// file content. name is the base name the caller wants LibreOffice to see
// ("deck.pptx" for pptx, "drawing.vsdx" for vsdx): the file is copied
// under name in the work directory, so odd file names cannot confuse
// soffice, and the PDF is the stem of name with a .pdf extension. A name
// with a path separator or without an extension is an error. filter is
// soffice's --convert-to value ("pdf" when empty); the caller names the
// format's filter (for example the impress filter that exports hidden
// slides).
func (r *Renderer) Convert(file, name, filter string, timeout time.Duration) (string, error) {
	ext := filepath.Ext(name)
	if strings.ContainsAny(name, `/\`) || ext == "" || ext == "." {
		return "", fmt.Errorf("render: Convert name %q must be a base name with an extension", name)
	}
	if filter == "" {
		filter = "pdf"
	}
	b, err := os.ReadFile(file) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
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
	src := filepath.Join(work, name)
	if err := os.WriteFile(src, b, 0o644); err != nil { //nolint:gosec // G306: scratch file under the renderer's own temp dir, or a fixture the caller reads back
		return "", err
	}
	profile := filepath.Join(work, "profile")
	out, err := soffice.Run(timeout, work, r.soffice,
		"-env:UserInstallation=file://"+profile,
		"--headless", "--norestore", "--nolockcheck", "--nodefault", "--nologo",
		"--convert-to", filter, "--outdir", work, src)
	pdf := filepath.Join(work, strings.TrimSuffix(name, ext)+".pdf")
	if err != nil {
		return "", fmt.Errorf("render: LibreOffice could not convert %s: %w%s", file, err, tail(out))
	}
	if st, err := os.Stat(pdf); err != nil || st.Size() == 0 { //nolint:gosec // G703: path built from the renderer's own temp dir
		return "", fmt.Errorf("render: LibreOffice produced no PDF for %s%s", file, tail(out))
	}
	r.pdf[key] = pdf
	return pdf, nil
}

// Rasterise renders page of pdf at dpi, returning the decoded image or an error.
func (r *Renderer) Rasterise(pdf string, page int, dpi float64, timeout time.Duration) (*image.RGBA, error) {
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

// CountPages counts page objects in the PDF; 0 means it could not tell
// (for example page objects inside compressed object streams).
func CountPages(pdf string) int {
	b, err := os.ReadFile(pdf) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
	if err != nil {
		return 0
	}
	return len(pageRE.FindAll(b, -1))
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

// CropToPage returns a new image holding only one page's own size from
// img, the raster of a LibreOffice PDF page. LibreOffice exports
// every PDF page with one uniform size, the union of all pages'
// sizes, and draws each page's content at the top-left, so img can
// be wider or taller than the page it shows; the pages' own width
// and height w and h (points, the read view's size) at scale pixels
// per point are that page's size at the scale, and the extra right
// and bottom margins are cut away. The crop is rounded to a pixel
// and clamped to the raster (never padded or stretched: a page
// nearer than a pixel to the raster's edge still gives the raster's
// size). When w, h and scale are not positive, finite numbers the
// page has no size to crop to, and img itself is returned.
func CropToPage(img *image.RGBA, w, h, scale float64) *image.RGBA {
	if img == nil {
		return img
	}
	for _, v := range [3]float64{w, h, scale} {
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return img
		}
	}
	b := img.Bounds()
	wpx, hpx := b.Dx(), b.Dy()
	if fw := math.Round(w * scale); fw < float64(wpx) {
		wpx = int(fw)
	}
	if fh := math.Round(h * scale); fh < float64(hpx) {
		hpx = int(fh)
	}
	if wpx <= 0 || hpx <= 0 {
		return img
	}
	if wpx == b.Dx() && hpx == b.Dy() {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, wpx, hpx))
	for y := range hpx {
		src := img.PixOffset(b.Min.X, b.Min.Y+y)
		copy(out.Pix[y*out.Stride:y*out.Stride+wpx*4], img.Pix[src:src+wpx*4])
	}
	return out
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
