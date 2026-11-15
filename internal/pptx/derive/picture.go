package derive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // decoders for DecodeConfig: PNG, JPEG and GIF media
	_ "image/jpeg"
	_ "image/png"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Pictures: the blip fill of a p:pic (crop, fill mode), its locks and the
// facts the media bytes give. The read model and the picture codecs read
// through these functions, so the value shown and the value a write
// compares are the same.

// SVGBlipExt is the extension URI under which PowerPoint stores the SVG
// of a picture (asvg:svgBlip) inside the main a:blip, whose own r:embed is
// the raster fallback. Taken from PowerPoint 16 decks in the corpus
// (ShapeCrawler's 059_crop-images.pptx).
const SVGBlipExt = "{96DAC541-7B7A-43D3-8B79-37D633B846F1}"

// SVGNS is the namespace of asvg:svgBlip.
const SVGNS = "http://schemas.microsoft.com/office/drawing/2016/SVG/main"

// RectSides are the attributes of a:srcRect and a:fillRect
// (CT_RelativeRect) in the order the views show them.
var RectSides = [4]string{"l", "t", "r", "b"}

// PicFill is a picture's blip fill as stored.
type PicFill struct {
	// Crop is a:srcRect's l, t, r, b in 1000ths of a percent of the
	// image (ST_Percentage; absent attributes are 0, the schema default),
	// nil when there is no a:srcRect.
	Crop *[4]int64
	// Fit is the fill mode: "stretch" or "tile", "" when the blip fill
	// holds neither.
	Fit string
	// FillRect is a:stretch/a:fillRect's l, t, r, b in 1000ths of a
	// percent of the box, nil when the stretch has no a:fillRect.
	FillRect *[4]int64
}

var percentRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?%$`)

// ParsePercent reads an a:ST_Percentage value: an integer in 1000ths of a
// percent, or the strict form "33.333%". A strict value finer than
// 0.001% is an error rather than rounded.
func ParsePercent(s string) (int64, error) {
	if v, err := strconv.ParseInt(s, 10, 32); err == nil {
		return v, nil
	}
	if percentRe.MatchString(s) {
		f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err == nil {
			v := math.Round(f * 1000)
			if math.Abs(v-f*1000) < 1e-6 && math.Abs(v) <= math.MaxInt32 {
				return int64(v), nil
			}
		}
	}
	return 0, fmt.Errorf("%q is not an ST_Percentage (1000ths of a percent)", s)
}

// readRelRect reads a CT_RelativeRect; absent attributes are 0, the
// schema default.
func readRelRect(n *tree.Node) (*[4]int64, error) {
	var r [4]int64
	for i, a := range RectSides {
		s, ok := n.Attr(a)
		if !ok {
			continue
		}
		v, err := ParsePercent(s)
		if err != nil {
			return nil, &AttrError{Elem: n.Name(), Attr: a, Value: s, Err: err}
		}
		r[i] = v
	}
	return &r, nil
}

// ReadPicFill reads a p:blipFill: its a:srcRect, its fill mode and the
// stretch's a:fillRect.
func ReadPicFill(blipFill *tree.Node) (PicFill, error) {
	var f PicFill
	if blipFill == nil {
		return f, errors.New("no p:blipFill")
	}
	var err error
	if sr := Child(blipFill, DML, "srcRect"); sr != nil {
		if f.Crop, err = readRelRect(sr); err != nil {
			return f, err
		}
	}
	switch {
	case Child(blipFill, DML, "stretch") != nil:
		f.Fit = "stretch"
		if fr := Child(Child(blipFill, DML, "stretch"), DML, "fillRect"); fr != nil {
			if f.FillRect, err = readRelRect(fr); err != nil {
				return f, err
			}
		}
	case Child(blipFill, DML, "tile") != nil:
		f.Fit = "tile"
	}
	return f, nil
}

// ReadPicLocks reads a:picLocks under a p:cNvPicPr: every attribute
// without a prefix as an xsd:boolean flag, by name. No a:picLocks is no
// flags; an unreadable flag is an error, never false.
func ReadPicLocks(cNvPicPr *tree.Node) (map[string]bool, error) {
	out := map[string]bool{}
	pl := Child(cNvPicPr, DML, "picLocks")
	if pl == nil {
		return out, nil
	}
	for _, a := range pl.Attrs() {
		if strings.Contains(a.Name, ":") || a.Name == "xmlns" {
			continue
		}
		v, err := AttrBool(pl, a.Name)
		if err != nil {
			return nil, err
		}
		out[a.Name] = v
	}
	return out, nil
}

// TrueLocks lists the flags that are set, sorted by name.
func TrueLocks(m map[string]bool) []string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// SVGBlip returns the asvg:svgBlip of a blip's SVG extension, or nil.
func SVGBlip(blip *tree.Node) *tree.Node {
	if e := BlipExt(blip, SVGBlipExt); e != nil {
		for c := range e.ElementsSeq() {
			if c.Space() == SVGNS && c.Local() == "svgBlip" {
				return c
			}
		}
	}
	return nil
}

// BlipExt returns the a:ext with the given uri in a blip's a:extLst, or
// nil.
func BlipExt(blip *tree.Node, uri string) *tree.Node {
	lst := Child(blip, DML, "extLst")
	if lst == nil {
		return nil
	}
	for e := range lst.ElementsSeq() {
		if u, _ := e.Attr("uri"); Is(e, DML, "ext") && u == uri {
			return e
		}
	}
	return nil
}

// MediaFormats describes the formats of picture images, keyed by the format named by the bytes: the
// extension PowerPoint gives such media parts and the content type it
// registers for that extension. Extensions and types are those of the
// PowerPoint-native corpus decks ($PPTX_CORPUS/manifest.jsonl, 1024
// decks): Default png image/png (180 decks), jpeg image/jpeg (948), gif
// image/gif (16), svg image/svg+xml (12).
var MediaFormats = map[string]struct{ Ext, ContentType string }{
	"png":  {"png", ctypes.CTImagePng},
	"jpeg": {"jpeg", ctypes.CTImageJpeg},
	"gif":  {"gif", ctypes.CTImageGif},
	"svg":  {"svg", ctypes.CTImageSvgXml},
}

// SniffImage names the format of image bytes from their signature: png,
// jpeg, gif, or svg for a well-formed XML document whose root is an SVG
// svg element. Anything else is "".
func SniffImage(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "gif"
	}
	if doc, err := tree.Parse(b); err == nil {
		if r := doc.Root(); r != nil && r.Local() == "svg" && r.Space() == "http://www.w3.org/2000/svg" {
			return "svg"
		}
	}
	return ""
}

// Media is what the bytes of a media part say: their SHA-256 and, for
// PNG, JPEG and GIF, the pixel size read from the header by the standard
// library's decoders.
type Media struct {
	SHA256 string
	Format string // SniffImage's name, "" when not recognised
	// W and H are the pixel size; HasPx is false when it is not derived
	// (SVG, other formats, or a header the decoder cannot read), with
	// the reason in PxReason.
	W, H     int
	HasPx    bool
	PxReason string
}

// ReadMedia derives the facts of media bytes.
func ReadMedia(b []byte) Media {
	sum := sha256.Sum256(b)
	m := Media{SHA256: hex.EncodeToString(sum[:]), Format: SniffImage(b)}
	switch m.Format {
	case "png", "jpeg", "gif":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			m.PxReason = "the " + m.Format + " header does not decode: " + err.Error()
			break
		}
		m.W, m.H, m.HasPx = cfg.Width, cfg.Height, true
	case "svg":
		m.PxReason = "an SVG has no pixel size"
	default:
		m.PxReason = "not a PNG, JPEG or GIF image"
	}
	return m
}
