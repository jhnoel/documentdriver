package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// planField is one plan field of a spec, for decodeFields: its wire
// name, whether a rule of the operation's grammar requires it, and the
// decode of a present value.
type planField struct {
	Name     string
	Required bool
	Decode   func(*cli.Value, string, *cli.Decoder)
}

// decodeFields decodes the object v into the handlers fields at path:
// the op member is skipped, an unknown member and a missing required
// member are invalid_plan faults with the member's or the operation's
// path, and each present member is handed to its handler in text order.
// Only a missing required member is recorded here; a handler records
// the faults of its own value.
func decodeFields(v *cli.Value, path string, d *cli.Decoder, fields []planField) {
	if v.Kind != cli.Object {
		d.Fail(cli.CodeInvalidPlan, path, v, "expected an object, found %s", v.Kind)
		return
	}
	seen := make([]bool, len(fields))
	for _, m := range v.Members {
		if m.Key == "op" {
			continue
		}
		idx := -1
		for i, f := range fields {
			if f.Name == m.Key {
				idx = i
				break
			}
		}
		if idx < 0 {
			names := make([]string, len(fields))
			for i, f := range fields {
				names[i] = f.Name
			}
			e := cli.Errorf(cli.CodeInvalidPlan, cli.Join(path, m.Key), "unknown field %q", m.Key)
			e.Hint = "fields: " + strings.Join(names, ", ")
			d.Add(e, m.Value)
			continue
		}
		seen[idx] = true
		fields[idx].Decode(m.Value, cli.Join(path, m.Key), d)
	}
	for i, f := range fields {
		if !seen[i] && f.Required {
			d.Fail(cli.CodeInvalidPlan, cli.Join(path, f.Name), v, "missing field %q", f.Name)
		}
	}
}

// DecodePlan implements cli.PlanDecoder: "none" or {"hex": "#RRGGBB"},
// with an invalid_operation fault at hex for a colour that is not
// #RRGGBB and an invalid_plan fault at fill for any other form.
func (p *PaintSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	if v.Kind == cli.String {
		if v.Str != "none" {
			e := cli.Errorf(cli.CodeInvalidOperation, path, "fill %q: want \"none\" or a colour object", v.Str)
			d.Add(e, v)
			return
		}
		p.None = true
		p.Hex = ""
		return
	}
	if v.Kind != cli.Object {
		d.Fail(cli.CodeInvalidPlan, path, v, "expected \"none\" or an object, found %s", v.Kind)
		return
	}
	seen := false
	for _, m := range v.Members {
		if m.Key != "hex" {
			e := cli.Errorf(cli.CodeInvalidPlan, cli.Join(path, m.Key), "unknown field %q", m.Key)
			e.Hint = "fields: hex"
			d.Add(e, m.Value)
			continue
		}
		seen = true
		var hex string
		n := len(d.Errs())
		d.Value(m.Value, cli.Join(path, "hex"), &hex)
		if len(d.Errs()) > n {
			continue
		}
		upper, ok := vsd.Color(hex)
		if !ok {
			e := cli.Errorf(cli.CodeInvalidOperation, cli.Join(path, "hex"), "hex %q is not #RRGGBB", hex)
			d.Add(e, m.Value)
			continue
		}
		p.None = false
		p.Hex = upper
	}
	if !seen {
		d.Fail(cli.CodeInvalidPlan, cli.Join(path, "hex"), v, "missing field %q", "hex")
	}
}

