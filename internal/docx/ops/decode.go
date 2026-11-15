package ops

import (
	"encoding/hex"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/model"
)

// Plan fields are decoded by the shared decoder (internal/cli): it
// carries the path of every value, refuses unknown fields, missing
// required ones and null, and reports every fault it finds. The types
// below with a wire form of their own implement cli.PlanDecoder; every
// fault is an invalid_plan error at the value's path.

// bad records an invalid_plan fault about v at path.
func bad(d *cli.Decoder, v *cli.Value, path, format string, args ...any) {
	d.Fail(cli.CodeInvalidPlan, path, v, format, args...)
}

// planString returns v's string, or records a fault.
func planString(d *cli.Decoder, v *cli.Value, path string) (string, bool) {
	if v.Kind != cli.String {
		bad(d, v, path, "expected a string, found %s", v.Kind)
		return "", false
	}
	return v.Str, true
}

// planNumber returns v's number, or records a fault.
func planNumber(d *cli.Decoder, v *cli.Value, path string) (float64, bool) {
	if v.Kind != cli.Number {
		bad(d, v, path, "expected a number, found %s", v.Kind)
		return 0, false
	}
	f, err := strconv.ParseFloat(v.Num, 64)
	if err != nil || math.IsInf(f, 0) {
		bad(d, v, path, "the number %s is out of range", v.Num)
		return 0, false
	}
	return f, true
}

// decodeSpec decodes an operation object into its Spec; the "op" member
// is the operation's name, not a field. It reports whether it decoded
// without a fault.
func decodeSpec(d *cli.Decoder, v *cli.Value, path string, spec Spec) bool {
	n := len(d.Errs())
	d.Struct(v, path, spec, "op")
	return len(d.Errs()) == n
}

// DecodePlan implements cli.PlanDecoder.
func (p *Points) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	f, ok := planNumber(d, v, path)
	if !ok {
		return
	}
	if f < 0 {
		bad(d, v, path, "point value must be finite and >= 0")
		return
	}
	n, msg := pointsFrom(f)
	if msg != "" {
		bad(d, v, path, "%s", msg)
		return
	}
	*p = Points(n)
}

// DecodePlan implements cli.PlanDecoder.
func (p *SignedPoints) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	f, ok := planNumber(d, v, path)
	if !ok {
		return
	}
	if math.Abs(f) > 100000 {
		bad(d, v, path, "indentation must be finite and between -100000 and 100000 points")
		return
	}
	n, msg := pointsFrom(f)
	if msg != "" {
		bad(d, v, path, "%s", msg)
		return
	}
	*p = SignedPoints(n)
}

// parseParagraphAddress checks a paragraph address; msg says what is
// wrong.
func parseParagraphAddress(s string) (a ParagraphAddress, msg string) {
	if name, ok := strings.CutPrefix(s, "$"); ok {
		if cli.ValidAlias(name) {
			return ParagraphAddress(s), ""
		}
		return "", "alias must match [A-Za-z_][A-Za-z0-9_]*"
	}
	id, dup, hasDup := strings.Cut(s, "~")
	if s == strings.ToUpper(s) && validParaID(id) && (!hasDup || validDuplicate(dup)) {
		return ParagraphAddress(s), ""
	}
	return "", "paragraph id must be eight uppercase hexadecimal digits, with ~N (N from 2) for the Nth paragraph sharing it"
}

// DecodePlan implements cli.PlanDecoder.
func (a *ParagraphAddress) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := planString(d, v, path)
	if !ok {
		return
	}
	p, msg := parseParagraphAddress(s)
	if msg != "" {
		bad(d, v, path, "%s", msg)
		return
	}
	*a = p
}

// DecodePlan implements cli.PlanDecoder.
func (a *InsertAnchor) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := planString(d, v, path)
	if !ok {
		return
	}
	if rest, ok := strings.CutPrefix(s, "table:"); ok {
		n, ok := model.ParseU32(rest)
		if !ok || n == 0 {
			bad(d, v, path, "table anchor must be table:N with N >= 1")
			return
		}
		*a = InsertAnchor("table:" + strconv.FormatUint(uint64(n), 10))
		return
	}
	p, msg := parseParagraphAddress(s)
	if msg != "" {
		bad(d, v, path, "%s", msg)
		return
	}
	*a = InsertAnchor(p)
}

// DecodePlan implements cli.PlanDecoder.
func (c *ParagraphContent) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	switch v.Kind {
	case cli.String:
		*c = ParagraphContent{Items: []string{v.Str}}
		return
	case cli.Array:
		items := make([]string, 0, len(v.Items))
		for _, e := range v.Items {
			if e.Kind != cli.String {
				bad(d, v, path, "html must be a string or a list of strings")
				return
			}
			items = append(items, e.Str)
		}
		*c = ParagraphContent{Items: items, List: true}
		return
	}
	bad(d, v, path, "html must be a string or a list of strings")
}

// DecodePlan implements cli.PlanDecoder.
func (h *SourceHash) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := planString(d, v, path)
	if !ok {
		return
	}
	digits, ok := strings.CutPrefix(s, "sha256:")
	if _, err := hex.DecodeString(digits); !ok || len(digits) != 64 || strings.ToLower(digits) != digits || err != nil {
		bad(d, v, path, "source must be sha256:<64 lowercase hex>")
		return
	}
	*h = SourceHash(s)
}

