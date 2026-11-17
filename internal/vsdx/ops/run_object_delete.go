package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
)

// Run implements object.delete: it deletes the shape at At with its
// children, every Connect row naming them, and the glue formulas of
// surviving connectors glued to them, keeping each freed cell's V.
// Changed: the freed connectors' addresses. Deleted: the deleted
// shapes' addresses. A master shape and a stencil are refused with
// operation_failed at at; a missing shape is not_found at at; an
// unreadable page part is unreadable_target.
func (s *ObjectDeleteSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	shapeAddr, err := resolveShape(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	d, err := doc.Open(ws.Package())
	if err != nil {
		return "", nil, nil, "", err
	}
	if d.Kind == doc.Stencil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is in a stencil: stencils cannot be edited", shapeAddr)
	}
	deleted, changed, err := ws.DeleteShape(shapeAddr)
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
	return "", changed, deleted, "", nil
}
