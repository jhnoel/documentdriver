package ops

import (
	"os"
	"strings"
	"testing"
)

// TestColorKindsWithModifiers: each of the six colour types is written
// with its modifiers in schema order, in fills and lines, and the same
// plan run again is a no-op (the colour reads back as given).
func TestColorKindsWithModifiers(t *testing.T) {
	for _, c := range []struct{ color, want string }{
		{`{"sys": "windowText", "mods": [{"lumMod": 75000}, {"lumOff": 25000}]}`,
			`<a:sysClr val="windowText"><a:lumMod val="75000"/><a:lumOff val="25000"/></a:sysClr>`},
		{`{"preset": "red", "mods": [{"alpha": 50000}]}`,
			`<a:prstClr val="red"><a:alpha val="50000"/></a:prstClr>`},
		{`{"hsl": [1200000, 100000, 50000], "mods": [{"shade": 50000}]}`,
			`<a:hslClr hue="1200000" sat="100000" lum="50000"><a:shade val="50000"/></a:hslClr>`},
		{`{"scrgb": [100000, 0, 0], "mods": ["inv"]}`,
			`<a:scrgbClr r="100000" g="0" b="0"><a:inv/></a:scrgbClr>`},
		{`{"scheme": "accent1", "mods": [{"satMod": 120000}]}`,
			`<a:schemeClr val="accent1"><a:satMod val="120000"/></a:schemeClr>`},
		{`{"rgb": "4472C4", "mods": [{"tint": 40000}]}`,
			`<a:srgbClr val="4472C4"><a:tint val="40000"/></a:srgbClr>`},
		{`{"sys": "window", "hex": "#FEFEFE"}`, `<a:sysClr val="window" lastClr="FEFEFE"/>`},
	} {
		for _, f := range []struct{ field, around string }{
			{`"fill": ` + c.color, `<a:solidFill>`},
			{`"line": {"color": ` + c.color + `}`, `<a:ln><a:solidFill>`},
		} {
			plan := `{"ops": [{"op": "object.set", "at": "slide1/s10", ` + f.field + `}]}`
			path, _, _ := diagram(t)
			if err := Apply(path, compile(t, plan)...); err != nil {
				t.Errorf("%s: %v", f.field, err)
				continue
			}
			s := slideXML(t, path)
			if !strings.Contains(s, f.around+c.want) {
				t.Errorf("%s: want %s%s in\n%s", f.field, f.around, c.want, s)
				continue
			}
			before, _ := os.ReadFile(path)
			st, _ := os.Stat(path)
			if err := Apply(path, compile(t, plan)...); err != nil {
				t.Errorf("%s again: %v", f.field, err)
				continue
			}
			assertUntouched(t, path, before, st.ModTime())
		}
	}
}

// TestSystemColorLastClr: a system colour keeps the lastClr it has when
// the plan names the same colour, and loses it for another system colour
// given without its displayed value.
func TestSystemColorLastClr(t *testing.T) {
	path, _, _ := diagram(t)
	run := func(fill string) string {
		t.Helper()
		if err := Apply(path, compile(t, `{"ops": [{"op": "object.set", "at": "slide1/s10", "fill": `+fill+`}]}`)...); err != nil {
			t.Fatal(err)
		}
		return slideXML(t, path)
	}
	if s := run(`{"sys": "windowText", "hex": "#000000"}`); !strings.Contains(s, `<a:sysClr val="windowText" lastClr="000000"/>`) {
		t.Fatalf("first write:\n%s", s)
	}
	if s := run(`{"sys": "windowText", "mods": [{"lumMod": 50000}]}`); !strings.Contains(s, `<a:sysClr val="windowText" lastClr="000000"><a:lumMod val="50000"/></a:sysClr>`) {
		t.Fatalf("modifier added:\n%s", s)
	}
	if s := run(`{"sys": "window"}`); !strings.Contains(s, `<a:sysClr val="window"></a:sysClr>`) {
		t.Fatalf("another system colour:\n%s", s)
	}
}
