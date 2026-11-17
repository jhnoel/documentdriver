package ops

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// pageInfo is one page in stored order.
type pageInfo struct {
	id    uint32
	name  string
	nameU string
}

// currentPages lists pages in stored order from the core tree.
func currentPages(root *tree.Node) []pageInfo {
	var out []pageInfo
	for c := range root.ElementsSeq() {
		if c.Local() != vsd.ElemPage || !vsd.IsMain(c.Space()) {
			continue
		}
		id, ok := shapeIDOf(c)
		if !ok {
			continue
		}
		name, _ := c.Attr("Name")
		nameU, _ := c.Attr("NameU")
		out = append(out, pageInfo{id: id, name: name, nameU: nameU})
	}
	return out
}

// duplicateName reports whether name names a page already.
func duplicateName(pages []pageInfo, name string) bool {
	for _, p := range pages {
		if ascii.EqualFold(p.name, name) || ascii.EqualFold(p.nameU, name) {
			return true
		}
	}
	return false
}

// afterFor returns the after address for a page.add.
func afterFor(pages []pageInfo, at, position string) (string, error) {
	if at == "" || position != "before" {
		return at, nil
	}
	a, err := vsd.Parse(at)
	if err != nil {
		return "", err
	}
	idx := -1
	seen := 0
	for i, p := range pages {
		if p.id != a.Container {
			continue
		}
		seen++
		if seen == a.PageDup {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", fmt.Errorf("%w: %s", edit.ErrNotFound, at)
	}
	if idx == 0 {
		return "", cli.Errorf(cli.CodeOperationFailed, "position", "cannot add before the first page")
	}
	prev := pages[idx-1]
	dup := 1
	for i := 0; i < idx-1; i++ {
		if pages[i].id == prev.id {
			dup++
		}
	}
	return vsd.PageID(prev.id, dup), nil
}

// addSize returns a new page's size in inches and its print value.
func addSize(s *PageAddSpec, units string) (float64, float64, int) {
	if s.Size != nil {
		wIn, hIn := sizeInches(s.Size, units)
		if s.Orientation != "" {
			return wIn, hIn, orientationInt(s.Orientation)
		}
		return wIn, hIn, orientationInt(orientationOf(wIn, hIn))
	}
	if s.Orientation == "landscape" {
		return 11, 8.5, 2
	}
	return 8.5, 11, 1
}

// Run applies page.add.
func (s *PageAddSpec) Run(ws *edit.Workspace, aliases map[string]string, units string, _ ids.Source) (string, []string, []string, string, error) {
	if s.Name == "" {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "name", "name must not be empty")
	}
	if s.Size != nil && !sizeOkay(s.Size) {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "size", "size must be two positive numbers")
	}
	if s.Orientation != "" && s.Orientation != "portrait" && s.Orientation != "landscape" {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "orientation", "orientation must be portrait or landscape")
	}
	if s.Size != nil && s.Orientation != "" {
		wIn, hIn := sizeInches(s.Size, units)
		if orientationOf(wIn, hIn) != s.Orientation {
			return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "orientation", "size [%v %v] is %s, not %s", s.Size[0], s.Size[1], orientationOf(wIn, hIn), s.Orientation)
		}
	}
	var atAddr string
	if s.At != "" {
		addr, err := resolvePage(s.At, aliases)
		if err != nil {
			return "", nil, nil, "", err
		}
		atAddr = addr
	}
	d, r, err := openDoc(ws)
	if err != nil {
		return "", nil, nil, "", err
	}
	part, err := pagesPartName(ws, d)
	if err != nil {
		return "", nil, nil, "", err
	}
	pagesTree, err := ws.Core().Tree(part)
	if err != nil {
		return "", nil, nil, "", err
	}
	pages := currentPages(pagesTree.Root())
	if duplicateName(pages, s.Name) {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "name", "page name %q is already used", s.Name)
	}
	after, err := afterFor(pages, atAddr, s.Position)
	if err != nil {
		return "", nil, nil, "", err
	}
	wIn, hIn, desired := addSize(s, units)
	emitOrientation := 0
	if desired != 0 && desired != inheritedPrint(r) {
		emitOrientation = desired
	}
	addr, err := ws.AddPage(func(relID string) ([]byte, error) {
		return emit.PageXML(emit.Page{Name: s.Name, Width: wIn, Height: hIn, Orientation: emitOrientation, RelID: relID})
	}, after)
	if err != nil {
		return "", nil, nil, "", err
	}
	return addr, []string{addr}, nil, "", nil
}
