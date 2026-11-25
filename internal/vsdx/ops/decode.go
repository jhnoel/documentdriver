package ops

import (
	"maps"
	"math"
	"slices"
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

// DecodePlan implements cli.PlanDecoder: "none", {"hex": "#RRGGBB"}
// (solid), or a pattern fill {"pattern", "hex", "bg"} (docs/contract.md
// §3, Pattern fills): pattern a name of vsd.FillPatternIndex or a
// FillPattern number from 2 to 24 (a number or its string), hex the
// pattern's colour (default #000000), bg the background, a colour
// (#RRGGBB or {"hex"}) or "none" (the default). A colour that is not
// #RRGGBB and a pattern that is neither are invalid_operation faults
// at the member; an unknown member, a missing hex on a solid fill and
// any other form are invalid_plan faults.
func (p *PaintSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	*p = PaintSpec{}
	if v.Kind == cli.String {
		if v.Str != "none" {
			e := cli.Errorf(cli.CodeInvalidOperation, path, "fill %q: want \"none\" or a colour object", v.Str)
			d.Add(e, v)
			return
		}
		p.None = true
		return
	}
	if v.Kind != cli.Object {
		d.Fail(cli.CodeInvalidPlan, path, v, "expected \"none\" or an object, found %s", v.Kind)
		return
	}
	seenHex, seenPattern, seenBg := false, false, false
	for _, m := range v.Members {
		switch m.Key {
		case "hex":
			seenHex = true
			if hex, ok := decodeFillColor(m.Value, cli.Join(path, "hex"), d, false); ok {
				p.Hex = hex
			}
		case "pattern":
			seenPattern = true
			p.Pattern = decodeFillPattern(m.Value, cli.Join(path, "pattern"), d)
		case "bg":
			seenBg = true
			bgPath := cli.Join(path, "bg")
			if m.Value.Kind == cli.String && m.Value.Str == "none" {
				p.BgNone = true
			} else if hex, ok := decodeFillColor(m.Value, bgPath, d, true); ok {
				p.BgHex = hex
			}
		default:
			e := cli.Errorf(cli.CodeInvalidPlan, cli.Join(path, m.Key), "unknown field %q", m.Key)
			e.Hint = "fields: hex, pattern, bg"
			d.Add(e, m.Value)
		}
	}
	switch {
	case seenPattern:
		if !seenHex {
			p.Hex = defaultPatternHex
		}
		if !seenBg {
			p.BgNone = true
		}
	case seenBg:
		d.Fail(cli.CodeInvalidPlan, cli.Join(path, "bg"), v, "bg needs pattern: a solid fill has no background")
	case !seenHex:
		d.Fail(cli.CodeInvalidPlan, cli.Join(path, "hex"), v, "missing field %q", "hex")
	}
}

// defaultPatternHex is a pattern fill's colour when the plan gives
// none: black, the colour of line art (docs/contract.md §3).
const defaultPatternHex = "#000000"

// decodeFillColor decodes a fill colour at path: a string "#RRGGBB",
// or, when object is true (bg), also {"hex": "#RRGGBB"}; it returns the
// upper-case hex and false after recording a fault.
func decodeFillColor(v *cli.Value, path string, d *cli.Decoder, object bool) (string, bool) {
	if object && v.Kind == cli.Object {
		hex := ""
		seen := false
		for _, m := range v.Members {
			if m.Key != "hex" {
				e := cli.Errorf(cli.CodeInvalidPlan, cli.Join(path, m.Key), "unknown field %q", m.Key)
				e.Hint = "fields: hex"
				d.Add(e, m.Value)
				return "", false
			}
			seen = true
			n := len(d.Errs())
			d.Value(m.Value, cli.Join(path, "hex"), &hex)
			if len(d.Errs()) > n {
				return "", false
			}
		}
		if !seen {
			d.Fail(cli.CodeInvalidPlan, cli.Join(path, "hex"), v, "missing field %q", "hex")
			return "", false
		}
		path = cli.Join(path, "hex")
		return checkFillHex(hex, path, v, d)
	}
	if v.Kind != cli.String {
		want := "a string"
		if object {
			want = "\"none\", a string or an object"
		}
		d.Fail(cli.CodeInvalidPlan, path, v, "expected %s, found %s", want, v.Kind)
		return "", false
	}
	return checkFillHex(v.Str, path, v, d)
}

// checkFillHex returns hex in upper case, or records an
// invalid_operation fault at path when it is not #RRGGBB.
func checkFillHex(hex, path string, v *cli.Value, d *cli.Decoder) (string, bool) {
	upper, ok := vsd.Color(hex)
	if !ok {
		d.Add(cli.Errorf(cli.CodeInvalidOperation, path, "hex %q is not #RRGGBB", hex), v)
		return "", false
	}
	return upper, true
}

// decodeFillPattern decodes a pattern at path to its stored FillPattern
// number as a string: a name of vsd.FillPatternIndex, or a whole
// number from vsd.FillPatternMin to vsd.FillPatternMax given as a
// number or a string. Anything else is an invalid_operation fault
// naming the names, and "" is returned.
func decodeFillPattern(v *cli.Value, path string, d *cli.Decoder) string {
	var text string
	switch v.Kind {
	case cli.String:
		if n, ok := vsd.FillPatternIndex[v.Str]; ok {
			return n
		}
		text = v.Str
	case cli.Number:
		text = v.Num
	default:
		d.Fail(cli.CodeInvalidPlan, path, v, "expected a pattern name or number, found %s", v.Kind)
		return ""
	}
	if n, ok := vsd.Number(text); ok && n >= vsd.FillPatternMin && n <= vsd.FillPatternMax && n == math.Trunc(n) {
		return vsd.FormatInches(n)
	}
	e := cli.Errorf(cli.CodeInvalidOperation, path, "pattern %q is not a pattern name or a number from %d to %d", text, vsd.FillPatternMin, vsd.FillPatternMax)
	e.Hint = "names: " + strings.Join(slices.Sorted(maps.Keys(vsd.FillPatternIndex)), ", ")
	d.Add(e, v)
	return ""
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
		{Name: "points", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Points) }},
		{Name: "closed", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Closed) }},
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
		{Name: "text_block", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			tb := &TextBlockSpec{}
			tb.DecodePlan(mv, fp, dd)
			s.TextBlock = tb
		}},
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
		{Name: "text_block", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			tb := &TextBlockSpec{}
			tb.DecodePlan(mv, fp, dd)
			s.TextBlock = tb
		}},
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

