package main

// sitePlans are negative cases aimed at one docxdriver refusal site each
// (sites.tsv; the note names the site), on the write fixtures whose
// content is known. sites-status.tsv lists every site with how it is
// covered or why it cannot be reached from a plan.
func sitePlans(g *gen, f *docFacts) {
	const neg = kindNegative
	add := func(op, note string, ops ...string) { g.add(op, neg, f, note, ops...) }
	switch f.file {
	case "mixed.docx":
		add("delete_equation", "site equations.rs:143 several equations, no index", obj("op", "delete_equation", "at", "10000015"))
		replaceEquation(g, neg, f, "site equations.rs:81 several equations, no index", "10000015", nil, nil, 0)
		add("delete_equation", "site equations.rs:148 equation inside a revision", obj("op", "delete_equation", "at", "10000016", "equation", 1))
		replaceEquation(g, neg, f, "site equations.rs:86 equation inside a revision", "10000016", 1, nil, 0)
		add("delete_paragraphs", "site typed.rs:3017 paragraph holding a content control", obj("op", "delete_paragraphs", "at", []string{"10000017"}))
		add("replace_text", "site typed.rs:850 selection crosses a field", obj("op", "replace_text", "at", "10000018", "select", "before today field", "with", "x"))
		add("comment_add", "site typed.rs:3291 selection crosses a field", obj("op", "comment_add", "at", "10000018", "select", "before today field", "text", "x"))
		add("replace_text", "site typed.rs:686 word diff across a content control", obj("op", "replace_text", "at", "10000017", "select", "control: controlled", "with", "x"))
		for _, c := range []struct{ site, html string }{
			{"typed.rs:271 table structure", "<table><tr><td>x</td></tr></table>"},
			{"typed.rs:280 revision tags", "<ins>x</ins>"},
			{"typed.rs:285 a field", `<span data-docx-field="PAGE">1</span>`},
			{"typed.rs:298 a pending-format tag", `<format pending="x">y</format>`},
			{"typed.rs:326 a footnote", "<footnote>x</footnote>"},
			{"typed.rs:331 an image", `<img src="assets/i1.png">`},
			{"typed.rs:336 a comment milestone", `<span data-docx-comment-start="0"></span>x`},
			{"typed.rs:227 mixed hyperlink targets", `<a href="https://a.example/">a</a><a href="https://b.example/">b</a>`},
		} {
			add("replace_paragraph", "site "+c.site+" in with", obj("op", "replace_paragraph", "at", "10000002", "with", c.html))
		}
		add("replace_text", "site typed.rs:494 select with markup", obj("op", "replace_text", "at", "10000002", "select", "<b>gamma</b>", "with", "x"))
		add("format_text", "site typed.rs:2024 select with markup", obj("op", "format_text", "at", "10000002", "select", "<b>gamma</b>", "italic", true))
		add("comment_add", "site typed.rs:3252 select with markup", obj("op", "comment_add", "at", "10000002", "select", "<b>gamma</b>", "text", "x"))
		add("bookmark_add", "site bookmarks.rs:409 select with markup", obj("op", "bookmark_add", "at", "10000002", "name", "M", "select", "<b>gamma</b>"))
		add("revise_text", "site typed.rs:1006 edit select with markup", obj("op", "revise_text", "at", "10000002", "edits", arr(obj("select", "<b>gamma</b>", "with", "x"))))
		add("revise_text", "site typed.rs:1050 within occurrence out of range", obj("op", "revise_text", "at", "10000012", "within", "repeat", "occurrence", 9, "edits", arr(obj("select", "repeat", "with", "x"))))
		add("revise_text", "site typed.rs:1061 within ambiguous in the body", obj("op", "revise_text", "within", "paragraph", "edits", arr(obj("select", "para", "with", "x"))))
		add("revise_text", "site typed.rs:1119 edit occurrence out of range", obj("op", "revise_text", "at", "10000012", "edits", arr(obj("select", "repeat", "with", "x", "occurrence", 9))))
		add("format_text", "site typed.rs:2057 formatting already matches", obj("op", "format_text", "at", "10000002", "select", "gamma", "bold", true))
		add("set_list", "site lists.rs:270 duplicate paragraph", obj("op", "set_list", "at", []string{"10000002", "10000002"}, "format", "decimal"))
		add("set_list", "site lists.rs:327 level the list does not define", obj("op", "set_list", "at", []string{"10000002"}, "num_id", 2, "level", 4))
		add("format_cell", "site tables.rs:176 a vertical-merge continuation", obj("op", "format_cell", "at", "table:1/row:3/cell:3", "format", raw(obj("fill", "#FF0000"))))
		add("format_table", "site tables.rs:516 column_widths count differs from the grid", obj("op", "format_table", "at", "table:1", "format", raw(obj("column_widths", raw("[100,100]")))))
		add("insert_table", "site table_structure.rs:700 last block of a table cell, tracked", obj("op", "insert_table", "at", "20000003", "position", "after", "rows", 1, "columns", 1))
		add("set_section_boundary", "site sections.rs:90 boundary in a table cell", obj("op", "set_section_boundary", "at", "20000003", "break_type", "nextPage"))
		add("set_section_boundary", "site sections.rs:126 remove the final section", obj("op", "set_section_boundary", "at", "10000014", "remove", true))
		add("set_section_layout", "site layout.rs:524 columns do not fit the text area", obj("op", "set_section_layout", "columns", 45, "column_spacing", 1584))
		add("set_section_layout", "site sections.rs:73 handle names no boundary", obj("op", "set_section_layout", "section_handle", "section:10000002", "orientation", "landscape"))
	case "sections.docx":
		add("edit_table", "site table_structure.rs:386 remove every row", obj("op", "edit_table", "at", "table:1", "action", raw(obj("kind", "delete_rows", "row", 1, "count", 2))))
		add("edit_table", "site table_structure.rs:552 split an unmerged cell", obj("op", "edit_table", "at", "table:1", "action", raw(obj("kind", "split_cell", "row", 1, "column", 1))))
		add("edit_table", "site table_structure.rs:529 split a vertical continuation", obj("op", "edit_table", "at", "table:1", "action", raw(obj("kind", "split_cell", "row", 2, "column", 2))))
		add("edit_table", "site table_structure.rs:520 column not at a cell start", obj("op", "edit_table", "at", "table:1", "action", raw(obj("kind", "merge_cells", "row", 2, "column", 2, "row_span", 1, "column_span", 2))))
		add("edit_table", "site table_structure.rs:439 remove every column", obj("op", "edit_table", "at", "table:1", "action", raw(obj("kind", "delete_columns", "column", 1, "count", 3))))
		add("set_page_margins", "site layout.rs:447 pending sectPrChange by another author", obj("op", "set_page_margins", "section", 3, "top", 30))
		add("set_first_page_mode", "site layout.rs:471 pending section change", obj("op", "set_first_page_mode", "section", 3, "enabled", true))
		add("set_section_layout", "site layout.rs:494 pending section change", obj("op", "set_section_layout", "section", 3, "columns", 2))
		add("set_section_boundary", "site sections.rs:129 merge into a section with a pending change", obj("op", "set_section_boundary", "at", f.byText("Section two ends"), "remove", true))
		add("set_section_boundary", "site sections.rs:115 boundary edit beside a pending change", obj("op", "set_section_boundary", "at", "30000006", "break_type", "nextPage"))
		add("insert_table", "site table_structure.rs:625 table at a section-boundary paragraph", obj("op", "insert_table", "at", "30000003", "position", "after", "rows", 1, "columns", 1))
		add("set_header", "site headers.rs:223 even header tracked while even/odd is off", obj("op", "set_header", "section", 1, "kind", "even", "with", "<p>Even</p>"))
		add("set_footer", "site headers.rs:407 tracked footer holding a field", obj("op", "set_footer", "section", 1, "with", "<p>plain</p>"))
	case "rich.docx":
		add("bookmark_rename", "site bookmarks.rs:305 bookmark in a header", obj("op", "bookmark_rename", "name", "H", "new_name", "H2"))
		add("bookmark_delete", "site bookmarks.rs:305 bookmark in a header", obj("op", "bookmark_delete", "name", "H"))
	case "malformed.docx":
		add("comment_add", "site comments.rs:407 malformed commentsExtended.xml", obj("op", "comment_add", "at", "72000003", "text", "x"))
		add("comment_set_status", "site comments.rs:797 malformed commentsExtended.xml", obj("op", "comment_set_status", "comment_id", "0", "status", "resolved"))
		add("comment_reply", "site comments.rs:407 reply with malformed commentsExtended.xml", obj("op", "comment_reply", "comment_id", "0", "text", "x"))
		add("set_even_and_odd_headers", "site headers.rs:643 malformed settings.xml", obj("op", "set_even_and_odd_headers", "even_and_odd", true))
		add("set_list", "site lists.rs:105 malformed numbering.xml", obj("op", "set_list", "at", []string{"72000003"}, "format", "decimal"))
	case "badroot.docx":
		add("comment_add", "site comments.rs:80 content types root is not Types", obj("op", "comment_add", "at", "71000002", "text", "x"))
		add("set_header", "site headers.rs:51 content types root is not Types", obj("op", "set_header", "with", "<p>x</p>"))
	}
}
