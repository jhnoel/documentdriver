package main

import (
	"slices"
	"strings"
)

// replaceEquation files a paired replace_equation plan.
func replaceEquation(g *gen, kind string, f *docFacts, note, at string, n any, display any, pair int) *WritePlan {
	e := equationPairs[pair]
	return g.add("replace_equation", kind, f, note, obj("op", "replace_equation", "at", at, "latex", e.latex, "equation", n, "display", display))
}

// positivePlans are the generic valid plans every source gets for the
// targets it has: each op's main variants, aimed at real paragraphs,
// tables, sections, comments, bookmarks and equations from the oracle's
// read.
func positivePlans(g *gen, f *docFacts, pos string) {
	plain := f.plain(12)
	pick := func(i int) (paraFact, bool) {
		if len(plain) == 0 {
			return paraFact{}, false
		}
		return plain[min(i, len(plain)-1)], true
	}
	if p, ok := pick(len(plain) / 2); ok {
		if w := uniqueWord(p.selectable); w != "" {
			g.add("replace_text", pos, f, "unique word", obj("op", "replace_text", "at", p.id, "select", w, "with", "REPLACED"))
			g.add("replace_text", pos, f, "delete a word", obj("op", "replace_text", "at", p.id, "select", w, "with", ""))
			g.add("format_text", pos, f, "bold, colour and size", obj("op", "format_text", "at", p.id, "select", w, "bold", true, "color", "#C00000", "font_size", 14.5))
			g.add("comment_add", pos, f, "comment on a word", obj("op", "comment_add", "at", p.id, "select", w, "text", "Oracle comment."))
			g.add("bookmark_add", pos, f, "bookmark a word", obj("op", "bookmark_add", "at", p.id, "name", "OracleWord", "select", w))
		}
		if w := repeatedWord(p.selectable); w != "" {
			g.add("replace_text", pos, f, "second occurrence", obj("op", "replace_text", "at", p.id, "select", w, "with", "SECOND", "occurrence", 2))
			g.add("format_text", pos, f, "italic underline on second occurrence", obj("op", "format_text", "at", p.id, "select", w, "occurrence", 2, "italic", true, "underline", true, "highlight", "yellow"))
		}
		if ws := uniqueWords(p.selectable, 2); len(ws) == 2 {
			g.add("revise_text", pos, f, "two edits in one paragraph", obj("op", "revise_text", "at", p.id, "edits",
				arr(obj("select", ws[0], "with", "FIRST"), obj("select", ws[1], "with", "LAST"))))
		}
		g.add("replace_paragraph", pos, f, "inline HTML", obj("op", "replace_paragraph", "at", p.id, "with", "Replaced with <b>bold</b>, <i>italic</i> and <a href=\"https://example.com/\">a link</a>."))
		g.add("format_paragraph", pos, f, "alignment and spacing", obj("op", "format_paragraph", "at", p.id, "alignment", "center", "space_before", 6, "space_after", 12.5,
			"line_spacing", raw(obj("mode", "multiple", "value", 1.5))))
		g.add("format_paragraph", pos, f, "indents, hanging first line", obj("op", "format_paragraph", "at", p.id, "indent_left", 36, "indent_right", 18, "first_line_indent", -18))
		g.add("comment_add", pos, f, "comment on a paragraph", obj("op", "comment_add", "at", p.id, "text", "Whole paragraph."))
		g.add("bookmark_add", pos, f, "bookmark a paragraph", obj("op", "bookmark_add", "at", p.id, "name", "OraclePara"))
		g.add("insert_paragraph", pos, f, "after, with HTML", obj("op", "insert_paragraph", "at", p.id, "position", "after", "with", "Inserted <b>after</b>."))
		g.add("insert_paragraph", pos, f, "before, list of items with alias", obj("op", "insert_paragraph", "at", p.id, "position", "before", "with", []string{"Item one", "Item two"}, "as", "items"),
			obj("op", "format_paragraph", "at", "$items_2", "alignment", "right"))
		g.add("delete_paragraphs", pos, f, "one paragraph", obj("op", "delete_paragraphs", "at", []string{p.id}))
		g.add("insert_table", pos, f, "2x2 with cells and alias", obj("op", "insert_table", "at", p.id, "position", "after", "rows", 2, "columns", 2,
			"cells", raw(`[["A1","<b>B1</b>"],["A2"]]`), "as", "t"), obj("op", "format_paragraph", "at", "$t_r2c2", "alignment", "center"))
		g.add("set_list", pos, f, "new decimal list", obj("op", "set_list", "at", []string{p.id}, "format", "decimal", "start", 3))
		if len(plain) >= 2 {
			last := plain[len(plain)-1]
			if last.id != p.id {
				g.add("delete_paragraphs", pos, f, "two paragraphs", obj("op", "delete_paragraphs", "at", []string{p.id, last.id}))
			}
		}
	}
	if p, ok := pick(0); ok {
		g.add("set_section_boundary", pos, f, "new boundary after a paragraph", obj("op", "set_section_boundary", "at", p.id, "break_type", "nextPage"))
		g.add("format_paragraph", pos, f, "style", obj("op", "format_paragraph", "at", p.id, "style", "Heading1"))
	}
	for _, p := range f.paras {
		if p.listNum != "" && !p.inTable {
			g.add("set_list", pos, f, "remove numbering", obj("op", "set_list", "at", []string{p.id}, "remove", true))
			g.add("set_list", pos, f, "join an existing list one level down", obj("op", "set_list", "at", []string{p.id}, "num_id", atoi(p.listNum), "level", 1))
			break
		}
	}
	if len(f.paras) > 0 {
		g.add("revise_text", pos, f, "body scope, replace all", obj("op", "revise_text", "edits", arr(obj("select", "the", "with", "THE", "all", true))))
	}
	tablePlans(g, f, pos)
	sectionPlans(g, f, pos)
	for i, e := range f.equations {
		if i == 2 {
			break
		}
		replaceEquation(g, pos, f, "replace equation", e.at, e.n, nil, i%len(equationPairs))
		g.add("delete_equation", pos, f, "delete equation", obj("op", "delete_equation", "at", e.at, "equation", e.n))
	}
	if len(f.comments) > 0 {
		c := f.comments[0]
		g.add("comment_reply", pos, f, "reply", obj("op", "comment_reply", "comment_id", c.id, "text", "Oracle reply."))
		g.add("comment_set_status", pos, f, "resolve", obj("op", "comment_set_status", "comment_id", c.id, "status", "resolved"))
		g.add("comment_set_status", pos, f, "reopen", obj("op", "comment_set_status", "comment_id", c.id, "status", "open"))
		g.add("comment_delete", pos, f, "delete", obj("op", "comment_delete", "comment_id", c.id))
		if len(c.replies) > 0 {
			g.add("comment_delete", pos, f, "delete a reply", obj("op", "comment_delete", "comment_id", c.replies[0]))
		}
	}
	if bms := f.bodyBookmarks(); len(bms) > 0 {
		b := bms[0]
		g.add("bookmark_rename", pos, f, "rename", obj("op", "bookmark_rename", "name", b.name, "new_name", "OracleRenamed"))
		g.add("bookmark_delete", pos, f, "delete", obj("op", "bookmark_delete", "name", b.name))
		if p, ok := pick(len(plain) - 1); ok {
			g.add("bookmark_move", pos, f, "move to a paragraph", obj("op", "bookmark_move", "name", b.name, "at", p.id))
			if w := uniqueWord(p.selectable); w != "" {
				g.add("bookmark_move", pos, f, "move to a word", obj("op", "bookmark_move", "name", b.name, "at", p.id, "select", w))
			}
		}
	}
	if len(f.revisions) > 0 {
		r := f.revisions[0]
		g.add("revision_settle", pos, f, "accept one", obj("op", "revision_settle", "target", r.id, "action", "accept"))
		g.add("revision_settle", pos, f, "reject one", obj("op", "revision_settle", "target", r.id, "action", "reject"))
		g.add("revision_settle", pos, f, "accept all", obj("op", "revision_settle", "target", "all", "action", "accept"))
		g.add("revision_settle", pos, f, "reject all", obj("op", "revision_settle", "target", "all", "action", "reject"))
		g.add("convert_revisions_to_formatting", pos, f, "all", obj("op", "convert_revisions_to_formatting", "target", "all"))
		g.add("convert_revisions_to_formatting", pos, f, "one author", obj("op", "convert_revisions_to_formatting", "target", raw(obj("author", r.author))))
		g.add("convert_revisions_to_formatting", pos, f, "one id", obj("op", "convert_revisions_to_formatting", "target", raw(obj("id", r.id))))
	}
}

