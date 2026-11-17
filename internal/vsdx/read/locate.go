package read

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PageInfo locates one page: its stored order, which is also its PDF
// page in LibreOffice's export, and its size in points.
type PageInfo struct {
	Page       string  // the page address, "page0"
	Index      int     // 0-based PDF page index, the page's stored order
	Count      int     // pages in stored order
	Background bool    // the page is a background page
	W, H       float64 // page size in points
}

// Locate maps address to its PageInfo. LibreOffice exports every page,
// background pages included, in pages.xml order (measured on V8: page0
// is PDF page 1 composited with its background page3, page1 is PDF
// page 2, and page3 is PDF page 3 on its own), so the index is the
// 0-based PDF page number. A non-page or unknown address is not_found
// at --page; a page whose size cannot be derived is an error.
func Locate(input []byte, address string) (PageInfo, error) {
	a, err := vsd.Parse(address)
	if err != nil {
		return PageInfo{}, cli.Errorf(cli.CodeNotFound, "--page", "%s", err)
	}
	if a.Kind != vsd.KindPage {
		return PageInfo{}, cli.Errorf(cli.CodeNotFound, "--page", "no page %s: not found", address)
	}
	pkg, d, r, err := openDoc(input)
	if err != nil {
		return PageInfo{}, err
	}
	defer pkg.Close()
	e, ok := pageEntry(d, a.Container, a.PageDup)
	if !ok {
		return PageInfo{}, cli.Errorf(cli.CodeNotFound, "--page", "no page %s: not found", address)
	}
	p := model.PageSheet(d, r, e)
	if !p.HasSize {
		return PageInfo{}, fmt.Errorf("vsdx: page %s has no page size", address)
	}
	info := PageInfo{Page: p.ID, Index: -1, Count: len(d.Pages), Background: p.Background,
		W: p.W * vsd.PointsPerInch, H: p.H * vsd.PointsPerInch}
	for i := range d.Pages {
		if &d.Pages[i] == e {
			info.Index = i
			break
		}
	}
	return info, nil
}
