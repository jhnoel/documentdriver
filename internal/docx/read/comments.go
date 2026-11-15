package read

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// commentPartName finds a comment part the OPC way: the target of the main
// document part's internal relationship of type relType, when that part
// exists; "" when there is none. docxdriver reads the parts at fixed names
// (word/comments.xml, ...), so it misses renamed parts and reads orphaned
// ones no relationship names (an intended difference).
func commentPartName(pkg *model.Package, relType string) string {
	main := pkg.DocumentPartName()
	for _, r := range pkg.Rels(main) {
		if !r.HasType || r.Type != relType || r.External() || !r.HasTarget {
			continue
		}
		if part := model.ResolveRelTarget(main, r.Target); pkg.Has(part) {
			return part
		}
	}
	return ""
}

type commentRecord struct {
	id, author, date, text, paraID string
}

type extendedRecord struct {
	paraID    string
	done      bool
	parent    string
	hasParent bool
}

// parseCommentPart parses a comment part; an absent part reads as empty.
// A part that is not well formed is an error worded as docxdriver's.
func parseCommentPart(pkg *model.Package, name string) (*model.XML, bool, string) {
	if name == "" {
		return nil, false, ""
	}
	if !pkg.Has(name) {
		return nil, false, ""
	}
	x, err := pkg.XML(name)
	if err != nil {
		detail := strings.TrimPrefix(err.Error(), "document.xml parse failed: ")
		pkg.Note(name, "the part is not well formed: "+detail)
		return nil, true, name + ": XML parse failed: " + detail
	}
	return x, true, ""
}

// rootChildren returns the root's child elements named local, or nil.
func rootChildren(x *model.XML, local string) []*tree.Node {
	if x == nil || x.Root() == nil {
		return nil
	}
	var out []*tree.Node
	for c := range x.Root().ChildrenSeq() {
		if model.IsLocal(c, local) {
			out = append(out, c)
		}
	}
	return out
}

// commentRecords returns one record per stored comment in x.
func commentRecords(x *model.XML) []commentRecord {
	var out []commentRecord
	for _, n := range rootChildren(x, "comment") {
		r := commentRecord{
			id:     model.AttrOr(n, "id"),
			author: model.AttrOr(n, "author"),
			date:   model.AttrOr(n, "date"),
		}
		var text strings.Builder
		var para *tree.Node
		n.Walk(func(d *tree.Node) bool {
			if d == n {
				return true
			}
			if para == nil && model.IsLocal(d, "p") {
				para = d
			}
			if model.IsLocal(d, "t") {
				text.WriteString(model.TextContent(d))
			}
			return true
		})
		r.text = text.String()
		if para != nil {
			r.paraID = model.AttrOr(para, "paraId")
		}
		out = append(out, r)
	}
	return out
}

// extendedRecords returns one record per stored commentEx element in x.
func extendedRecords(x *model.XML) []extendedRecord {
	var out []extendedRecord
	for _, n := range rootChildren(x, "commentEx") {
		e := extendedRecord{paraID: model.AttrOr(n, "paraId")}
		d, ok := model.Attr(n, "done")
		e.done = ok && d == "1"
		e.parent, e.hasParent = model.Attr(n, "paraIdParent")
		out = append(out, e)
	}
	return out
}

// durableIDs returns each commentId element's paragraph and durable ids.
func durableIDs(x *model.XML) [][2]string {
	var out [][2]string
	for _, n := range rootChildren(x, "commentId") {
		out = append(out, [2]string{model.AttrOr(n, "paraId"), model.AttrOr(n, "durableId")})
	}
	return out
}

// commentThreads lists the comment threads: the root comments (those
// without a paraIdParent) with their replies, anchors and resolution
// state. One shape serves every read kind that shows comments: the
// thread's id is its address (comment:3), its anchor ranges are the
// selections a plan can use ({at, select, occurrence} with start and end
// for orientation), and root and replies carry author, date, text and
// the ids the file stores (para_id, durable_id) where it stores them. A
// comments part that is not well formed is the rejection message; the
// other comment parts then count as empty.
func commentThreads(st *model.Document) ([]any, string) {
	pkg := st.Pkg
	comments, ok, msg := parseCommentPart(pkg, commentPartName(pkg, ctypes.RelComments))
	if !ok {
		return []any{}, ""
	}
	if msg != "" {
		return nil, msg
	}
	extended, _, _ := parseCommentPart(pkg, commentPartName(pkg, ctypes.RelCommentsExtended))
	ids, _, _ := parseCommentPart(pkg, commentPartName(pkg, ctypes.RelCommentsIds))
	anchors := documentCommentAnchors(st)
	records := commentRecords(comments)
	ext := extendedRecords(extended)
	durable := durableIDs(ids)

	extOf := func(paraID string) *extendedRecord {
		for i := range ext {
			if ext[i].paraID == paraID {
				return &ext[i]
			}
		}
		return nil
	}
	durableOf := func(paraID string) string {
		for _, d := range durable {
			if d[0] == paraID {
				return d[1]
			}
		}
		return ""
	}
	commentJSON := func(r commentRecord) Object {
		o := Object{"author": r.author, "date": r.date, "text": r.text}
		if r.paraID != "" {
			o["para_id"] = r.paraID
		}
		if d := durableOf(r.paraID); d != "" {
			o["durable_id"] = d
		}
		return o
	}
	threads := []any{}
	for _, root := range records {
		if e := extOf(root.paraID); e != nil && e.hasParent {
			continue
		}
		replies := []any{}
		for _, r := range records {
			if e := extOf(r.paraID); e != nil && e.hasParent && e.parent == root.paraID {
				reply := commentJSON(r)
				reply["id"] = CommentAddress(r.id)
				replies = append(replies, reply)
			}
		}
		ranges := []any{}
		for _, r := range anchors[root.id] {
			ranges = append(ranges, r)
		}
		status := "open"
		if e := extOf(root.paraID); e != nil && e.done {
			status = "resolved"
		}
		threads = append(threads, Object{
			"id": CommentAddress(root.id), "status": status, "anchor": Object{"ranges": ranges},
			"root": commentJSON(root), "replies": replies,
		})
	}
	return threads, ""
}

// CommentAddress is the address of the comment with the file's id.
func CommentAddress(id string) string { return "comment:" + id }