// DecodePlan implements cli.PlanDecoder: "none" or an object with w, a
// colour (a string #RRGGBB or {"hex": "#RRGGBB"}) and dash (a string or
// a number, as stored), with an invalid_operation fault for a colour
// that is not #RRGGBB.
func (l *LineSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	if v.Kind == cli.String {
		if v.Str != "none" {
			e := cli.Errorf(cli.CodeInvalidOperation, path, "line %q: want \"none\" or an object", v.Str)
			d.Add(e, v)
			return
		}
		l.None = true
		l.W = nil
		l.Color = ""
		l.Dash = ""
		return
	}
	if v.Kind != cli.Object {
		d.Fail(cli.CodeInvalidPlan, path, v, "expected \"none\" or an object, found %s", v.Kind)
		return
	}
	l.None = false
	for _, m := range v.Members {
		switch m.Key {
		case "w":
			var w float64
			n := len(d.Errs())
			d.Value(m.Value, cli.Join(path, "w"), &w)
			if len(d.Errs()) > n {
				continue
			}
			if w < 0 {
				e := cli.Errorf(cli.CodeInvalidOperation, cli.Join(path, "w"), "w %v is negative", w)
				d.Add(e, m.Value)
				continue
			}
			l.W = &w
		case "color":
			decodeLineColor(l, m.Value, cli.Join(path, "color"), d)
		case "dash":
			switch m.Value.Kind {
			case cli.String:
				l.Dash = m.Value.Str
			case cli.Number:
				l.Dash = m.Value.Num
			default:
				d.Fail(cli.CodeInvalidPlan, cli.Join(path, "dash"), m.Value, "expected a string or a number, found %s", m.Value.Kind)
			}
		case "begin":
			l.Begin = decodeLineEnd(m.Value, cli.Join(path, "begin"), d)
		case "end":
			l.End = decodeLineEnd(m.Value, cli.Join(path, "end"), d)
		default:
			e := cli.Errorf(cli.CodeInvalidPlan, cli.Join(path, m.Key), "unknown field %q", m.Key)
			e.Hint = "fields: w, color, dash, begin, end"
			d.Add(e, m.Value)
		}
	}
}

// decodeLineEnd decodes one arrowhead: a string (a name, "none", or
// an index string) or {arrow, size}; size must be a number from 0 to
// 6 (invalid_operation otherwise). Whether the arrow is written is
// decided against the document when the op runs.
func decodeLineEnd(v *cli.Value, path string, d *cli.Decoder) *LineEndSpec {
	if v.Kind == cli.String {
		return &LineEndSpec{Arrow: v.Str}
	}
	if v.Kind != cli.Object {
		d.Fail(cli.CodeInvalidPlan, path, v, "expected \"none\", an arrow name or an object, found %s", v.Kind)
		return nil
	}
	e := &LineEndSpec{}
	seen := false
	for _, m := range v.Members {
		switch m.Key {
		case "arrow":
			n := len(d.Errs())
			d.Value(m.Value, cli.Join(path, "arrow"), &e.Arrow)
			seen = len(d.Errs()) == n
		case "size":
			var sz float64
			n := len(d.Errs())
			d.Value(m.Value, cli.Join(path, "size"), &sz)
			if len(d.Errs()) > n {
				continue
			}
			if sz < 0 || sz > 6 || sz != float64(int(sz)) {
				d.Add(cli.Errorf(cli.CodeInvalidOperation, cli.Join(path, "size"), "size %v: want a whole number from 0 to 6", sz), m.Value)
				continue
			}
			e.Size = &sz
		default:
			er := cli.Errorf(cli.CodeInvalidPlan, cli.Join(path, m.Key), "unknown field %q", m.Key)
			er.Hint = "fields: arrow, size"
			d.Add(er, m.Value)
		}
	}
	if !seen {
		d.Fail(cli.CodeInvalidPlan, cli.Join(path, "arrow"), v, "missing field %q", "arrow")
	}
	return e
}

