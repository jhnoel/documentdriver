package oracle

import (
	"fmt"
	"io"
	"os"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Variants of an output deck used by the self-test.
const (
	Reference    = "reference"    // the reference solution
	NoOp         = "noop"         // the input, unchanged
	Sloppy       = "sloppy"       // the reference plus a 1 pt nudge of one untargeted object
	Reserialised = "reserialised" // the reference with whitespace added inside every spPr, as a re-serialising tool might
)

// Make writes one variant of a task's output to out. targets are the
// task's targeted input ids; the sloppy variant never nudges those.
func Make(variant, taskID, input, slide string, targets []string, out string) error {
	if variant == NoOp {
		return copyFile(input, out)
	}
	ref, ok := Refs[taskID]
	if !ok && variant != Sloppy {
		if _, isID := Answers[taskID]; isID {
			return copyFile(input, out)
		}
		return fmt.Errorf("oracle: no reference for %s", taskID)
	}
	e, err := Open(input, slide)
	if err != nil {
		return err
	}
	defer e.Close()
	if ref != nil {
		if err := ref(e); err != nil {
			return fmt.Errorf("oracle: %s: %w", taskID, err)
		}
		if err := e.Refresh(); err != nil {
			return err
		}
	}
	switch variant {
	case Sloppy:
		if err := e.nudgeUntargeted(targets); err != nil {
			return err
		}
	case Reserialised:
		e.reserialise()
	}
	return e.SaveAs(out)
}

// nudgeUntargeted moves the first untargeted, pre-existing top-level
// object with its own transform 1 pt to the right.
func (e *Ed) nudgeUntargeted(targets []string) error {
	skip := map[string]bool{}
	for _, t := range targets {
		skip[t] = true
	}
	for _, o := range e.Slide.Objects {
		if skip[o.ID] || e.Added(o.RawID) || o.Parent != "" || o.Ph != nil {
			continue
		}
		if _, err := e.xfrm(o); err != nil {
			continue
		}
		return e.MoveBy(o.ID, emu, 0)
	}
	return fmt.Errorf("oracle: no untargeted object to nudge")
}

// reserialise inserts a newline before every element child inside each
// object's spPr: formatting only, as a pretty-printing tool would write.
func (e *Ed) reserialise() {
	var parents []*tree.Node
	e.doc.Root().Walk(func(n *tree.Node) bool {
		if n.Local() == "spPr" {
			n.Walk(func(c *tree.Node) bool {
				if len(c.Elements()) > 0 {
					parents = append(parents, c)
				}
				return true
			})
			return false
		}
		return true
	})
	for _, p := range parents {
		for _, c := range p.Elements() {
			_ = p.InsertBefore(tree.NewText("\n"), c)
		}
	}
}

// copyFile copies the file at src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
