package read

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/casefold"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// DefaultLimit is find's default --limit.
const DefaultLimit = 10000

// FindRequest is a find command.
type FindRequest struct {
	Query      string
	IgnoreCase bool // casefold.Simple on both sides
	Page       string
	Limit      int // 0: no limit
	// Checkpoint, when not nil, is passed to model.VisitPage's
	// Checkpoint: the memory gate's mark inside the streamed page
	// walk (chunk RM2). The CLI leaves it nil.
	Checkpoint func(name string)
}

// FindResult is find's output.
type FindResult struct {
	Base      string   `json:"base"`
	Matches   []Match  `json:"matches"`
	Truncated *FindCut `json:"truncated,omitempty"`
	Notes     []Note   `json:"notes"`
}

// Match is one match: the shape (its text matched) or the shape data
// row (its value matched), and that text as read shows it.
type Match struct {
	At   string `json:"at"`
	Text string `json:"text"`
}

// FindCut names the last match returned when --limit cut the search.
type FindCut struct {
	After string `json:"after"`
}

// Find searches the shapes' effective text and shape data values of
// every page in pages.xml order (or one page), one page tree at a time.
// The empty query matches every shape with text and every row with a
// value. Chunk R3.
func Find(input []byte, base string, req FindRequest) (*FindResult, error) {
	pkg, d, r, err := openDoc(input)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	res := &FindResult{Base: base, Matches: []Match{}, Notes: []Note{}}
	for _, n := range d.Notes {
		res.Notes = append(res.Notes, Note{At: n.At, Reason: n.Reason})
	}
	f := &finder{req: req, res: res, folded: req.Query}
	if req.IgnoreCase {
		f.folded = casefold.Simple(req.Query)
	}
	if req.Page != "" {
		e, err := findPage(d, req.Page)
		if err != nil {
			return nil, err
		}
		if err := f.scanPage(d, r, e, true); err != nil {
			return nil, err
		}
	} else {
		for i := range d.Pages {
			if f.cut {
				break
			}
			if err := f.scanPage(d, r, &d.Pages[i], false); err != nil {
				return nil, err
			}
		}
	}
	for _, n := range r.Notes() {
		res.Notes = append(res.Notes, Note{At: n.At, Reason: n.Reason})
	}
	return res, nil
}

// XML returns the exact source bytes of the addressed shape (page or
// master shape), its nested shapes included. Chunk R3.
func XML(input []byte, address string) ([]byte, error) {
	a, err := vsd.Parse(address)
	if err != nil {
		return nil, cli.Errorf(cli.CodeNotFound, "", "%s", err)
	}
	if a.Kind != vsd.KindShape {
		return nil, noObject(address)
	}
	pkg, d, r, err := openDoc(input)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	var page *model.Page
	if a.InMaster {
		m, ok := d.Master(a.Container)
		if !ok {
			return nil, noObject(address)
		}
		page, err = model.BuildMaster(d, r, m, model.Options{})
		if err != nil {
			return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Message: "the master cannot be read", Detail: err.Error(), Cause: err}
		}
	} else {
		e, ok := pageEntry(d, a.Container, a.PageDup)
		if !ok {
			return nil, noObject(address)
		}
		page, err = model.BuildPage(d, r, e, model.Options{})
		if err != nil {
			return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Message: "the page cannot be read", Detail: err.Error(), Cause: err}
		}
	}
	for _, o := range page.Objects {
		if o.ID == address && o.Node != nil {
			return o.Node.Bytes(), nil
		}
	}
	return nil, noObject(address)
}

// noObject reports that address names no object, in pptx's form: the
// short id and the page or master it sits on ("no object s9999 on
// page0"), or the plain not-found form when the address has no
// container to name.
func noObject(address string) error {
	if i := strings.LastIndex(address, "/"); i > 0 {
		return cli.Errorf(cli.CodeNotFound, "", "no object %s on %s", address[i+1:], address[:i])
	}
	return cli.Errorf(cli.CodeNotFound, "", "no object %s: not found", address)
}

// openDoc opens the package in input, its document structure and the
// inheritance resolver over it, as Document does. The caller closes
// the package.
func openDoc(input []byte) (*opc.Package, *doc.Document, *inherit.Resolver, error) {
	pkg, err := opc.OpenBytes(input, opc.DefaultLimits())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("vsdx: %w", err)
	}
	d, err := doc.Open(pkg)
	if err != nil {
		_ = pkg.Close()
		return nil, nil, nil, err // doc.Open's errors already carry one "vsdx:" prefix
	}
	return pkg, d, inherit.New(d), nil
}

