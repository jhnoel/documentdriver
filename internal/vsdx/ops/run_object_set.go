package ops

import (
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
)

// Run applies object.set: it calls setTransform, setStyle and setEnds
// for the fields the spec gives, in that order, and merges their
// changed and stale reports into one. Changed holds each changed
// address once, in the order the fields first changed it, and stale
// joins the non-empty stale details with "; ". The first field that
// refuses stops the run with its error, so a refused field's own
// refusal is the one reported; the transaction rolls back any field
// already written. A spec whose fields all already show their wanted
// values changes nothing.
func (s *ObjectSetSpec) Run(ws *edit.Workspace, aliases map[string]string, units string, _ ids.Source) (string, []string, []string, string, error) {
	var changed []string
	var staleParts []string
	for _, step := range []func() ([]string, string, error){
		func() ([]string, string, error) { return setTransform(ws, s, aliases, units) },
		func() ([]string, string, error) { return setStyle(ws, s, aliases, units) },
		func() ([]string, string, error) { return setEnds(ws, s, aliases, units) },
	} {
		c, stale, err := step()
		if err != nil {
			return "", nil, nil, "", err
		}
		for _, addr := range c {
			if !slices.Contains(changed, addr) {
				changed = append(changed, addr)
			}
		}
		if stale != "" {
			staleParts = append(staleParts, stale)
		}
	}
	return "", changed, nil, strings.Join(staleParts, "; "), nil
}