// DecodePlan implements cli.PlanDecoder: "none" or {color,
// transparency, offset}; color as line.color, transparency 0 to 100,
// offset two finite numbers.
func (sh *ShadowSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	if v.Kind == cli.String {
		if v.Str != "none" {
			d.Add(cli.Errorf(cli.CodeInvalidOperation, path, "shadow %q: want \"none\" or an object", v.Str), v)
			return
		}
		*sh = ShadowSpec{None: true}
		return
	}
	if v.Kind != cli.Object {
		d.Fail(cli.CodeInvalidPlan, path, v, "expected \"none\" or an object, found %s", v.Kind)
		return
	}
	for _, m := range v.Members {
		switch m.Key {
		case "color":
			var l LineSpec
			decodeLineColor(&l, m.Value, cli.Join(path, "color"), d)
			sh.Color = l.Color
		case "transparency":
			var t float64
			n := len(d.Errs())
			d.Value(m.Value, cli.Join(path, "transparency"), &t)
			if len(d.Errs()) > n {
				continue
			}
			if t < 0 || t > 100 {
				d.Add(cli.Errorf(cli.CodeInvalidOperation, cli.Join(path, "transparency"), "transparency %v: want 0 to 100", t), m.Value)
				continue
			}
			sh.Transparency = &t
		case "offset":
			var off []float64
			n := len(d.Errs())
			d.Value(m.Value, cli.Join(path, "offset"), &off)
			if len(d.Errs()) > n {
				continue
			}
			if len(off) != 2 || !addFinite(off) {
				d.Add(cli.Errorf(cli.CodeInvalidOperation, cli.Join(path, "offset"), "offset: want [dx, dy], two finite numbers"), m.Value)
				continue
			}
			sh.Offset = off
		default:
			er := cli.Errorf(cli.CodeInvalidPlan, cli.Join(path, m.Key), "unknown field %q", m.Key)
			er.Hint = "fields: color, transparency, offset"
			d.Add(er, m.Value)
		}
	}
}

// decodeLineColor decodes one line colour value into l: a string
// #RRGGBB stored in upper case, or an object {"hex": "#RRGGBB"}.
func decodeLineColor(l *LineSpec, v *cli.Value, path string, d *cli.Decoder) {
	switch v.Kind {
	case cli.String:
		upper, ok := vsd.Color(v.Str)
		if !ok {
			e := cli.Errorf(cli.CodeInvalidOperation, path, "color %q is not #RRGGBB", v.Str)
			d.Add(e, v)
			return
		}
		l.Color = upper
	case cli.Object:
		seen := false
		for _, m := range v.Members {
			if m.Key != "hex" {
				e := cli.Errorf(cli.CodeInvalidPlan, cli.Join(path, m.Key), "unknown field %q", m.Key)
				e.Hint = "fields: hex"
				d.Add(e, m.Value)
				continue
			}
			seen = true
			var hex string
			n := len(d.Errs())
			d.Value(m.Value, cli.Join(path, "hex"), &hex)
			if len(d.Errs()) > n {
				continue
			}
			upper, ok := vsd.Color(hex)
			if !ok {
				e := cli.Errorf(cli.CodeInvalidOperation, cli.Join(path, "hex"), "hex %q is not #RRGGBB", hex)
				d.Add(e, m.Value)
				continue
			}
			l.Color = upper
		}
		if !seen {
			d.Fail(cli.CodeInvalidPlan, cli.Join(path, "hex"), v, "missing field %q", "hex")
		}
	default:
		d.Fail(cli.CodeInvalidPlan, path, v, "expected a string or an object, found %s", v.Kind)
	}
}

// DecodePlan implements cli.PlanDecoder for object.add.
func (s *ObjectAddSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "as", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.As) }},
		{Name: "kind", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Kind) }},
		{Name: "master", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Master) }},
		{Name: "geom", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Geom) }},
		{Name: "box", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Box) }},
		{Name: "pin", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Pin) }},
		{Name: "name", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Name) }},
		{Name: "html", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var str string
			dd.Value(mv, fp, &str)
			s.HTML = &str
		}},
		{Name: "fill", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			ps := &PaintSpec{}
			ps.DecodePlan(mv, fp, dd)
			s.Fill = ps
		}},
		{Name: "line", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			ls := &LineSpec{}
			ls.DecodePlan(mv, fp, dd)
			s.Line = ls
		}},
		{Name: "from", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.From) }},
		{Name: "to", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.To) }},
		{Name: "from_glue", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.FromGlue) }},
		{Name: "to_glue", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.ToGlue) }},
		{Name: "shadow", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			sp := &ShadowSpec{}
			sp.DecodePlan(mv, fp, dd)
			s.Shadow = sp
		}},
		{Name: "image", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Image) }},
		{Name: "crop", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Crop) }},
	})
}

