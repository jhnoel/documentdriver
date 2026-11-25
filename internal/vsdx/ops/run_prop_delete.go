package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run deletes a shape data row: it checks the row exists and is not
// data-linked, and asks the edit layer to remove the local row or hide
// the master's with Del. Changed: none; deleted: the row's address.
func (s *PropDeleteSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	shapeAddr, rowName, err := resolveProp(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	if err := targetRefusal(ws, shapeAddr, "at", false); err != nil {
		return "", nil, nil, "", err
	}
	state, err := ws.PropState(shapeAddr, rowName)
	if err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		if _, ok := errors.AsType[*edit.Unreadable](err); ok {
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
	if err := ws.DeleteProp(shapeAddr, rowName); err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		if _, ok := errors.AsType[*edit.Unreadable](err); ok {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	return "", nil, []string{vsd.PropID(shapeAddr, rowName)}, "", nil
}
