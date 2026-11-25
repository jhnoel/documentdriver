package ops

import (
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/path"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// addressOK reports whether s parses as an address whose kind is one
// of allowed, or as an alias (bare $name, or $name with a suffix
// that could follow a page or shape address), which any op may
// name; the op's resolver refuses a suffix that names a kind it
// does not take.
func addressOK(s string, allowed ...vsd.Kind) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "$") {
		_, suffix, ok := splitAlias(s)
		if !ok {
			return false
		}
		return suffixValid(suffix)
	}
	a, err := vsd.Parse(s)
	if err != nil {
		return false
	}
	return slices.Contains(allowed, a.Kind)
}

// masterOK reports whether s names a master: a master address, or a
// master name, which carries no address punctuation.
func masterOK(s string) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "$") {
		return cli.ValidAlias(s[1:])
	}
	if a, err := vsd.Parse(s); err == nil {
		return a.Kind == vsd.KindMaster
	}
	if strings.Contains(s, "/") || strings.HasPrefix(s, "page") || strings.HasPrefix(s, "master") {
		return false
	}
	return true
}

// glueOK reports whether s is a glue reference: a shape or an alias
// (with what may follow a shape, such as a page alias's /s5),
// with an optional colon and a point number, and "none" only on
// the ops that disconnect.
func glueOK(s string, allowNone bool) bool {
	if s == "" {
		return false
	}
	if s == "none" {
		return allowNone
	}
	if strings.HasPrefix(s, "$") {
		head, point, hasPoint := strings.Cut(s, ":")
		if hasPoint && !pointOK(point) {
			return false
		}
		_, suffix, ok := splitAlias(head)
		if !ok {
			return false
		}
		return suffixShapeValid(suffix)
	}
	if strings.HasPrefix(s, "s") {
		head, point, hasPoint := strings.Cut(s[1:], ":")
		if !idOK(head) {
			return false
		}
		return !hasPoint || pointOK(point)
	}
	return false
}

// idOK reports whether s is a canonical stored id, as an address
// holds it.
func idOK(s string) bool {
	if s == "" {
		return false
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return false
	}
	return strconv.FormatUint(n, 10) == s
}

// pointOK reports whether s is a canonical 1-based point number.
func pointOK(s string) bool {
	if s == "" {
		return false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return false
	}
	return strconv.Itoa(n) == s
}

// boxOK reports whether b is [x, y, width, height] with positive
// width and height.
func boxOK(b []float64) bool { return len(b) == 4 && b[2] > 0 && b[3] > 0 }

// pointOK2 reports whether p is [x, y].
func pointOK2(p []float64) bool { return len(p) == 2 }

// sizeOK reports whether s is [width, height] with both positive.
func sizeOK(s []float64) bool { return len(s) == 2 && s[0] > 0 && s[1] > 0 }

// orientationOK reports whether s is portrait or landscape.
func orientationOK(s string) bool { return s == "portrait" || s == "landscape" }

// geomOK reports whether s is rect, ellipse, polyline or curve.
func geomOK(s string) bool {
	return s == path.Rect || s == path.Ellipse || isPathGeom(s)
}

// isPathGeom reports whether s is a geom placed by points (polyline or
// curve) rather than by box or pin.
func isPathGeom(s string) bool { return s == path.Polyline || s == path.Curve }

