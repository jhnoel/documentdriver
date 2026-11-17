package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
)

// Run applies page.rename.
func (s *PageRenameSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	addr, err := resolvePage(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	if s.Name == "" {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "name", "name must not be empty")
	}
	if err := ws.RenamePage(addr, s.Name); err != nil {
		return "", nil, nil, "", renameRefusal(err, s.Name)
	}
	return "", []string{addr}, nil, "", nil
}

// renameRefusal maps a duplicate-name edit error to operation_failed.
func renameRefusal(err error, name string) error {
	if err != nil && strings.Contains(err.Error(), "already used") {
		return cli.Errorf(cli.CodeOperationFailed, "name", "page name %q is already used", name)
	}
	return err
}
