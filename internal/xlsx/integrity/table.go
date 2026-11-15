package integrity

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// tableInfo is a table part as the checks need it.
type tableInfo struct {
	part    string
	ref     rect
	headers int // headerRowCount
	names   []string
}

// readTable parses a table part (a small part, as a tree) and checks what
// needs no sheet: its ref, header row count, column list and names.
func (c *checker) readTable(part string, ids, names map[string]bool) (tableInfo, bool) {
	doc := c.tree(part)
	if doc == nil {
		return tableInfo{}, false
	}
	root := doc.Root()
	if root == nil || !root.Is(sml.NSMain, "table") {
		c.f.add(TableRef, part, "root", "root is not table")
		return tableInfo{}, false
	}
	t := tableInfo{part: part, headers: 1}
	ref := attr(root, "ref")
	r, ok := parseRange(ref)
	if !ok || !r.inLimits() {
		c.f.add(TableRef, part, "", "table ref "+ref)
		return tableInfo{}, false
	}
	t.ref = r
	if v, ok := root.Attr("headerRowCount"); ok {
		n, ok := sml.ParseIndex(v)
		if !ok || n > 1 {
			c.f.add(TableHeaderRows, part, v, "headerRowCount="+v)
		} else {
			t.headers = n
		}
	}
	if v := attr(root, "id"); v != "" {
		if ids[v] {
			c.f.add(TableIDDuplicate, part, v, "table id "+v+" is used twice")
		}
		ids[v] = true
	}
	for _, n := range []string{"name", "displayName"} {
		if v := strings.ToLower(attr(root, n)); v != "" {
			if names[v] {
				c.f.add(TableNameDuplicate, part, v, n+" "+v+" is used twice")
			}
			names[v] = true
			if n == "name" && attr(root, "displayName") != "" {
				break
			}
		}
	}
	cols := root.Child(sml.NSMain, "tableColumns")
	seen := map[string]bool{}
	if cols != nil {
		for e := range cols.ElementsSeq() {
			name := attr(e, "name")
			t.names = append(t.names, name)
			if k := strings.ToLower(name); seen[k] {
				c.f.add(TableColumnDup, part, k, "column name "+name+" is used twice")
			} else {
				seen[k] = true
			}
		}
		if v, ok := cols.Attr("count"); ok {
			if n, ok := sml.ParseIndex(v); !ok || n != len(t.names) {
				c.f.add(TableColumnCount, part, "count", "tableColumns count "+v+", "+strconv.Itoa(len(t.names))+" columns")
			}
		}
	}
	if width := r.c2 - r.c1 + 1; len(t.names) != width {
		c.f.add(TableColumnCount, part, "ref", "ref "+ref+" is "+strconv.Itoa(width)+" columns wide, "+strconv.Itoa(len(t.names))+" tableColumns")
	}
	if af := root.Child(sml.NSMain, "autoFilter"); af != nil {
		if ar, ok := parseRange(attr(af, "ref")); !ok || ar.c1 < r.c1 || ar.c2 > r.c2 || ar.r1 < r.r1 || ar.r2 > r.r2 {
			c.f.add(TableFilter, part, "", "autoFilter ref "+attr(af, "ref")+" is not within "+ref)
		}
	}
	return t, true
}

// checkHeaders compares each table's header cells with its column names:
// Excel repairs a workbook whose header text differs from the name. text
// resolves shared string indexes.
func (c *checker) checkHeaders(sheet string, ts []tableInfo, hdr map[[2]int]headerCell, text map[int]string) {
	for _, t := range ts {
		if t.headers != 1 {
			continue
		}
		for i, name := range t.names {
			col, row := t.ref.c1+i, t.ref.r1
			h, ok := hdr[[2]int{row, col}]
			if !ok || !h.set {
				c.f.add(TableHeaderMissing, t.part, strings.ToLower(name), "no header cell "+cellName(col, row)+" on "+sheet+" for column "+name)
				continue
			}
			got := h.text
			if h.t == "s" {
				n, _ := sml.ParseIndex(got)
				got = text[n]
			}
			if sml.UnescapeX(got) != sml.UnescapeX(name) {
				c.f.add(TableHeaderMismatch, t.part, strings.ToLower(name), "header "+cellName(col, row)+" on "+sheet+" reads "+got+", the column is named "+name)
			}
		}
	}
}