// DecodePlan implements cli.PlanDecoder: an object {valign, margins,
// background} with at least one member. valign is top, middle or
// bottom; margins one number (all four sides) or four, each finite and
// 0 or more; background "none", #RRGGBB, or {hex, transparency} with
// transparency 0 to 100. A value out of range is an invalid_operation
// fault at the member; an unknown member, an empty object and any
// other form are invalid_plan faults.
func (tb *TextBlockSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	*tb = TextBlockSpec{}
	if v.Kind != cli.Object {
		d.Fail(cli.CodeInvalidPlan, path, v, "expected an object {valign, margins, background}, found %s", v.Kind)
		return
	}
	if len(v.Members) == 0 {
		d.Fail(cli.CodeInvalidPlan, path, v, "text_block needs valign, margins or background")
		return
	}
	for _, m := range v.Members {
		mp := cli.Join(path, m.Key)
		switch m.Key {
		case "valign":
			var s string
			n := len(d.Errs())
			d.Value(m.Value, mp, &s)
			if len(d.Errs()) > n {
				continue
			}
			if s != "top" && s != "middle" && s != "bottom" {
				d.Add(cli.Errorf(cli.CodeInvalidOperation, mp, "valign %q: want top, middle or bottom", s), m.Value)
				continue
			}
			tb.VAlign = s
		case "margins":
			tb.Margins = decodeMargins(m.Value, mp, d)
		case "background":
			tb.Background = decodeTextBg(m.Value, mp, d)
		default:
			er := cli.Errorf(cli.CodeInvalidPlan, mp, "unknown field %q", m.Key)
			er.Hint = "fields: valign, margins, background"
			d.Add(er, m.Value)
		}
	}
}

// decodeMargins decodes text margins: one number for all four sides or
// [left, top, right, bottom], each finite and 0 or more; nil after a
// fault.
func decodeMargins(v *cli.Value, path string, d *cli.Decoder) []float64 {
	var out []float64
	n := len(d.Errs())
	if v.Kind == cli.Number {
		var x float64
		d.Value(v, path, &x)
		out = []float64{x, x, x, x}
	} else {
		d.Value(v, path, &out)
	}
	if len(d.Errs()) > n {
		return nil
	}
	if len(out) != 4 || !addFinite(out) || slices.ContainsFunc(out, func(x float64) bool { return x < 0 }) {
		d.Add(cli.Errorf(cli.CodeInvalidOperation, path, "margins: want one number or [left, top, right, bottom], finite and 0 or more"), v)
		return nil
	}
	return out
}

// decodeTextBg decodes a text background: "none", "#RRGGBB", or
// {"hex", "transparency"}; nil after a fault.
func decodeTextBg(v *cli.Value, path string, d *cli.Decoder) *TextBgSpec {
	switch v.Kind {
	case cli.String:
		if v.Str == "none" {
			return &TextBgSpec{None: true}
		}
		hex, ok := vsd.Color(v.Str)
		if !ok {
			d.Add(cli.Errorf(cli.CodeInvalidOperation, path, "background %q: want \"none\", #RRGGBB or {hex, transparency}", v.Str), v)
			return nil
		}
		return &TextBgSpec{Hex: hex}
	case cli.Object:
		bg := &TextBgSpec{}
		ok := true
		for _, m := range v.Members {
			mp := cli.Join(path, m.Key)
			switch m.Key {
			case "hex":
				var s string
				n := len(d.Errs())
				d.Value(m.Value, mp, &s)
				if len(d.Errs()) > n {
					ok = false
					continue
				}
				hex, good := vsd.Color(s)
				if !good {
					d.Add(cli.Errorf(cli.CodeInvalidOperation, mp, "hex %q is not #RRGGBB", s), m.Value)
					ok = false
					continue
				}
				bg.Hex = hex
			case "transparency":
				var t float64
				n := len(d.Errs())
				d.Value(m.Value, mp, &t)
				if len(d.Errs()) > n {
					ok = false
					continue
				}
				if math.IsNaN(t) || t < 0 || t > 100 {
					d.Add(cli.Errorf(cli.CodeInvalidOperation, mp, "transparency %v: want 0 to 100", t), m.Value)
					ok = false
					continue
				}
				bg.Transparency = &t
			default:
				er := cli.Errorf(cli.CodeInvalidPlan, mp, "unknown field %q", m.Key)
				er.Hint = "fields: hex, transparency"
				d.Add(er, m.Value)
				ok = false
			}
		}
		if ok && bg.Hex == "" {
			d.Fail(cli.CodeInvalidPlan, cli.Join(path, "hex"), v, "missing field %q", "hex")
			return nil
		}
		if !ok {
			return nil
		}
		return bg
	default:
		d.Fail(cli.CodeInvalidPlan, path, v, "expected \"none\", #RRGGBB or an object, found %s", v.Kind)
		return nil
	}
}
