package integrity

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// checkWorkbook checks workbook.xml and returns its sheets with the parts
// they name.
func (c *checker) checkWorkbook(wb string, rels *opc.Rels) []sheetInfo {
	doc := c.tree(wb)
	if doc == nil {
		return nil
	}
	root := doc.Root()
	if root != nil && root.Is(sml.NSStrict, "workbook") {
		// Strict OOXML: another vocabulary, which nothing here reads.
		c.f.add(PackageStrict, wb, "", "the workbook is Strict OOXML, which is not checked")
		return nil
	}
	if root == nil || !root.Is(sml.NSMain, "workbook") {
		c.f.add(WorkbookRoot, wb, "", "root is not workbook")
		return nil
	}
	var out []sheetInfo
	sheetsEl := root.Child(sml.NSMain, "sheets")
	names, ids, rids := map[string]bool{}, map[string]bool{}, map[string]bool{}
	visible := false
	n := 0
	byID := sml.RelsByID(rels) // one pass: Rels.Get walks every relationship
	if sheetsEl != nil {
		for e := range sheetsEl.ElementsSeq() {
			if !e.Is(sml.NSMain, "sheet") {
				continue
			}
			n++
			info := sheetInfo{name: attr(e, "name")}
			c.checkSheetName(wb, info.name, names)
			if id := attr(e, "sheetId"); id != "" {
				if ids[id] {
					c.f.add(SheetIDDuplicate, wb, id, "sheetId "+id+" is used twice")
				}
				ids[id] = true
			}
			if st := attr(e, "state"); st == "" || st == "visible" {
				visible = true
			}
			rid, _ := e.AttrNS(sml.NSRel, "id")
			if rid != "" {
				if rids[rid] {
					c.f.add(SheetRIDDuplicate, wb, rid, "r:id "+rid+" is used twice")
				}
				rids[rid] = true
			}
			switch r, ok := relOf(byID, rid); {
			case !ok:
				c.f.add(SheetRIDUnresolved, wb, strings.ToLower(info.name), "sheet "+info.name+" has r:id "+rid+", which names no relationship")
			case !r.External():
				if p, err := rels.TargetPart(r); err == nil && c.present(p, wb) {
					info.part = p
					info.isCell = ctypes.Is(r.Type, ctypes.RelWorksheet)
				}
			}
			out = append(out, info)
		}
	}
	switch {
	case n == 0:
		c.f.add(WorkbookNoSheets, wb, "", "the workbook has no sheets")
	case !visible:
		c.f.add(WorkbookNoVisible, wb, "", "every sheet is hidden")
	}
	c.checkDefinedNames(wb, root, n)
	return out
}

// relOf returns the relationship named id and whether it exists.
func relOf(byID map[string]opc.Relationship, id string) (opc.Relationship, bool) {
	r, ok := byID[id]
	return r, ok && id != ""
}

// checkSheetName applies Excel's rules to a sheet name: not empty, at
// most 31 characters, none of : \ / ? * [ ], no apostrophe at either end
// and not "History" (reserved), unique ignoring case.
func (c *checker) checkSheetName(wb, name string, seen map[string]bool) {
	key := strings.ToLower(name)
	if seen[key] {
		c.f.add(SheetNameDuplicate, wb, key, "sheet name "+name+" is used twice (case ignored)")
	}
	seen[key] = true
	switch {
	case name == "":
		c.f.add(SheetNameInvalid, wb, "", "a sheet has no name")
	case strings.ContainsAny(name, ":\\/?*[]"):
		c.f.add(SheetNameInvalid, wb, key, "sheet name "+name+" has a character Excel refuses")
	}
	if utf16Len(name) > maxSheetName {
		c.f.add(LimitSheetName, wb, key, "sheet name "+name+" is longer than 31 characters")
	}
	if strings.HasPrefix(name, "'") || strings.HasSuffix(name, "'") {
		c.f.add(SheetNameApostrophe, wb, key, "sheet name "+name+" starts or ends with an apostrophe")
	}
	if key == "history" {
		c.f.add(SheetNameHistory, wb, "", "sheet name History is reserved")
	}
}

// checkDefinedNames checks the names of the workbook: unique per scope
// ignoring case, valid, not shaped like a cell reference, with a sheet
// scope that exists. What a name refers to is not lexed (pending).
func (c *checker) checkDefinedNames(wb string, root *tree.Node, nsheets int) {
	el := root.Child(sml.NSMain, "definedNames")
	if el == nil {
		return
	}
	seen := map[string]bool{}
	n := 0
	for e := range el.ElementsSeq() {
		if !e.Is(sml.NSMain, "definedName") {
			continue
		}
		n++
		name := attr(e, "name")
		scope := "wb"
		if v, ok := e.Attr("localSheetId"); ok {
			id, ok := sml.ParseIndex(v)
			if !ok || id >= nsheets {
				c.f.add(NameLocalSheet, wb, "", "name "+name+" has localSheetId "+v+" with "+strconv.Itoa(nsheets)+" sheets")
			}
			scope = v
		}
		key := scope + "|" + strings.ToLower(name)
		if seen[key] {
			c.f.add(NameDuplicate, wb, key, "defined name "+name+" is used twice in scope "+scope+" (case ignored)")
		}
		seen[key] = true
		switch {
		case !validName(name):
			c.f.add(NameInvalid, wb, strings.ToLower(name), "defined name "+name+" is not a valid name")
		case formula.LooksLikeCellRef(name):
			c.f.add(NameRefShaped, wb, strings.ToLower(name), "defined name "+name+" looks like a cell reference")
		}
		if utf16Len(name) > maxDefinedName {
			c.f.add(LimitDefinedNameSize, wb, "", "defined name of more than 255 characters")
		}
	}
	if n == 0 {
		c.f.add(NameEmptyContainer, wb, "", "definedNames has no definedName")
	}
}

// validName reports whether s can be a defined name: a letter, _ or \
// first, then letters, digits, _ . \ and ?, no spaces.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case unicode.IsLetter(r) || r == '_' || r == '\\':
		case i > 0 && (unicode.IsDigit(r) || r == '.' || r == '?'):
		default:
			return false
		}
	}
	return true
}