// checkObjectAdd reports the first rule object.add breaks: its
// message and its field relative to the operation, or false.
func checkObjectAdd(s *ObjectAddSpec) (string, string, bool) {
	if !addressOK(s.At, vsd.KindPage, vsd.KindShape) {
		return "at must be a page or shape address or $name", "at", true
	}
	if s.As != "" && !cli.ValidAlias(s.As) {
		return "as must match [A-Za-z_][A-Za-z0-9_]*", "as", true
	}
	if s.Kind != "" && s.Kind != "connector" && s.Kind != "picture" {
		return "kind must be connector or picture", "kind", true
	}
	if s.Kind == "picture" {
		return checkPictureAdd(s)
	}
	if s.Image != "" || s.Crop != nil {
		return "image and crop go with kind picture", "image", true
	}
	if s.Geom != "" && !geomOK(s.Geom) {
		return "geom must be rect, ellipse, polyline or curve", "geom", true
	}
	if msg, field, bad := checkPathAdd(s); bad {
		return msg, field, true
	}
	if s.Master != "" && !masterOK(s.Master) {
		return "master must be a master address or a master name", "master", true
	}
	if s.Geom != "" && s.Master != "" {
		return "object.add takes master or geom, not both", "geom", true
	}
	if s.Kind == "connector" && s.Master != "" {
		return "connector takes no master", "master", true
	}
	if s.Kind == "connector" && s.Geom != "" {
		return "connector takes no geom", "geom", true
	}
	if s.Kind != "connector" && s.Master == "" && s.Geom == "" {
		return "object.add needs master, geom or kind connector", "", true
	}
	if s.Kind == "connector" && s.Box != nil {
		return "connector takes no box", "box", true
	}
	if s.Kind == "connector" && s.Pin != nil {
		return "connector takes no pin", "pin", true
	}
	// A master may be 1-D, whose placement is from/to or glue, or
	// 2-D, whose placement is box or pin; whether it is 1-D is only
	// in the document, so both placements pass here and Run refuses
	// the wrong one operation_failed (chunk CC3). A master-less
	// shape is known 2-D, so it still takes no from/to or glue and
	// still needs box or pin here.
	if s.Kind != "connector" && s.Master == "" {
		if s.From != nil {
			return "shape takes no from", "from", true
		}
		if s.To != nil {
			return "shape takes no to", "to", true
		}
		if s.FromGlue != "" {
			return "shape takes no from_glue", "from_glue", true
		}
		if s.ToGlue != "" {
			return "shape takes no to_glue", "to_glue", true
		}
		if s.Box == nil && s.Pin == nil && !isPathGeom(s.Geom) {
			return "shape needs box or pin", "box", true
		}
	}
	if s.Kind != "connector" && s.Box != nil && s.Pin != nil {
		return "shape takes box or pin, not both", "pin", true
	}
	if s.Box != nil && !boxOK(s.Box) {
		return "box must be four numbers with positive width and height", "box", true
	}
	if s.Pin != nil && !pointOK2(s.Pin) {
		return "pin must be two numbers", "pin", true
	}
	if s.From != nil && !pointOK2(s.From) {
		return "from must be two numbers", "from", true
	}
	if s.To != nil && !pointOK2(s.To) {
		return "to must be two numbers", "to", true
	}
	if s.Kind == "connector" && s.From == nil && s.FromGlue == "" {
		return "connector needs from or from_glue", "from", true
	}
	if s.Kind == "connector" && s.To == nil && s.ToGlue == "" {
		return "connector needs to or to_glue", "to", true
	}
	if s.FromGlue != "" && !glueOK(s.FromGlue, false) {
		return "from_glue must name a shape or $name, with an optional point", "from_glue", true
	}
	if s.ToGlue != "" && !glueOK(s.ToGlue, false) {
		return "to_glue must name a shape or $name, with an optional point", "to_glue", true
	}
	return "", "", false
}

// checkObjectSet reports the first rule object.set breaks: its
// message and its field relative to the operation, or false.
func checkObjectSet(s *ObjectSetSpec) (string, string, bool) {
	if !addressOK(s.At, vsd.KindShape) {
		return "at must be a shape address or $name", "at", true
	}
	if s.Box != nil && !boxOK(s.Box) {
		return "box must be four numbers with positive width and height", "box", true
	}
	if s.From != nil && !pointOK2(s.From) {
		return "from must be two numbers", "from", true
	}
	if s.To != nil && !pointOK2(s.To) {
		return "to must be two numbers", "to", true
	}
	if s.FromGlue != nil && !glueOK(*s.FromGlue, true) {
		return "from_glue must name a shape or $name, with an optional point, or none", "from_glue", true
	}
	if s.ToGlue != nil && !glueOK(*s.ToGlue, true) {
		return "to_glue must name a shape or $name, with an optional point, or none", "to_glue", true
	}
	if s.Box == nil && s.Rot == nil && s.FlipH == nil && s.FlipV == nil && s.Name == nil && s.Fill == nil && s.Line == nil && s.Shadow == nil && s.Image == nil && s.Crop == nil && s.From == nil && s.To == nil && s.FromGlue == nil && s.ToGlue == nil {
		return "object.set needs a field to set", "", true
	}
	return "", "", false
}

// checkTextSet reports the first rule text.set breaks: its message
// and its field relative to the operation, or false. Its delete
// must list field addresses (a $name may name one, as at);
// whether a listed field is one of the shape's is the op's
// (not_found, with the document open).
func checkTextSet(s *TextSetSpec) (string, string, bool) {
	if !addressOK(s.At, vsd.KindShape) {
		return "at must be a shape address or $name", "at", true
	}
	for _, del := range s.Delete {
		if !addressOK(del, vsd.KindField) {
			return "delete must list field addresses or $name", "delete", true
		}
	}
	return "", "", false
}

// checkPropAdd reports the first rule prop.add breaks: its message
// and its field relative to the operation, or false.
func checkPropAdd(s *PropAddSpec) (string, string, bool) {
	if !addressOK(s.At, vsd.KindShape) {
		return "at must be a shape address or $name", "at", true
	}
	if s.Name == "" {
		return "name must not be empty", "name", true
	}
	return "", "", false
}

// checkPropSet reports the first rule prop.set breaks: its message
// and its field relative to the operation, or false.
func checkPropSet(s *PropSetSpec) (string, string, bool) {
	if !addressOK(s.At, vsd.KindProp) {
		return "at must name a shape data row or $name", "at", true
	}
	return "", "", false
}

