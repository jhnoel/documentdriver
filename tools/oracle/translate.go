package main

import (
	"fmt"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"regexp"
	"strconv"
	"strings"
)

// Plans are written in the agent contract's form (docs/contract.md §3,
// version 1): contract op names, `at` addresses for sections, comments,
// bookmarks, equations, rows, columns and cells, `html` for HTML content,
// `all` and `to` from the shared vocabulary. docxdriver 0.5.2 reads its
// own plans, as TOML, so the oracle side always goes through
// downTranslate (contract.md §3, the mapping table) and then planTOML.
// The translation lives only here: the docx binary never reads 0.5.2
// plans. The generators write their cases with docxdriver's op shapes and
// upTranslate them to the contract form that is stored; the round trip
// is a test (translate_test.go).
//
// Each oracle result is translated up the same way (upCode, upPath), so
// the record holds contract codes and paths.

// contractOps maps each contract op to the docxdriver op it runs as
// (contract.md §3). text.insert has none: its cases are Go-only laws.
var contractOps = map[string]string{
	"text.replace": "replace_text", "text.revise": "revise_text", "text.format": "format_text",
	"paragraph.insert": "insert_paragraph", "paragraph.replace": "replace_paragraph",
	"paragraph.delete": "delete_paragraphs", "paragraph.format": "format_paragraph",
	"list.set":         "set_list",
	"equation.replace": "replace_equation", "equation.delete": "delete_equation",
	"table.insert": "insert_table", "table.delete": "edit_table", "table.format": "format_table",
	"row.insert": "edit_table", "row.delete": "edit_table", "row.format": "format_row",
	"column.insert": "edit_table", "column.delete": "edit_table",
	"cell.merge": "edit_table", "cell.split": "edit_table", "cell.format": "format_cell",
	"section.set_margins": "set_page_margins", "section.set_layout": "set_section_layout",
	"section.set_boundary": "set_section_boundary", "section.set_first_page": "set_first_page_mode",
	"document.set_even_odd_headers": "set_even_and_odd_headers",
	"header.set":                    "set_header", "header.delete": "clear_header",
	"footer.set": "set_footer", "footer.delete": "clear_footer",
	"comment.add": "comment_add", "comment.reply": "comment_reply",
	"comment.set_status": "comment_set_status", "comment.delete": "comment_delete",
	"bookmark.add": "bookmark_add", "bookmark.rename": "bookmark_rename",
	"bookmark.move": "bookmark_move", "bookmark.delete": "bookmark_delete",
	"revision.accept": "revision_settle", "revision.reject": "revision_settle",
	"revision.convert_to_formatting": "convert_revisions_to_formatting",
}

// tableActions maps the edit_table action kinds to their contract ops.
var tableActions = map[string]string{
	"delete": "table.delete", "insert_rows": "row.insert", "delete_rows": "row.delete",
	"insert_columns": "column.insert", "delete_columns": "column.delete",
	"merge_cells": "cell.merge", "split_cell": "cell.split",
}

// Paired equations: the LaTeX the contract takes and the MathML docxdriver
// 0.5.2 takes for the same equation. The comparison is at the
// projections, which read both engines' outputs with the oracle. The
// last pair is malformed on both sides (the validation-rule case).
var equationPairs = []struct{ latex, mathml string }{
	{`x^{2}`, `<math xmlns="http://www.w3.org/1998/Math/MathML"><msup><mi>x</mi><mn>2</mn></msup></math>`},
	{`\frac{a+1}{b}`, `<math xmlns="http://www.w3.org/1998/Math/MathML"><mfrac><mrow><mi>a</mi><mo>+</mo><mn>1</mn></mrow><mi>b</mi></mfrac></math>`},
	{`\sqrt{y}`, `<math xmlns="http://www.w3.org/1998/Math/MathML"><msqrt><mi>y</mi></msqrt></math>`},
	{`x^2`, `<math xmlns="http://www.w3.org/1998/Math/MathML"><msup><mi>x</mi><mn>2</mn></msup></math>`},
	{`\frac{a}`, `<math xmlns="http://www.w3.org/1998/Math/MathML"><mfrac><mi>a</mi></mfrac></math>`},
}

// str returns a JSON string jv holding s.
func str(s string) *jv { return &jv{kind: 's', s: s} }

// num returns a JSON number jv holding the literal n.
func num(n string) *jv { return &jv{kind: 'n', s: n} }

