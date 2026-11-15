package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// TestCompileRejectsOutOfSchema: a value outside the schema type of the
// attribute it would be written to fails the plan at compile, before the
// deck is opened, naming the field, the value and some valid values.
func TestCompileRejectsOutOfSchema(t *testing.T) {
	set := func(f string) string { return `{"op": "object.set", "at": "slide1/s10", ` + f + `}` }
	for _, c := range []struct{ op, field, value, valid string }{
		{set(`"geom": "notAShape"`), "geom", "notAShape", "rtTriangle"},
		{`{"op": "object.add", "at": "slide1", "kind": "shape", "name": "x", "geom": "notAShape", "box": [1, 2, 3, 4]}`, "geom", "notAShape", "rect"},
		{set(`"line": {"dash": "foo"}`), "line dash", "foo", "dashDot"},
		{set(`"line": {"cap": "round"}`), "line cap", "round", "rnd"},
		{set(`"line": {"tail": "foo"}`), "line tail type", "foo", "triangle"},
		{set(`"line": {"head": "triangle w=huge"}`), "line head w", "huge", "med"},
		{set(`"line": {"head": "triangle len=huge"}`), "line head len", "huge", "lg"},
		{set(`"line": {"w": 1600}`), "line w 1600", "20320000", "20116800"},
		{set(`"line": {"w": -1}`), "line w -1", "-12700", "from 0"},
		{set(`"fill": {"scheme": "accent9"}`), "fill: colour: scheme colour", "accent9", "accent1"},
		{set(`"fill": {"preset": "reddish"}`), "fill: colour: prst colour", "reddish", "aliceBlue"},
		{set(`"fill": {"sys": "notASystemColour"}`), "fill: colour: sys colour", "notASystemColour", "scrollBar"},
		{set(`"line": {"color": {"hsl": [21600000, 0, 0]}}`), "line color: colour: hsl hue", "21600000", "21599999"},
		{set(`"fill": {"scheme": "accent1", "mods": [{"alpha": 100001}]}`), "modifier alpha", "100001", "100000"},
		{set(`"fill": {"scheme": "accent1", "mods": [{"frob": 1}]}`), "fill: colour", "frob", "tint"},
		{set(`"fill": {"scheme": "accent1", "mods": ["lumMod"]}`), "modifier lumMod", "", "needs a value"},
		{set(`"from_glue": "s11:-1"`), "from_glue", "-1", "0 to 4294967295"},
		{set(`"box": [0, 0, -5, 10]`), "box width -5", "-63500", "from 0"},
	} {
		p, err := DecodePlan(strings.NewReader(`{"ops": [` + c.op + `]}`))
		if err != nil {
			t.Fatalf("%s: %v", c.op, err)
		}
		_, err = p.Compile()
		if err == nil {
			t.Errorf("%s: compiled", c.op)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, c.field) || !strings.Contains(msg, c.value) || !strings.Contains(msg, c.valid) {
			t.Errorf("%s: %v\nwant it to name %q, %q and %q", c.op, err, c.field, c.value, c.valid)
		}
		if c.value != "" && c.field != "fill: colour" && !errors.Is(err, edit.ErrSchema) {
			t.Errorf("%s: %v does not wrap edit.ErrSchema", c.op, err)
		}
	}
}

// TestCompileRejectsPlaceholderColor: phClr, valid only in a theme's
// style matrix, is refused as a slide colour wherever a plan gives one.
func TestCompileRejectsPlaceholderColor(t *testing.T) {
	for _, op := range []string{
		`{"op": "object.set", "at": "slide1/s10", "fill": {"scheme": "phClr"}}`,
		`{"op": "object.set", "at": "slide1/s10", "fill": {"scheme": "phClr", "mods": [{"lumMod": 50000}]}}`,
		`{"op": "object.set", "at": "slide1/s10", "line": {"color": {"scheme": "phClr"}}}`,
		`{"op": "object.add", "at": "slide1", "kind": "shape", "name": "x", "geom": "rect", "box": [1, 2, 3, 4], "fill": {"scheme": "phClr"}}`,
		`{"op": "text.set", "at": "slide1/s10", "html": "<span style=\"color:var(--phClr)\">x</span>"}`,
		`{"op": "object.set", "at": "slide1/s10", "html": "<span data-color=\"phClr lumMod=50000\">x</span>"}`,
	} {
		p, err := DecodePlan(strings.NewReader(`{"ops": [` + op + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Compile(); err == nil || !strings.Contains(err.Error(), "phClr is the placeholder colour") {
			t.Errorf("%s: %v", op, err)
		}
	}
}

// TestCompileAcceptsSchemaValues: values the schema allows compile,
// including the tool's own words for shown-only values.
func TestCompileAcceptsSchemaValues(t *testing.T) {
	for _, op := range []string{
		`{"op": "object.set", "at": "slide1/s10", "geom": "custom"}`,
		`{"op": "object.set", "at": "slide1/s10", "line": {"dash": "custom", "cap": "flat", "w": 1584, "head": "stealth w=lg len=sm"}}`,
		`{"op": "object.set", "at": "slide1/s10", "fill": {"sys": "windowText", "mods": [{"lumMod": 50000}, "inv"]}}`,
		`{"op": "object.set", "at": "slide1/s10", "fill": {"type": "gradFill"}}`,
		`{"op": "object.set", "at": "slide1/s12", "from_glue": "#999:4", "to_glue": "none"}`,
	} {
		p, err := DecodePlan(strings.NewReader(`{"ops": [` + op + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Compile(); err != nil {
			t.Errorf("%s: %v", op, err)
		}
	}
}
