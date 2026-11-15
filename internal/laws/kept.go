package laws

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// Kept is the untargeted law for an operation that may remove or merge
// the units it targets (settling a paragraph's revisions can join it
// with the next one, or remove it): in part, every unit of src for which
// owns reports false keeps its own bytes in dst, in the same order, and
// so does the document outside its units; units owns reports true may
// change, go or merge, and dst may hold units between the kept ones.
// Own bytes are compared with nested units left out entirely (not
// masked), so a container whose owned child went still compares equal.
// Other parts are not compared; use SameParts for them.
func Kept(src, dst, part string, units plan.Units, owns func(e *tree.Node) bool) error {
	pa, err := Parts(src)
	if err != nil {
		return err
	}
	pb, err := Parts(dst)
	if err != nil {
		return err
	}
	var a, b []byte
	for n := range pa {
		if strings.EqualFold(strings.TrimPrefix(n, "/"), strings.TrimPrefix(part, "/")) {
			a, b = pa[n], pb[n]
		}
	}
	if a == nil || b == nil {
		return fmt.Errorf("no part %s on both sides", part)
	}
	da, err := tree.Parse(a)
	if err != nil {
		return err
	}
	db, err := tree.Parse(b)
	if err != nil {
		return fmt.Errorf("%s does not parse after the plan: %w", part, err)
	}
	if !units.HasUnits(da) {
		return nil
	}
	ua, ub := unitList(da, units), unitList(db, units)
	own := func(n *tree.Node) []byte { return bytes.ReplaceAll(ownBytes(n, units), []byte("\x00unit\x00"), nil) }
	if !bytes.Equal(own(ua[0]), own(ub[0])) {
		return fmt.Errorf("%s: the document outside its units changed", part)
	}
	var errs []error
	j := 1
	for _, u := range ua[1:] {
		if owns(u) {
			continue
		}
		want := own(u)
		for j < len(ub) && !bytes.Equal(own(ub[j]), want) {
			j++
		}
		if j == len(ub) {
			errs = append(errs, fmt.Errorf("%s: untargeted %s changed or moved", part, units.Describe(u)))
			break
		}
		j++
	}
	return errors.Join(errs...)
}
