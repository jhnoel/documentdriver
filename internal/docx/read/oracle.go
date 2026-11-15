package read

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/view"
)

// FromOracle turns the CommandResult JSON docxdriver 0.5.2 prints for a
// read or find into the contract's result object, so that a parity run
// compares content in one shape: the Go side's. It is the read-side
// counterpart of ops.FromOracle, for tests and tools/oracle; the docx
// binary never calls it.
//
// kind is the read kind ("find" for find). What it does, each an
// intended difference from docxdriver's surface (docs/contract.md):
//
//   - the outcome wrapper goes: the result's fields are the object, with
//     base (docxdriver's source) and without the repeated source;
//   - null members are left out;
//   - camelCase keys become snake_case (idRepairsNeeded, paragraphIndex,
//     basedOn), and the key of a thing's own address is id (a style's
//     styleId, a section's handle, a table's at);
//   - ids become addresses: comment:N, revision:N, bookmark:Name,
//     <paragraph>/equation:N;
//   - the markup's attributes are the contract's (view.FromOracleMarkup);
//   - each table cell has its grid column (grid_column), counting the
//     columns its row skips (w:gridBefore) as the grid model does;
//   - a comment thread has the one shape of the contract: id, status,
//     anchor, root and replies with author, date, text and the stored
//     para_id and durable_id. docxdriver's two shapes repeat the id under
//     five names; nothing else is dropped.
//
// One thing cannot be converted: the anchor ranges of --kind comments,
// which docxdriver gives as positional locators (p3:5-9) where the
// contract gives the selection a plan takes. For that kind the ranges
// are returned as docxdriver wrote them; compare their number.
//
// An outcome other than completed is returned as an error holding
// docxdriver's message.
func FromOracle(kind string, stdout []byte) (Object, error) {
	// The markup's attribute names first (data-docx-x is data-x, a
	// handle is an id, an equation's number its address).
	dec := json.NewDecoder(strings.NewReader(view.FromOracleMarkup(string(stdout))))
	dec.UseNumber()
	var top map[string]any
	if err := dec.Decode(&top); err != nil {
		return nil, err
	}
	if top["outcome"] != "completed" {
		msg := ""
		if d, ok := top["diagnostic"].(map[string]any); ok {
			msg, _ = d["message"].(string)
		}
		return nil, fmt.Errorf("docxdriver: %v: %s", top["outcome"], msg)
	}
	res, _ := plain(top["result"]).(Object)
	if res == nil {
		res = Object{}
	}
	delete(res, "source")
	rename(res, "idRepairsNeeded", "id_repairs_needed")
	list := func(key string, each func(Object)) {
		items, _ := res[key].([]any)
		for _, it := range items {
			if o, ok := it.(Object); ok {
				each(o)
			}
		}
	}
	list("equations", func(e Object) { equationID(e, e["at"], "equation") })
	list("sections", func(s Object) { rename(s, "handle", "id") })
	list("tables", func(t Object) {
		rename(t, "at", "id")
		// Each cell's grid column, which the contract's read shows and
		// docxdriver's leaves to be counted from the spans before it.
		rows, _ := t["rows"].([]any)
		for _, r := range rows {
			row, _ := r.(Object)
			cells, _ := row["cells"].([]any)
			column := int64(1)
			if props, ok := row["properties"].(Object); ok {
				if gb, ok := props["gridBefore"].(Object); ok {
					v, _ := gb["val"].(string)
					column += int64(view.GridBeforeVal(v)) //nolint:gosec // G115: at most 65536
				}
			}
			for _, c := range cells {
				cell, ok := c.(Object)
				if !ok {
					continue
				}
				cell["grid_column"] = column
				if span, ok := cell["grid_span"].(int64); ok && span > 1 {
					column += span
				} else {
					column++
				}
			}
		}
	})
	list("comments", func(t Object) { thread(t, kind == "comments") })
	list("revisions", func(r Object) {
		rename(r, "paragraphIndex", "paragraph_index")
		if id, ok := r["id"].(string); ok {
			r["id"] = "revision:" + id
		}
	})
	list("bookmarks", func(b Object) {
		rename(b, "id", "ooxml_id")
		if name, ok := b["name"].(string); ok {
			b["id"] = "bookmark:" + name
		}
	})
	if kind == "styles" {
		list("styles", func(s Object) {
			rename(s, "styleId", "id")
			rename(s, "basedOn", "based_on")
		})
	}
	list("matches", func(p Object) {
		eqs, _ := p["equations"].([]any)
		for _, e := range eqs {
			if o, ok := e.(Object); ok {
				equationID(o, p["id"], "equation")
				rename(o, "equation", "index")
			}
		}
	})
	if kind == "document" {
		list("equations", func(e Object) { delete(e, "equation") })
	}
	src, _ := top["source"].(string)
	out := Result{Base: src, Value: res}.Object()
	return out, nil
}

