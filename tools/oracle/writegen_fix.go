package main

import (
	"encoding/json"
	"strings"
)

// byText returns the id of the first body paragraph whose text contains
// s ("" when none): fixture paragraphs without a paraId get the id the
// oracle repairs for them.
func (f *docFacts) byText(s string) string {
	for _, p := range f.paras {
		if strings.Contains(p.text, s) {
			return p.id
		}
	}
	return ""
}

// fixturePlans are the mode-sensitive cases on the authored write
// fixtures, whose content is known (writefixtures.go).
func fixturePlans(g *gen, f *docFacts) {
	switch f.file {
	case "mixed.docx":
		mixedPlans(g, f)
	case "sections.docx":
		sectionsPlans(g, f)
	}
}

// mixedPlans adds the mode-sensitive positive cases for mixed.docx.
func mixedPlans(g *gen, f *docFacts) {
	const pos = kindPositive
	foreign := f.byText("Foreign revisions")
	own := "10000004"
	crossing := f.byText("into the next paragraph")
	quote := f.byText("Quoted paragraph")
	second := f.byText("Second numbered item")
	analyst := f.byText("Analyst")
	add := func(op, note string, ops ...string) *WritePlan { return g.add(op, pos, f, note, ops...) }

	// The plan author's own pending insertion and foreign authors' revisions.
	add("replace_text", "inside the author's own pending insertion", obj("op", "replace_text", "at", own, "select", "insertion", "with", "addition"))
	add("replace_text", "over own insertion and plain text", obj("op", "replace_text", "at", own, "select", "my insertion then", "with", "rewritten"))
	add("replace_text", "inside a foreign insertion", obj("op", "replace_text", "at", foreign, "select", "inserted", "with", "changed"))
	add("replace_text", "across a foreign insertion's edge", obj("op", "replace_text", "at", foreign, "select", "keep inserted", "with", "hold added"))
	add("replace_text", "across a bold run", obj("op", "replace_text", "at", "10000002", "select", "beta gamma beta", "with", "BGB"))
	add("replace_text", "foreign author's own insertion edited by that author", obj("op", "replace_text", "at", foreign, "select", "words and", "with", "text and")).Author = "Alice"
	add("replace_text", "inside a line, not crossing the break", obj("op", "replace_text", "at", "1000000C", "select", "line two", "with", "LINE TWO"))
	add("replace_text", "B8: a selection across a line break (docxdriver counts the break as no character)", obj("op", "replace_text", "at", "1000000C", "select", "oneline", "with", "X"))
	add("replace_text", "B8: a selection across an inline equation", obj("op", "replace_text", "at", "1000000A", "select", "math between", "with", "Y"))
	add("revise_text", "edits over own and foreign insertions", obj("op", "revise_text", "at", own, "edits", arr(obj("select", "Own", "with", "Mine"), obj("select", "tail", "with", "end"))))
	add("revise_text", "within a span, every match", obj("op", "revise_text", "at", "10000012", "within", "repeat repeat repeat", "edits", arr(obj("select", "repeat", "with", "again", "all", true))))
	add("revise_text", "within a body-wide unique span", obj("op", "revise_text", "within", "Alpha beta", "edits", arr(obj("select", "beta", "with", "BETA"))))
	add("revise_text", "occurrence of within", obj("op", "revise_text", "at", "10000012", "within", "repeat", "occurrence", 2, "edits", arr(obj("select", "repeat", "with", "twice"))))
	add("format_text", "own insertion", obj("op", "format_text", "at", own, "select", "my insertion", "strike", true))
	add("format_text", "run with a foreign rPrChange", obj("op", "format_text", "at", "10000005", "select", "formatted", "bold", true, "superscript", true))
	add("format_text", "clear properties", obj("op", "format_text", "at", "10000002", "select", "gamma", "clear", []string{"bold"}, "subscript", true))
	add("format_paragraph", "paragraph with a foreign pPrChange", obj("op", "format_paragraph", "at", "10000005", "alignment", "justify", "clear", []string{"alignment"}))
	add("format_paragraph", "exact line spacing", obj("op", "format_paragraph", "at", quote, "line_spacing", raw(obj("mode", "exact", "value", 14))))
	add("format_paragraph", "at-least line spacing, zero first line", obj("op", "format_paragraph", "at", "10000002", "line_spacing", raw(obj("mode", "at_least", "value", 12)), "first_line_indent", 0))
	add("delete_paragraphs", "own inserted text paragraph", obj("op", "delete_paragraphs", "at", []string{own}))
	add("delete_paragraphs", "foreign revisions paragraph", obj("op", "delete_paragraphs", "at", []string{foreign}))
	add("delete_paragraphs", "last paragraph of the body", obj("op", "delete_paragraphs", "at", []string{"10000014"}))
	add("delete_paragraphs", "paragraph before a table", obj("op", "delete_paragraphs", "at", []string{"10000011"}))
	add("delete_paragraphs", "paragraph in a table cell", obj("op", "delete_paragraphs", "at", []string{analyst}))
	add("delete_paragraphs", "paragraph ending a comment range", obj("op", "delete_paragraphs", "at", []string{crossing}))
	add("delete_paragraphs", "consecutive list items", obj("op", "delete_paragraphs", "at", []string{"1000000D", "1000000E"}))
	add("insert_paragraph", "after a heading (next style)", obj("op", "insert_paragraph", "at", "10000001", "position", "after", "with", "Body after heading"))
	add("insert_paragraph", "after a list item (inherits the list)", obj("op", "insert_paragraph", "at", second, "position", "after", "with", "Third numbered item"))
	add("insert_paragraph", "with an explicit style", obj("op", "insert_paragraph", "at", "10000014", "position", "after", "with", "Styled", "style", "Quote"))
	add("insert_paragraph", "chained on an alias", obj("op", "insert_paragraph", "at", "10000002", "position", "after", "with", "One", "as", "one"),
		obj("op", "insert_paragraph", "at", "$one", "position", "after", "with", "Two"))
	add("insert_paragraph", "after the table by handle", obj("op", "insert_paragraph", "at", "table:1", "position", "after", "with", "Right after the table"))
	add("insert_paragraph", "equation in HTML", obj("op", "insert_paragraph", "at", "10000002", "position", "after", "with", `Area <equation display="inline">x^2</equation> here`))
	add("replace_paragraph", "paragraph holding a comment range", obj("op", "replace_paragraph", "at", "10000006", "with", "Comment-free text"))
	add("replace_paragraph", "bookmarked paragraph", obj("op", "replace_paragraph", "at", "10000009", "with", "Still bookmarked?"))
	add("replace_paragraph", "foreign revisions paragraph", obj("op", "replace_paragraph", "at", foreign, "with", "Clean text"))
	add("replace_paragraph", "with a list of runs and a break", obj("op", "replace_paragraph", "at", "10000002", "with", "Before<br/>after <u>under</u> <s>struck</s> <sup>up</sup><sub>down</sub>"))

	// Contract-only shapes: text.replace's all (docxdriver: revise_text
	// with an all edit), the delete field of paragraph.replace (docxdriver
	// deletes what the html leaves out), and text.insert, which has no
	// docxdriver op: its cases are laws recorded through an equivalent
	// text.replace of the span by itself plus the new text.
	g.addContract("replace_text", pos, f, "every match with all", obj("op", "text.replace", "at", "10000012", "select", "repeat", "with", "again", "all", true))
	g.addContract("replace_paragraph", pos, f, "equation listed in delete", obj("op", "paragraph.replace", "at", "1000000A", "html", "No math left", "delete", []string{"1000000A/equation:1"}))
	g.addContract("replace_paragraph", pos, f, "equation sent back", obj("op", "paragraph.replace", "at", "1000000A", "html", "Kept <equation>x^{2}</equation> math"))
	g.addContract("replace_paragraph", kindNegative, f, "equation left out without delete", obj("op", "paragraph.replace", "at", "1000000A", "html", "Equation omitted"))
	for _, c := range []struct{ note, at, sel, pos, text, with string }{
		{"after a word", "10000002", "gamma", "after", " INSERTED", "gamma INSERTED"},
		{"before a word", "10000002", "delta", "before", "NEW ", "NEW delta"},
		{"after own insertion", own, "insertion", "after", "!", "insertion!"},
		{"in a foreign insertion", foreign, "inserted", "after", " really", "inserted really"},
	} {
		p := g.addContract(opTextInsert, pos, f, c.note, obj("op", opTextInsert, "at", c.at, "select", c.sel, "position", c.pos, "with", c.text))
		p.Equivalent = json.RawMessage("[" + obj("op", "text.replace", "at", c.at, "select", c.sel, "with", c.with) + "]")
	}
	g.addContract(opTextInsert, kindNegative, f, "select not found", obj("op", opTextInsert, "at", "10000002", "select", "zzzqqq", "position", "after", "with", "x")).
		Equivalent = json.RawMessage("[" + obj("op", "text.replace", "at", "10000002", "select", "zzzqqq", "with", "zzzqqqx") + "]")

	// Comments over runs and across paragraphs, with a reply.
	add("comment_add", "over a bold run", obj("op", "comment_add", "at", "10000002", "select", "beta gamma", "text", "Spans runs"))
	add("comment_add", "second occurrence", obj("op", "comment_add", "at", "10000012", "select", "repeat", "occurrence", 2, "text", "Second repeat"))
	add("comment_add", "in an own insertion", obj("op", "comment_add", "at", own, "select", "insertion", "text", "On my text"))
	add("comment_reply", "reply to the cross-paragraph comment", obj("op", "comment_reply", "comment_id", "1", "text", "Reply across"))
	add("comment_reply", "reply to a reply", obj("op", "comment_reply", "comment_id", "2", "text", "Nested"))
	add("comment_set_status", "reopen a resolved comment", obj("op", "comment_set_status", "comment_id", "1", "status", "open"))
	add("comment_set_status", "resolve a reply", obj("op", "comment_set_status", "comment_id", "2", "status", "resolved"))
	add("comment_delete", "comment with a reply", obj("op", "comment_delete", "comment_id", "0"))
	add("comment_delete", "cross-paragraph comment", obj("op", "comment_delete", "comment_id", "1"))
	add("comment_add", "add then reply in one plan", obj("op", "comment_add", "at", "10000014", "text", "New"),
		obj("op", "comment_reply", "comment_id", "3", "text", "Reply to new"))

	// Bookmarks.
	add("bookmark_add", "over a bold run", obj("op", "bookmark_add", "at", "10000002", "name", "Runs", "select", "beta gamma"))
	add("bookmark_add", "second occurrence", obj("op", "bookmark_add", "at", "10000012", "name", "Rep2", "select", "repeat", "occurrence", 2))
	add("bookmark_rename", "rename a whole-paragraph bookmark", obj("op", "bookmark_rename", "name", "Whole", "new_name", "Entire"))
	add("bookmark_move", "move into a list item", obj("op", "bookmark_move", "name", "Target", "at", "1000000D", "select", "numbered"))
	add("bookmark_move", "move a paragraph bookmark to a table cell", obj("op", "bookmark_move", "name", "Whole", "at", "20000001"))
	add("bookmark_delete", "delete a whole-paragraph bookmark", obj("op", "bookmark_delete", "name", "Whole"))
	add("bookmark_add", "add, rename, move, delete in one plan", obj("op", "bookmark_add", "at", "10000014", "name", "Temp"),
		obj("op", "bookmark_rename", "name", "Temp", "new_name", "Temp2"), obj("op", "bookmark_move", "name", "Temp2", "at", "10000002", "select", "delta"),
		obj("op", "bookmark_delete", "name", "Temp2"))

	// Revisions by id, author and paragraph.
	for _, id := range []string{"1", "2", "3", "4", "5", "40", "41"} {
		add("revision_settle", "accept revision "+id, obj("op", "revision_settle", "target", id, "action", "accept"))
		add("revision_settle", "reject revision "+id, obj("op", "revision_settle", "target", id, "action", "reject"))
	}
	add("convert_revisions_to_formatting", "one author, one paragraph", obj("op", "convert_revisions_to_formatting", "target", raw(obj("author", "Alice")), "at", []string{foreign}))
	add("convert_revisions_to_formatting", "own insertions", obj("op", "convert_revisions_to_formatting", "target", raw(obj("author", writeAuthor))))
	add("convert_revisions_to_formatting", "tracked rows", obj("op", "convert_revisions_to_formatting", "target", raw(obj("id", "41"))))

	// Tables with merges and tracked rows.
	tbl := func(action string) string { return obj("op", "edit_table", "at", "table:1", "action", raw(action)) }
	add("edit_table", "delete a foreign tracked-inserted row", tbl(obj("kind", "delete_rows", "row", 4, "count", 1)))
	add("edit_table", "delete a tracked-deleted row", tbl(obj("kind", "delete_rows", "row", 5, "count", 1)))
	add("edit_table", "insert rows after a tracked row", tbl(obj("kind", "insert_rows", "row", 4, "position", "after", "count", 1)))
	add("edit_table", "insert rows then delete them (own rows go outright)", tbl(obj("kind", "insert_rows", "row", 1, "position", "after", "count", 1, "as", "new")),
		tbl(obj("kind", "delete_rows", "row", 2, "count", 1)))
	add("edit_table", "split a horizontally merged cell", tbl(obj("kind", "split_cell", "row", 2, "column", 1)))
	add("edit_table", "merge across a vertical merge", tbl(obj("kind", "merge_cells", "row", 3, "column", 1, "row_span", 1, "column_span", 2)))
	add("edit_table", "delete the vertically merged column", tbl(obj("kind", "delete_columns", "column", 3, "count", 1)))
	add("edit_table", "insert columns before the first with alias", tbl(obj("kind", "insert_columns", "column", 1, "position", "before", "count", 2, "as", "cols")))
	add("format_row", "a tracked row", obj("op", "format_row", "at", "table:1/row:4", "format", raw(obj("height", 30, "height_rule", "exact"))))
	add("format_row", "clear a row property", obj("op", "format_row", "at", "table:1/row:1", "format", raw(obj("repeating_header", true, "clear", []string{"height"}))))
	add("format_cell", "vertically merged cell", obj("op", "format_cell", "at", "table:1/row:2/cell:2", "format", raw(obj("fill", "#EEEEEE", "width", raw(obj("unit", "points", "value", 120))))))
	add("format_cell", "percent width, all borders", obj("op", "format_cell", "at", "table:1/row:1/cell:3", "format", raw(obj("width", raw(obj("unit", "percent", "value", 25)),
		"borders", raw(obj("top", raw(obj("style", "single")), "left", raw(obj("style", "dotted")), "bottom", raw(obj("style", "triple")), "right", raw(obj("style", "wave"))))))))
	add("format_table", "column widths and clear", obj("op", "format_table", "at", "table:1", "format", raw(obj("column_widths", raw("[100,150,200]"), "clear", []string{"style", "layout"}))))
	add("insert_table", "before a paragraph, larger, empty", obj("op", "insert_table", "at", "10000014", "position", "before", "rows", 3, "columns", 4))
	add("insert_table", "in a table cell", obj("op", "insert_table", "at", "20000003", "position", "before", "rows", 1, "columns", 1))

	// Equations.
	replaceEquation(g, pos, f, "inline equation, keep form", "1000000A", nil, nil, 1)
	replaceEquation(g, pos, f, "display equation, display true", "1000000B", 1, true, 0)
	replaceEquation(g, pos, f, "inline equation made display", "1000000A", 1, true, 2)
	add("delete_equation", "inline equation, no index", obj("op", "delete_equation", "at", "1000000A"))
	add("delete_equation", "display equation", obj("op", "delete_equation", "at", "1000000B", "equation", 1))

	// Lists.
	add("set_list", "custom levels", obj("op", "set_list", "at", []string{"10000002", quote}, "levels", raw("["+
		obj("format", "upperRoman", "label", "%1.", "start", 2, "indent_left", 36, "first_line_indent", -18, "marker_bold", true)+","+
		obj("format", "lowerLetter", "label", "%1.%2)", "restart_after", 0, "marker_color", "#336699", "marker_font", "Arial")+"]")))
	add("set_list", "bullet list at level 2", obj("op", "set_list", "at", []string{"10000012"}, "format", "bullet", "level", 2))
	add("set_list", "change level in an existing list", obj("op", "set_list", "at", []string{"1000000E"}, "num_id", 1, "level", 0))
	add("set_list", "insert then list the alias group", obj("op", "insert_paragraph", "at", "10000014", "position", "before", "with", []string{"a", "b", "c"}, "as", "g"),
		obj("op", "set_list", "at", []string{"$g"}, "format", "lowerRoman"))
}

