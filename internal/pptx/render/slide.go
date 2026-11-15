package render

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/opc"
)

const (
	nsP   = "http://schemas.openxmlformats.org/presentationml/2006/main"
	nsR   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	emuPt = 12700.0
)

// SlideInfo locates one slide in its deck.
type SlideInfo struct {
	Part    string  // part name, e.g. "/ppt/slides/slide3.xml"
	Index   int     // 0-based position in sldIdLst
	Count   int     // number of slides in sldIdLst
	Hidden  bool    // the slide has show="0"
	Hiddens int     // number of hidden slides in the deck
	W, H    float64 // slide size in points (sldSz)
}

// Locate maps a slide name ("slide3", or a part name such as
// "/ppt/slides/slide3.xml") to its place in presentation.xml's sldIdLst and
// reads the slide size from sldSz.
func Locate(pkg *opc.Package, slide string) (SlideInfo, error) {
	want := slide
	if !strings.HasPrefix(want, "/") {
		want = "/ppt/slides/" + strings.TrimSuffix(want, ".xml") + ".xml"
	}
	root, err := pkg.Rels("/")
	if err != nil {
		return SlideInfo{}, err
	}
	var pres string
	if rs := root.ByType(ctypes.RelOfficeDocument); len(rs) > 0 {
		if pres, err = root.TargetPart(rs[0]); err != nil {
			return SlideInfo{}, err
		}
	}
	if pres == "" {
		return SlideInfo{}, fmt.Errorf("render: no presentation part in package")
	}
	pt, err := pkg.Part(pres)
	if err != nil {
		return SlideInfo{}, err
	}
	doc, err := pt.Tree()
	if err != nil {
		return SlideInfo{}, err
	}
	rels, err := pkg.Rels(pres)
	if err != nil {
		return SlideInfo{}, err
	}
	info := SlideInfo{Index: -1}
	sz := doc.Root().Child(nsP, "sldSz")
	if sz == nil {
		return info, fmt.Errorf("render: %s has no sldSz", pres)
	}
	cx, ok1 := emuAttr(sz.Attr("cx"))
	cy, ok2 := emuAttr(sz.Attr("cy"))
	if !ok1 || !ok2 || cx <= 0 || cy <= 0 {
		return info, fmt.Errorf("render: %s has an unreadable sldSz", pres)
	}
	info.W, info.H = cx/emuPt, cy/emuPt
	lst := doc.Root().Child(nsP, "sldIdLst")
	if lst == nil {
		return info, fmt.Errorf("render: %s has no slides", pres)
	}
	var names []string
	for e := range lst.ElementsSeq() {
		if !e.Is(nsP, "sldId") {
			continue
		}
		rid, _ := e.AttrNS(nsR, "id")
		rel, ok := rels.Get(rid)
		if !ok {
			return info, fmt.Errorf("render: sldId %s has no relationship", rid)
		}
		part, err := rels.TargetPart(rel)
		if err != nil {
			return info, err
		}
		if strings.EqualFold(part, want) && info.Index < 0 {
			info.Index = len(names)
			info.Part = part
		}
		names = append(names, part)
	}
	info.Count = len(names)
	if info.Index < 0 {
		return info, fmt.Errorf("render: slide %q is not in the presentation (slides: %s)", slide, strings.Join(shortNames(names), ", "))
	}
	for i, n := range names {
		sp, err := pkg.Part(n)
		if err != nil {
			return info, err
		}
		d, err := sp.Tree()
		if err != nil {
			return info, err
		}
		if v, _ := d.Root().Attr("show"); v == "0" || v == "false" {
			info.Hiddens++
			if i == info.Index {
				info.Hidden = true
			}
		}
	}
	return info, nil
}

// shortNames returns each part's base name without its .xml suffix.
func shortNames(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSuffix(path.Base(p), ".xml")
	}
	return out
}

// emuAttr parses attribute value v as an integer EMU count, reporting success.
func emuAttr(v string, ok bool) (float64, bool) {
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return float64(n), err == nil
}
