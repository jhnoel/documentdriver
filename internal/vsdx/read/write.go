package read

import (
	"encoding/json"
	"io"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// WritePage reads one page (or master, for an address master<N>) of
// input and streams its JSON result to w in the layout Page uses:
// base, page, name, units, size, orientation, background,
// background_page, layers, objects, connections, hazards, notes, and
// a trailing newline. The head, each object and the tail are each
// marshalled once, through one reused cli.ObjectEncoder, so no
// output buffer is held and no object is marshalled twice (chunk
// RM3). An error before output starts keeps its code (not_found,
// unreadable_target); a marshal or write failure after the page is
// built is reported as io, as cli.WriteJSON reports it, because
// every value is a plain Go value and no marshal can fail for one.
func WritePage(w io.Writer, input []byte, base, address string, o Options) error {
	in := o.Units == "in"
	units := "pt"
	if in {
		units = "in"
	}
	full := o.Detail == "full"
	// ReleaseNodes is set on this (the CLI) path: BuildPage drops
	// every Object.Node once the glue pass is done, so the page tree
	// is not held while the page is marshalled. read.XML, edit and
	// the model tests keep the nodes.
	mo := model.Options{Sites: o.Detail == "sites" || full, Full: full, ReleaseNodes: true, Checkpoint: o.Checkpoint}
	a, err := vsd.Parse(address)
	if err != nil {
		return cli.Errorf(cli.CodeNotFound, "", "%s", err)
	}
	if a.Kind != vsd.KindPage && a.Kind != vsd.KindMaster {
		return cli.Errorf(cli.CodeNotFound, "", "no page %q in the drawing's page list", address)
	}
	pkg, d, r, err := openDoc(input)
	if err != nil {
		return err
	}
	defer pkg.Close()
	var p *model.Page
	if a.Kind == vsd.KindMaster {
		m, ok := d.Master(a.Container)
		if !ok {
			return cli.Errorf(cli.CodeNotFound, "--master", "no master %s: not found", address)
		}
		p, err = model.BuildMaster(d, r, m, mo)
		if err != nil {
			return &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--master", Message: "the master cannot be read", Detail: err.Error(), Cause: err}
		}
	} else {
		e, ok := pageEntry(d, a.Container, a.PageDup)
		if !ok {
			return cli.Errorf(cli.CodeNotFound, "--page", "no page %q in the drawing's page list", address)
		}
		p, err = model.BuildPage(d, r, e, mo)
		if err != nil {
			return &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--page", Message: "the page cannot be read", Detail: err.Error(), Cause: err}
		}
	}
	head := pageHeadJSON{Base: base, Page: p.ID, Name: p.Name, Units: units,
		Orientation: p.Orientation, PrintOrientation: p.PrintOrientation,
		Background: p.Background, BackgroundPage: p.BackPage,
		Layers: []layerJSON{}}
	if p.HasSize {
		if in {
			head.Size = []json.Number{json.Number(vsd.FormatInches(p.W)), json.Number(vsd.FormatInches(p.H))}
		} else {
			head.Size = []json.Number{json.Number(geom.FormatNum(p.W * vsd.PointsPerInch)), json.Number(geom.FormatNum(p.H * vsd.PointsPerInch))}
		}
	}
	for _, l := range p.Layers {
		head.Layers = append(head.Layers, layerJSON{ID: l.ID, Name: l.Name, Index: l.Index, Visible: l.Visible, Print: l.Print, Lock: l.Lock})
	}
	tail := pageTailJSON{Connections: []connectionJSON{}, Hazards: []hazardJSON{}, Notes: []Note{}}
	for _, c := range p.Connections {
		tail.Connections = append(tail.Connections, connectionJSON{Connector: c.Connector, From: c.From, To: c.To, FromGlue: c.FromGlue, ToGlue: c.ToGlue})
	}
	for _, h := range p.Hazards {
		tail.Hazards = append(tail.Hazards, hazardJSON{Kind: h.Kind, ID: h.ID, Detail: h.Detail})
	}
	for _, n := range d.Notes {
		tail.Notes = append(tail.Notes, Note{At: n.At, Reason: n.Reason})
	}
	for _, n := range p.Notes {
		tail.Notes = append(tail.Notes, Note{At: n.At, Reason: n.Reason})
	}
	for _, n := range r.Notes() {
		tail.Notes = append(tail.Notes, Note{At: n.At, Reason: n.Reason})
	}
	return writePageJSON(w, head, tail, selectObjects(p, o), in, full)
}

// writePageJSON streams a built page result to w: the marshalled head
// (without its closing brace), then the objects one by one, then the
// marshalled tail (without its opening brace), through one reused
// cli.ObjectEncoder, each value marshalled once. A marshal or write
// failure is io, as cli.WriteJSON reports one.
func writePageJSON(w io.Writer, head pageHeadJSON, tail pageTailJSON, objects []*model.Object, in, full bool) error {
	enc := cli.NewObjectEncoder(w)
	headB, err := enc.EncodeBuffered(head)
	if err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
	}
	if _, err := w.Write(headB[:len(headB)-1]); err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
	}
	if err := enc.WriteString(`,"objects":[`); err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
	}
	for i, obj := range objects {
		if i > 0 {
			if err := enc.WriteString(","); err != nil {
				return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
			}
		}
		if err := enc.Encode(objectJSONOf(obj, in, full)); err != nil {
			return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
		}
	}
	if err := enc.WriteString("],"); err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
	}
	tailB, err := enc.EncodeBuffered(tail)
	if err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
	}
	if _, err := w.Write(tailB[1:]); err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
	}
	if err := enc.WriteString("\n"); err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
	}
	return nil
}
