package ops

import (
	"errors"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run applies object.reorder: it resolves at and (for a shape in to)
// to through aliases, refuses masters, and moves the shape in paint
// order within its own parent through the edit layer. A move to where
// the shape already stands changes nothing. An anchor in another
// parent (or on another page) is operation_failed at to; a missing
// shape is not_found at at or to.
func (s *ObjectReorderSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	at, err := addAddr(s.At, aliases, "at")
	if err != nil {
		return "", nil, nil, "", err
	}
	if err := reorderShapeAddr(at, "at"); err != nil {
		return "", nil, nil, "", err
	}
	if err := targetRefusal(ws, at, "at", false); err != nil {
		return "", nil, nil, "", err
	}
	to := s.To
	if to != "front" && to != "back" {
		resolved, err := addAddr(s.To, aliases, "to")
		if err != nil {
			return "", nil, nil, "", err
		}
		if err := reorderShapeAddr(resolved, "to"); err != nil {
			return "", nil, nil, "", err
		}
		if resolved == at {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "to", "%s cannot be placed relative to itself", resolved)
		}
		to = resolved
	}
	if (to == "front" || to == "back") && s.Position != "" {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "position", "position goes with a shape in to, not front or back")
	}
	if to != "front" && to != "back" && s.Position != "before" && s.Position != "after" {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "position", "position (before or after) is needed with a shape in to")
	}
	moved, err := ws.ReorderShape(at, to, s.Position)
	if err != nil {
		if errors.Is(err, edit.ErrDifferentParent) {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "to", "%s", err)
		}
		if errors.Is(err, edit.ErrNotFound) {
			field := "at"
			if to != "front" && to != "back" && strings.Contains(err.Error(), to) {
				field = "to"
			}
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, field, "%s", err)
		}
		var un *edit.Unreadable
		if errors.As(err, &un) {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	if !moved {
		return "", nil, nil, "", nil
	}
	return "", []string{at}, nil, "", nil
}

// reorderShapeAddr reports whether addr is a page shape address (not
// a master shape) for the field field of object.reorder.
func reorderShapeAddr(addr, field string) error {
	a, err := vsd.Parse(addr)
	if err != nil || a.Kind != vsd.KindShape {
		return cli.Errorf(cli.CodeOperationFailed, field, "%s is not a shape address", addr)
	}
	if a.InMaster {
		return cli.Errorf(cli.CodeOperationFailed, field, "%s is a master shape: masters cannot be edited", addr)
	}
	return nil
}
