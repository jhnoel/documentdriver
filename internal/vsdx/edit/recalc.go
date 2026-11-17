package edit

import (
	"errors"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// customPart is the custom properties part that carries RecalcDocument
// (docs/vsdx-design.md, decision c).
const customPart = "/docProps/custom.xml"

// customTarget is customPart relative to the package relationships.
const customTarget = "docProps/custom.xml"

// The custom properties vocabulary and its value types (OPC document
// properties, ECMA-376 Part 1, 22.3).
const (
	customNS   = "http://schemas.openxmlformats.org/officeDocument/2006/custom-properties"
	customVTNS = "http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"
	// customFMTID is the format id every corpus custom property carries
	// (and the MS how-to's RecalcDocument example).
	customFMTID = "{D5CDD505-2E9C-101B-9397-08002B2CF9AE}"
)

// ErrCustomUnreadable is returned by SetRecalcDocument when
// docProps/custom.xml exists but cannot be read as a Properties part:
// the part is left as it is (a defect the input had never blocks the
// save) and the caller reports that RecalcDocument was not set.
var ErrCustomUnreadable = errors.New("vsdx edit: docProps/custom.xml cannot be read; RecalcDocument not set")

// SetRecalcDocument asks Visio to recalculate the document on open: it
// sets the RecalcDocument custom property true (notes section 2, [C]).
//   - No custom.xml: the part is added (one property, pid 2) with its
//     Override and its package relationship.
//   - A custom.xml with RecalcDocument true: nothing changes.
//   - One with RecalcDocument otherwise: that property's value becomes
//     <vt:bool>true</vt:bool>, nothing else changes.
//   - One without it: a property is added before </Properties> with
//     pid one more than the largest pid there (at least 2), the other
//     bytes kept.
//   - A missing relationship or Override for an existing part is added.
//
// Edits are byte edits of the small part (xmlscan.Apply), declared as a
// whole part. It reports whether anything changed.
func (w *Workspace) SetRecalcDocument() (bool, error) {
	if !w.pkg.Has(customPart) {
		body := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
			`<Properties xmlns="` + customNS + `" xmlns:vt="` + customVTNS + `">` + recalcProperty(2, "") + `</Properties>`
		if err := w.AddPart(customPart, []byte(body), ctypes.CTCustomProperties); err != nil {
			return false, err
		}
		if _, err := w.AddRel("/", ctypes.RelCustomProperties, customTarget); err != nil {
			return false, err
		}
		return true, nil
	}
	pt, err := w.pkg.Part(customPart)
	if err != nil {
		return false, ErrCustomUnreadable
	}
	cur, err := pt.Bytes()
	if err != nil {
		return false, ErrCustomUnreadable
	}
	next, changed, err := recalcEdit(string(cur))
	if err != nil {
		return false, ErrCustomUnreadable
	}
	if changed {
		w.core.DeclarePart(customPart)
		pt.SetBytes([]byte(next))
	}
	fixed, err := w.ensureCustomLinks()
	return changed || fixed, err
}

// ensureCustomLinks adds the package relationship and the Override of
// an existing custom.xml when either is missing, reporting whether it
// added one.
func (w *Workspace) ensureCustomLinks() (bool, error) {
	added := false
	rels, err := w.pkg.Rels("/")
	if err != nil {
		return false, err
	}
	hasRel := false
	for _, rel := range rels.All() {
		if rel.External() || !ctypes.Is(rel.Type, ctypes.RelCustomProperties) {
			continue
		}
		if target, err := rels.TargetPart(rel); err == nil && opc.PartKey(target) == opc.PartKey(customPart) {
			hasRel = true
			break
		}
	}
	if !hasRel {
		if _, err := w.AddRel("/", ctypes.RelCustomProperties, customTarget); err != nil {
			return false, err
		}
		added = true
	}
	ct, err := w.pkg.ContentTypes()
	if err != nil {
		return added, err
	}
	if _, ok := ct.Override(customPart); !ok {
		w.core.DeclarePart(opc.ContentTypesName)
		if err := ct.SetOverride(customPart, ctypes.CTCustomProperties); err != nil {
			return added, err
		}
		added = true
	}
	return added, nil
}

// recalcProperty returns a RecalcDocument property element with pid,
// the vt prefix as the part declares it ("" for "vt").
func recalcProperty(pid int, vt string) string {
	if vt == "" {
		vt = "vt"
	}
	return `<property fmtid="` + customFMTID + `" pid="` + strconv.Itoa(pid) + `" name="RecalcDocument"><` + vt + `:bool>true</` + vt + `:bool></property>`
}

// recalcEdit returns src with RecalcDocument true, and whether it
// changed, by one token walk (xmlscan) and xmlscan.Apply. src that is
// not a Properties part, or that does not lex, is an error.
func recalcEdit(src string) (string, bool, error) {
	var (
		depth, maxPID      = 0, 1
		inRecalc           = false
		rootEnd            = -1
		sawRoot            = false
		valStart, valEnd   = -1, -1
		valText            string
		inValue, foundProp bool
	)
	for i := 0; i < len(src); {
		t, err := xmlscan.Next(src, i)
		if err != nil {
			return "", false, err
		}
		switch t.Kind {
		case xmlscan.StartTag, xmlscan.EmptyTag:
			local := t.Local(src)
			if depth == 0 {
				if local != "Properties" {
					return "", false, errors.New("not a Properties part")
				}
				sawRoot = true
			}
			if depth == 1 && local == "property" {
				if a, ok := xmlscan.FindAttr(src, t, "pid"); ok {
					if n, err := strconv.Atoi(a.Value(src)); err == nil && n > maxPID {
						maxPID = n
					}
				}
				name := ""
				if a, ok := xmlscan.FindAttr(src, t, "name"); ok {
					name = a.Value(src)
				}
				inRecalc = name == "RecalcDocument" && t.Kind == xmlscan.StartTag
				if inRecalc {
					foundProp = true
				}
			}
			if depth == 2 && inRecalc && t.Kind == xmlscan.StartTag {
				inValue = true
				valStart, valEnd = t.Start, -1
			}
			if t.Kind == xmlscan.StartTag {
				depth++
			}
		case xmlscan.EndTag:
			depth--
			if depth == 2 && inValue {
				valEnd = t.End
				inValue = false
			}
			if depth == 1 {
				inRecalc = false
			}
			if depth == 0 {
				rootEnd = t.Start
			}
		case xmlscan.Text:
			if inValue {
				valText += src[t.Start:t.End]
			}
		}
		i = t.End
	}
	if !sawRoot || rootEnd < 0 {
		return "", false, errors.New("no Properties element")
	}
	prefix := vtPrefix(src)
	if foundProp {
		if valStart >= 0 && valEnd > valStart && xmlscan.DecodeText(valText) == "true" && isBoolElement(src, valStart, prefix) {
			return src, false, nil
		}
		if valStart < 0 || valEnd < 0 {
			return "", false, errors.New("RecalcDocument has no value element")
		}
		out, err := xmlscan.Apply(src, []xmlscan.Edit{{Start: valStart, End: valEnd, Text: "<" + prefix + ":bool>true</" + prefix + ":bool>"}})
		return out, err == nil, err
	}
	out, err := xmlscan.Apply(src, []xmlscan.Edit{{Start: rootEnd, End: rootEnd, Text: recalcProperty(maxPID+1, prefix)}})
	return out, err == nil, err
}

// vtPrefix returns the prefix the root element binds to the value types
// namespace, "vt" when it binds none (corpus parts always bind vt).
func vtPrefix(src string) string {
	for i := 0; i < len(src); {
		t, err := xmlscan.Next(src, i)
		if err != nil {
			break
		}
		if t.IsElement() {
			for a := range xmlscan.Attrs(src, t) {
				if name, ok := strings.CutPrefix(a.Name(src), "xmlns:"); ok && a.Value(src) == customVTNS {
					return name
				}
			}
			break
		}
		i = t.End
	}
	return "vt"
}

// isBoolElement reports whether the element starting at start is
// <prefix:bool>.
func isBoolElement(src string, start int, prefix string) bool {
	t, err := xmlscan.Next(src, start)
	return err == nil && t.Name(src) == prefix+":bool"
}