// findPage resolves req.Page to the page entry it names, counting
// duplicate page IDs as read addresses them (page5, page5~2). A page
// address that parses to anything else, or names no page, is
// not_found.
func findPage(d *doc.Document, page string) (*doc.PageEntry, error) {
	a, err := vsd.Parse(page)
	if err != nil {
		return nil, cli.Errorf(cli.CodeNotFound, "--page", "%s", err)
	}
	if a.Kind != vsd.KindPage {
		return nil, cli.Errorf(cli.CodeNotFound, "--page", "no page %q in the drawing's page list", page)
	}
	e, ok := pageEntry(d, a.Container, a.PageDup)
	if !ok {
		return nil, cli.Errorf(cli.CodeNotFound, "--page", "no page %q in the drawing's page list", page)
	}
	return e, nil
}

// pageEntry returns the dup-th page of d with stored ID id (1 for the
// first), and false when there is none.
func pageEntry(d *doc.Document, id uint32, dup int) (*doc.PageEntry, bool) {
	n := 0
	for i := range d.Pages {
		if d.Pages[i].ID != id {
			continue
		}
		n++
		if n == dup {
			return &d.Pages[i], true
		}
	}
	return nil, false
}

// finder is one Find's state: the request, the result being filled, the
// query folded once when the search ignores case, and cut, which says
// the limit stopped the search.
type finder struct {
	req    FindRequest
	res    *FindResult
	folded string
	cut    bool
}

// scanPage streams one page through model.VisitPage and matches its
// objects in paint order (a shape before its shape data rows),
// dropping each object after it is visited: Find holds no page
// tree and no model between pages, only the matches already
// recorded. With single set, a page that cannot be read is the
// command's error (unreadable_target); otherwise it is skipped
// with a note. The page's notes are merged at the place the
// --limit cut note (if any) was added, as the BuildPage scan
// recorded them before matching.
func (f *finder) scanPage(d *doc.Document, r *inherit.Resolver, e *doc.PageEntry, single bool) error {
	noteStart := len(f.res.Notes)
	pageNotes, err := model.VisitPage(d, r, e, model.Options{Checkpoint: f.req.Checkpoint}, func(o *model.Object) bool {
		if f.cut {
			return false
		}
		if o.HasText && f.match(o.Text) && f.add(o.ID, o.Text) {
			return false
		}
		for _, pr := range o.Props {
			if f.cut {
				return false
			}
			if text := propText(pr); text != "" && f.match(text) && f.add(pr.ID, text) {
				return false
			}
		}
		return true
	})
	if err != nil {
		if single {
			return &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--page", Message: "the page cannot be read", Detail: err.Error(), Cause: err}
		}
		f.res.Notes = append(f.res.Notes, Note{At: model.PageSheet(d, r, e).ID, Reason: "unreadable: " + err.Error()})
		return nil
	}
	var notes []Note
	for _, n := range pageNotes {
		notes = append(notes, Note{At: n.At, Reason: n.Reason})
	}
	if f.cut && len(f.res.Notes) > noteStart {
		f.res.Notes = slices.Insert(f.res.Notes, noteStart, notes...)
	} else {
		f.res.Notes = append(f.res.Notes, notes...)
	}
	return nil
}

// match reports whether text contains the query, folding text first
// when the search ignores case.
func (f *finder) match(text string) bool {
	if !f.req.IgnoreCase {
		return strings.Contains(text, f.folded)
	}
	return strings.Contains(casefold.Simple(text), f.folded)
}

// add records a match at at with text, and reports whether the limit
// cuts the search. A match beyond the first Limit is not recorded:
// the cut names the last match returned, and exactly Limit matches is
// not a cut.
func (f *finder) add(at, text string) bool {
	if f.req.Limit > 0 && len(f.res.Matches) >= f.req.Limit {
		after := f.res.Matches[len(f.res.Matches)-1].At
		f.res.Truncated = &FindCut{After: after}
		f.res.Notes = append(f.res.Notes, Note{At: after, Reason: fmt.Sprintf("search stopped after %d matches at --limit", len(f.res.Matches))})
		f.cut = true
		return true
	}
	f.res.Matches = append(f.res.Matches, Match{At: at, Text: text})
	return false
}

// propText returns a shape data row's value as read prints it, without
// JSON quoting: a string itself, a number in its shortest decimal
// form, a boolean true or false, and a date its stored text. A row
// with no value gives "".
func propText(p model.Prop) string {
	switch v := p.Value.(type) {
	case string:
		return v
	case float64:
		return vsd.FormatInches(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case map[string]string:
		return v["date"]
	}
	return ""
}
