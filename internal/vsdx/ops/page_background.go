package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// bgPage is one page in stored order with its background facts.
type bgPage struct {
	addr       string
	id         uint32
	background bool
	back       uint32
	hasBack    bool
}

// bgPages lists the pages of pages.xml in stored order with their
// Background and BackPage attributes, from the working tree.
func bgPages(ws *edit.Workspace) ([]bgPage, error) {
	root, err := ws.PagesRoot()
	if err != nil {
		return nil, err
	}
	var out []bgPage
	seen := map[uint32]int{}
	for c := range root.ElementsSeq() {
		if c.Local() != vsd.ElemPage || !vsd.IsMain(c.Space()) {
			continue
		}
		id, ok := shapeIDOf(c)
		if !ok {
			continue
		}
		seen[id]++
		p := bgPage{addr: vsd.PageID(id, seen[id]), id: id}
		if v, ok := c.Attr("Background"); ok {
			p.background, _ = vsd.Bool(v)
		}
		p.back, p.hasBack = backPageOf(c)
		out = append(out, p)
	}
	return out, nil
}

// backPageOf reads a Page element's BackPage ID.
func backPageOf(c *tree.Node) (uint32, bool) {
	v, ok := c.Attr("BackPage")
	if !ok {
		return 0, false
	}
	n, okN := vsd.Number(v)
	if !okN || n < 0 || n != float64(uint32(n)) {
		return 0, false
	}
	return uint32(n), true
}

// pageOrderMixed reports whether a foreground page comes after a
// background page in pages.
func pageOrderMixed(pages []bgPage) bool {
	sawBack := false
	for _, p := range pages {
		if p.background {
			sawBack = true
		} else if sawBack {
			return true
		}
	}
	return false
}

// checkPageOrder refuses (operation_failed at field) an op that
// left the stored order mixed when it was not mixed before.
func checkPageOrder(ws *edit.Workspace, before []bgPage, field string) error {
	if pageOrderMixed(before) {
		return nil
	}
	after, err := bgPages(ws)
	if err != nil {
		return err
	}
	if pageOrderMixed(after) {
		return cli.Errorf(cli.CodeOperationFailed, field, "foreground pages come before background pages: this order would put a foreground page after a background page")
	}
	return nil
}

// findBg returns the page of pages at addr.
func findBg(pages []bgPage, addr string) (bgPage, int, bool) {
	for i, p := range pages {
		if p.addr == addr {
			return p, i, true
		}
	}
	return bgPage{}, -1, false
}

// applyBackground applies page.add's or page.set's background and
// background_page to the page at addr (docs/contract.md §3,
// Background pages), kind first. It reports whether anything was
// written. back is a page address, "none", or "" when not given.
func applyBackground(ws *edit.Workspace, aliases map[string]string, addr string, background *bool, back string) (bool, error) {
	if background == nil && back == "" {
		return false, nil
	}
	pages, err := bgPages(ws)
	if err != nil {
		return false, err
	}
	cur, _, ok := findBg(pages, addr)
	if !ok {
		return false, cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", addr)
	}
	wrote := false
	if background != nil && *background != cur.background {
		if *background {
			fg := 0
			for _, p := range pages {
				if !p.background {
					fg++
				}
			}
			if fg <= 1 {
				return false, cli.Errorf(cli.CodeOperationFailed, "background", "%s is the last foreground page", addr)
			}
		} else {
			for _, p := range pages {
				if p.hasBack && p.back == cur.id && p.addr != addr {
					return false, cli.Errorf(cli.CodeOperationFailed, "background", "%s uses %s as its background: set its background_page to \"none\" first", p.addr, addr)
				}
			}
		}
		if err := ws.SetPageBackground(addr, background, nil, false); err != nil {
			return false, err
		}
		if err := moveForKind(ws, addr, *background); err != nil {
			return false, err
		}
		wrote = true
		if pages, err = bgPages(ws); err != nil {
			return false, err
		}
		cur, _, _ = findBg(pages, addr)
	}
	switch back {
	case "":
	case "none":
		if cur.hasBack {
			if err := ws.SetPageBackground(addr, nil, nil, true); err != nil {
				return false, err
			}
			wrote = true
		}
	default:
		target, err := ResolveAlias(back, aliases, "background_page")
		if err != nil {
			return false, err
		}
		a, err := vsd.Parse(target)
		if err != nil || a.Kind != vsd.KindPage || a.InMaster {
			return false, cli.Errorf(cli.CodeNotFound, "background_page", "%s is not a page of the drawing", target)
		}
		tp, _, found := findBg(pages, target)
		if !found {
			return false, cli.Errorf(cli.CodeNotFound, "background_page", "no %s: not found", target)
		}
		if target == addr {
			return false, cli.Errorf(cli.CodeOperationFailed, "background_page", "%s cannot use itself as its background", addr)
		}
		if !tp.background {
			return false, cli.Errorf(cli.CodeOperationFailed, "background_page", "%s is a foreground page: a background_page must be a background page", target)
		}
		for p, steps := tp, 0; p.hasBack && steps <= len(pages); steps++ {
			if p.back == cur.id {
				return false, cli.Errorf(cli.CodeOperationFailed, "background_page", "%s would make a cycle of background pages back to %s", target, addr)
			}
			next, ok := byID(pages, p.back)
			if !ok {
				break
			}
			p = next
		}
		if !cur.hasBack || cur.back != tp.id {
			id := tp.id
			if err := ws.SetPageBackground(addr, nil, &id, false); err != nil {
				return false, err
			}
			wrote = true
		}
	}
	return wrote, nil
}

// byID returns the first page with stored ID id.
func byID(pages []bgPage, id uint32) (bgPage, bool) {
	for _, p := range pages {
		if p.id == id {
			return p, true
		}
	}
	return bgPage{}, false
}

// moveForKind moves addr after changing its kind: a background page
// to the end, a foreground page after the last foreground page.
func moveForKind(ws *edit.Workspace, addr string, background bool) error {
	pages, err := bgPages(ws)
	if err != nil {
		return err
	}
	if background {
		last := pages[len(pages)-1]
		if last.addr != addr {
			_, err := ws.ReorderPage(addr, last.addr, "after")
			return err
		}
		return nil
	}
	lastFg := ""
	firstBg := ""
	for _, p := range pages {
		if p.addr == addr {
			continue
		}
		if !p.background {
			lastFg = p.addr
		} else if firstBg == "" {
			firstBg = p.addr
		}
	}
	switch {
	case lastFg != "":
		_, err = ws.ReorderPage(addr, lastFg, "after")
	case firstBg != "":
		_, err = ws.ReorderPage(addr, firstBg, "before")
	}
	return err
}