// field helpers on op objects.
func (v *jv) take(key string) *jv {
	for i, m := range v.members {
		if m.key == key {
			v.members = append(v.members[:i], v.members[i+1:]...)
			return m.val
		}
	}
	return nil
}

// put sets key to val on object v, appending it if absent and ignoring nil.
func (v *jv) put(key string, val *jv) {
	if val == nil {
		return
	}
	for i, m := range v.members {
		if m.key == key {
			v.members[i].val = val
			return
		}
	}
	v.members = append(v.members, jmember{key, val})
}

// rename renames member from to to on object v.
func (v *jv) rename(from, to string) {
	for i, m := range v.members {
		if m.key == from {
			v.members[i].key = to
		}
	}
}

// clone deep-copies v by round-tripping it through JSON encoding.
func clone(v *jv) *jv {
	c, err := parseJV([]byte(encodeJV(v)))
	if err != nil {
		panic(err) // unreachable: encodeJV writes valid JSON
	}
	return c
}

var addrNum = regexp.MustCompile(`^(.*?)/(row|column|cell|equation):([^/]+)$`)

// splitAddr cuts the last "/kind:n" component off an address.
func splitAddr(a, kind string) (base, n string, ok bool) {
	m := addrNum.FindStringSubmatch(a)
	if m == nil || m[2] != kind {
		return a, "", false
	}
	return m[1], m[3], true
}

// numVal is a number literal as a JSON value; a non-number (a rule case's
// deliberately broken value) stays a string.
func numVal(s string) *jv {
	if _, err := strconv.ParseUint(s, 10, 64); err == nil {
		return num(s)
	}
	return str(s)
}

// addrPart returns an address value's string or number text, or its JSON encoding.
func addrPart(v *jv) string {
	if v == nil {
		return ""
	}
	if v.kind == 's' || v.kind == 'n' {
		return v.s
	}
	return encodeJV(v)
}

// downOp turns one contract op into docxdriver's.
func downOp(op *jv) (*jv, error) {
	op = clone(op)
	htmlNames(op, toOracleMarkup)
	return downOpShape(op)
}

