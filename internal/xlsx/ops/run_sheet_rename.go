package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
)

// SheetRename is sheet.rename.
type SheetRename struct {
	At   SheetAddress `json:"at" doc:"The sheet: sheet:Budget, or $name."`
	Name string       `json:"name" doc:"The new name (rules as for sheet.add)."`
}

// opSheetRename is sheet.rename's entry in the operation table.
var opSheetRename = OpDef{
	Name: "sheet.rename", New: func() Spec { return &SheetRename{} },
	Summary: "Renames a sheet and every reference to it: formulas on every sheet, defined names, and the other references help lists.",
	Rules:   sheetRenameRules,
	Adjusts: renameAdjusts, Refuses: renameRefuses,
}

// Run (chunk G12): not_found for the sheet; a name taken by another
// sheet (ignoring case) is operation_failed (path name); the same name
// is a no-op. First RenameGuard (renameguard.go, K15) over the chart,
// pivot-cache and external-link parts (PartText), refusing with
// operation_failed and the construct as detail. Then it renames
// (WS.RenameSheet), rewrites every cell formula that names the sheet
// (RenameInSheet per worksheet, written with Sheet.SetFormulaText,
// Released when unchanged), defined names (RenameText,
// SetDefinedNameText), and each sheet's conditionalFormatting,
// dataValidations and hyperlinks (RenameElement, renameelement.go, K16;
// Element/SetElement). It records env.Renamed = [old, new] so apply
// rebases earlier units and aliases. Changed: the renamed sheet and
// every sheet whose formulas changed.
func (o *SheetRename) Run(env *Env) error {
	ref, ok := env.WS.SheetRef(o.At.Name)
	if !ok {
		return Refuse(cli.CodeNotFound, "at", "sheet %s not found", o.At.Name)
	}
	if ref.Name == o.Name {
		return nil
	}
	if other, exists := env.WS.SheetRef(o.Name); exists && other.Index != ref.Index {
		return Refuse(cli.CodeOperationFailed, "name", "sheet %s already exists", o.Name)
	}

	var parts RenameParts
	ct, err := env.WS.Package().ContentTypes()
	if err != nil {
		return err
	}
	for _, part := range env.WS.Package().Names() {
		typ, _ := ct.Lookup(part)
		var target *[]string
		switch typ {
		case ctypes.CTDrawingmlChart:
			target = &parts.Charts
		case ctypes.CTSpreadsheetmlPivotCacheDefinition:
			target = &parts.PivotCaches
		case ctypes.CTSpreadsheetmlExternalLink:
			target = &parts.ExternalLinks
		}
		if target != nil {
			text, err := env.WS.PartText(part)
			if err != nil {
				return err
			}
			*target = append(*target, text)
		}
	}
	if err := RenameGuard(parts, ref.Name); err != nil {
		return err
	}
	if err := env.WS.RenameSheet(ref.Name, o.Name); err != nil {
		return FromEdit(err, "at")
	}
	for _, sr := range env.WS.Sheets() {
		if sr.Kind != edit.KindWorksheet {
			continue
		}
		s, err := env.Sheet(sr.Name, "at")
		if err != nil {
			return err
		}
		text, err := s.Text()
		if err != nil {
			return err
		}
		rewrites, err := RenameInSheet(text, ref.Name, o.Name)
		if err != nil {
			return err
		}
		for _, f := range rewrites {
			if err := s.SetFormulaText(f.Row, f.Col, f.Text); err != nil {
				return err
			}
		}
		adjusted := len(rewrites)
		for _, local := range []string{"conditionalFormatting", "dataValidations", "hyperlinks"} {
			xml, present, err := s.Element(local)
			if err != nil {
				return err
			}
			if !present {
				continue
			}
			// Element can return several sibling elements; the kernel scans
			// one XML document, so supply a temporary enclosing root
			// with the worksheet's namespace declarations (an element
			// uses the root's prefixes: x:, r:id, xr:uid).
			const root = "renameRoot"
			open := "<" + root + s.RootDeclarations() + ">"
			out, count, err := RenameElement(local, open+xml+"</"+root+">", ref.Name, o.Name)
			if err != nil {
				return err
			}
			if count > 0 {
				if local != "hyperlinks" {
					adjusted += count
				}
				out = out[len(open) : len(out)-len(root)-3]
				if err := s.SetElement(local, out); err != nil {
					return err
				}
			}
		}
		if adjusted > 0 || strings.EqualFold(sr.Name, o.Name) {
			env.ChangedSheet(sr.Name, adjusted)
		}
		env.WS.Release(sr.Name)
	}
	names, err := env.WS.DefinedNames()
	if err != nil {
		return err
	}
	for i, n := range names {
		text := RenameText(n.Text, ref.Name, o.Name)
		if text != n.Text {
			if err := env.WS.SetDefinedNameText(i, text); err != nil {
				return err
			}
			env.WS.MarkValues()
		}
	}
	// A non-worksheet sheet can still be renamed.
	if ref.Kind != edit.KindWorksheet {
		env.ChangedSheet(o.Name, 0)
	}
	env.Renamed = append(env.Renamed, [2]string{ref.Name, o.Name})
	return nil
}