// atoi parses a decimal string into an int, returning 0 for non-digits.
func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// tablePlans covers the table operations on the first table (and a
// merged one when there is one).
func tablePlans(g *gen, f *docFacts, pos string) {
	if len(f.tables) == 0 {
		return
	}
	t := f.tables[0]
	h := t.handle
	g.add("format_table", pos, f, "width, layout, alignment, borders", obj("op", "format_table", "at", h, "format", raw(obj(
		"width", raw(obj("unit", "percent", "value", 80)), "layout", "fixed", "alignment", "center",
		"borders", raw(obj("top", raw(obj("style", "single", "color", "#FF0000", "width", 1.5)), "inside_horizontal", raw(obj("style", "dashed"))))))))
	g.add("format_table", pos, f, "padding, spacing, style, clear", obj("op", "format_table", "at", h, "format", raw(obj(
		"cell_spacing", 2, "padding", raw(obj("top", 3, "left", 5.5)), "style", "TableGrid", "clear", []string{"borders"}))))
	g.add("format_table", pos, f, "points width", obj("op", "format_table", "at", h, "format", raw(obj("width", raw(obj("unit", "points", "value", 400))))))
	g.add("format_row", pos, f, "height, header, split", obj("op", "format_row", "at", h+"/row:1", "format", raw(obj("height", 24, "height_rule", "at_least", "repeating_header", true, "allow_split", false))))
	g.add("format_cell", pos, f, "fill, alignment, padding, border", obj("op", "format_cell", "at", h+"/row:1/cell:1", "format", raw(obj(
		"fill", "#DDEEFF", "vertical_alignment", "center", "padding", raw(obj("left", 4)), "borders", raw(obj("bottom", raw(obj("style", "double", "width", 0.75))))))))
	g.add("format_cell", pos, f, "auto width, clear fill", obj("op", "format_cell", "at", h+"/row:1/cell:1", "format", raw(obj("width", raw(obj("unit", "auto")), "clear", []string{"fill"}))))
	g.add("edit_table", pos, f, "insert rows after row 1 with alias", obj("op", "edit_table", "at", h, "action", raw(obj("kind", "insert_rows", "row", 1, "position", "after", "count", 2, "as", "rows"))))
	g.add("edit_table", pos, f, "insert a row before row 1", obj("op", "edit_table", "at", h, "action", raw(obj("kind", "insert_rows", "row", 1, "position", "before", "count", 1))))
	if len(t.rows) > 1 {
		g.add("edit_table", pos, f, "delete the last row", obj("op", "edit_table", "at", h, "action", raw(obj("kind", "delete_rows", "row", len(t.rows), "count", 1))))
	}
	g.add("edit_table", pos, f, "insert a column", obj("op", "edit_table", "at", h, "action", raw(obj("kind", "insert_columns", "column", 1, "position", "after", "count", 1))))
	if len(t.rows) > 0 && t.rows[0] > 1 {
		g.add("edit_table", pos, f, "delete a column", obj("op", "edit_table", "at", h, "action", raw(obj("kind", "delete_columns", "column", t.rows[0], "count", 1))))
		g.add("edit_table", pos, f, "merge two cells", obj("op", "edit_table", "at", h, "action", raw(obj("kind", "merge_cells", "row", 1, "column", 1, "row_span", 1, "column_span", 2))))
		g.add("edit_table", pos, f, "merge then split", obj("op", "edit_table", "at", h, "action", raw(obj("kind", "merge_cells", "row", 1, "column", 1, "row_span", 1, "column_span", 2))),
			obj("op", "edit_table", "at", h, "action", raw(obj("kind", "split_cell", "row", 1, "column", 1))))
	}
	g.add("edit_table", pos, f, "delete the table", obj("op", "edit_table", "at", h, "action", raw(obj("kind", "delete"))))
	g.add("insert_paragraph", pos, f, "before a table", obj("op", "insert_paragraph", "at", h, "position", "before", "with", "Before the table."))
	for _, p := range f.paras {
		if p.inTable && !p.hasBreak && !p.hasMath {
			g.add("replace_paragraph", pos, f, "in a table cell", obj("op", "replace_paragraph", "at", p.id, "with", "Cell text"))
			g.add("insert_paragraph", pos, f, "in a table cell", obj("op", "insert_paragraph", "at", p.id, "position", "after", "with", "Second cell paragraph"))
			break
		}
	}
	for _, tt := range f.tables[1:] {
		if tt.merged {
			g.add("format_cell", pos, f, "merged table", obj("op", "format_cell", "at", tt.handle+"/row:1/cell:1", "format", raw(obj("fill", "#FFEEDD"))))
			break
		}
	}
}