// Decode parses a result this package printed into the values Encode
// writes, for comparing it with FromOracle's.
func Decode(stdout []byte) (Object, error) {
	dec := json.NewDecoder(bytes.NewReader(stdout))
	dec.UseNumber()
	var top map[string]any
	if err := dec.Decode(&top); err != nil {
		return nil, err
	}
	o, _ := plain(top).(Object)
	return o, nil
}

// plain converts decoded JSON into the values Encode writes: objects
// without their null members, integers as int64, other numbers as their
// text.
func plain(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := Object{}
		for k, e := range x {
			if e != nil {
				out[k] = plain(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	case json.Number:
		if n, err := strconv.ParseInt(string(x), 10, 64); err == nil {
			return n
		}
		return x
	}
	return v
}

// rename moves object o's member from key from to key to.
func rename(o Object, from, to string) {
	if v, ok := o[from]; ok {
		delete(o, from)
		o[to] = v
	}
}

// equationID gives an equation entry its address from its paragraph's id
// and its 1-based number under key.
func equationID(e Object, para any, key string) {
	p, ok := para.(string)
	n, isNum := e[key].(int64)
	if ok && isNum {
		e["id"] = EquationAddress(p, int(n))
	}
}

// thread reshapes a docxdriver comment thread, of either read kind, into
// the contract's.
func thread(t Object, listing bool) {
	comment := func(c Object, withID bool) Object {
		out := Object{}
		for _, k := range []string{"author", "date", "text"} {
			if v, ok := c[k]; ok {
				out[k] = v
			}
		}
		if p, _ := c["paraId"].(string); p != "" {
			out["para_id"] = p
		}
		if d, _ := c["durableId"].(string); d != "" {
			out["durable_id"] = d
		}
		if id, ok := c["id"].(string); ok && withID {
			out["id"] = CommentAddress(id)
		}
		return out
	}
	root, _ := t["root"].(Object)
	if root == nil {
		root = Object{}
	}
	// The document kind keeps the root's durable id on the thread only.
	if d, _ := t["durableId"].(string); d != "" && !listing {
		root["durableId"] = d
	}
	replies := []any{}
	if rs, ok := t["replies"].([]any); ok {
		for _, r := range rs {
			if o, ok := r.(Object); ok {
				replies = append(replies, comment(o, true))
			}
		}
	}
	id, _ := t["id"].(string)
	anchor, status := t["anchor"], t["status"]
	for k := range t {
		delete(t, k)
	}
	t["id"], t["status"], t["anchor"] = CommentAddress(id), status, anchor
	t["root"], t["replies"] = comment(root, false), replies
}

// WithoutStoryAddresses returns v with view.WithoutStoryAddresses applied
// to every string in it: a Go read put in docxdriver's terms for the
// oracle comparison, whose markup gives other stories' paragraphs no
// address (known difference 16).
func WithoutStoryAddresses(v any) any {
	switch x := v.(type) {
	case string:
		return view.WithoutStoryAddresses(x)
	case Object:
		for k, e := range x {
			x[k] = WithoutStoryAddresses(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = WithoutStoryAddresses(e)
		}
		return x
	}
	return v
}
