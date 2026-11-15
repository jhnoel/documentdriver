package view

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// nonNegative parses a finite, non-negative length.
func nonNegative(v string, ok bool) (float64, bool) {
	if !ok {
		return 0, false
	}
	f, ok := parseRustF64(v)
	if !ok || f != f || f < 0 || f > 1.7976931348623157e308 {
		return 0, false
	}
	return f, true
}

// attrF returns attribute local of n as a finite non-negative float, or false.
func attrF(n *tree.Node, local string) (float64, bool) {
	v, ok := model.Attr(n, local)
	return nonNegative(v, ok)
}

// tableCSS is the CSS of a table, row or cell's supported direct
// properties (docxdriver's commands/tables.rs css): widths, layout,
// alignment, shading, cell alignment, row height, cantSplit, cell spacing,
// margins and borders. With original, a tracked property change's
// snapshot stands in for the properties.
func tableCSS(n *tree.Node, tag string, original bool) string {
	p := model.ChildW(n, tag)
	if p == nil {
		return ""
	}
	if original {
		if ch := model.ChildW(p, tag+"Change"); ch != nil {
			if old := model.ChildW(ch, tag); old != nil {
				p = old
			}
		}
	}
	var b strings.Builder
	for c := range p.ChildrenSeq() {
		v := model.AttrOr(c, "val")
		switch localName(c) {
		case "tblW", "tcW":
			if w, ok := attrF(c, "w"); ok {
				switch model.AttrOr(c, "type") {
				case "dxa":
					b.WriteString("width:" + formatFloat(w/20) + "pt;")
				case "pct":
					b.WriteString("width:" + formatFloat(w/50) + "%;")
				case "auto":
					b.WriteString("width:auto;")
				}
			}
		case "tblLayout":
			switch model.AttrOr(c, "type") {
			case "fixed":
				b.WriteString("table-layout:fixed;")
			case "autofit":
				b.WriteString("table-layout:auto;")
			}
		case "jc":
			switch v {
			case "center":
				b.WriteString("margin-left:auto;margin-right:auto;")
			case "right":
				b.WriteString("margin-left:auto;margin-right:0;")
			case "left":
				b.WriteString("margin-left:0;margin-right:auto;")
			}
		case "shd":
			if fill, ok := model.Attr(c, "fill"); ok && len(fill) == 6 && allHex(fill) {
				b.WriteString("background-color:#" + fill + ";")
			}
		case "vAlign":
			switch v {
			case "top", "bottom":
				b.WriteString("vertical-align:" + v + ";")
			case "center":
				b.WriteString("vertical-align:middle;")
			}
		case "trHeight":
			if h, ok := attrF(c, "val"); ok {
				b.WriteString("height:" + formatFloat(h/20) + "pt;")
			}
		case "cantSplit":
			if !offValues[v] {
				b.WriteString("break-inside:avoid;")
			}
		case "tblCellSpacing":
			if w, ok := attrF(c, "w"); ok {
				b.WriteString("border-collapse:separate;border-spacing:" + formatFloat(w/20) + "pt;")
			}
		case "tcMar", "tblCellMar":
			for _, side := range []string{"top", "left", "bottom", "right"} {
				if s := model.ChildW(c, side); s != nil {
					if w, ok := attrF(s, "w"); ok {
						b.WriteString("padding-" + side + ":" + formatFloat(w/20) + "pt;")
					}
				}
			}
		case "tcBorders", "tblBorders":
			for _, side := range []string{"top", "left", "bottom", "right"} {
				s := model.ChildW(c, side)
				if s == nil {
					continue
				}
				style := ""
				switch model.AttrOr(s, "val") {
				case "nil", "none":
					style = "none"
				case "double":
					style = "double"
				case "dotted":
					style = "dotted"
				case "dashed", "dotDash", "dotDotDash":
					style = "dashed"
				case "single", "triple", "wave", "doubleWave":
					style = "solid"
				default:
					continue
				}
				width := 0.5
				if sz, ok := model.ParseU32(model.AttrOr(s, "sz")); ok {
					width = float64(sz) / 8
				}
				color := "currentColor"
				if col, ok := model.Attr(s, "color"); ok && len(col) == 6 && allHex(col) {
					color = "#" + col
				}
				b.WriteString("border-" + side + ":" + formatFloat(width) + "pt " + style + " " + color + ";")
			}
		}
	}
	return b.String()
}

