package read

import (
	"bytes"
	"io"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/render"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/view"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// maxBackgroundDepth bounds the chain of background pages drawn under
// a page; a longer chain, or a cycle, stops with a note.
const maxBackgroundDepth = 8

// maxPictureBytes bounds the bytes of one picture the drawing view
// embeds; a larger picture is left out with a note.
const maxPictureBytes = 64 << 20

// PageScene builds the drawing view of one page (or master) of input:
// its objects, kept by o.IDs and o.Region as read keeps them, drawn
// from the read model (built with model.Options.Draw) over the
// objects of its background pages, and the notes on what the drawing
// leaves out or approximates. Errors are Page's: not_found for an
// unknown address, unreadable_target for a page that cannot be read.
func PageScene(input []byte, base, address string, o Options) (*render.Scene, []Note, error) {
	a, err := vsd.Parse(address)
	if err != nil {
		return nil, nil, cli.Errorf(cli.CodeNotFound, "", "%s", err)
	}
	if a.Kind != vsd.KindPage && a.Kind != vsd.KindMaster {
		return nil, nil, cli.Errorf(cli.CodeNotFound, "", "no page %q in the drawing's page list", address)
	}
	pkg, d, r, err := openDoc(input)
	if err != nil {
		return nil, nil, err
	}
	defer pkg.Close()
	mo := model.Options{Draw: true}
	var p *model.Page
	if a.Kind == vsd.KindMaster {
		m, ok := d.Master(a.Container)
		if !ok {
			return nil, nil, cli.Errorf(cli.CodeNotFound, "--master", "no master %s: not found", address)
		}
		if p, err = model.BuildMaster(d, r, m, mo); err != nil {
			return nil, nil, &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--master", Message: "the master cannot be read", Detail: err.Error(), Cause: err}
		}
	} else {
		e, ok := pageEntry(d, a.Container, a.PageDup)
		if !ok {
			return nil, nil, cli.Errorf(cli.CodeNotFound, "--page", "no page %s: not found", address)
		}
		if p, err = model.BuildPage(d, r, e, mo); err != nil {
			return nil, nil, &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--page", Message: "the page cannot be read", Detail: err.Error(), Cause: err}
		}
	}
	if !p.HasSize {
		return nil, nil, &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--page", Message: "the page has no size to draw"}
	}
	p.Objects = selectObjects(p, o)
	vo := view.Options{Media: mediaReader(pkg)}
	backs := backgrounds(d, r, p)
	scene, notes := view.Build(p, vo)
	var all []doc.Note
	// Background pages are drawn first, bottom-most first; their
	// notes come before the page's own.
	var under []render.Group
	for i := len(backs.pages) - 1; i >= 0; i-- {
		bs, bn := view.Build(backs.pages[i], vo)
		under = append(under, bs.Groups...)
		all = append(all, bn...)
	}
	all = append(all, backs.notes...)
	all = append(all, notes...)
	scene.Groups = append(under, scene.Groups...)
	scene.Attrs = [][2]string{{"data-base", base}, {"data-page", p.ID}, {"data-units", "pt"}}
	out := make([]Note, 0, len(all))
	for _, n := range all {
		out = append(out, Note{At: n.At, Reason: n.Reason})
	}
	j, err := cli.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	scene.Attrs = append(scene.Attrs, [2]string{"data-notes", string(j)})
	return scene, out, nil
}

// backgroundSet is the background pages under a page, nearest first,
// and the notes on the chain.
type backgroundSet struct {
	pages []*model.Page
	notes []doc.Note
}

// backgrounds builds the chain of background pages under p (its
// BackPage, that page's, and so on), each with all its objects.
func backgrounds(d *doc.Document, r *inherit.Resolver, p *model.Page) backgroundSet {
	var out backgroundSet
	seen := map[string]bool{p.ID: true}
	next := p.BackPage
	for depth := 0; next != ""; depth++ {
		if seen[next] || depth == maxBackgroundDepth {
			out.notes = append(out.notes, doc.Note{At: p.ID, Reason: "background page chain stops at " + next + " (a cycle or longer than 8)"})
			break
		}
		seen[next] = true
		a, err := vsd.Parse(next)
		if err != nil || a.Kind != vsd.KindPage {
			out.notes = append(out.notes, doc.Note{At: p.ID, Reason: "background page " + next + " not drawn"})
			break
		}
		e, ok := pageEntry(d, a.Container, a.PageDup)
		if !ok {
			out.notes = append(out.notes, doc.Note{At: p.ID, Reason: "background page " + next + " not found"})
			break
		}
		bp, err := model.BuildPage(d, r, e, model.Options{Draw: true})
		if err != nil {
			out.notes = append(out.notes, doc.Note{At: next, Reason: "background page cannot be read: " + err.Error()})
			break
		}
		out.pages = append(out.pages, bp)
		next = bp.BackPage
	}
	return out
}

// mediaReader returns a view.Media reading parts of pkg, at most
// maxPictureBytes each.
func mediaReader(pkg *opc.Package) view.Media {
	return func(part string) ([]byte, string, bool) {
		if !pkg.Has(part) {
			return nil, "", false
		}
		pt, err := pkg.Part(part)
		if err != nil {
			return nil, "", false
		}
		typ, _ := pt.ContentType()
		var buf bytes.Buffer
		lw := &limitWriter{w: &buf, left: maxPictureBytes}
		if _, err := pt.WriteTo(lw); err != nil || lw.over {
			return nil, "", false
		}
		return buf.Bytes(), typ, true
	}
}

// limitWriter writes at most left bytes to w and records whether more
// were offered.
type limitWriter struct {
	w    io.Writer
	left int
	over bool
}

// Write passes b on while it fits and fails once it does not.
func (l *limitWriter) Write(b []byte) (int, error) {
	if len(b) > l.left {
		l.over = true
		return 0, io.ErrShortWrite
	}
	l.left -= len(b)
	return l.w.Write(b)
}

// WritePageSVG writes the drawing view of one page (PageScene) to w as
// a self-contained SVG in points.
func WritePageSVG(w io.Writer, input []byte, base, address string, o Options) error {
	scene, _, err := PageScene(input, base, address, o)
	if err != nil {
		return err
	}
	return render.WriteSVG(w, scene)
}
