package edit

import (
	"fmt"
	"path"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// emptySettings is a new document settings part (headers.rs
// EMPTY_SETTINGS).
const emptySettings = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"></w:settings>`

// SettingsPart returns the document settings part the main document's
// relationships name, or "" when it has none.
func (w *Workspace) SettingsPart() (string, error) {
	parts, err := w.relatedParts(ctypes.RelSettings)
	if err != nil || len(parts) == 0 {
		return "", err
	}
	return parts[0], nil
}

// EnsureSettingsPart returns the document settings part, adding an empty
// one beside the main document (settings.xml, related and registered)
// when the document has none. It refuses up front, adding nothing, when
// the content types stream cannot register the part (opc.ErrContentTypes)
// or the name is taken by a part the document does not relate to. It
// declares what it changes.
func (w *Workspace) EnsureSettingsPart() (string, error) {
	name, err := w.SettingsPart()
	if err != nil || name != "" {
		return name, err
	}
	if err := w.pkg.RegistersTypes(); err != nil {
		return "", err
	}
	name = path.Join(path.Dir(w.doc), "settings.xml")
	if w.pkg.Has(name) {
		return "", fmt.Errorf("edit: %s exists but the document does not relate to it as settings", name)
	}
	w.core.DeclarePart(name)
	w.core.DeclarePart(opc.ContentTypesName)
	if _, err := w.pkg.AddPart(name, []byte(emptySettings), ctypes.CTWordprocessingmlSettings); err != nil {
		return "", err
	}
	rels, err := w.pkg.Rels(w.doc)
	if err != nil {
		return "", err
	}
	w.core.DeclarePart(rels.Name())
	if _, err := rels.Add(ctypes.RelSettings, path.Base(name), false); err != nil {
		return "", err
	}
	return name, nil
}
