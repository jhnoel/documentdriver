package ops

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"image"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Image limits (docs/contract.md §3, Pictures).
const (
	maxImageFile  = 16 << 20
	maxImagePlan  = 64 << 20
	maxImagePixel = 16384
	mediaDir      = "/visio/media/"
)

// statImage and readImage reach the file system for image files (the
// caller's own paths, §5); tests replace them to fake a file at a
// media part's name.
var (
	statImage = os.Stat
	readImage = os.ReadFile
)

// imageInput is an image a plan names: the bytes of a local file
// (file), or a media part of the drawing (part, checked when the op
// runs).
type imageInput struct {
	file        bool
	data        []byte
	part        string
	contentType string
	ext         string
	compression string
	px          [2]int
}

// imageFormats are the formats vsdx writes, from the bytes' signature:
// the content type and extension are the MS-VSDX/OPC registrations
// (image/png, image/jpeg) the Visio-authored V18 sample uses for JPEG
// (Default Extension "jpeg"), and CompressionType the token Visio
// stores on ForeignData (V18: JPEG; PNG by the same pattern).
var imageFormats = []struct {
	sig              string
	contentType, ext string
	compression      string
}{
	{"\x89PNG\r\n\x1a\n", ctypes.CTImagePng, "png", "PNG"},
	{"\xff\xd8\xff", ctypes.CTImageJpeg, "jpeg", "JPEG"},
}

// CheckImages reads and checks every image file a plan names, and
// every crop, before the document opens (docs/contract.md §3,
// Pictures), returning every fault together: a file that is missing,
// unreadable or not regular is io; a file over 16 MiB, a format other
// than PNG or JPEG, a header that does not decode, a side over 16,384
// pixels, files over 64 MiB together, and a crop out of range are
// invalid_operation. A value under /visio/media/ that names no file is
// a media part, checked when the op runs.
func CheckImages(p *Plan) cli.Errors {
	if p == nil {
		return nil
	}
	var errs cli.Errors
	total := 0
	for i, s := range p.Ops {
		var img *string
		var crop []float64
		var dst **imageInput
		switch v := s.(type) {
		case *ObjectAddSpec:
			if v.Image != "" {
				img = &v.Image
			}
			crop, dst = v.Crop, &v.img
		case *ObjectSetSpec:
			img, crop, dst = v.Image, v.Crop, &v.img
		default:
			continue
		}
		at := cli.Index("ops", i)
		if *dst != nil {
			continue // checked already (ParsePlan, then apply.Run)
		}
		if crop != nil {
			if msg, bad := cropFault(crop); bad {
				errs = append(errs, cli.Errorf(cli.CodeInvalidOperation, cli.Join(at, "crop"), "%s", msg))
			}
		}
		if img == nil {
			continue
		}
		in, n, err := loadImage(*img)
		if err != nil {
			if ce, ok := errors.AsType[*cli.Error](err); ok {
				ce.Path = cli.Join(at, "image")
				errs = append(errs, ce)
			}
			continue
		}
		total += n
		if total > maxImagePlan {
			errs = append(errs, cli.Errorf(cli.CodeInvalidOperation, cli.Join(at, "image"), "the plan's image files hold more than 64 MiB together"))
			continue
		}
		*dst = in
	}
	return errs
}

// cropFault checks a crop [l, t, r, b]: each from 0 to under 100, l+r
// and t+b under 100.
func cropFault(c []float64) (string, bool) {
	if len(c) != 4 {
		return "crop must be [l, t, r, b]", true
	}
	for _, v := range c {
		if !(v >= 0 && v < 100) {
			return "each crop side must be from 0 to under 100 percent", true
		}
	}
	if c[0]+c[2] >= 100 || c[1]+c[3] >= 100 {
		return "crop l + r and t + b must be under 100 percent", true
	}
	return "", false
}

// loadImage reads and checks the file at value (a path as the
// operating system resolves it), or returns a media-part reference
// when value is under /visio/media/ and names no file.
func loadImage(value string) (*imageInput, int, error) {
	st, err := statImage(value)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && strings.HasPrefix(strings.ToLower(value), mediaDir) {
			return &imageInput{part: value}, 0, nil
		}
		return nil, 0, cli.Errorf(cli.CodeIO, "", "image %s: %v", value, err)
	}
	if !st.Mode().IsRegular() {
		return nil, 0, cli.Errorf(cli.CodeIO, "", "image %s is not a regular file", value)
	}
	if st.Size() > maxImageFile {
		return nil, 0, cli.Errorf(cli.CodeInvalidOperation, "", "image %s is over 16 MiB", value)
	}
	b, err := readImage(value)
	if err != nil {
		return nil, 0, cli.Errorf(cli.CodeIO, "", "image %s: %v", value, err)
	}
	in, err := sniffImage(b)
	if err != nil {
		return nil, 0, cli.Errorf(cli.CodeInvalidOperation, "", "image %s: %v", value, err)
	}
	in.file = true
	if strings.HasPrefix(strings.ToLower(value), mediaDir) {
		in.part = value
	}
	return in, len(b), nil
}

