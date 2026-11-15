package main

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
)

// validOps is one minimal valid operation per op, with placeholders the
// generators fill in: $P a body paragraph id, $T a table handle.
var validOps = map[string]string{
	"replace_equation":                `{"op":"replace_equation","at":"$P","latex":"x^{2}"}`,
	"delete_equation":                 `{"op":"delete_equation","at":"$P"}`,
	"replace_text":                    `{"op":"replace_text","at":"$P","select":"$W","with":"y"}`,
	"revise_text":                     `{"op":"revise_text","at":"$P","edits":[{"select":"$W","with":"y"}]}`,
	"replace_paragraph":               `{"op":"replace_paragraph","at":"$P","with":"text"}`,
	"edit_table":                      `{"op":"edit_table","at":"$T","action":{"kind":"delete"}}`,
	"insert_table":                    `{"op":"insert_table","at":"$P","position":"after","rows":1,"columns":1}`,
	"format_table":                    `{"op":"format_table","at":"$T","format":{"layout":"fixed"}}`,
	"format_row":                      `{"op":"format_row","at":"$T/row:1","format":{"height":20}}`,
	"format_cell":                     `{"op":"format_cell","at":"$T/row:1/cell:1","format":{"fill":"#FF0000"}}`,
	"format_text":                     `{"op":"format_text","at":"$P","select":"$W","bold":true}`,
	"format_paragraph":                `{"op":"format_paragraph","at":"$P","alignment":"center"}`,
	"set_list":                        `{"op":"set_list","at":["$P"],"format":"decimal"}`,
	"insert_paragraph":                `{"op":"insert_paragraph","at":"$P","position":"after","with":"x"}`,
	"delete_paragraphs":               `{"op":"delete_paragraphs","at":["$P"]}`,
	"set_page_margins":                `{"op":"set_page_margins","top":72}`,
	"set_section_boundary":            `{"op":"set_section_boundary","at":"$P","break_type":"nextPage"}`,
	"set_section_layout":              `{"op":"set_section_layout","orientation":"landscape"}`,
	"set_first_page_mode":             `{"op":"set_first_page_mode","enabled":true}`,
	"set_even_and_odd_headers":        `{"op":"set_even_and_odd_headers","even_and_odd":true}`,
	"set_header":                      `{"op":"set_header","with":"<p>x</p>"}`,
	"set_footer":                      `{"op":"set_footer","with":"<p>x</p>"}`,
	"clear_header":                    `{"op":"clear_header"}`,
	"clear_footer":                    `{"op":"clear_footer"}`,
	"comment_add":                     `{"op":"comment_add","at":"$P","text":"x"}`,
	"comment_reply":                   `{"op":"comment_reply","comment_id":"0","text":"x"}`,
	"comment_set_status":              `{"op":"comment_set_status","comment_id":"0","status":"resolved"}`,
	"comment_delete":                  `{"op":"comment_delete","comment_id":"0"}`,
	"bookmark_add":                    `{"op":"bookmark_add","at":"$P","name":"N"}`,
	"bookmark_rename":                 `{"op":"bookmark_rename","name":"Target","new_name":"N"}`,
	"bookmark_move":                   `{"op":"bookmark_move","name":"Target","at":"$P"}`,
	"bookmark_delete":                 `{"op":"bookmark_delete","name":"Target"}`,
	"revision_settle":                 `{"op":"revision_settle","target":"all","action":"accept"}`,
	"convert_revisions_to_formatting": `{"op":"convert_revisions_to_formatting","target":"all"}`,
}

// fill substitutes the placeholders.
func fill(op, p, w, t string) string {
	return strings.NewReplacer("$P", p, "$W", w, "$T", t).Replace(validOps[op])
}

// paragraphOps take a body paragraph address in `at`.
var paragraphOps = []string{"replace_equation", "delete_equation", "replace_text", "revise_text", "replace_paragraph",
	"insert_table", "format_text", "format_paragraph", "set_list", "insert_paragraph", "delete_paragraphs",
	"set_section_boundary", "comment_add", "bookmark_add", "bookmark_move"}

