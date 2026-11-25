package ops

import (
	"errors"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
)

// Run applies page.reorder: it resolves at and to through aliases
// and moves the page in pages.xml order through the edit layer. A
// move to where the page already stands (including to itself)
// changes nothing. A missing page is not_found at at or to.
func (s *PageReorderSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	at, err := addAddr(s.At, aliases, "at")
	if err != nil {
		return "", nil, nil, "", err
	}
	to, err := addAddr(s.To, aliases, "to")
	if err != nil {
		return "", nil, nil, "", err
	}
	if s.Position != "before" && s.Position != "after" {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "position", "position must be before or after")
	}
	order, err := bgPages(ws)
	if err != nil {
		return "", nil, nil, "", err
	}
	moved, err := ws.ReorderPage(at, to, s.Position)
	if err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			field := "at"
			if strings.HasSuffix(err.Error(), to) && !strings.HasSuffix(err.Error(), at) {
				field = "to"
			}
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, field, "%s", err)
		}
		if _, ok := errors.AsType[*edit.Unreadable](err); ok {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "", "%s", strings.TrimPrefix(err.Error(), "vsdx edit: "))
	}
	if !moved {
		return "", nil, nil, "", nil
	}
	if err := checkPageOrder(ws, order, "position"); err != nil {
		return "", nil, nil, "", err
	}
	return "", []string{at}, nil, "", nil
}