// checkPageAdd reports the first rule page.add breaks: its message
// and its field relative to the operation, or false.
func checkPageAdd(s *PageAddSpec) (string, string, bool) {
	if s.Name == "" {
		return "name must not be empty", "name", true
	}
	if s.As != "" && !cli.ValidAlias(s.As) {
		return "as must match [A-Za-z_][A-Za-z0-9_]*", "as", true
	}
	if s.At != "" && !addressOK(s.At, vsd.KindPage) {
		return "at must be a page address or $name", "at", true
	}
	if s.Orientation != "" && !orientationOK(s.Orientation) {
		return "orientation must be portrait or landscape", "orientation", true
	}
	if s.Size != nil && !sizeOK(s.Size) {
		return "size must be two positive numbers", "size", true
	}
	return "", "", false
}

// checkPageRename reports the first rule page.rename breaks: its
// message and its field relative to the operation, or false.
func checkPageRename(s *PageRenameSpec) (string, string, bool) {
	if !addressOK(s.At, vsd.KindPage) {
		return "at must be a page address or $name", "at", true
	}
	if s.Name == "" {
		return "name must not be empty", "name", true
	}
	return "", "", false
}

// checkPageSet reports the first rule page.set breaks: its message
// and its field relative to the operation, or false.
func checkPageSet(s *PageSetSpec) (string, string, bool) {
	if !addressOK(s.At, vsd.KindPage) {
		return "at must be a page address or $name", "at", true
	}
	if s.Orientation != "" && !orientationOK(s.Orientation) {
		return "orientation must be portrait or landscape", "orientation", true
	}
	if s.Size != nil && !sizeOK(s.Size) {
		return "size must be two positive numbers", "size", true
	}
	if s.Orientation == "" && s.Size == nil && s.Background == nil && s.BackgroundPage == "" {
		return "page.set needs orientation, size, background or background_page", "", true
	}
	return "", "", false
}

// extraRules are document-independent rules that chunks add in files
// of their own (rules_<topic>.go, each appending in an init function),
// checked after the op's own rules in the order they were added: the
// extension rule for this file (contract-review chunks CC5, CC8, CC10,
// CC13). A rule returns false for a spec it does not apply to.
var extraRules []func(Spec) (msg, field string, broken bool)

// checkSpec reports the first rule s breaks: its message and its
// field relative to the operation, or false when s breaks none. The
// op's own rules come first, then extraRules.
func checkSpec(s Spec) (string, string, bool) {
	if msg, field, bad := checkOwn(s); bad {
		return msg, field, true
	}
	for _, rule := range extraRules {
		if msg, field, bad := rule(s); bad {
			return msg, field, true
		}
	}
	return "", "", false
}

// checkOwn reports the first of the op's own rules s breaks.
func checkOwn(s Spec) (string, string, bool) {
	switch v := s.(type) {
	case *ObjectAddSpec:
		return checkObjectAdd(v)
	case *ObjectSetSpec:
		return checkObjectSet(v)
	case *TextSetSpec:
		return checkTextSet(v)
	case *PropAddSpec:
		return checkPropAdd(v)
	case *PropSetSpec:
		return checkPropSet(v)
	case *PageAddSpec:
		return checkPageAdd(v)
	case *PageRenameSpec:
		return checkPageRename(v)
	case *PageSetSpec:
		return checkPageSet(v)
	case checker:
		return v.check()
	}
	return "", "", false
}

// checker is a spec whose rules live in its own spec_<name>.go file
// (the extension rule: a new op adds files, it does not extend this
// switch).
type checker interface {
	check() (msg, field string, broken bool)
}

// needAddress reports the rule "field must be <what> or $name" broken
// when s is not an address of one of kinds (or an alias), for the
// spec_<name>.go checks.
func needAddress(s, field, what string, kinds ...vsd.Kind) (string, string, bool) {
	if !addressOK(s, kinds...) {
		return field + " must be " + what + " or $name", field, true
	}
	return "", "", false
}

// checkPictureAdd reports the first rule an object.add of kind
// picture breaks (docs/contract.md §3, Pictures).
func checkPictureAdd(s *ObjectAddSpec) (string, string, bool) {
	for _, f := range []struct {
		set   bool
		field string
	}{
		{s.Master != "", "master"}, {s.Geom != "", "geom"}, {s.Fill != nil, "fill"}, {s.Line != nil, "line"},
		{s.HTML != nil, "html"}, {s.Shadow != nil, "shadow"}, {s.From != nil, "from"}, {s.To != nil, "to"},
		{s.FromGlue != "", "from_glue"}, {s.ToGlue != "", "to_glue"},
	} {
		if f.set {
			return "picture takes no master, geom, fill, line, html, shadow, from, to, from_glue or to_glue", f.field, true
		}
	}
	if s.Image == "" {
		return "picture needs image", "image", true
	}
	if s.Box == nil && s.Pin == nil {
		return "shape needs box or pin", "box", true
	}
	if s.Box != nil && s.Pin != nil {
		return "shape takes box or pin, not both", "pin", true
	}
	if s.Box != nil && !boxOK(s.Box) {
		return "box must be four numbers with positive width and height", "box", true
	}
	if s.Pin != nil && !pointOK2(s.Pin) {
		return "pin must be two numbers", "pin", true
	}
	return "", "", false
}
