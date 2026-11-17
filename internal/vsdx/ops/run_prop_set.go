package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run sets a shape data row's value: it checks the row exists, is
// not data-linked and its Value formula is a constant, encodes the
// plan value by the row's type and writes it through the edit layer.
// Changed: the row's address.
func (s *PropSetSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	shapeAddr, rowName, err := resolveProp(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	state, err := ws.PropState(shapeAddr, rowName)
	if err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		var un *edit.Unreadable
		if errors.As(err, &un) {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	if !state.Exists {
		return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "no row named %q on %s", rowName, shapeAddr)
	}
	if state.Linked {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is data-linked", vsd.PropID(shapeAddr, rowName))
	}
	if refusal := propValueRefusal(rowName, state.Formula, state.HasFormula); refusal != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s", refusal)
	}
	if s.Value == nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "value", "value is required")
	}
	v, _, err := encodePropValue(state.TypeName, s.Value)
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "value", "%s", err)
	}
	u := state.Unit
	if err := ws.SetProp(shapeAddr, rowName, v, u); err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		var un *edit.Unreadable
		if errors.As(err, &un) {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	return "", []string{vsd.PropID(shapeAddr, rowName)}, nil, "", nil
}