// TableInventory is docxdriver's table inventory (commands/tables.rs
// inventory): every body table with its grid, properties, rows and cells.
func TableInventory(x *model.XML) []any {
	out := []any{}
	for ti, t := range Tables(x) {
		at := "table:" + itoa(ti+1)
		grid := []any{}
		if g := model.ChildW(t, "tblGrid"); g != nil {
			for _, col := range childrenW(g, "gridCol") {
				if w, ok := model.ParseI64(model.AttrOr(col, "w")); ok {
					grid = append(grid, w)
				} else {
					grid = append(grid, nil)
				}
			}
		}
		rows := []any{}
		for ri, r := range childrenW(t, "tr") {
			ra := at + "/row:" + itoa(ri+1)
			cells := []any{}
			// column is the grid column the next cell starts at: a cell
			// address counts cells (the td position), a column address
			// counts grid columns, and the columns a row skips
			// (w:gridBefore) or a cell that spans several makes the two
			// differ.
			column := 1 + GridBefore(r)
			for ci, c := range childrenW(r, "tc") {
				var span any = 1
				width := uint64(1)
				var merge any
				logical := true
				if tcpr := model.ChildW(c, "tcPr"); tcpr != nil {
					if gs := model.ChildW(tcpr, "gridSpan"); gs != nil {
						if v, ok := model.ParseU64(model.AttrOr(gs, "val")); ok {
							span = jsonUint(v)
							width = max(v, 1)
						}
					}
					if vm := model.ChildW(tcpr, "vMerge"); vm != nil {
						m, ok := model.Attr(vm, "val")
						if !ok {
							m = "continue"
						}
						merge = m
						logical = m != "continue"
					}
				}
				cells = append(cells, map[string]any{
					"id": ra + "/cell:" + itoa(ci+1), "physical_index": ci + 1, "grid_span": span,
					"grid_column":    jsonUint(column),
					"vertical_merge": merge, "logical": logical, "properties": properties(c, "tcPr"),
				})
				column += width
			}
			rows = append(rows, map[string]any{"id": ra, "properties": properties(r, "trPr"), "cells": cells})
		}
		out = append(out, map[string]any{"id": at, "grid_twips": grid, "properties": properties(t, "tblPr"), "rows": rows})
	}
	return out
}

// properties lists a property container's children (except tracked
// change records) by local name, each with its attributes by local name;
// a non-element child is listed under "" with none.
func properties(n *tree.Node, tag string) map[string]any {
	props := map[string]any{}
	p := model.ChildW(n, tag)
	if p == nil {
		return props
	}
	for c := range p.ChildrenSeq() {
		name := localName(c)
		if strings.HasSuffix(name, "Change") {
			continue
		}
		props[name] = AttrMap(c)
	}
	return props
}

// AttrMap is an element's attributes by local name (namespace
// declarations by their declared prefix; a later duplicate wins).
func AttrMap(n *tree.Node) map[string]any {
	attrs := map[string]any{}
	if !model.IsElement(n) {
		return attrs
	}
	for name, value := range n.AttrsSeq() {
		if i := strings.LastIndexByte(name, ':'); i >= 0 {
			name = name[i+1:]
		}
		attrs[name] = value
	}
	return attrs
}

// AgentTableInventory is the inventory without what positions already
// give (commands/tables.rs agent_inventory): no row or cell handles or
// physical indexes, grid_span 1 and logical true omitted, and empty values
// pruned.
func AgentTableInventory(x *model.XML) []any {
	tables := TableInventory(x)
	for _, t := range tables {
		for _, r := range t.(map[string]any)["rows"].([]any) {
			row := r.(map[string]any)
			delete(row, "id")
			for _, c := range row["cells"].([]any) {
				cell := c.(map[string]any)
				delete(cell, "id")
				delete(cell, "physical_index")
				if cell["grid_span"] == 1 {
					delete(cell, "grid_span")
				}
				if cell["logical"] == true {
					delete(cell, "logical")
				}
			}
		}
	}
	return PruneEmpty(tables).([]any)
}

// PruneEmpty drops null values and empty objects and arrays from objects,
// recursively, except inside a "properties" value (commands/mod.rs
// prune_empty). Arrays keep their items.
func PruneEmpty(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			if k != "properties" {
				val = PruneEmpty(val)
			}
			if isEmptyJSON(val) {
				continue
			}
			out[k] = val
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, it := range x {
			out[i] = PruneEmpty(it)
		}
		return out
	}
	return v
}

// isEmptyJSON reports whether v is null, an empty object or an empty array.
func isEmptyJSON(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	}
	return false
}

// jsonUint is v as an int when it fits, so that it compares equal to int
// literals, else as a uint64.
func jsonUint(v uint64) any {
	if v <= uint64(int(^uint(0)>>1)) {
		return int(v)
	}
	return v
}

// GridBefore is the number of grid columns row tr skips before its first
// cell (w:trPr/w:gridBefore), as the table operations' grid model reads
// it.
func GridBefore(tr *tree.Node) uint64 {
	trpr := model.ChildW(tr, "trPr")
	if trpr == nil {
		return 0
	}
	gb := model.ChildW(trpr, "gridBefore")
	if gb == nil {
		return 0
	}
	return GridBeforeVal(model.AttrOr(gb, "val"))
}

// GridBeforeVal reads a w:gridBefore val: 0 when it is not a count up to
// 65536.
func GridBeforeVal(v string) uint64 {
	n, ok := model.ParseU32(v)
	if !ok || n > 1<<16 {
		return 0
	}
	return uint64(n)
}