// negativePlans are valid plans aimed at apply-time refusals: missing or
// stale targets, out-of-range indexes, conflicting operations, a stale
// base and a wrong preview key. They run on the write fixtures, the
// authored and docxdriver fixtures, not on the corpus; the per-op
// missing-target cases only on the write fixtures.
func negativePlans(g *gen, f *docFacts) {
	const neg = kindNegative
	if f.set == setCorpus {
		return
	}
	plain := f.plain(12)
	if len(plain) == 0 {
		return
	}
	p := plain[0]
	w := uniqueWord(p.selectable)
	if w == "" {
		w = string([]rune(p.selectable)[:3])
	}
	table := "table:1"
	if len(f.tables) > 0 {
		table = f.tables[0].handle
	}
	add := func(op, note string, ops ...string) *WritePlan { return g.add(op, neg, f, note, ops...) }
	if f.set == setWrite {
		for _, op := range paragraphOps {
			add(op, "no such paragraph", fill(op, "DEADBEEF", w, table))
			add(op, "undefined alias", fill(op, "$nope", w, table))
		}
	}
	add("replace_text", "select not found", obj("op", "replace_text", "at", p.id, "select", "zzzqqq", "with", "x"))
	add("format_text", "select not found", obj("op", "format_text", "at", p.id, "select", "zzzqqq", "bold", true))
	add("comment_add", "select not found", obj("op", "comment_add", "at", p.id, "select", "zzzqqq", "text", "x"))
	add("bookmark_add", "select not found", obj("op", "bookmark_add", "at", p.id, "name", "N", "select", "zzzqqq"))
	add("revise_text", "edit not found", obj("op", "revise_text", "at", p.id, "edits", arr(obj("select", "zzzqqq", "with", "x"))))
	add("revise_text", "within not found", obj("op", "revise_text", "at", p.id, "within", "zzzqqq", "edits", arr(obj("select", w, "with", "x"))))
	add("replace_text", "occurrence out of range", obj("op", "replace_text", "at", p.id, "select", w, "with", "x", "occurrence", 99))
	if rw := repeatedWord(p.selectable); rw != "" {
		add("replace_text", "ambiguous select", obj("op", "replace_text", "at", p.id, "select", rw, "with", "x"))
		add("format_text", "ambiguous select", obj("op", "format_text", "at", p.id, "select", rw, "italic", true))
		add("comment_add", "ambiguous select", obj("op", "comment_add", "at", p.id, "select", rw, "text", "x"))
	}
	add("revise_text", "overlapping edits", obj("op", "revise_text", "at", p.id, "edits", arr(obj("select", w, "with", "x"), obj("select", string([]rune(w)[:len([]rune(w))-1]), "with", "y"))))
	add("revise_text", "body scope, ambiguous", obj("op", "revise_text", "edits", arr(obj("select", "e", "with", "E"))))
	add("delete_equation", "paragraph without an equation", obj("op", "delete_equation", "at", p.id))
	replaceEquation(g, neg, f, "paragraph without an equation", p.id, nil, nil, 0)
	for _, e := range f.equations {
		add("delete_equation", "equation index out of range", obj("op", "delete_equation", "at", e.at, "equation", 99))
		replaceEquation(g, neg, f, "equation index out of range", e.at, 99, nil, 0)
		replaceEquation(g, neg, f, "display LaTeX lines into an inline equation", e.at, e.n, false, 0).Note = "display false on the target"
		break
	}
	add("format_table", "table out of range", obj("op", "format_table", "at", "table:99", "format", raw(obj("layout", "fixed"))))
	add("format_row", "row out of range", obj("op", "format_row", "at", table+"/row:99", "format", raw(obj("height", 20))))
	add("format_cell", "cell out of range", obj("op", "format_cell", "at", table+"/row:1/cell:99", "format", raw(obj("fill", "#FF0000"))))
	add("edit_table", "table out of range", obj("op", "edit_table", "at", "table:99", "action", raw(obj("kind", "delete"))))
	add("insert_paragraph", "table anchor out of range", obj("op", "insert_paragraph", "at", "table:99", "position", "after", "with", "x"))
	if len(f.tables) > 0 {
		t := f.tables[0]
		add("edit_table", "delete rows out of range", obj("op", "edit_table", "at", table, "action", raw(obj("kind", "delete_rows", "row", 99, "count", 1))))
		add("edit_table", "delete every row", obj("op", "edit_table", "at", table, "action", raw(obj("kind", "delete_rows", "row", 1, "count", len(t.rows)))))
		add("edit_table", "insert rows out of range", obj("op", "edit_table", "at", table, "action", raw(obj("kind", "insert_rows", "row", 99, "position", "after", "count", 1))))
		add("edit_table", "delete columns out of range", obj("op", "edit_table", "at", table, "action", raw(obj("kind", "delete_columns", "column", 99, "count", 1))))
		add("edit_table", "merge out of range", obj("op", "edit_table", "at", table, "action", raw(obj("kind", "merge_cells", "row", 1, "column", 1, "row_span", 99, "column_span", 99))))
		add("edit_table", "split an unmerged cell", obj("op", "edit_table", "at", table, "action", raw(obj("kind", "split_cell", "row", 1, "column", 1))))
		add("edit_table", "delete then format the table", obj("op", "edit_table", "at", table, "action", raw(obj("kind", "delete"))),
			obj("op", "format_table", "at", table, "format", raw(obj("layout", "fixed"))))
		add("edit_table", "alias reused", obj("op", "edit_table", "at", table, "action", raw(obj("kind", "insert_rows", "row", 1, "position", "after", "count", 1, "as", "r"))),
			obj("op", "edit_table", "at", table, "action", raw(obj("kind", "insert_rows", "row", 1, "position", "after", "count", 1, "as", "r"))))
	}
	for _, op := range []string{"set_page_margins", "set_section_layout", "set_first_page_mode", "set_header", "set_footer", "clear_header", "clear_footer"} {
		add(op, "section out of range", strings.Replace(validOps[op], `{"op":"`+op+`"`, `{"op":"`+op+`","section":99`, 1))
	}
	add("set_section_layout", "no such section handle", obj("op", "set_section_layout", "section_handle", "section:DEADBEEF", "orientation", "landscape"))
	add("set_section_boundary", "remove a boundary that is not one", obj("op", "set_section_boundary", "at", p.id, "remove", true))
	add("clear_header", "clear a first header that is not there", obj("op", "clear_header", "section", 1, "kind", "first"))
	add("set_header", "invalid HTML", obj("op", "set_header", "with", "<video>no</video>"))
	add("set_header", "text outside a paragraph", obj("op", "set_header", "with", "bare text"))
	add("replace_paragraph", "unsupported HTML element", obj("op", "replace_paragraph", "at", p.id, "with", "<video>x</video>"))
	add("insert_paragraph", "block HTML in inline content", obj("op", "insert_paragraph", "at", p.id, "position", "after", "with", "<table><tr><td>x</td></tr></table>"))
	add("replace_paragraph", "unsafe link", obj("op", "replace_paragraph", "at", p.id, "with", "<a href=\"javascript:alert(1)\">x</a>"))
	add("format_paragraph", "unknown style", obj("op", "format_paragraph", "at", p.id, "style", "NoSuchStyle"))
	add("insert_paragraph", "unknown style", obj("op", "insert_paragraph", "at", p.id, "position", "after", "with", "x", "style", "NoSuchStyle"))
	add("set_list", "no such list", obj("op", "set_list", "at", []string{p.id}, "num_id", 999))
	add("comment_reply", "no such comment", obj("op", "comment_reply", "comment_id", "999", "text", "x"))
	add("comment_set_status", "no such comment", obj("op", "comment_set_status", "comment_id", "999", "status", "resolved"))
	add("comment_delete", "no such comment", obj("op", "comment_delete", "comment_id", "999"))
	add("bookmark_rename", "no such bookmark", obj("op", "bookmark_rename", "name", "NoSuchMark", "new_name", "N"))
	add("bookmark_move", "no such bookmark", obj("op", "bookmark_move", "name", "NoSuchMark", "at", p.id))
	add("bookmark_delete", "no such bookmark", obj("op", "bookmark_delete", "name", "NoSuchMark"))
	add("bookmark_add", "the same name twice", obj("op", "bookmark_add", "at", p.id, "name", "Twice"), obj("op", "bookmark_add", "at", p.id, "name", "Twice"))
	if bms := f.bodyBookmarks(); len(bms) > 0 {
		add("bookmark_add", "an existing name", obj("op", "bookmark_add", "at", p.id, "name", bms[0].name))
		if len(bms) > 1 {
			add("bookmark_rename", "to an existing name", obj("op", "bookmark_rename", "name", bms[0].name, "new_name", bms[1].name))
		}
		add("bookmark_rename", "delete then rename", obj("op", "bookmark_delete", "name", bms[0].name), obj("op", "bookmark_rename", "name", bms[0].name, "new_name", "N"))
	}
	if len(f.comments) > 0 {
		add("comment_reply", "delete then reply", obj("op", "comment_delete", "comment_id", f.comments[0].id), obj("op", "comment_reply", "comment_id", f.comments[0].id, "text", "x"))
	}
	add("revision_settle", "no such revision", obj("op", "revision_settle", "target", "999", "action", "accept"))
	add("convert_revisions_to_formatting", "no such revision", obj("op", "convert_revisions_to_formatting", "target", raw(obj("id", "999"))))
	add("convert_revisions_to_formatting", "no revisions in the paragraphs", obj("op", "convert_revisions_to_formatting", "target", "all", "at", []string{p.id}))
	add("delete_paragraphs", "delete then format the paragraph", obj("op", "delete_paragraphs", "at", []string{p.id}), obj("op", "format_paragraph", "at", p.id, "alignment", "right"))
	add("delete_paragraphs", "the same paragraph twice", obj("op", "delete_paragraphs", "at", []string{p.id, p.id}))
	add("insert_paragraph", "alias reused", obj("op", "insert_paragraph", "at", p.id, "position", "after", "with", "a", "as", "x"),
		obj("op", "insert_paragraph", "at", p.id, "position", "after", "with", "b", "as", "x"))
	add("insert_paragraph", "group member alias clash", obj("op", "insert_paragraph", "at", p.id, "position", "after", "with", "a", "as", "g_1"),
		obj("op", "insert_paragraph", "at", p.id, "position", "after", "with", []string{"b", "c"}, "as", "g"))
	add("replace_text", "selector drift after an earlier edit", obj("op", "replace_text", "at", p.id, "select", w, "with", "changed"),
		obj("op", "replace_text", "at", p.id, "select", w, "with", "again"))
	st := add("replace_paragraph", "stale base", obj("op", "replace_paragraph", "at", p.id, "with", "x"))
	st.Base = "stale"
}

