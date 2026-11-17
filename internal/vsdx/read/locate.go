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
// at --page; a page whose size cannot be derived is an error; a page
// that cannot be read (its part is missing or not well formed) is
// unreadable_target at --page, as read refuses it, so a caller that
// renders the page refuses it before any rendering.
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
	if _, err := model.BuildPage(d, r, e, model.Options{}); err != nil {
		return PageInfo{}, &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--page", Message: "the page cannot be read", Detail: err.Error(), Cause: err}
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

// PageModel returns the read model of one page (or master) of input,
// with its objects kept by o.IDs and o.Region, as Page shows them but
// without the JSON. An unknown address is not_found; a page that
// cannot be read is unreadable_target at --page (at --master for a
// master), as Page reports it.
func PageModel(input []byte, address string, o Options) (*model.Page, error) {
	a, err := vsd.Parse(address)
	if err != nil {
		return nil, cli.Errorf(cli.CodeNotFound, "", "%s", err)
	}
	if a.Kind != vsd.KindPage && a.Kind != vsd.KindMaster {
		return nil, cli.Errorf(cli.CodeNotFound, "", "no page %s: not found", address)
	}
	pkg, d, r, err := openDoc(input)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	mo := model.Options{Sites: o.Detail == "sites" || o.Detail == "full", Full: o.Detail == "full"}
	var p *model.Page
	if a.Kind == vsd.KindMaster {
		m, ok := d.Master(a.Container)
		if !ok {
			return nil, cli.Errorf(cli.CodeNotFound, "--master", "no master %s: not found", address)
		}
		p, err = model.BuildMaster(d, r, m, mo)
		if err != nil {
			return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--master", Message: "the master cannot be read", Detail: err.Error(), Cause: err}
		}
	} else {
		e, ok := pageEntry(d, a.Container, a.PageDup)
		if !ok {
			return nil, cli.Errorf(cli.CodeNotFound, "--page", "no page %s: not found", address)
		}
		p, err = model.BuildPage(d, r, e, mo)
		if err != nil {
			return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--page", Message: "the page cannot be read", Detail: err.Error(), Cause: err}
		}
	}
	p.Objects = selectObjects(p, o)
	return p, nil
}
