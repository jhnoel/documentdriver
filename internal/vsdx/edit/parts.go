package edit

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/opc"
)

// AddPart adds a part of the given content type (a ctypes constant),
// declared as a whole with the content types stream, as the xlsx edit
// layer's AddPart does. A new page and docProps/custom.xml are added
// through it.
func (w *Workspace) AddPart(name string, data []byte, contentType string) error {
	w.core.DeclarePart(name)
	w.core.DeclarePart(opc.ContentTypesName)
	if _, err := w.pkg.AddPart(name, data, contentType); err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	return nil
}

// AddRel adds a relationship of relType (a ctypes constant) from source
// ("/" for the package) to target, relative to the source as stored,
// declaring the relationships part, and returns its id.
func (w *Workspace) AddRel(source, relType, target string) (string, error) {
	rels, err := w.pkg.Rels(source)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: %w", err)
	}
	w.core.DeclarePart(rels.Name())
	id, err := rels.Add(relType, target, false)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: %w", err)
	}
	return id, nil
}
