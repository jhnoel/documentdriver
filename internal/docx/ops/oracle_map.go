package ops

import (
	"fmt"
	"strconv"
	"strings"
)

// FromOracle translates a plan in docxdriver 0.5.2's grammar into the
// contract's (docs/contract.md, section 3, the mapping table): each
// operation gets its contract name and the fields the contract moved.
// It is for parity runs and tests; the docx binary takes only the
// contract's grammar.
//
// The translation is by shape, not by meaning: a value of the wrong type
// is carried across as it is, so that a plan docxdriver refuses is still
// refused. Two things do not carry: a docxdriver operation the contract
// has no name for (an unknown op, an unknown edit_table action), which
// keeps its docxdriver name and is refused as unknown; and
// set_section_layout with both section and section_handle, which
// docxdriver refuses and the contract cannot say, so both fields are
// kept and refused as unknown.
func FromOracle(plan []byte) ([]byte, error) {
	v, err := parseJSON(plan)
	if err != nil {
		return nil, err
	}
	if v.kind == kObject {
		if list := v.get("ops"); list != nil && list.kind == kArray {
			for i, op := range list.a {
				list.a[i] = opFromOracle(op)
			}
		}
	}
	var b strings.Builder
	writeJSON(&b, v)
	return []byte(b.String()), nil
}

// OpFromOracle translates one operation object.
func OpFromOracle(op []byte) ([]byte, error) {
	v, err := parseJSON(op)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	writeJSON(&b, opFromOracle(v))
	return []byte(b.String()), nil
}

// obj is an operation object being rewritten: its members by key.
type obj struct{ v *value }

// get returns the member value for key k, or nil.
func (o obj) get(k string) *value { return o.v.get(k) }

// del removes the member with key k from the object.
func (o obj) del(k string) {
	for i, m := range o.v.m {
		if m.key == k {
			o.v.m = append(o.v.m[:i], o.v.m[i+1:]...)
			return
		}
	}
}

// set stores value val under key k, replacing any existing member.
func (o obj) set(k string, val *value) {
	o.del(k)
	i := 0
	for i < len(o.v.m) && o.v.m[i].key < k {
		i++
	}
	o.v.m = append(o.v.m, member{})
	copy(o.v.m[i+1:], o.v.m[i:])
	o.v.m[i] = member{key: k, val: val}
}

// rename moves a member to another key.
func (o obj) rename(from, to string) {
	if v := o.get(from); v != nil {
		o.del(from)
		o.set(to, v)
	}
}

// str wraps string s as a JSON string value.
func str(s string) *value { return &value{kind: kString, s: s} }

// number is a value's text when it is a JSON integer.
func number(v *value) (string, bool) {
	switch v.kind {
	case kU64:
		return strconv.FormatUint(v.u, 10), true
	case kI64:
		return strconv.FormatInt(v.i, 10), true
	}
	return "", false
}

// prefixed moves the member from to the key to as prefix+value. The
// value joins the prefix when it has the type docxdriver takes there (a
// number when numeric, else a string); any other value is carried as it
// is, to be refused for its type as docxdriver refuses it.
func (o obj) prefixed(from, to, prefix string, numeric bool) {
	v := o.get(from)
	if v == nil {
		return
	}
	o.del(from)
	switch n, isNum := number(v); {
	case isNum && numeric:
		o.set(to, str(prefix+n))
	case v.kind == kString && !numeric:
		o.set(to, str(prefix+v.s))
	case v.kind == kNull:
		// serde reads null as absent.
	default:
		o.set(to, v)
	}
}

// oracleNames are the operations whose translation is the name alone.
var oracleNames = map[string]string{
	"replace_text": "text.replace", "revise_text": "text.revise", "format_text": "text.format",
	"delete_paragraphs": "paragraph.delete", "format_paragraph": "paragraph.format", "set_list": "list.set",
	"insert_table": "table.insert", "format_table": "table.format", "format_row": "row.format",
	"format_cell": "cell.format", "set_section_boundary": "section.set_boundary",
	"set_even_and_odd_headers": "document.set_even_odd_headers", "comment_add": "comment.add",
	"bookmark_add": "bookmark.add",
}