// sniffImage identifies PNG or JPEG bytes by signature and decodes
// the header's pixel size.
func sniffImage(b []byte) (*imageInput, error) {
	for _, f := range imageFormats {
		if !bytes.HasPrefix(b, []byte(f.sig)) {
			continue
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			return nil, errors.New("the header does not decode")
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImagePixel || cfg.Height > maxImagePixel {
			return nil, errors.New("width and height must be 1 to 16,384 pixels")
		}
		return &imageInput{data: b, contentType: f.contentType, ext: f.ext, compression: f.compression, px: [2]int{cfg.Width, cfg.Height}}, nil
	}
	return nil, errors.New("not a PNG or JPEG image (from the bytes' signature)")
}

// resolveImage completes a plan image against the drawing: a media
// part reference must name a PNG or JPEG part (not_found otherwise);
// a file whose value also names an existing part is refused.
func resolveImage(ws *edit.Workspace, in *imageInput) (*imageInput, error) {
	pkg := ws.Package()
	if in.file {
		if in.part != "" && pkg.Has(in.part) {
			return nil, cli.Errorf(cli.CodeInvalidOperation, "image", "%s names both a media part and a file", in.part)
		}
		return in, nil
	}
	if !pkg.Has(in.part) {
		return nil, cli.Errorf(cli.CodeNotFound, "image", "no media part %s and no such file", in.part)
	}
	pt, err := pkg.Part(in.part)
	if err != nil {
		return nil, &edit.Unreadable{Target: in.part, Detail: err.Error()}
	}
	var buf bytes.Buffer
	if _, err := pt.WriteTo(&buf); err != nil {
		return nil, &edit.Unreadable{Target: in.part, Detail: err.Error()}
	}
	out, err := sniffImage(buf.Bytes())
	if err != nil {
		return nil, cli.Errorf(cli.CodeOperationFailed, "image", "%s: %v", in.part, err)
	}
	out.part = in.part
	return out, nil
}

// mediaFor returns the media part holding in's bytes: an existing part
// under /visio/media/ with the same SHA-256 and content type, else a
// new /visio/media/imageN.<ext> (N one more than the largest imageN of
// any part or relationship target), with the content-type Default for
// the extension added when the drawing has none.
func mediaFor(ws *edit.Workspace, in *imageInput) (string, bool, error) {
	pkg := ws.Package()
	if !in.file {
		return in.part, false, nil
	}
	want := sha256.Sum256(in.data)
	largest := 0
	for _, name := range pkg.Names() {
		if n, ok := imageNumber(name); ok {
			largest = max(largest, n)
		}
		if !strings.HasPrefix(strings.ToLower(name), mediaDir) {
			continue
		}
		pt, err := pkg.Part(name)
		if err != nil {
			continue
		}
		if t, _ := pt.ContentType(); t != in.contentType {
			continue
		}
		h := sha256.New()
		if _, err := pt.WriteTo(h); err == nil && bytes.Equal(h.Sum(nil), want[:]) {
			return name, false, nil
		}
	}
	for _, name := range pkg.Names() {
		if !strings.HasSuffix(name, ".rels") {
			continue
		}
		source := relsSource(name)
		rels, err := pkg.Rels(source)
		if err != nil {
			continue
		}
		for _, rel := range rels.All() {
			if t, err := rels.TargetPart(rel); err == nil {
				if n, ok := imageNumber(t); ok {
					largest = max(largest, n)
				}
			}
		}
	}
	name := mediaDir + "image" + strconv.Itoa(largest+1) + "." + in.ext
	ct, err := pkg.ContentTypes()
	if err != nil {
		return "", false, err
	}
	if got, ok := ct.Default(in.ext); !ok || got != in.contentType {
		if !ok {
			if err := ct.SetDefault(in.ext, in.contentType); err != nil {
				return "", false, err
			}
		}
	}
	if err := ws.AddPart(name, in.data, in.contentType); err != nil {
		return "", false, err
	}
	return name, true, nil
}

// relsSource returns the source part of a relationships part name.
func relsSource(name string) string {
	dir, file := path.Split(name)
	dir = strings.TrimSuffix(dir, "_rels/")
	if dir == "/" && file == ".rels" {
		return "/"
	}
	return dir + strings.TrimSuffix(file, ".rels")
}

// imageNumber reports N of a /visio/media/imageN.<ext> name.
func imageNumber(name string) (int, bool) {
	rest, ok := strings.CutPrefix(strings.ToLower(name), mediaDir+"image")
	if !ok {
		return 0, false
	}
	num, _, _ := strings.Cut(rest, ".")
	n, err := strconv.Atoi(num)
	return n, err == nil && n >= 0
}

// pageImageRel returns the page part's image relationship to media,
// adding one with a relative target when the page has none.
func pageImageRel(ws *edit.Workspace, pagePart, media string) (string, error) {
	rels, err := ws.Package().Rels(pagePart)
	if err != nil {
		return "", err
	}
	for _, rel := range rels.ByType(ctypes.RelImage) {
		if t, err := rels.TargetPart(rel); err == nil && opc.PartKey(t) == opc.PartKey(media) {
			return rel.ID, nil
		}
	}
	return ws.AddRel(pagePart, ctypes.RelImage, relTarget(pagePart, media))
}

// relTarget is media relative to the folder of source.
func relTarget(source, media string) string {
	from := strings.Split(strings.Trim(path.Dir(source), "/"), "/")
	to := strings.Split(strings.Trim(media, "/"), "/")
	i := 0
	for i < len(from) && i < len(to)-1 && from[i] == to[i] {
		i++
	}
	return strings.Repeat("../", len(from)-i) + strings.Join(to[i:], "/")
}

// releaseMedia removes the page's image relationships among ids that
// no shape of the page uses any more, and each released target part
// that no relationship of the package targets; it returns the parts
// it removed.
func releaseMedia(ws *edit.Workspace, pagePart string, root *tree.Node, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	used := map[string]bool{}
	root.Walk(func(n *tree.Node) bool {
		if n.Local() == "ForeignData" {
			if id := foreignRelID(n); id != "" {
				used[id] = true
			}
		}
		return true
	})
	pkg := ws.Package()
	rels, err := pkg.Rels(pagePart)
	if err != nil {
		return nil, err
	}
	var targets []string
	for _, id := range ids {
		if used[id] {
			continue
		}
		rel, ok := rels.Get(id)
		if !ok || rel.Type != ctypes.RelImage {
			continue
		}
		if t, err := rels.TargetPart(rel); err == nil {
			targets = append(targets, t)
		}
		if err := ws.RemoveRel(pagePart, id); err != nil {
			return nil, err
		}
	}
	var removed []string
	for _, t := range targets {
		if targetedAnywhere(pkg, t) || !pkg.Has(t) {
			continue
		}
		if err := ws.DeletePart(t); err != nil {
			return nil, err
		}
		removed = append(removed, t)
	}
	return removed, nil
}

// targetedAnywhere reports whether any relationship of pkg targets
// part.
func targetedAnywhere(pkg *opc.Package, part string) bool {
	for _, name := range pkg.Names() {
		if !strings.HasSuffix(name, ".rels") {
			continue
		}
		rels, err := pkg.Rels(relsSource(name))
		if err != nil {
			continue
		}
		for _, rel := range rels.All() {
			if t, err := rels.TargetPart(rel); err == nil && opc.PartKey(t) == opc.PartKey(part) {
				return true
			}
		}
	}
	return false
}

// pictureRelIDs returns the image relationship ids the ForeignData
// elements under n use.
func pictureRelIDs(n *tree.Node) []string {
	var out []string
	n.Walk(func(c *tree.Node) bool {
		if c.Local() == "ForeignData" {
			if id := foreignRelID(c); id != "" && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		return true
	})
	return out
}

// cropCells returns the Img cells of crop c on a picture of width w
// and height h inches, in the corpus's formula forms:
// ImgWidth = Width*100/(100-l-r), ImgOffsetX = ImgWidth*(-l/100),
// ImgHeight = Height*100/(100-t-b), ImgOffsetY = ImgHeight*(-b/100).
func cropCells(c []float64, w, h float64) []edit.Cell {
	l, t, r, b := c[0], c[1], c[2], c[3]
	kw, kh := 100/(100-l-r), 100/(100-t-b)
	ox, oy := -l/100, -b/100
	iw, ih := w*kw, h*kh
	return []edit.Cell{
		{N: "ImgOffsetX", V: vsd.FormatInches(iw * ox), F: "ImgWidth*" + vsd.FormatInches(ox)},
		{N: "ImgOffsetY", V: vsd.FormatInches(ih * oy), F: "ImgHeight*" + vsd.FormatInches(oy)},
		{N: "ImgWidth", V: vsd.FormatInches(iw), F: "Width*" + vsd.FormatInches(kw)},
		{N: "ImgHeight", V: vsd.FormatInches(ih), F: "Height*" + vsd.FormatInches(kh)},
	}
}

// foreignDataXML is the ForeignData element of a picture, in the form
// of the Visio-authored V18 sample (ForeignType Bitmap and
// CompressionType, one Rel; MS-VSDX ForeignData_Type, visio.xsd
// lines 4298-4312).
func foreignDataXML(compression, relID string) []byte {
	return []byte("<ForeignData ForeignType='Bitmap' CompressionType='" + compression + "'><Rel r:id='" + relID + "'/></ForeignData>")
}

// foreignRelID returns the r:id of a ForeignData element's Rel child.
func foreignRelID(foreign *tree.Node) string {
	for c := range foreign.ElementsSeq() {
		if c.Local() == vsd.ElemRel {
			if v, ok := c.AttrNS(vsd.NSRel, "id"); ok {
				return v
			}
		}
	}
	return ""
}
