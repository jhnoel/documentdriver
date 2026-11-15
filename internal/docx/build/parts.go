package build

import (
	"path"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// emptyNumbering is a numbering part without definitions.
const emptyNumbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" + `<w:numbering xmlns:w="` + model.NSW + `"/>`

// Numbering returns the main document's numbering part, adding an empty
// one (word/numbering.xml beside the main part) when it has none, a
// region over its root, and its root.
func Numbering(ws *docxedit.Workspace) (string, *core.Handle, *tree.Node, error) {
	part, ok, err := ws.NumberingPart()
	if err != nil {
		return "", nil, nil, err
	}
	if !ok {
		part = "/" + path.Join(strings.TrimPrefix(path.Dir(ws.DocumentPart()), "/"), "numbering.xml")
		if _, err := ws.AddPart(ctypes.RelNumbering, ctypes.CTWordprocessingmlNumbering, part, emptyNumbering); err != nil {
			return "", nil, nil, err
		}
	}
	h, err := ws.Core().Part(part)
	if err != nil {
		return "", nil, nil, err
	}
	doc, err := ws.Tree(part)
	if err != nil {
		return "", nil, nil, err
	}
	return part, h, doc.Root(), nil
}

// NewListIn is Lists.New over the workspace's numbering part: a fresh
// definition of the template levels, created on first use. The part's
// ids and slots are read once, on the first list, and kept: the lists of
// one lowering cost the same however many came before.
func NewListIn(ws *docxedit.Workspace) func(format string, start uint32) (string, error) {
	var h *core.Handle
	var root, firstNum, cleanup *tree.Node
	var num, abs uint64
	return func(format string, start uint32) (string, error) {
		if h == nil {
			part, ph, r, err := Numbering(ws)
			if err != nil {
				return "", err
			}
			n, err := ws.NumID(part)
			if err != nil {
				return "", err
			}
			a, err := ws.AbstractNumID(part)
			if err != nil {
				return "", err
			}
			if num, err = strconv.ParseUint(n, 10, 64); err != nil {
				return "", err
			}
			if abs, err = strconv.ParseUint(a, 10, 64); err != nil {
				return "", err
			}
			h, root = ph, r
			firstNum, cleanup = numberingSlots(root)
		}
		numID, absID := strconv.FormatUint(num, 10), strconv.FormatUint(abs, 10)
		an, nn, err := listNodes(h, root, numID, absID, TemplateLevels(format, start))
		if err != nil {
			return "", err
		}
		if firstNum, err = placeList(h, root, an, nn, firstNum, cleanup); err != nil {
			return "", err
		}
		num++
		abs++
		return numID, nil
	}
}
