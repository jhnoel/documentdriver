package ops

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
)

// ErrNotImplemented is returned for plan operations and fields that
// arrive in a later milestone.
var ErrNotImplemented = errors.New("ops: not implemented yet")

// ErrMissing is returned when an operation lacks a field it needs: a
// choice the tool does not make for the agent.
var ErrMissing = errors.New("ops: missing field")

// Compile checks every operation's values as far as it can without the
// deck and turns the plan into operations. Every fault is reported, each
// as an invalid_operation with the path of the field it is about; the
// error is a cli.Errors.
func (p *Plan) Compile() ([]Op, error) {
	units := prop.Units(p.Units)
	var out []Op
	var errs cli.Errors
	c := &compiler{aliases: map[string]string{}}
	for i, spec := range p.Ops {
		c.path, c.fields = cli.Index("ops", i), cli.Fields(reflect.TypeOf(spec).Elem())
		n := len(c.errs)
		op := c.compile(spec, units)
		if len(c.errs) == n && op != nil {
			out = append(out, op)
		}
	}
	errs = c.errs
	if len(errs) > 0 {
		return nil, errs
	}
	p.slides = c.slides
	return out, nil
}

// Slides lists the slides the compiled plan's operations address, in
// the order first used.
func (p *Plan) Slides() []string { return p.slides }

// compiler holds what Compile tracks across operations: the slide each
// alias was defined on, the slides used, and the errors.
type compiler struct {
	aliases map[string]string
	slides  []string
	errs    cli.Errors
	path    string
	fields  []cli.Field
}

// fail records an invalid_operation at field ("" for the operation).
func (c *compiler) fail(field, format string, args ...any) {
	path := c.path
	if field != "" {
		path = cli.Join(path, field)
	}
	c.errs = append(c.errs, cli.Errorf(cli.CodeInvalidOperation, path, format, args...))
}