// rulePlans is one plan per validation rule of the op table that has an
// example (internal/docx/ops: Rule.Example breaks that rule and no
// earlier one).
func rulePlans(g *gen, f *docFacts) {
	for _, d := range ops.OracleOps() {
		for i, r := range d.Rules {
			if r.Example == "" {
				continue
			}
			p := g.add(d.Name, kindRule, f, r.Msg, r.Example)
			p.Rule = &i
			p.ID = fmt.Sprintf("%s/rule/%d", d.Name, i)
		}
	}
}

// schemaPlans break each op's field shape, from the op table's Spec
// structs: a missing required field, an unknown field, a wrongly typed
// field; and the plan-level shapes (empty plan, empty author, a bad
// change mode, an unknown op, and a missing field in a later operation).
func schemaPlans(g *gen, f *docFacts) {
	const sch = kindSchema
	p := "10000002"
	for _, d := range ops.OracleOps() {
		base, err := parseJV([]byte(fill(d.Name, p, "beta", "table:1")))
		if err != nil {
			panic(fmt.Sprintf("validOps[%s]: %v", d.Name, err)) // a bug in the table above
		}
		for _, field := range d.Fields {
			if field.Required {
				g.add(d.Name, sch, f, "missing required field "+field.Name, encodeJV(without(base, field.Name)))
			}
		}
		g.add(d.Name, sch, f, "unknown field", encodeJV(with(base, "bogus", &jv{kind: 'n', s: "1"})))
		if len(base.members) > 1 {
			m := base.members[1]
			wrong := &jv{kind: 'n', s: "7"}
			if m.val.kind == 'n' {
				wrong = &jv{kind: 's', s: "seven"}
			}
			g.add(d.Name, sch, f, "wrong type for "+m.key, encodeJV(with(base, m.key, wrong)))
		}
	}
	plan := func(op, note, text string) {
		up, err := mapOps(text, func(o *jv) (*jv, error) { return upOp(o), nil })
		if err != nil {
			up = text // deliberately broken JSON shapes stay as written
		}
		g.add(op, sch, f, note).Plan = up
	}
	plan("replace_text", "empty ops", `{"base":"$BASE","author":"Oracle","change_mode":"$MODE","ops":[]}`)
	plan("replace_text", "empty author", `{"base":"$BASE","author":"","change_mode":"$MODE","ops":[`+fill("replace_text", p, "beta", "")+`]}`)
	plan("replace_text", "bad change mode", `{"base":"$BASE","author":"Oracle","change_mode":"suggest","ops":[`+fill("replace_text", p, "beta", "")+`]}`)
	plan("replace_text", "missing base", `{"author":"Oracle","change_mode":"$MODE","ops":[`+fill("replace_text", p, "beta", "")+`]}`)
	plan("replace_text", "malformed base", `{"base":"sha256:xyz","author":"Oracle","change_mode":"$MODE","ops":[`+fill("replace_text", p, "beta", "")+`]}`)
	plan("replace_text", "unknown top-level field", `{"base":"$BASE","author":"Oracle","change_mode":"$MODE","extra":1,"ops":[`+fill("replace_text", p, "beta", "")+`]}`)
	plan("replace_text", "unknown op", `{"base":"$BASE","author":"Oracle","change_mode":"$MODE","ops":[{"op":"replace_everything","at":"`+p+`"}]}`)
	plan("replace_text", "missing field in ops[1]", `{"base":"$BASE","author":"Oracle","change_mode":"$MODE","ops":[`+fill("replace_text", p, "beta", "")+`,{"op":"replace_text","at":"`+p+`","with":"x"}]}`)
	plan("replace_text", "unknown field in ops[1]", `{"base":"$BASE","author":"Oracle","change_mode":"$MODE","ops":[`+fill("replace_text", p, "beta", "")+`,{"op":"replace_text","at":"`+p+`","select":"Alpha","with":"x","bogus":1}]}`)
	plan("replace_text", "rule broken in ops[1]", `{"base":"$BASE","author":"Oracle","change_mode":"$MODE","ops":[`+fill("replace_text", p, "beta", "")+`,{"op":"replace_text","at":"`+p+`","select":"","with":"x"}]}`)
	plan("replace_text", "malformed paragraph address", `{"base":"$BASE","author":"Oracle","change_mode":"$MODE","ops":[{"op":"replace_text","at":"12345","select":"a","with":"b"}]}`)
}

// without returns a copy of object v without member key.
func without(v *jv, key string) *jv {
	out := &jv{kind: 'o'}
	for _, m := range v.members {
		if m.key != key {
			out.members = append(out.members, m)
		}
	}
	return out
}

// with returns a copy of object v with member key set to val.
func with(v *jv, key string, val *jv) *jv {
	out := &jv{kind: 'o'}
	set := false
	for _, m := range v.members {
		if m.key == key {
			m.val, set = val, true
		}
		out.members = append(out.members, m)
	}
	if !set {
		out.members = append(out.members, jmember{key, val})
	}
	return out
}