// sectionPlans covers the section, header and footer operations.
func sectionPlans(g *gen, f *docFacts, pos string) {
	if len(f.sections) == 0 {
		return
	}
	last := len(f.sections)
	g.add("set_page_margins", pos, f, "all margins of section 1", obj("op", "set_page_margins", "section", 1, "top", 72, "right", 54, "bottom", 72, "left", 54, "header", 36, "footer", 36, "gutter", 0))
	g.add("set_page_margins", pos, f, "last section, one margin", obj("op", "set_page_margins", "section", last, "left", 90.5))
	g.add("set_section_layout", pos, f, "landscape A4", obj("op", "set_section_layout", "section", last, "page_size", "a4", "orientation", "landscape"))
	g.add("set_section_layout", pos, f, "columns", obj("op", "set_section_layout", "section", 1, "columns", 2, "column_spacing", 18, "column_separator", true))
	g.add("set_section_layout", pos, f, "explicit column widths", obj("op", "set_section_layout", "section", 1, "columns", 2, "column_widths", raw("[200,250]")))
	g.add("set_section_layout", pos, f, "custom size, page numbers", obj("op", "set_section_layout", "section_handle", f.sections[0].handle,
		"width", 500, "height", 700, "page_number_format", "upperRoman", "page_number_start", 5))
	g.add("set_section_layout", pos, f, "break type and continue numbering", obj("op", "set_section_layout", "section", last, "break_type", "oddPage", "page_number_continue", true))
	g.add("set_first_page_mode", pos, f, "enable", obj("op", "set_first_page_mode", "section", 1, "enabled", true))
	g.add("set_first_page_mode", pos, f, "disable", obj("op", "set_first_page_mode", "enabled", false))
	g.add("set_even_and_odd_headers", pos, f, "on", obj("op", "set_even_and_odd_headers", "even_and_odd", true))
	g.add("set_even_and_odd_headers", pos, f, "off", obj("op", "set_even_and_odd_headers", "even_and_odd", false))
	g.add("set_header", pos, f, "default header", obj("op", "set_header", "with", "<p>Oracle <b>header</b></p>"))
	g.add("set_header", pos, f, "first-page header", obj("op", "set_header", "section", 1, "kind", "first", "with", "<p>First page</p>"))
	g.add("set_header", pos, f, "even header after enabling even and odd", obj("op", "set_even_and_odd_headers", "even_and_odd", true),
		obj("op", "set_header", "kind", "even", "with", "<p>Even page</p>"))
	g.add("set_footer", pos, f, "footer with page field", obj("op", "set_footer", "with", "<p>Page <page-number></page-number> of <page-count></page-count></p>"))
	g.add("set_footer", pos, f, "last section footer", obj("op", "set_footer", "section", last, "with", "<p>Last section</p>"))
	for _, s := range f.sections {
		if slices.Contains(s.explicit, "hdr/default") {
			g.add("clear_header", pos, f, "clear an explicit default header", obj("op", "clear_header", "section", s.index))
		}
		if slices.Contains(s.explicit, "ftr/default") {
			g.add("clear_footer", pos, f, "clear an explicit default footer", obj("op", "clear_footer", "section", s.index))
		}
		for _, k := range s.explicit {
			if kind := strings.TrimPrefix(k, "hdr/"); kind != k && kind != "default" {
				g.add("clear_header", pos, f, "clear the "+kind+" header", obj("op", "clear_header", "section", s.index, "kind", kind))
			}
		}
	}
	if last >= 2 {
		if b := f.sections[0].handle; strings.HasPrefix(b, "section:") && b != "section:final" {
			g.add("set_section_boundary", pos, f, "remove the first boundary", obj("op", "set_section_boundary", "at", strings.TrimPrefix(b, "section:"), "remove", true))
			g.add("set_section_boundary", pos, f, "change the first boundary's break", obj("op", "set_section_boundary", "at", strings.TrimPrefix(b, "section:"), "break_type", "continuous"))
		}
	}
}