// sectionsPlans adds the positive cases for sections.docx.
func sectionsPlans(g *gen, f *docFacts) {
	const pos = kindPositive
	end2 := f.byText("Section two ends")
	add := func(op, note string, ops ...string) *WritePlan { return g.add(op, pos, f, note, ops...) }
	add("set_header", "section 2 inherits: new header part", obj("op", "set_header", "section", 2, "with", "<p>Section two header</p>"))
	add("set_header", "replace a header with a foreign revision", obj("op", "set_header", "section", 1, "with", "<p>New default header</p>"))
	add("set_header", "even header while even/odd is off", obj("op", "set_header", "section", 3, "kind", "even", "with", "<p>Even?</p>"))
	add("set_header", "first-page header in a section without titlePg", obj("op", "set_header", "section", 3, "kind", "first", "with", "<p>Section three first</p>"))
	add("set_footer", "replace a footer holding a PAGE field", obj("op", "set_footer", "section", 1, "with", "<p>Folio <page-number></page-number></p>"))
	add("set_footer", "a footer with a simple field span", obj("op", "set_footer", "section", 3, "with", "<p><span data-docx-field=\"NUMPAGES\">3</span> pages</p>"))
	add("clear_header", "section 1 default", obj("op", "clear_header", "section", 1))
	add("clear_header", "section 1 first", obj("op", "clear_header", "section", 1, "kind", "first"))
	add("clear_header", "section 3 default (after section 1)", obj("op", "clear_header", "section", 3))
	add("clear_header", "section 3 even", obj("op", "clear_header", "section", 3, "kind", "even"))
	add("clear_footer", "section 1 default with a field", obj("op", "clear_footer", "section", 1))
	add("clear_footer", "section 2 inherits: nothing explicit", obj("op", "clear_footer", "section", 2))
	add("set_first_page_mode", "section 1 off (removes titlePg)", obj("op", "set_first_page_mode", "section", 1, "enabled", false))
	add("set_first_page_mode", "section 3 on", obj("op", "set_first_page_mode", "section", 3, "enabled", true))
	add("set_section_boundary", "remove the section 2 boundary (no paraId)", obj("op", "set_section_boundary", "at", end2, "remove", true))
	add("set_section_boundary", "remove the section 1 boundary (carries headers)", obj("op", "set_section_boundary", "at", "30000003", "remove", true))
	add("set_section_boundary", "new boundary in section 3", obj("op", "set_section_boundary", "at", "30000006", "break_type", "evenPage"))
	add("set_section_boundary", "change a boundary's break type", obj("op", "set_section_boundary", "at", end2, "break_type", "nextColumn"))
	add("set_section_layout", "one column, continue numbering", obj("op", "set_section_layout", "section", 2, "columns", 1, "page_number_continue", true))
	add("set_section_layout", "portrait letter", obj("op", "set_section_layout", "section", 3, "page_size", "letter", "orientation", "portrait"))
	add("set_section_layout", "by boundary handle", obj("op", "set_section_layout", "section_handle", "section:30000003", "page_number_format", "decimal", "page_number_start", 1))
	add("set_section_layout", "final section handle", obj("op", "set_section_layout", "section_handle", "section:final", "break_type", "continuous"))
	add("set_page_margins", "section 2", obj("op", "set_page_margins", "section", 2, "top", 36, "bottom", 36))
	add("revision_settle", "accept the header revision", obj("op", "revision_settle", "target", "50", "action", "accept"))
	add("revision_settle", "reject the header revision", obj("op", "revision_settle", "target", "50", "action", "reject"))
	add("convert_revisions_to_formatting", "header revision", obj("op", "convert_revisions_to_formatting", "target", "all"))
	add("insert_paragraph", "after the paragraph ending section 1", obj("op", "insert_paragraph", "at", "30000003", "position", "after", "with", "Start of section two?"))
	add("insert_paragraph", "before the paragraph ending section 1", obj("op", "insert_paragraph", "at", "30000003", "position", "before", "with", "Still section one"))
	add("delete_paragraphs", "paragraph ending section 1", obj("op", "delete_paragraphs", "at", []string{"30000003"}))
	add("delete_paragraphs", "last body paragraph", obj("op", "delete_paragraphs", "at", []string{"30000007"}))
	add("set_even_and_odd_headers", "on, then an even header in section 3", obj("op", "set_even_and_odd_headers", "even_and_odd", true),
		obj("op", "set_header", "section", 3, "kind", "even", "with", "<p>Even three</p>"))
}