// tableActions are edit_table's action kinds: the contract operation and
// the address component its row or column becomes.
var tableActions = map[string]struct {
	op    string
	parts []string
}{
	"delete":         {"table.delete", nil},
	"insert_rows":    {"row.insert", []string{"row"}},
	"delete_rows":    {"row.delete", []string{"row"}},
	"insert_columns": {"column.insert", []string{"column"}},
	"delete_columns": {"column.delete", []string{"column"}},
	"merge_cells":    {"cell.merge", []string{"row", "column"}},
	"split_cell":     {"cell.split", []string{"row", "column"}},
}

// opFromOracle returns one operation translated from docxdriver grammar.
func opFromOracle(v *value) *value {
	if v.kind != kObject {
		return v
	}
	name := v.get("op")
	if name == nil || name.kind != kString {
		return v
	}
	o := obj{v}
	op := func(n string) { o.set("op", str(n)) }
	section := func() { o.prefixed("section", "at", "section:", true) }
	switch name.s {
	case "replace_equation", "delete_equation":
		op(map[string]string{"replace_equation": "equation.replace", "delete_equation": "equation.delete"}[name.s])
		if eq := o.get("equation"); eq != nil {
			at := o.get("at")
			n, isNum := number(eq)
			switch {
			case eq.kind == kNull:
				o.del("equation")
			case isNum && at != nil && at.kind == kString:
				o.del("equation")
				o.set("at", str(at.s+equationInfix+n))
			}
		}
	case "insert_paragraph":
		op("paragraph.insert")
		o.rename("with", "html")
	case "replace_paragraph":
		op("paragraph.replace")
		o.rename("with", "html")
	case "edit_table":
		tableFromOracle(o)
	case "set_page_margins":
		op("section.set_margins")
		section()
	case "set_first_page_mode":
		op("section.set_first_page")
		section()
	case "set_section_layout":
		op("section.set_layout")
		s, h := o.get("section"), o.get("section_handle")
		switch {
		case s != nil && s.kind != kNull && h != nil && h.kind != kNull:
			// Both: docxdriver refuses, the contract cannot say it.
		case h != nil && h.kind != kNull:
			o.rename("section_handle", "at")
			o.del("section")
		default:
			o.del("section_handle")
			section()
		}
	case "set_header", "set_footer":
		op(map[string]string{"set_header": "header.set", "set_footer": "footer.set"}[name.s])
		section()
		o.rename("with", "html")
	case "clear_header", "clear_footer":
		op(map[string]string{"clear_header": "header.delete", "clear_footer": "footer.delete"}[name.s])
		section()
	case "comment_reply", "comment_set_status", "comment_delete":
		op("comment." + strings.TrimPrefix(name.s, "comment_"))
		o.prefixed("comment_id", "at", "comment:", false)
	case "bookmark_rename":
		op("bookmark.rename")
		o.prefixed("name", "at", "bookmark:", false)
		o.rename("new_name", "name")
	case "bookmark_move":
		op("bookmark.move")
		o.rename("at", "to")
		o.prefixed("name", "at", "bookmark:", false)
	case "bookmark_delete":
		op("bookmark.delete")
		o.prefixed("name", "at", "bookmark:", false)
	case "revision_settle":
		if a := o.get("action"); a != nil && a.kind == kString && (a.s == "accept" || a.s == "reject") {
			op("revision." + a.s)
			o.del("action")
			revisionTarget(o)
		}
	case "convert_revisions_to_formatting":
		op("revision.convert_to_formatting")
		convertTarget(o)
	default:
		if n, ok := oracleNames[name.s]; ok {
			op(n)
		}
	}
	return v
}