// DecodePlan implements cli.PlanDecoder.
func (m *ChangeMode) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	cli.DecodeEnum(d, v, path, unitEnums["ChangeMode"], m)
}

// DecodePlan implements cli.PlanDecoder.
func (p *InsertPosition) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	cli.DecodeEnum(d, v, path, unitEnums["InsertPosition"], p)
}

// DecodePlan implements cli.PlanDecoder.
func (c *CommentStatus) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	cli.DecodeEnum(d, v, path, unitEnums["CommentStatus"], c)
}

// DecodePlan implements cli.PlanDecoder.
func (k *ChromeKind) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	cli.DecodeEnum(d, v, path, unitEnums["ChromeKind"], k)
}

// DecodePlan implements cli.PlanDecoder.
func (c *HighlightColor) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	cli.DecodeEnum(d, v, path, unitEnums["HighlightColor"], c)
}

// DecodePlan implements cli.PlanDecoder: an object whose unit names the
// kind, with a value for points and percent.
func (w *TableWidth) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	if v.Kind != cli.Object {
		bad(d, v, path, "expected an object with unit (auto, points or percent), found %s", v.Kind)
		return
	}
	u := v.Get("unit")
	if u == nil {
		bad(d, v, cli.Join(path, "unit"), "missing field \"unit\"")
		return
	}
	var unit string
	if !cli.DecodeEnum(d, u, cli.Join(path, "unit"), []string{"auto", "points", "percent"}, &unit) {
		return
	}
	*w = TableWidth{Unit: unit}
	switch unit {
	case "points":
		var f struct {
			Value Points `json:"value"`
		}
		d.Struct(v, path, &f, "unit")
		w.Points = f.Value
	case "percent":
		var f struct {
			Value float64 `json:"value"`
		}
		d.Struct(v, path, &f, "unit")
		w.Percent = f.Value
	default:
		var f struct{}
		d.Struct(v, path, &f, "unit")
	}
}

// DecodePlan implements cli.PlanDecoder: an object with a mode and a
// value, a multiple or a length in points by the mode.
func (l *LineSpacing) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	if v.Kind != cli.Object {
		bad(d, v, path, "expected an object with mode and value, found %s", v.Kind)
		return
	}
	*l = LineSpacing{}
	var content *cli.Value
	for _, m := range v.Members {
		switch m.Key {
		case "mode":
			var mode string
			if cli.DecodeEnum(d, m.Value, cli.Join(path, "mode"), []string{"multiple", "exact", "at_least"}, &mode) {
				l.Mode = mode
			}
		case "value":
			content = m.Value
		default:
			bad(d, m.Value, cli.Join(path, m.Key), "unknown field %q: line_spacing has mode and value", m.Key)
		}
	}
	switch {
	case v.Get("mode") == nil:
		bad(d, v, cli.Join(path, "mode"), "missing field \"mode\"")
	case content == nil:
		bad(d, v, cli.Join(path, "value"), "missing field \"value\"")
	case l.Mode == "multiple":
		if f, ok := planNumber(d, content, cli.Join(path, "value")); ok {
			l.Multiple = f
		}
	case l.Mode != "":
		l.Points.DecodePlan(content, cli.Join(path, "value"), d)
	}
}

// DecodePlan implements cli.PlanDecoder.
func (a *EquationAddress) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := planString(d, v, path)
	if !ok {
		return
	}
	para, num, numbered := strings.Cut(s, equationInfix)
	p, msg := parseParagraphAddress(para)
	if msg != "" {
		bad(d, v, path, "%s", msg)
		return
	}
	*a = EquationAddress{Paragraph: p}
	if numbered {
		n, ok := model.ParseU32(num)
		if !ok {
			bad(d, v, path, "an equation address is <paragraph>/equation:N")
			return
		}
		a.N = &n
	}
}

// DecodePlan implements cli.PlanDecoder.
func (a *RevisionAuthor) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := planString(d, v, path)
	if !ok {
		return
	}
	if s == "" {
		bad(d, v, path, "author must not be empty")
		return
	}
	*a = RevisionAuthor(s)
}

// DecodePlan implements cli.PlanDecoder: one revision address, or a list
// of paragraph addresses.
func (r *RevisionScope) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	switch v.Kind {
	case cli.String:
		id, ok := strings.CutPrefix(v.Str, "revision:")
		if !ok || !ascii.Digits(id) {
			bad(d, v, path, "at must be a revision address (revision:N, N the revision's decimal id) or a list of paragraph addresses")
			return
		}
		*r = RevisionScope{ID: id}
		return
	case cli.Array:
		out := RevisionScope{Paragraphs: make([]ParagraphAddress, len(v.Items))}
		for i, e := range v.Items {
			out.Paragraphs[i].DecodePlan(e, cli.Index(path, i), d)
		}
		*r = out
		return
	}
	bad(d, v, path, "at is a revision address (revision:7) or a list of paragraph addresses")
}

// validDuplicate reports whether n is the N of a duplicated id's "~N":
// decimal, from 2, without a leading zero (as read numbers duplicates).
func validDuplicate(n string) bool {
	v, ok := model.ParseU32(n)
	return ok && v >= 2 && n[0] >= '1' && n[0] <= '9'
}