// DecodePlan implements cli.PlanDecoder for object.set.
func (s *ObjectSetSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "box", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Box) }},
		{Name: "rot", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var f float64
			dd.Value(mv, fp, &f)
			s.Rot = &f
		}},
		{Name: "flip_h", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var b bool
			dd.Value(mv, fp, &b)
			s.FlipH = &b
		}},
		{Name: "flip_v", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var b bool
			dd.Value(mv, fp, &b)
			s.FlipV = &b
		}},
		{Name: "name", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var str string
			dd.Value(mv, fp, &str)
			s.Name = &str
		}},
		{Name: "fill", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			ps := &PaintSpec{}
			ps.DecodePlan(mv, fp, dd)
			s.Fill = ps
		}},
		{Name: "line", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			ls := &LineSpec{}
			ls.DecodePlan(mv, fp, dd)
			s.Line = ls
		}},
		{Name: "from", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.From) }},
		{Name: "to", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.To) }},
		{Name: "from_glue", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var str string
			dd.Value(mv, fp, &str)
			s.FromGlue = &str
		}},
		{Name: "to_glue", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var str string
			dd.Value(mv, fp, &str)
			s.ToGlue = &str
		}},
		{Name: "shadow", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			sp := &ShadowSpec{}
			sp.DecodePlan(mv, fp, dd)
			s.Shadow = sp
		}},
		{Name: "image", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var str string
			dd.Value(mv, fp, &str)
			s.Image = &str
		}},
		{Name: "crop", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Crop) }},
	})
}

// DecodePlan implements cli.PlanDecoder for text.set.
func (s *TextSetSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "html", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.HTML) }},
		{Name: "delete", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Delete) }},
	})
}

// DecodePlan implements cli.PlanDecoder for prop.add.
func (s *PropAddSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "name", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Name) }},
		{Name: "label", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Label) }},
		{Name: "type", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			cli.DecodeEnum(dd, mv, fp, []string{"string", "number", "boolean", "date", "duration", "currency", "fixed_list", "variable_list"}, &s.Type)
		}},
		{Name: "value", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Value) }},
		{Name: "format", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Format) }},
	})
}

// DecodePlan implements cli.PlanDecoder for prop.set.
func (s *PropSetSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "value", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Value) }},
	})
}

// DecodePlan implements cli.PlanDecoder for page.add.
func (s *PageAddSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "name", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Name) }},
		{Name: "as", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.As) }},
		{Name: "at", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "position", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			cli.DecodeEnum(dd, mv, fp, []string{"before", "after"}, &s.Position)
		}},
		{Name: "size", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Size) }},
		{Name: "orientation", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Orientation) }},
		{Name: "background", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var b bool
			dd.Value(mv, fp, &b)
			s.Background = &b
		}},
		{Name: "background_page", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.BackgroundPage) }},
	})
}

// DecodePlan implements cli.PlanDecoder for page.rename.
func (s *PageRenameSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "name", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Name) }},
	})
}

// DecodePlan implements cli.PlanDecoder for page.set.
func (s *PageSetSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "size", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Size) }},
		{Name: "orientation", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Orientation) }},
		{Name: "background", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var b bool
			dd.Value(mv, fp, &b)
			s.Background = &b
		}},
		{Name: "background_page", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.BackgroundPage) }},
	})
}
