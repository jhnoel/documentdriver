package ops

import (
	"errors"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
)

// Run applies page.delete.
func (s *PageDeleteSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	addr, err := resolvePage(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	if err := ws.DeletePage(addr); err != nil {
		var u *edit.Unreadable
		if errors.Is(err, edit.ErrNotFound) || errors.As(err, &u) {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s", strings.TrimPrefix(err.Error(), "vsdx edit: "))
	}
	return "", nil, []string{addr}, "", nil
}
