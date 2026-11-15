package create

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
)

// TestScaling: create of the families beyond paragraphs (tables with
// spans and styles, lists, images) costs at most linearly more on an input of
// four times the units (laws.Scales; internal/docx/apply's TestScaling
// covers paragraphs).
func TestScaling(t *testing.T) {
	laws.Scales(t, "docx create tables", func(n int) (func() error, error) {
		var b strings.Builder
		for i := range n / 4 {
			fmt.Fprintf(&b, `<table style="width:100%%;"><tr><td colspan="2" style="background-color:#EEEEEE;">Table %d</td></tr>`+
				`<tr><td rowspan="2">a</td><td><b>b</b></td></tr><tr><td>c</td></tr></table>`+"\n", i)
		}
		in := b.String()
		return func() error { _, err := Build(in); return err }, nil
	})
	laws.Scales(t, "docx create lists", func(n int) (func() error, error) {
		var b strings.Builder
		for i := range n / 4 {
			fmt.Fprintf(&b, "<ol><li>item %d<ul><li>nested</li></ul></li><li>two</li></ol>\n", i)
		}
		in := b.String()
		return func() error { _, err := Build(in); return err }, nil
	})
	laws.Scales(t, "docx create images", func(n int) (func() error, error) {
		var b strings.Builder
		for i := range n / 4 {
			// Distinct images (each its own media part), and one
			// repeated.
			fmt.Fprintf(&b, `<p><img src="%s" alt="%d"> <img src="%s"></p>`+"\n", pngURL(t, 1+i%97, 1+i/97), i, pngURL(t, 2, 2))
		}
		in := b.String()
		return func() error { _, err := Build(in); return err }, nil
	})
}
