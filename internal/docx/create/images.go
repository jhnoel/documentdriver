package create

import (
	"crypto/sha256"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/docx/build"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// media allocates the pictures of one created document: one media part
// per distinct image (word/media/imageN.<ext>), one relationship per part
// that shows it, and drawing ids counted from 1 across the document.
type media struct {
	ws *docxedit.Workspace
	// parts are the media parts by the SHA-256 of their bytes, with the
	// image's size.
	parts map[[32]byte]mediaPart
	// rels are the relationship ids by source part and media part.
	rels map[[2]string]string
	// next is the last drawing id given, last the last media part
	// number taken.
	next, last int
}

type mediaPart struct {
	name          string
	width, height int
}

// newMedia returns an empty media collector bound to the workspace.
func newMedia(ws *docxedit.Workspace) *media {
	return &media{ws: ws, parts: map[[32]byte]mediaPart{}, rels: map[[2]string]string{}}
}

// in is build.Options.Image for pictures in source, a part of the
// package.
func (m *media) in(source string) func(src string) (build.Picture, error) {
	return func(src string) (build.Picture, error) {
		key := sha256.Sum256([]byte(src))
		mp, ok := m.parts[key]
		rk := [2]string{source, ""}
		if !ok {
			img, err := build.DecodeImage(src)
			if err != nil {
				return build.Picture{}, err
			}
			name := m.partName(img.Ext)
			rid, err := m.ws.AddPartFrom(source, ctypes.RelImage, img.ContentType, name, img.Data)
			if err != nil {
				return build.Picture{}, err
			}
			mp = mediaPart{name: name, width: img.Width, height: img.Height}
			m.parts[key] = mp
			rk[1] = name
			m.rels[rk] = rid
		}
		rk[1] = mp.name
		rid, ok := m.rels[rk]
		if !ok {
			var err error
			if rid, err = m.ws.RelateFrom(source, ctypes.RelImage, mp.name); err != nil {
				return build.Picture{}, err
			}
			m.rels[rk] = rid
		}
		m.next++
		return build.Picture{RID: rid, ID: m.next, Width: mp.width, Height: mp.height}, nil
	}
}

// partName is the next free word/media/imageN.ext, counting on from the
// last one taken, so naming a part costs the same however many came
// before (ws.PartName counts from 1).
func (m *media) partName(ext string) string {
	for {
		m.last++
		name := "/word/media/image" + strconv.Itoa(m.last) + "." + ext
		if !m.ws.Package().Has(name) {
			return name
		}
	}
}
