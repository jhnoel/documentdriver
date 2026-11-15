package main

import (
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/render"
	"github.com/jhnoel/documentdriver/internal/pptx/view"
)

// slideMarks reads slide part of the deck into the model and converts it.
func slideMarks(deck, part string, ids []string) ([]render.Mark, error) {
	p, err := opc.Open(deck)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		return nil, err
	}
	s, err := d.ReadSlide(part)
	if err != nil {
		return nil, err
	}
	return marksFromModel(s, ids), nil
}

// marksFromModel converts the read model to render marks: slide-space boxes
// and rotation for every object (group children included), the derived
// connection sites (the slice index is the site index pptx read reports),
// and, for connectors, the path from the derived end points with a glue end
// wherever stCxn or endCxn is present. A connector is marked by its path
// rather than its box: the box of a straight or elbow connector is mostly
// empty space and says nothing about where the ends are. Objects without a
// derived box have no outline but keep their sites. ids filters as
// pptx read --ids does.
func marksFromModel(s *model.Slide, ids []string) []render.Mark {
	objs := s.Objects
	if len(ids) > 0 {
		objs = view.Select(s, ids)
	}
	var marks []render.Mark
	for _, o := range objs {
		m := render.Mark{ID: o.ID}
		if o.Box != nil {
			m.Box = [4]float64{pt(o.Box.X), pt(o.Box.Y), pt(o.Box.W), pt(o.Box.H)}
			m.Rot = o.Box.Rot
		}
		for _, st := range o.Sites {
			if st.Index != len(m.Sites) {
				break // indices are positional in render; never mislabel
			}
			m.Sites = append(m.Sites, [2]float64{pt(st.X), pt(st.Y)})
		}
		if c := o.Conn; c != nil {
			m.Path = connPath(c)
			if c.Start != nil {
				m.Ends = append(m.Ends, ptPair(c.From))
			}
			if c.End != nil {
				m.Ends = append(m.Ends, ptPair(c.To))
			}
		} else if o.Box == nil {
			if len(m.Sites) == 0 {
				continue
			}
			m.Box = [4]float64{} // zero box: nothing to outline
		}
		marks = append(marks, m)
	}
	return marks
}

// connPath returns a connector's path as point coordinates, sampling curves.
func connPath(c *model.Conn) [][2]float64 {
	path := [][2]float64{ptPair(c.From)}
	switch {
	case len(c.Curve) >= 4:
		// start, then (c1, c2, end) per cubic segment.
		p0 := c.Curve[0]
		for i := 1; i+2 < len(c.Curve); i += 3 {
			p1, p2, p3 := c.Curve[i], c.Curve[i+1], c.Curve[i+2]
			for k := 1; k <= 16; k++ {
				t := float64(k) / 16
				u := 1 - t
				path = append(path, ptPair(derive.Pt{
					X: u*u*u*p0.X + 3*u*u*t*p1.X + 3*u*t*t*p2.X + t*t*t*p3.X,
					Y: u*u*u*p0.Y + 3*u*u*t*p1.Y + 3*u*t*t*p2.Y + t*t*t*p3.Y,
				}))
			}
			p0 = p3
		}
		return path
	default:
		for _, e := range c.Elbows {
			path = append(path, ptPair(e))
		}
	}
	return append(path, ptPair(c.To))
}

// pt converts an EMU length to points.
func pt(emu float64) float64 { return emu / model.EMUPerPoint }

// ptPair returns a point's coordinates converted from EMU to points.
func ptPair(p derive.Pt) [2]float64 { return [2]float64{pt(p.X), pt(p.Y)} }