// downOpShape is downOp's renaming of the operation and its fields.
func downOpShape(op *jv) (*jv, error) {
	name := op.get("op")
	if name == nil || name.kind != 's' {
		return op, nil // a broken op passes through for the oracle to refuse
	}
	target, ok := contractOps[name.s]
	if !ok {
		if name.s == "text.insert" {
			return nil, fmt.Errorf("text.insert has no docxdriver op")
		}
		return op, nil
	}
	op.put("op", str(target))
	switch name.s {
	case "text.replace":
		if all := op.take("all"); all != nil {
			// docxdriver has no `all` on replace_text: revise_text does it.
			edit := &jv{kind: 'o'}
			edit.put("select", op.take("select"))
			edit.put("with", op.take("with"))
			edit.put("all", all)
			op.put("op", str("revise_text"))
			op.put("edits", &jv{kind: 'a', items: []*jv{edit}})
		}
	case "paragraph.insert", "paragraph.replace":
		op.take("delete") // docxdriver deletes what the html leaves out
		op.rename("html", "with")
	case "equation.replace", "equation.delete":
		if at := op.get("at"); at != nil && at.kind == 's' {
			if base, n, ok := splitAddr(at.s, "equation"); ok {
				op.put("at", str(base))
				op.put("equation", numVal(n))
			}
		}
		if l := op.get("latex"); l != nil && l.kind == 's' {
			mathml, found := "", false
			for _, e := range equationPairs {
				if e.latex == l.s {
					mathml, found = e.mathml, true
				}
			}
			if !found {
				return nil, fmt.Errorf("equation LaTeX %q has no MathML pair (translate.go)", l.s)
			}
			op.take("latex")
			op.put("mathml", str(mathml))
		}
	case "table.delete", "row.insert", "row.delete", "column.insert", "column.delete", "cell.merge", "cell.split":
		action := &jv{kind: 'o'}
		for k, v := range tableActions {
			if v == name.s {
				action.put("kind", str(k))
			}
		}
		at := addrPart(op.get("at"))
		if base, c, ok := splitAddr(at, "cell"); ok {
			at = base
			action.put("column", numVal(c))
		}
		if base, r, ok := splitAddr(at, "row"); ok {
			at = base
			action.put("row", numVal(r))
		}
		if base, c, ok := splitAddr(at, "column"); ok {
			at = base
			action.put("column", numVal(c))
		}
		op.put("at", str(at))
		for _, k := range []string{"position", "count", "as", "row_span", "column_span"} {
			action.put(k, op.take(k))
		}
		op.put("action", action)
	case "section.set_margins", "section.set_first_page", "header.set", "header.delete", "footer.set", "footer.delete", "section.set_layout":
		if at := op.take("at"); at != nil {
			s, _ := strings.CutPrefix(addrPart(at), "section:")
			// Eight hex digits are a boundary paraId (contract §6), a
			// docxdriver section_handle; another number is a section.
			if _, err := strconv.ParseUint(s, 10, 32); err == nil && !isHex8(s) {
				op.put("section", num(s))
			} else {
				op.put("section_handle", at)
			}
		}
		if name.s == "header.set" || name.s == "footer.set" {
			op.take("delete")
			op.rename("html", "with")
		}
	case "comment.reply", "comment.set_status", "comment.delete":
		if at := op.take("at"); at != nil {
			op.put("comment_id", str(strings.TrimPrefix(addrPart(at), "comment:")))
		}
	case "bookmark.rename":
		newName := op.take("name")
		if at := op.take("at"); at != nil {
			op.put("name", str(strings.TrimPrefix(addrPart(at), "bookmark:")))
		}
		op.put("new_name", newName)
	case "bookmark.move":
		to := op.take("to")
		if at := op.take("at"); at != nil {
			op.put("name", str(strings.TrimPrefix(addrPart(at), "bookmark:")))
		}
		op.put("at", to)
	case "bookmark.delete":
		if at := op.take("at"); at != nil {
			op.put("name", str(strings.TrimPrefix(addrPart(at), "bookmark:")))
		}
	case "revision.accept", "revision.reject":
		op.put("action", str(strings.TrimPrefix(name.s, "revision.")))
		at, all := op.take("at"), op.take("all")
		switch {
		case all != nil:
			op.put("target", str("all"))
		case at != nil:
			op.put("target", str(strings.TrimPrefix(addrPart(at), "revision:")))
		}
	case "revision.convert_to_formatting":
		at, all, author := op.take("at"), op.take("all"), op.take("author")
		switch {
		case author != nil:
			sel := &jv{kind: 'o'}
			sel.put("author", author)
			op.put("target", sel)
		case at != nil && at.kind == 's' && strings.HasPrefix(at.s, "revision:"):
			sel := &jv{kind: 'o'}
			sel.put("id", str(strings.TrimPrefix(at.s, "revision:")))
			op.put("target", sel)
		case all != nil || at != nil:
			op.put("target", str("all"))
		}
		if at != nil && at.kind == 'a' {
			op.put("at", at)
		}
	}
	return op, nil
}

// upOp turns one docxdriver op into the contract form (the generators
// write docxdriver shapes).
func upOp(op *jv) *jv {
	out := upOpShape(op)
	htmlNames(out, view.FromOracleMarkup)
	return out
}

// htmlFields are the fields that hold HTML, in either grammar.
var htmlFields = []string{"html", "with", "cells"}

// htmlNames rewrites the attribute names in an operation's HTML: the
// contract's markup has no data-docx- prefix and gives an equation its
// address (docs/contract.md, section 7), docxdriver's has the prefix and
// the equation's number.
func htmlNames(op *jv, f func(string) string) {
	var walk func(v *jv)
	walk = func(v *jv) {
		switch {
		case v == nil:
		case v.kind == 's':
			v.s = f(v.s)
		case v.kind == 'a':
			for _, it := range v.items {
				walk(it)
			}
		}
	}
	if op == nil || op.kind != 'o' {
		return
	}
	for _, k := range htmlFields {
		walk(op.get(k))
	}
	// text.revise's edits carry plain text, not HTML: nothing to do.
}

var contractEquationRef = regexp.MustCompile(`data-ref=(\\?")(?:[^"\\/]*/)?equation:(\d+)(\\?")`)

// toOracleMarkup is the inverse of view.FromOracleMarkup for HTML a plan
// carries: data-ref on an equation is docxdriver's data-docx-equation
// (the number), and data-<name> is data-docx-<name>.
func toOracleMarkup(s string) string {
	if !strings.Contains(s, "data-") {
		return s
	}
	s = contractEquationRef.ReplaceAllString(s, "data-equation=$1$2$3")
	s = strings.ReplaceAll(s, "data-docx-", "data-") // already docxdriver's: not doubled below
	return strings.ReplaceAll(s, "data-", "data-docx-")
}

