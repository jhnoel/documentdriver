package view

import (
	"math"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// imageInfo is one picture site in run content (docxdriver's
// media::ImageInfo): its relationship id and display metadata.
type imageInfo struct {
	rid                 string
	width, height       uint32
	hasWidth, hasHeight bool
	alt                 string
	hasAlt              bool
}

// isImageContainer reports whether a run child can hold a picture.
func isImageContainer(n *tree.Node) bool {
	return model.IsW(n, "drawing") || model.IsW(n, "pict") || model.IsW(n, "object") || model.IsLocal(n, "AlternateContent")
}

// descendantElements returns all element descendants of n.
func descendantElements(n *tree.Node) []*tree.Node {
	return model.Descendants(n, model.IsElement)
}

// pictureInfo extracts the picture a container holds: the first a:blip
// (r:embed, else r:link) or v:imagedata (r:id), DrawingML extent and
// docPr alt text, else the VML shape's style size and alt or title. A
// container without a picture (a chart, a shape) gives false.
func pictureInfo(c *tree.Node) (imageInfo, bool) {
	var info imageInfo
	if model.IsLocal(c, "AlternateContent") {
		c = model.AlternateContentBranch(c)
		if c == nil {
			return info, false
		}
	}
	found := false
	var shape *tree.Node
	nodes := descendantElements(c)
	for _, n := range nodes {
		if model.IsLocal(n, "blip") {
			if v, ok := model.Attr(n, "embed"); ok {
				info.rid, found = v, true
				break
			}
			if v, ok := model.Attr(n, "link"); ok {
				info.rid, found = v, true
				break
			}
		}
		if model.IsLocal(n, "imagedata") {
			if v, ok := model.Attr(n, "id"); ok {
				info.rid, found = v, true
				shape = n.Parent()
				break
			}
		}
	}
	if !found {
		return info, false
	}
	for _, n := range nodes {
		if model.IsLocal(n, "extent") && !info.hasWidth {
			info.width, info.hasWidth = emuToPx(model.AttrOr(n, "cx"), hasAttr(n, "cx"))
			info.height, info.hasHeight = emuToPx(model.AttrOr(n, "cy"), hasAttr(n, "cy"))
		}
		if model.IsLocal(n, "docPr") && !info.hasAlt {
			info.alt, info.hasAlt = firstNonEmpty(n, "descr", "name")
		}
	}
	if shape != nil {
		if style, ok := model.Attr(shape, "style"); ok && !info.hasWidth {
			info.width, info.hasWidth = styleDimensionPx(style, "width")
			info.height, info.hasHeight = styleDimensionPx(style, "height")
		}
		if !info.hasAlt {
			info.alt, info.hasAlt = firstNonEmpty(shape, "alt", "title")
		}
	}
	return info, true
}

// hasAttr reports whether n has attribute local.
func hasAttr(n *tree.Node, local string) bool {
	_, ok := model.Attr(n, local)
	return ok
}

// firstNonEmpty returns the first of the attributes that is present and
// not empty.
func firstNonEmpty(n *tree.Node, names ...string) (string, bool) {
	for _, a := range names {
		if v, ok := model.Attr(n, a); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

// roundPx rounds a positive pixel size half away from zero, at least 1,
// saturating like Rust's float to u32 cast.
func roundPx(px float64) uint32 {
	r := math.Max(math.Round(px), 1)
	if r >= math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(r)
}

// emuToPx converts an EMU length to pixels, reporting false for missing or non-positive values.
func emuToPx(v string, ok bool) (uint32, bool) {
	if !ok {
		return 0, false
	}
	emu, ok := parseRustF64(v)
	if !ok || !(emu > 0) {
		return 0, false
	}
	return roundPx(emu / 9525), true
}

// styleDimensionPx reads one "name:123.4pt" entry (pt, in, cm, mm, px or
// no unit) of a VML style string. Like docxdriver, an entry without a
// colon met before the name ends the search.
func styleDimensionPx(style, name string) (uint32, bool) {
	for entry := range strings.SplitSeq(style, ";") {
		key, value, ok := strings.Cut(entry, ":")
		if !ok {
			return 0, false
		}
		if rustTrim(key) != name {
			continue
		}
		value = rustTrim(value)
		split := strings.IndexFunc(value, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
		if split < 0 {
			split = len(value)
		}
		number, ok := parseRustF64(value[:split])
		if !ok {
			return 0, false
		}
		var px float64
		switch value[split:] {
		case "pt":
			px = number * 96 / 72
		case "in":
			px = number * 96
		case "cm":
			px = number * 96 / 2.54
		case "mm":
			px = number * 96 / 25.4
		case "", "px":
			px = number
		default:
			return 0, false
		}
		if !(px > 0) {
			return 0, false
		}
		return roundPx(px), true
	}
	return 0, false
}

// imageSources maps the part's image relationship ids to asset URLs (for
// targets that exist with an image extension) and to their target parts.
func imageSources(pkg *model.Package, part string) (urls, parts map[string]string) {
	urls, parts = map[string]string{}, map[string]string{}
	for _, r := range pkg.Rels(part) {
		if r.External() || !strings.HasSuffix(r.Type, "/image") || !r.HasType {
			continue
		}
		if !r.HasID || !r.HasTarget {
			continue
		}
		target := model.ResolveRelTarget(part, r.Target)
		parts[r.ID] = target
		if b, ok := pkg.Get(target); ok {
			if _, img := model.ImageMIME(target); img {
				urls[r.ID] = model.AssetURL(target, b)
			}
		}
	}
	return urls, parts
}
