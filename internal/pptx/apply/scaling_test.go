package apply

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/view"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// TestScaling is the pptx half of the scaling gate (laws.Scales): read
// and apply cost at most linearly more on a slide of four times the
// shapes.
func TestScaling(t *testing.T) {
	deck := func(n int) string {
		return testdeck.MinimalWith(t, map[string][]byte{"ppt/slides/slide1.xml": testdeck.ManyShapesSlide(n)})
	}
	laws.Scales(t, "pptx read", func(n int) (func() error, error) {
		path := deck(n)
		return func() error {
			p, err := opc.Open(path)
			if err != nil {
				return err
			}
			defer p.Close()
			d, err := model.OpenDeck(p)
			if err != nil {
				return err
			}
			s, err := d.ReadSlide("/ppt/slides/slide1.xml")
			if err != nil {
				return err
			}
			_, err = view.JSON(s, view.Options{})
			return err
		}, nil
	})
	laws.Scales(t, "pptx apply object.set", func(n int) (func() error, error) {
		path := deck(n)
		plan := fmt.Appendf(nil, `{"ops": [{"op": "object.set", "at": "slide1/s%d", "rot": 15}]}`, n/2)
		out := filepath.Join(t.TempDir(), "out.pptx")
		return func() error {
			_, err := Run(path, plan, Options{Out: out})
			return err
		}, nil
	})
}