// upOpShape is upOp's renaming of the operation and its fields.
func upOpShape(op *jv) *jv {
	op = clone(op)
	name := op.get("op")
	if name == nil || name.kind != 's' {
		return op
	}
	switch name.s {
	case "edit_table":
		action := op.take("action")
		kind := action.get("kind")
		if kind == nil || tableActions[kind.s] == "" {
			op.put("action", action)
			return op
		}
		op.put("op", str(tableActions[kind.s]))
		at := addrPart(op.get("at"))
		if r := action.get("row"); r != nil {
			at += "/row:" + addrPart(r)
		}
		if c := action.get("column"); c != nil {
			if action.get("row") != nil {
				at += "/cell:" + addrPart(c)
			} else {
				at += "/column:" + addrPart(c)
			}
		}
		op.put("at", str(at))
		for _, k := range []string{"position", "count", "as", "row_span", "column_span"} {
			op.put(k, action.get(k))
		}
		return op
	case "revision_settle":
		action := op.take("action")
		target := op.take("target")
		op.put("op", str("revision."+addrPart(action)))
		if t := addrPart(target); t == "all" {
			op.put("all", &jv{kind: 'b', b: true})
		} else if target != nil {
			op.put("at", str("revision:"+t))
		}
		return op
	case "convert_revisions_to_formatting":
		op.put("op", str("revision.convert_to_formatting"))
		target, at := op.take("target"), op.take("at")
		switch {
		case target != nil && target.kind == 'o' && target.get("id") != nil:
			op.put("at", str("revision:"+addrPart(target.get("id"))))
		case target != nil && target.kind == 'o' && target.get("author") != nil:
			op.put("all", &jv{kind: 'b', b: true})
			op.put("author", target.get("author"))
		case at != nil:
			op.put("at", at)
		default:
			op.put("all", &jv{kind: 'b', b: true})
		}
		return op
	}
	for c, d := range contractOps {
		if d == name.s && !strings.HasPrefix(c, "revision.") && d != "edit_table" {
			op.put("op", str(c))
			name = op.get("op")
			break
		}
	}
	switch name.s {
	case "paragraph.insert", "paragraph.replace":
		op.rename("with", "html")
	case "equation.replace", "equation.delete":
		if n := op.take("equation"); n != nil {
			op.put("at", str(addrPart(op.get("at"))+"/equation:"+addrPart(n)))
		}
		if m := op.get("mathml"); m != nil {
			for _, e := range equationPairs {
				if e.mathml == m.s {
					op.take("mathml")
					op.put("latex", str(e.latex))
					break
				}
			}
		}
	case "section.set_margins", "section.set_first_page", "header.set", "header.delete", "footer.set", "footer.delete", "section.set_layout":
		if s := op.take("section"); s != nil {
			op.put("at", str("section:"+addrPart(s)))
		}
		if h := op.take("section_handle"); h != nil {
			op.put("at", h)
		}
		op.rename("with", "html")
	case "comment.reply", "comment.set_status", "comment.delete":
		if id := op.take("comment_id"); id != nil {
			op.put("at", str("comment:"+addrPart(id)))
		}
	case "bookmark.rename":
		n, nn := op.take("name"), op.take("new_name")
		if n != nil {
			op.put("at", str("bookmark:"+addrPart(n)))
		}
		op.put("name", nn)
	case "bookmark.move":
		n, to := op.take("name"), op.take("at")
		if n != nil {
			op.put("at", str("bookmark:"+addrPart(n)))
		}
		op.put("to", to)
	case "bookmark.delete":
		if n := op.take("name"); n != nil {
			op.put("at", str("bookmark:"+addrPart(n)))
		}
	}
	return op
}

// mapOps applies f to every op of a plan's ops array (a broken plan
// shape passes through).
func mapOps(plan string, f func(*jv) (*jv, error)) (string, error) {
	v, err := parseJV([]byte(plan))
	if err != nil {
		return "", err
	}
	ops := v.get("ops")
	if ops == nil || ops.kind != 'a' {
		return encodeJV(v), nil
	}
	for i, op := range ops.items {
		if op.kind != 'o' {
			continue
		}
		o, err := f(op)
		if err != nil {
			return "", fmt.Errorf("ops[%d]: %w", i, err)
		}
		ops.items[i] = o
	}
	return encodeJV(v), nil
}

