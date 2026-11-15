package view

import (
	"maps"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
)

// TestAgentRender is the law that agent mode emits what AgentHTML leaves
// of the full markup, and names the style ids StyleIDs and the src values
// AliasSources finds in it, in
// every view, on every fixture (and the corpus under DOCX_CORPUS).
func TestAgentRender(t *testing.T) {
	opt := Options{Equation: math.OMMLToLaTeX}
	for path, d := range fixtures(t) {
		form := agentForm(LoadStyles(d.Pkg))
		for _, v := range []View{Markup, Final, Original} {
			full, err := Render(d, v, opt)
			got, gerr := RenderAgent(d, v, opt)
			if (err == nil) != (gerr == nil) {
				t.Fatalf("%s view %d: error %v, full render %v", path, v, gerr, err)
			}
			if err != nil {
				continue
			}
			want := form(full.HTML)
			if got.HTML != want {
				i := 0
				for i < len(got.HTML) && i < len(want) && got.HTML[i] == want[i] {
					i++
				}
				lo := max(0, i-80)
				t.Fatalf("%s view %d: agent markup differs at %d:\nagent %q\nwant  %q", path, v, i,
					got.HTML[lo:min(len(got.HTML), i+80)], want[lo:min(len(want), i+80)])
			}
			if ids := StyleIDs(want); !maps.Equal(got.StyleIDs, ids) {
				t.Fatalf("%s view %d: style ids %v, want %v", path, v, got.StyleIDs, ids)
			}
			srcs := map[string]bool{}
			for _, tag := range scanTags(want) {
				for _, a := range tag.attrs {
					if a.name == "src" {
						srcs[a.value] = true
					}
				}
			}
			if !maps.Equal(got.Sources, srcs) {
				t.Fatalf("%s view %d: sources %v, want %v", path, v, got.Sources, srcs)
			}
		}
	}
}
