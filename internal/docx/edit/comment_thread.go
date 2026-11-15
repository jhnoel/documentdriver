package edit

import (
	"fmt"
	"path"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// Comment threads: reading a comment's records, its replies, resolution,
// and removing a thread (comment.reply, comment.set_status,
// comment.delete).

// Comment is one w:comment record of the comments part.
type Comment struct {
	// ID is its w:id; ParaID the w14:paraId of its last paragraph ("" when
	// it has none), which its commentsExtended and commentsIds records
	// name (Word's rule; docxdriver reads the first paragraph's, the same
	// for the one-paragraph comments it writes).
	ID, ParaID string
}

// Comment returns the comment whose w:id is id, the first such record.
func (w *Workspace) Comment(id string) (Comment, bool, error) {
	cp, err := w.CommentParts()
	if err != nil {
		return Comment{}, false, err
	}
	recs, err := w.commentRecords(cp.Comments, "comment")
	if err != nil {
		return Comment{}, false, err
	}
	for _, c := range recs {
		if model.AttrOr(c, "id") == id {
			return Comment{ID: id, ParaID: lastParaID(c)}, true, nil
		}
	}
	return Comment{}, false, nil
}

// CommentThread returns the comments of the thread rooted at root: root
// and every reply whose commentsExtended paraIdParent chains to it
// (comments.rs delete_comment_typed), in comments-part order, each once.
// A comment without a paragraph id is a thread of its own (docxdriver
// threads every comment without one together, W14).
func (w *Workspace) CommentThread(root Comment) ([]Comment, error) {
	cp, err := w.CommentParts()
	if err != nil {
		return nil, err
	}
	exs, err := w.commentRecords(cp.Extended, "commentEx")
	if err != nil {
		return nil, err
	}
	in := map[string]bool{}
	if root.ParaID != "" {
		in[strings.ToUpper(root.ParaID)] = true
	}
	for grew := len(in) > 0; grew; {
		grew = false
		for _, e := range exs {
			p, parent := strings.ToUpper(model.AttrOr(e, "paraId")), strings.ToUpper(model.AttrOr(e, "paraIdParent"))
			if parent != "" && in[parent] && !in[p] {
				in[p] = true
				grew = true
			}
		}
	}
	recs, err := w.commentRecords(cp.Comments, "comment")
	if err != nil {
		return nil, err
	}
	out := []Comment{}
	seen := map[string]bool{}
	for _, c := range recs {
		id, p := model.AttrOr(c, "id"), lastParaID(c)
		if seen[id] {
			continue
		}
		if id == root.ID || p != "" && in[strings.ToUpper(p)] {
			seen[id] = true
			out = append(out, Comment{ID: id, ParaID: p})
		}
	}
	return out, nil
}

// RemoveComments removes the records of comments from each comment part
// that exists: every w:comment with one of their ids, the commentEx and
// commentId of their paragraph ids, and the commentExtensible of those
// commentIds' durable ids. It declares what it changes.
func (w *Workspace) RemoveComments(comments []Comment) error {
	cp, err := w.CommentParts()
	if err != nil {
		return err
	}
	ids, paras := map[string]bool{}, map[string]bool{}
	for _, c := range comments {
		ids[c.ID] = true
		if c.ParaID != "" {
			paras[strings.ToUpper(c.ParaID)] = true
		}
	}
	durable := map[string]bool{}
	steps := []struct {
		part string
		pick func(*tree.Node) bool
	}{
		{cp.Comments, func(c *tree.Node) bool { return model.IsLocal(c, "comment") && ids[model.AttrOr(c, "id")] }},
		{cp.Extended, func(c *tree.Node) bool {
			return model.IsLocal(c, "commentEx") && paras[strings.ToUpper(model.AttrOr(c, "paraId"))]
		}},
		{cp.IDs, func(c *tree.Node) bool {
			if model.IsLocal(c, "commentId") && paras[strings.ToUpper(model.AttrOr(c, "paraId"))] {
				durable[strings.ToUpper(model.AttrOr(c, "durableId"))] = true
				return true
			}
			return false
		}},
		{cp.Extensible, func(c *tree.Node) bool {
			return model.IsLocal(c, "commentExtensible") && durable[strings.ToUpper(model.AttrOr(c, "durableId"))]
		}},
	}
	for _, s := range steps {
		if s.part == "" {
			continue
		}
		doc, err := w.Tree(s.part)
		if err != nil {
			return err
		}
		if doc.Root() == nil {
			continue
		}
		for _, c := range doc.Root().Elements() {
			if s.pick(c) {
				if err := w.core.Delete(s.part, c); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// CommentDone reports a comment's resolution as its commentsExtended
// record gives it (w15:done="1"), and whether it has one.
func (w *Workspace) CommentDone(c Comment) (done, found bool, err error) {
	cp, err := w.CommentParts()
	if err != nil {
		return false, false, err
	}
	exs, err := w.commentRecords(cp.Extended, "commentEx")
	if err != nil {
		return false, false, err
	}
	for _, e := range exs {
		if c.ParaID != "" && strings.EqualFold(model.AttrOr(e, "paraId"), c.ParaID) {
			return model.AttrOr(e, "done") == "1", true, nil
		}
	}
	return false, false, nil
}

// SetCommentDone writes a comment's resolution (comments.rs
// resolve_comment_typed): w15:done on its commentEx, under whichever name
// the attribute already has, or a new commentEx where it has none; and a
// commentsExtended part, related and registered, where the document has
// none (docxdriver registers the parts and writes none, W1). It declares
// what it changes.
func (w *Workspace) SetCommentDone(c Comment, done bool) error {
	cp, err := w.CommentParts()
	if err != nil {
		return err
	}
	part := cp.Extended
	if part == "" {
		if part, err = w.addCommentPart(1); err != nil {
			return err
		}
	}
	v := "0"
	if done {
		v = "1"
	}
	exs, err := w.commentRecords(part, "commentEx")
	if err != nil {
		return err
	}
	for _, e := range exs {
		if !strings.EqualFold(model.AttrOr(e, "paraId"), c.ParaID) {
			continue
		}
		name := ""
		for _, a := range e.Attrs() {
			if a.Name == "done" || strings.HasSuffix(a.Name, ":done") {
				name = a.Name
				break
			}
		}
		if name == "" {
			prefix, ok := e.LookupNamespace(W15.URIs[0])
			if !ok || prefix == "" {
				return fmt.Errorf("edit: the commentEx of comment %s is not in the w15 namespace", c.ID)
			}
			name = prefix + ":done"
		}
		h, err := w.core.Part(part)
		if err != nil {
			return err
		}
		return h.SetNodeAttr(e, name, v)
	}
	if err := w.core.EnsureRootNamespace(part, W15.Prefix, W15.URIs[0], true); err != nil {
		return err
	}
	doc, err := w.Tree(part)
	if err != nil {
		return err
	}
	root := doc.Root()
	nodes, err := w.core.Parse(root, []byte(`<w15:commentEx w15:paraId="`+esc(c.ParaID)+`" w15:done="`+v+`"/>`),
		map[string]string{"w15": W15.URIs[0]})
	if err != nil {
		return err
	}
	return w.core.Insert(part, core.Into(root), nodes[0], nil)
}

// addCommentPart adds the empty comment part of kind k (an index of
// commentKinds) beside the main document, related and registered, and
// returns its name. It refuses up front, adding nothing, when the content
// types stream cannot register it.
func (w *Workspace) addCommentPart(k int) (string, error) {
	if err := w.pkg.RegistersTypes(); err != nil {
		return "", err
	}
	kind := commentKinds[k]
	name := path.Join(path.Dir(w.doc), kind.file)
	if w.pkg.Has(name) {
		return "", fmt.Errorf("edit: %s exists but the document does not relate to it", name)
	}
	w.core.DeclarePart(name)
	w.core.DeclarePart(opc.ContentTypesName)
	if _, err := w.pkg.AddPart(name, []byte(kind.empty), kind.ct); err != nil {
		return "", err
	}
	rels, err := w.pkg.Rels(w.doc)
	if err != nil {
		return "", err
	}
	w.core.DeclarePart(rels.Name())
	if _, err := rels.Add(kind.rel, path.Base(name), false); err != nil {
		return "", err
	}
	return name, nil
}

// lastParagraph is the last w:p of comment record c, or nil.
func lastParagraph(c *tree.Node) *tree.Node {
	var last *tree.Node
	c.Walk(func(n *tree.Node) bool {
		if model.IsLocal(n, "p") {
			last = n
		}
		return true
	})
	return last
}

// lastParaID is the w14:paraId of comment record c's last paragraph, or
// "".
func lastParaID(c *tree.Node) string {
	if p := lastParagraph(c); p != nil {
		return model.AttrOr(p, "paraId")
	}
	return ""
}

// EnsureCommentParaID gives comment c's last paragraph a fresh w14:paraId
// (CommentParaID) when it has none, so that its commentsExtended and
// commentsIds records can name it, and returns the comment with it. A
// comment without a paragraph is returned as it is. It declares what it
// changes.
func (w *Workspace) EnsureCommentParaID(c Comment) (Comment, error) {
	if c.ParaID != "" {
		return c, nil
	}
	cp, err := w.CommentParts()
	if err != nil {
		return c, err
	}
	recs, err := w.commentRecords(cp.Comments, "comment")
	if err != nil {
		return c, err
	}
	var p *tree.Node
	for _, r := range recs {
		if model.AttrOr(r, "id") == c.ID {
			p = lastParagraph(r)
			break
		}
	}
	if p == nil {
		return c, nil
	}
	id, err := w.CommentParaID()
	if err != nil {
		return c, err
	}
	if err := w.core.EnsureRootNamespace(cp.Comments, W14.Prefix, W14.URIs[0], true); err != nil {
		return c, err
	}
	h, err := w.core.Part(cp.Comments)
	if err != nil {
		return c, err
	}
	prefix, ok := p.LookupNamespace(W14.URIs[0])
	if !ok || prefix == "" {
		return c, fmt.Errorf("edit: the w14 namespace is not in scope in comment %s", c.ID)
	}
	if err := h.SetNodeAttr(p, prefix+":paraId", id); err != nil {
		return c, err
	}
	c.ParaID = id
	return c, nil
}