// check records err, naming the field its message starts with when that
// is one of the operation's fields ("box x ...", "geom: ...", a missing
// field).
func (c *compiler) check(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	lead := strings.TrimPrefix(msg, ErrMissing.Error()+": ")
	end := strings.IndexFunc(lead, func(r rune) bool { return r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') })
	if end < 0 {
		end = len(lead)
	}
	field := ""
	for _, f := range c.fields {
		if f.Name == lead[:end] {
			field = f.Name
		}
	}
	// Inside an html field the path goes on to the attribute or element
	// at fault (ops[1].html@data-foo, ops[1].html<div>).
	if field == "html" {
		if m := htmlAttr.FindStringSubmatch(msg); m != nil {
			field += "@" + m[1]
		} else if m := htmlElement.FindStringSubmatch(msg); m != nil {
			field += "<" + m[1] + ">"
		}
	}
	c.fail(field, "%s", strings.TrimPrefix(msg, "ops: "))
	c.errs[len(c.errs)-1].Cause = err
}

// htmlAttr and htmlElement find what an html error is about in its
// message, as the text grammar words it.
var (
	htmlAttr    = regexp.MustCompile(`attribute ([a-z][a-z0-9-]*)`)
	htmlElement = regexp.MustCompile(`element <([a-z][a-z0-9-]*)>`)
)

// addr splits an address into its slide and the object id on it: a
// slide ("slide3": id ""), an object ("slide3/s17") or an alias
// ("$name", whose slide is the one it was defined on).
func (c *compiler) addr(field, at string, wantObject bool) (slide, id string, ok bool) {
	switch {
	case at == "":
		c.fail(field, "%s is empty; give an address such as slide3/s17", field)
		return "", "", false
	case strings.HasPrefix(at, "$"):
		slide, ok = c.aliases[at[1:]]
		if !ok {
			c.fail(field, "alias %s is not defined by an earlier operation's as", at)
			return "", "", false
		}
		id = at
	default:
		slide, id, _ = strings.Cut(at, "/")
		if !strings.HasPrefix(slide, "slide") || strings.ContainsAny(id, "/#") {
			c.fail(field, "%q is not an address; an object is slide3/s17, an alias $name", at)
			return "", "", false
		}
		if wantObject && id == "" {
			c.fail(field, "%q names a slide; this operation needs an object, such as %s/s17", at, at)
			return "", "", false
		}
	}
	if !slices.Contains(c.slides, slide) {
		c.slides = append(c.slides, slide)
	}
	return slide, id, true
}

// define records the slide of the alias an operation creates.
func (c *compiler) define(as Opt[string], slide string) {
	if as.Set {
		c.aliases[as.V] = slide
	}
}

// compile resolves one plan specification into a runnable operation or records its faults.
func (c *compiler) compile(spec PlanOp, units prop.Units) Op {
	switch s := spec.(type) {
	case *AddSpec:
		slide, id, ok := c.addr("at", s.At, false)
		c.check(checkAdd(s))
		c.check(checkFields(s.fields(), units, s.HTML))
		if !ok {
			return nil
		}
		if id != "" {
			s.Parent = Some(id)
		}
		c.define(s.Alias, slide)
		return &Add{Slide: slide, Units: units, Spec: *s}
	case *SetSpec:
		slide, id, ok := c.addr("at", s.Target, true)
		c.check(checkFields(s.fields(), units, s.HTML))
		s.Target = id
		if !ok {
			return nil
		}
		return &Set{Slide: slide, Units: units, Spec: *s}
	case *TextSetSpec:
		slide, id, _ := c.addr("at", s.Target, true)
		if !s.HTML.Set {
			c.check(fmt.Errorf("%w: html (the new text body; \"\" for an empty one)", ErrMissing))
		} else if _, err := text.Parse(s.HTML.V); err != nil {
			c.check(fmt.Errorf("html: %w", err))
		}
		return &TextSet{Slide: slide, Target: id, HTML: s.HTML.V}
	case *TextReplaceSpec:
		slide, id, _ := c.addr("at", s.Target, true)
		switch {
		case !s.Find.Set || s.Find.V == "":
			c.check(fmt.Errorf("%w: select (the text to replace, not empty)", ErrMissing))
		case !s.With.Set:
			c.check(fmt.Errorf("%w: with (the replacement; \"\" deletes)", ErrMissing))
		case s.Occ.Set && s.All.Set && s.All.V:
			c.fail("occurrence", "occurrence and all exclude each other")
		case s.Occ.Set && s.Occ.V < 1:
			c.fail("occurrence", "occurrence counts from 1")
		}
		return &TextReplace{Slide: slide, Target: id, Find: s.Find.V, With: s.With.V, All: s.All.V, Occurrence: s.Occ.V}
	case *MoveSpec:
		slide, id, _ := c.addr("at", s.Target, true)
		return &Move{Slide: slide, Target: id, Units: units, DX: s.DX.V, DY: s.DY.V}
	case *DeleteSpec:
		slide, id, _ := c.addr("at", s.Target, true)
		return &Delete{Slide: slide, Target: id}
	case *DuplicateSpec:
		slide, id, _ := c.addr("at", s.Target, true)
		s.Target = id
		c.define(s.Alias, slide)
		return &Duplicate{Slide: slide, Units: units, Spec: *s}
	case *ReorderSpec:
		slide, id, ok := c.addr("at", s.Target, true)
		s.Target = id
		switch s.To {
		case "front", "back":
			if s.Pos.Set {
				c.fail("position", "position goes with an object address in to, not with %s", s.To)
			}
		default:
			toSlide, toID, toOK := c.addr("to", s.To, true)
			switch {
			case !toOK:
			case ok && toSlide != slide:
				c.fail("to", "%s is on another slide than %s", s.To, slide)
			case !s.Pos.Set:
				c.check(fmt.Errorf("%w: position (before or after %s)", ErrMissing, s.To))
			case s.Pos.V != "before" && s.Pos.V != "after":
				c.fail("position", "position is before or after, not %q", s.Pos.V)
			}
			s.Ref, s.To = Some(toID), s.Pos.V
		}
		return &Reorder{Slide: slide, Spec: *s}
	case *GroupCreateSpec:
		slide := ""
		if len(s.Targets) == 0 {
			c.check(fmt.Errorf("%w: at (the objects to group)", ErrMissing))
		}
		ids := make([]string, len(s.Targets))
		for k, at := range s.Targets {
			sl, id, ok := c.addr(cli.Index("at", k), at, true)
			if ok && slide != "" && sl != slide {
				c.fail(cli.Index("at", k), "%s is on another slide than %s", at, slide)
			}
			if ok && slide == "" {
				slide = sl
			}
			ids[k] = id
		}
		if !s.Name.Set {
			c.check(fmt.Errorf("%w: name (every object has a name; give one, \"\" allowed)", ErrMissing))
		}
		s.Targets = ids
		c.define(s.Alias, slide)
		return &GroupCreate{Slide: slide, Spec: *s}
	case *GroupUngroupSpec:
		slide, id, _ := c.addr("at", s.Target, true)
		return &GroupUngroup{Slide: slide, Target: id}
	case *GroupFitSpec:
		slide, id, _ := c.addr("at", s.Target, true)
		return &GroupFit{Slide: slide, Target: id}
	}
	c.check(fmt.Errorf("op: %s: %w", spec.opName(), ErrNotImplemented))
	return nil
}

// checkAdd checks that object.add names every choice DrawingML requires
// and that it does not use fields that are not implemented.
func checkAdd(s *AddSpec) error {
	missing := func(f, why string) error { return fmt.Errorf("%w: %s (%s)", ErrMissing, f, why) }
	switch {
	case (s.HTML.Set || s.Text.Set) && s.Kind == "connector":
		return errors.New("html: connectors hold no text; add a shape")
	case s.Text.Set && !s.HTML.Set:
		return missing("html", "text body properties need a text body, which html creates")
	case !s.Name.Set:
		return missing("name", "every object has a name; give one, \"\" allowed")
	case (s.Path.Set || s.Custom.Set) && s.Kind != "shape":
		return errors.New("path: a freeform is a shape (kind shape); connectors are drawn by their preset; a picture takes a path with object.set")
	case s.Kind == "picture":
		return checkAddPicture(s)
	case s.any():
		return errors.New("image, svg, crop, fit, fill_rect and locks are for pictures (kind picture)")
	case (s.Path.Set || s.Custom.Set) && s.Geom.Set && s.Geom.V != "custom":
		return fmt.Errorf("path: geom %s is a preset; give geom custom (or leave geom out) with a path", s.Geom.V)
	case s.Custom.Set && !s.Path.Set:
		return missing("path", "a custom geometry's path; custom gives its other lists")
	case s.Geom.Set && s.Geom.V == "custom" && !s.Path.Set:
		return missing("path", "the custom geometry's path, such as \"M 0 0 L 100 0 L 50 80 Z\"")
	case !s.Geom.Set && !s.Path.Set:
		return missing("geom", "the preset geometry, such as rect or straightConnector1, or a path for a freeform")
	}
	switch s.Kind {
	case "shape":
		if derive.IsConnectorPreset(s.Geom.V) {
			return fmt.Errorf("geom %s is a connector preset: use kind connector", s.Geom.V)
		}
		if !s.Box.Set {
			return missing("box", "[x, y, w, h] of the new shape")
		}
		for _, f := range []struct {
			set  bool
			name string
		}{{s.From.Set, "from"}, {s.To.Set, "to"}, {s.Start.Set, "from_glue"}, {s.End.Set, "to_glue"}, {s.First.Set, "first"}} {
			if f.set {
				return fmt.Errorf("%s is for connectors", f.name)
			}
		}
	case "connector":
		if !derive.IsConnectorPreset(s.Geom.V) {
			return fmt.Errorf("geom %q is not a connector preset (line, straightConnector1, bentConnector2-5, curvedConnector2-5)", s.Geom.V)
		}
		if s.Box.Set || s.Rot.Set || s.FlipH.Set || s.FlipV.Set {
			return errors.New("box: a connector is placed by from and to, not by box, rot or flips")
		}
		if s.Fill.Set && !s.Fill.V.None {
			return errors.New("fill: a connector's fill is never drawn: only \"none\" (a:noFill, as PowerPoint and pdraw write it)")
		}
		if s.TxBox.Set {
			return errors.New("tx_box is for shapes")
		}
		if !s.From.Set || !s.To.Set {
			return missing("from", "from and to are the connector's endpoints")
		}
		if strings.HasPrefix(s.Geom.V, "bentConnector") && !s.First.Set {
			return missing("first", "h or v: the direction of an elbow connector's first segment")
		}
	default:
		return fmt.Errorf("kind %q: want shape, connector or picture", s.Kind)
	}
	return nil
}

// checkAddPicture checks object.add of a picture: name, box and image are
// required (the schema needs a name; where the picture goes and what it
// shows are the agent's choices); geometry, line, crop, fit, fill
// rectangle, locks and svg are written only when given.
func checkAddPicture(s *AddSpec) error {
	missing := func(f, why string) error { return fmt.Errorf("%w: %s (%s)", ErrMissing, f, why) }
	switch {
	case !s.Box.Set:
		return missing("box", "[x, y, w, h] of the new picture")
	case !s.Image.Set:
		return missing("image", "a PNG, JPEG or GIF file, or an image part of the deck; an SVG goes in svg with its raster fallback in image")
	case s.Fill.Set:
		return errors.New("fill: a picture's fill is its image")
	}
	for _, f := range []struct {
		set  bool
		name string
	}{{s.From.Set, "from"}, {s.To.Set, "to"}, {s.Start.Set, "from_glue"}, {s.End.Set, "to_glue"}, {s.First.Set, "first"}, {s.HTML.Set, "html"}, {s.Text.Set, "text"}, {s.TxBox.Set, "tx_box"}} {
		if f.set {
			return fmt.Errorf("%s: not for pictures", f.name)
		}
	}
	if s.Geom.Set && derive.IsConnectorPreset(s.Geom.V) {
		return fmt.Errorf("geom %s is a connector preset", s.Geom.V)
	}
	return nil
}

// slidePart resolves a plan's slide argument.
func (t *Tx) slidePart(slide string) (string, error) { return t.ws.SlidePart(slide) }

// resolve resolves an object id (s17, s17~2 or $alias) on a slide.
func (t *Tx) resolve(slide, id string) (edit.Ref, error) {
	part, err := t.slidePart(slide)
	if err != nil {
		return edit.Ref{}, err
	}
	return t.ws.ResolveIn(part, id)
}

// target resolves an object id (s17, s17~2 or $alias) on a slide, checks
// that it may be written, and declares it as the operation's target.
func (t *Tx) target(slide, id string) (*edit.Handle, error) {
	ref, err := t.writable(slide, id)
	if err != nil {
		return nil, err
	}
	return t.ws.Target(ref)
}

// ErrReadOnlyObject is returned when an operation would write an object
// the design shows read-only: graphic frames (charts, tables, SmartArt,
// OLE objects) and content parts (ink).
var ErrReadOnlyObject = errors.New("ops: the object is read-only")

// readOnlyKinds are the "other" object kinds of the design's Objects
// table, read-only: id, kind, bounds and raw XML.
var readOnlyKinds = map[string]string{
	"graphicFrame": "a graphic frame (chart, table, SmartArt or OLE object)",
	"contentPart":  "a content part (ink)",
}

// writable resolves an object id that an operation will write (set,
// move, delete, duplicate, reorder, text) and refuses objects the design
// shows read-only. It runs when the operation resolves its target,
// before the operation changes anything; a plan is all or nothing, so a
// refusal writes nothing. (Kinds are known only against the working
// state, since aliases bind during the plan, so Compile cannot check
// them.) Objects named only as a glue end or a reorder anchor are not
// written and are not refused.
func (t *Tx) writable(slide, id string) (edit.Ref, error) {
	ref, err := t.resolve(slide, id)
	if err != nil {
		return ref, err
	}
	kind, err := t.ws.Kind(ref)
	if err != nil {
		return ref, err
	}
	if what, ok := readOnlyKinds[kind]; ok {
		return ref, fmt.Errorf("%w: %s is %s, which is read-only (id, kind, bounds and raw XML are shown; nothing is written)", ErrReadOnlyObject, ref.Short(), what)
	}
	return ref, nil
}

// bind binds a plan alias to ref.
func (t *Tx) bind(alias Opt[string], ref edit.Ref) error {
	if !alias.Set {
		return nil
	}
	a, ok := edit.ParseAlias("$" + alias.V)
	if !ok {
		return fmt.Errorf("alias %q: letters, digits, _ and - only", alias.V)
	}
	return t.ws.Bind(a, ref)
}

// SetTransform is object.set restricted to the transform: each written
// field of Transform replaces the object's slide-space value; the others
// keep the value shown. Writing the value shown is a no-op that touches
// no XML.
type SetTransform struct {
	Slide, Target string
	Units         prop.Units
	Transform     Transform
}

// Name implements Op.
func (*SetTransform) Name() string { return "object.set" }

// apply writes the transform fields given, leaving the others as shown.
func (o *SetTransform) apply(t *Tx) error {
	h, err := t.target(o.Slide, o.Target)
	if err != nil {
		return err
	}
	return setTransform(h, o.Units, o.Transform)
}

// setTransform writes the given box, rotation, or flips, keeping unset fields as shown.
func setTransform(h *edit.Handle, u prop.Units, tf Transform) error {
	if !tf.Box.Set && !tf.Rot.Set && !tf.FlipH.Set && !tf.FlipV.Set {
		return nil
	}
	// A rotation or flip the file stores wrong does not block setting it.
	var setting []string
	for name, set := range map[string]bool{"rot": tf.Rot.Set, "flipH": tf.FlipH.Set, "flipV": tf.FlipV.Set} {
		if set {
			setting = append(setting, name)
		}
	}
	slices.Sort(setting)
	if err := prop.DropInvalid(h, setting...); err != nil {
		return err
	}
	c := prop.Transform(u)
	cur := c.Read(h, 0)
	want, ok := cur.Get()
	if !ok && !tf.Box.Set {
		return fmt.Errorf("%s: %w: %s", h.Ref(), prop.ErrNotDerived, cur.Reason())
	}
	// With no transform at all (a new object), the box is given and an
	// unset rotation or flip is the schema default: absent.
	if tf.Box.Set {
		b := tf.Box.V
		want.X, want.Y, want.W, want.H = u.ToEMU(b[0]), u.ToEMU(b[1]), u.ToEMU(b[2]), u.ToEMU(b[3])
	}
	if tf.Rot.Set {
		want.Rot = tf.Rot.V
	}
	if tf.FlipH.Set {
		want.FlipH = tf.FlipH.V
	}
	if tf.FlipV.Set {
		want.FlipV = tf.FlipV.V
	}
	if err := prop.Set(h, c, want); err != nil {
		return fmt.Errorf("%s: %w", h.Ref(), err)
	}
	return nil
}

// Move is object.move: translate by DX, DY (in Units) applied to the
// exact stored EMU.
type Move struct {
	Slide, Target string
	Units         prop.Units
	DX, DY        float64
}

// Name implements Op.
func (*Move) Name() string { return "object.move" }

// apply moves the target object by the operation's dx and dy in its units.
func (o *Move) apply(t *Tx) error {
	h, err := t.target(o.Slide, o.Target)
	if err != nil {
		return err
	}
	if err := prop.Move(h, prop.Transform(o.Units), o.Units.ToEMU(o.DX), o.Units.ToEMU(o.DY)); err != nil {
		return fmt.Errorf("%s: %w", h.Ref(), err)
	}
	return nil
}

// Delete is object.delete: remove an object (a group with its children).
// Connectors glued to it are left as they are; the edit result reports
// them as dangling-glue. Media its pictures used and nothing else refers
// to is removed with their relationships.
type Delete struct {
	Slide, Target string
}

// Name implements Op.
func (*Delete) Name() string { return "object.delete" }

// apply removes the target object and releases its pictures' unused media.
func (o *Delete) apply(t *Tx) error {
	ref, err := t.writable(o.Slide, o.Target)
	if err != nil {
		return err
	}
	// The media the object's pictures embed (a group's included) is
	// released as an image replace releases it: the relationship when no
	// attribute in the slide uses its id any more, the part when no
	// relationship targets it.
	var rids []string
	if h, err := t.ws.Target(ref); err == nil {
		rids = t.embedded(h)
	}
	if err := t.ws.RemoveObject(ref); err != nil {
		return err
	}
	return t.release(ref.Part(), rids)
}

// Reorder is object.reorder: move an object to the front or back of its
// container (the slide, or its group), or immediately before (behind) or
// after (in front of) another object in the same container.
type Reorder struct {
	Slide string
	Spec  ReorderSpec
}

// Name implements Op.
func (*Reorder) Name() string { return "object.reorder" }

// apply moves the target object to the requested position in its container.
func (o *Reorder) apply(t *Tx) error {
	ref, err := t.writable(o.Slide, o.Spec.Target)
	if err != nil {
		return err
	}
	var at edit.Anchor
	switch o.Spec.To {
	case "front":
		at = edit.Front()
	case "back":
		at = edit.Back()
	default:
		other, err := t.resolve(o.Slide, o.Spec.Ref.V)
		if err != nil {
			return err
		}
		if o.Spec.To == "before" {
			at = edit.Before(other)
		} else {
			at = edit.After(other)
		}
	}
	return t.ws.MoveObject(ref, at)
}

// Duplicate is object.duplicate: copy an object on the same slide,
// immediately in front of the original in the same container, with new
// ids and creation ids (pictures share their media), then move the copy
// by dx, dy when given.
type Duplicate struct {
	Slide string
	Units prop.Units
	Spec  DuplicateSpec
}

// Name implements Op.
func (*Duplicate) Name() string { return "object.duplicate" }

// apply duplicates the target object in front of it, then offsets the copy when requested.
func (o *Duplicate) apply(t *Tx) error {
	ref, err := t.writable(o.Slide, o.Spec.Target)
	if err != nil {
		return err
	}
	cp, err := t.ws.DuplicateObject(ref, t.ids().GUID)
	if err != nil {
		return err
	}
	if err := t.bind(o.Spec.Alias, cp); err != nil {
		return err
	}
	if o.Spec.DX.Set || o.Spec.DY.Set {
		h, err := t.ws.Target(cp)
		if err != nil {
			return err
		}
		if err := prop.Move(h, prop.Transform(o.Units), o.Units.ToEMU(o.Spec.DX.V), o.Units.ToEMU(o.Spec.DY.V)); err != nil {
			return fmt.Errorf("%s: %w", cp, err)
		}
	}
	return nil
}
