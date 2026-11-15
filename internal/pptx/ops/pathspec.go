package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
)

// PathSpec is a custom geometry's paths in the views' form: a string in
// the SVG path subset (M L H V C Q Z, absolute or relative) for one path
// with the default fill and stroke, or an array of {"d", "fill",
// "stroke"} objects, one per a:path. Coordinates are in the plan's units,
// relative to the shape's slide-space box (its top-left before rotation
// and flips), as the read view shows them.
type PathSpec struct {
	Items []PathItemSpec
}

// PathItemSpec is one path of a PathSpec.
type PathItemSpec struct {
	D      string      `json:"d"`
	Fill   Opt[string] `json:"fill"`
	Stroke Opt[bool]   `json:"stroke"`
}

// DecodePlan implements cli.PlanDecoder: a path string, or a list of
// {d, fill, stroke}, with the path of any fault inside it.
func (p *PathSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	if v.Kind == cli.String {
		p.Items = []PathItemSpec{{D: v.Str}}
		return
	}
	if v.Kind != cli.Array {
		d.Fail(cli.CodeInvalidPlan, path, v, "path: want a string or a list of {d, fill, stroke}")
		return
	}
	if len(v.Items) == 0 {
		d.Fail(cli.CodeInvalidPlan, path, v, "path: empty list; a custom geometry needs at least one path")
		return
	}
	d.Value(v, path, &p.Items)
}

// value parses the paths into the codec's value, lengths in EMU.
func (p PathSpec) value(u prop.Units) (prop.PathVal, error) {
	out := make(prop.PathVal, len(p.Items))
	for k, it := range p.Items {
		cmds, err := derive.ParsePath(it.D)
		if err != nil {
			return nil, fmt.Errorf("path %d: %w", k, err)
		}
		for c := range cmds {
			for j := range cmds[c].Pts {
				cmds[c].Pts[j] = derive.Pt{X: u.ToEMU(cmds[c].Pts[j].X), Y: u.ToEMU(cmds[c].Pts[j].Y)}
			}
		}
		out[k] = prop.PathItem{Cmds: cmds}
		if it.Fill.Set && it.Fill.V != "norm" {
			if err := edit.CheckAttr("a:CT_Path2D", "fill", it.Fill.V); err != nil {
				return nil, fmt.Errorf("path %d fill: %w", k, err)
			}
			out[k].Fill = it.Fill.V
		}
		if it.Stroke.Set {
			out[k].NoStroke = !it.Stroke.V
		}
	}
	return out, nil
}

// CustomSpec is a custom geometry's lists in the views' form ("custom"
// in --detail full): av and gd as [name, formula] pairs, ah as handles
// ({"ahXY": {attributes}, "pos": [x, y]}), cxn as [angle, x, y] sites and
// rect as [l, t, r, b]. Coordinates and angles are numbers (the plan's
// units relative to the box; degrees) or guide names (strings). Adjust
// values, guides and handles are read-only: they are written back as
// shown. A key left out keeps the list shown (as other fields do); an
// empty array empties a list.
type CustomSpec struct {
	Av   Opt[[][2]string]          `json:"av"`
	Gd   Opt[[][2]string]          `json:"gd"`
	Ah   Opt[json.RawMessage]      `json:"ah"`
	Cxn  Opt[[][3]json.RawMessage] `json:"cxn"`
	Rect Opt[[4]json.RawMessage]   `json:"rect"`
}

// value converts the lists into the codec's value, lengths in EMU.
func (c CustomSpec) value(u prop.Units, base prop.CustomVal) (prop.CustomVal, error) {
	v := base
	pairs := func(ps [][2]string) []derive.Adjust {
		var out []derive.Adjust
		for _, p := range ps {
			out = append(out, derive.Adjust{Name: p[0], Fmla: p[1]})
		}
		return out
	}
	if c.Av.Set {
		v.HasAv, v.Av = true, pairs(c.Av.V)
	}
	if c.Gd.Set {
		v.HasGd, v.Gd = true, pairs(c.Gd.V)
	}
	if c.Ah.Set {
		v.HasAh = true
		hs, err := handles(c.Ah.V)
		if err != nil {
			return v, err
		}
		v.Ah = hs
	}
	ref := func(raw json.RawMessage, conv func(float64) float64) (derive.ShownRef, error) {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if s == "" {
				return derive.ShownRef{}, errors.New("empty guide name")
			}
			return derive.ShownRef{Name: s}, nil
		}
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return derive.ShownRef{}, fmt.Errorf("%s: want a number or a guide name", raw)
		}
		return derive.ShownRef{V: conv(f)}, nil
	}
	deg := func(f float64) float64 { return f }
	if c.Cxn.Set {
		v.HasCxn = true
		v.Cxn = nil
		for k, s := range c.Cxn.V {
			var r [3]derive.ShownRef
			var err error
			if r[0], err = ref(s[0], deg); err != nil {
				return v, fmt.Errorf("custom cxn %d angle: %w", k, err)
			}
			for j := 1; j < 3; j++ {
				if r[j], err = ref(s[j], u.ToEMU); err != nil {
					return v, fmt.Errorf("custom cxn %d: %w", k, err)
				}
			}
			v.Cxn = append(v.Cxn, r)
		}
	}
	if c.Rect.Set {
		var r [4]derive.ShownRef
		for k, raw := range c.Rect.V {
			var err error
			if r[k], err = ref(raw, u.ToEMU); err != nil {
				return v, fmt.Errorf("custom rect: %w", err)
			}
		}
		v.Rect = &r
	}
	return v, nil
}

// handles reads the ah list keeping each handle's attribute order, so it
// can be compared with the list as written.
func handles(raw json.RawMessage) ([]derive.Handle, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("custom ah: want an array of handles: %w", err)
	}
	var out []derive.Handle
	for k, it := range items {
		d := json.NewDecoder(bytes.NewReader(it))
		var h derive.Handle
		if t, err := d.Token(); err != nil || t != json.Delim('{') {
			return nil, fmt.Errorf("custom ah %d: want {\"ahXY\": {...}, \"pos\": [x, y]}", k)
		}
		for d.More() {
			kt, err := d.Token()
			if err != nil {
				return nil, err
			}
			key := kt.(string)
			if key == "pos" {
				var pos [2]string
				if err := d.Decode(&pos); err != nil {
					return nil, fmt.Errorf("custom ah %d pos: %w", k, err)
				}
				h.Pos = pos
				continue
			}
			h.Kind = key
			if t, err := d.Token(); err != nil || t != json.Delim('{') {
				return nil, fmt.Errorf("custom ah %d %s: want an object of attributes", k, key)
			}
			for d.More() {
				at, err := d.Token()
				if err != nil {
					return nil, err
				}
				var val string
				if err := d.Decode(&val); err != nil {
					return nil, fmt.Errorf("custom ah %d %s: %w", k, at, err)
				}
				h.Attrs = append(h.Attrs, [2]string{at.(string), val})
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
		}
		out = append(out, h)
	}
	return out, nil
}