// downTranslate turns a contract plan (JSON) into docxdriver 0.5.2's.
func downTranslate(plan string) (string, error) { return mapOps(plan, downOp) }

// upOps turns a docxdriver ops array into the contract form.
func upOps(ops string) (string, error) {
	out, err := mapOps(`{"ops":`+ops+`}`, func(op *jv) (*jv, error) { return upOp(op), nil })
	if err != nil {
		return "", err
	}
	v, err := parseJV([]byte(out))
	if err != nil {
		return "", err
	}
	return encodeJV(v.get("ops")), nil
}

// upCode maps a docxdriver diagnostic to the contract's error codes
// (contract.md §2). Document refusals all arrive as operation_rejected;
// their message says which contract code they are.
func upCode(code, message string) string {
	switch code {
	case "invalid_toml":
		return "invalid_json"
	case "empty_plan", "invalid_author":
		return "invalid_plan"
	case "source_mismatch":
		return "stale_base"
	case "integrity_regression":
		return "invalid_output"
	case "source_invalid", "paragraph_id_allocation", "paragraph_id_repair":
		return "invalid_document"
	case "unknown_alias":
		return "not_found"
	case "duplicate_alias":
		return "invalid_operation"
	case "operation_rejected":
		m := strings.ToLower(message)
		switch {
		case strings.Contains(m, "track") && (strings.Contains(m, "refuse") || strings.Contains(m, "cannot be tracked") || strings.Contains(m, "direct")):
			return "refused_in_mode"
		case strings.Contains(m, "matched nothing") || strings.Contains(m, "not found") || strings.Contains(m, "no such") ||
			strings.Contains(m, "does not exist") || strings.Contains(m, "out of range") || strings.Contains(m, "no paragraph") ||
			strings.Contains(m, "unknown plan-local alias") || strings.Contains(m, "has no current equations"):
			return "not_found"
		case strings.Contains(m, "ambiguous") || regexp.MustCompile(`matched \d+ times`).MatchString(m):
			return "ambiguous"
		}
		return "operation_failed"
	}
	return code
}

// fieldUp maps docxdriver field names to the contract's, per contract op.
var fieldUp = map[string]map[string]string{
	"paragraph.insert": {"with": "html"}, "paragraph.replace": {"with": "html"},
	"header.set": {"with": "html", "section": "at"}, "footer.set": {"with": "html", "section": "at"},
	"header.delete": {"section": "at"}, "footer.delete": {"section": "at"},
	"section.set_margins": {"section": "at"}, "section.set_first_page": {"section": "at"},
	"section.set_layout": {"section": "at", "section_handle": "at"},
	"equation.replace":   {"equation": "at", "mathml": "latex"}, "equation.delete": {"equation": "at"},
	"comment.reply": {"comment_id": "at"}, "comment.set_status": {"comment_id": "at"}, "comment.delete": {"comment_id": "at"},
	"bookmark.rename": {"name": "at", "new_name": "name"}, "bookmark.move": {"name": "at", "at": "to"},
	"bookmark.delete": {"name": "at"},
	"revision.accept": {"target": "at"}, "revision.reject": {"target": "at"},
	"revision.convert_to_formatting": {"target": "at"},
}

var opPath = regexp.MustCompile(`^ops\[(\d+)\]\.([A-Za-z_]+)(.*)$`)

// upPath maps a docxdriver diagnostic path into the contract plan's
// terms, given the contract ops (by index).
func upPath(path string, ops []string) string {
	m := opPath.FindStringSubmatch(path)
	if m == nil {
		return path
	}
	i, err := strconv.Atoi(m[1])
	if err != nil || i >= len(ops) {
		return path
	}
	field := m[2]
	if field == "action" {
		sub, rest, _ := strings.Cut(strings.TrimPrefix(m[3], "."), ".")
		if sub == "" || sub == "row" || sub == "column" || sub == "kind" {
			return "ops[" + m[1] + "].at"
		}
		if rest != "" {
			sub += "." + rest
		}
		return "ops[" + m[1] + "]." + sub
	}
	if to, ok := fieldUp[ops[i]][field]; ok {
		field = to
	}
	return "ops[" + m[1] + "]." + field + m[3]
}

// isHex8 reports whether s is eight hex digits: a paragraph id.
func isHex8(s string) bool {
	if len(s) != 8 {
		return false
	}
	_, err := strconv.ParseUint(s, 16, 32)
	return err == nil
}