// tableFromOracle lifts an edit_table action: its kind names the
// operation, its row and column join the table's address, and its other
// fields become the operation's.
//
// For merge_cells and split_cell docxdriver's column is a grid column,
// and the contract's cell:c is the cell's position in its row. Without
// the document the two cannot be told apart, so the number is carried
// across as it is: right for a row with no spanning cell before the
// target, which is all a plan checked without its document needs. A
// parity run on a document translates through the table's spans (read
// shows each cell's grid_column).
func tableFromOracle(o obj) {
	action := o.get("action")
	if action == nil || action.kind != kObject {
		return
	}
	kind := action.get("kind")
	if kind == nil || kind.kind != kString {
		return
	}
	a, ok := tableActions[kind.s]
	if !ok {
		return
	}
	at := o.get("at")
	addr, addressed := "", at != nil && at.kind == kString
	if addressed {
		addr = at.s
	}
	names := map[string]string{"row": "row", "column": "column"}
	if len(a.parts) == 2 {
		names["column"] = "cell"
	}
	inner := obj{action}
	for _, part := range a.parts {
		v := inner.get(part)
		n, isNum := "", false
		if v != nil {
			n, isNum = number(v)
		}
		if !isNum || !addressed {
			return // not the shape the contract can address: left as docxdriver's
		}
		addr += "/" + names[part] + ":" + n
		inner.del(part)
	}
	inner.del("kind")
	o.del("action")
	o.set("op", str(a.op))
	if addressed {
		o.set("at", str(addr))
	}
	for _, m := range action.m {
		o.set(m.key, m.val)
	}
}

// revisionTarget turns revision_settle's target into at or all.
func revisionTarget(o obj) {
	t := o.get("target")
	if t == nil {
		return
	}
	o.del("target")
	switch {
	case t.kind == kString && t.s == "all":
		o.set("all", &value{kind: kBool, b: true})
	case t.kind == kString:
		o.set("at", str("revision:"+t.s))
	default:
		o.set("at", t)
	}
}

// convertTarget turns convert_revisions_to_formatting's target ("all",
// {"id"} or {"author"}) and at (paragraphs) into at, all and author:
// all or the paragraphs say which revisions, the author narrows them. A
// revision id with paragraphs keeps the id: the contract's at holds one
// or the other.
func convertTarget(o obj) {
	t := o.get("target")
	if t == nil {
		return
	}
	paragraphs := o.get("at")
	limited := paragraphs != nil && paragraphs.kind != kNull
	switch {
	case t.kind == kString && t.s == "all":
		o.del("target")
		if !limited {
			o.del("at")
			o.set("all", &value{kind: kBool, b: true})
		}
	case t.kind == kObject && len(t.m) == 1 && t.m[0].key == "id" && t.m[0].val.kind == kString:
		o.del("target")
		o.set("at", str("revision:"+t.m[0].val.s))
	case t.kind == kObject && len(t.m) == 1 && t.m[0].key == "author":
		o.del("target")
		if !limited {
			o.del("at")
			o.set("all", &value{kind: kBool, b: true})
		}
		o.set("author", t.m[0].val)
	}
}

// writeJSON writes v as compact JSON, nulls kept and members in the
// order the value holds them (sorted by key).
func writeJSON(b *strings.Builder, v *value) {
	switch v.kind {
	case kArray:
		b.WriteByte('[')
		for i, e := range v.a {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, e)
		}
		b.WriteByte(']')
	case kObject:
		b.WriteByte('{')
		for i, m := range v.m {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, m.key)
			b.WriteByte(':')
			writeJSON(b, m.val)
		}
		b.WriteByte('}')
	default:
		writeCanonical(b, v)
	}
}

// OracleName is the docxdriver operation and fixed fields a contract
// operation runs as, for messages: "edit_table (action.kind
// \"insert_rows\")".
func (d *OpDef) OracleName() string {
	if len(d.Fixed) == 0 {
		return d.Oracle
	}
	var fixed []string
	for k, v := range d.Fixed {
		fixed = append(fixed, fmt.Sprintf("%s %s", k, v))
	}
	return d.Oracle + " (" + strings.Join(fixed, ", ") + ")"
}
