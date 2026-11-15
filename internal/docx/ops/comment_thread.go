package ops

import (
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// The helpers of comment.reply, comment.set_status and comment.delete:
// the comment an address names, and its anchors in the stories.

// existingCommentParts checks, before any change, that every comment
// part the document has can be read (unreadable_target otherwise).
func (env *Env) existingCommentParts() error {
	cp, err := env.WS.CommentParts()
	if err != nil {
		return err
	}
	for _, name := range []string{cp.Comments, cp.Extended, cp.IDs, cp.Extensible} {
		if name == "" {
			continue
		}
		if doc, err := env.WS.Tree(name); err != nil || doc.Root() == nil {
			return &Rejection{Code: "unreadable_target", Msg: "the comment part " + strings.TrimPrefix(name, "/") + " cannot be read: " + errText(err)}
		}
	}
	return nil
}

// comment resolves a comment address to its record.
func (env *Env) comment(at CommentAddress) (docxedit.Comment, error) {
	id, _ := at.ID()
	c, ok, err := env.WS.Comment(id)
	if err != nil {
		return c, err
	}
	if !ok {
		return c, reject("not_found", "at", "comment not found: %s", id)
	}
	return c, nil
}

// anchor is one element of a comment's anchor in a story: a range
// marker (w:commentRangeStart or w:commentRangeEnd) or the run holding
// its w:commentReference.
type anchor struct {
	part string
	n    *tree.Node
}

// anchors returns the anchors of the comments whose ids are listed, in
// every story part (the main document, headers, footers, notes),
// document order within each.
func (env *Env) anchors(ids map[string]bool) ([]anchor, error) {
	parts, err := env.WS.StoryParts()
	if err != nil {
		return nil, err
	}
	cp, err := env.WS.CommentParts()
	if err != nil {
		return nil, err
	}
	var out []anchor
	for _, part := range parts {
		if part == cp.Comments {
			continue
		}
		doc, err := env.WS.Tree(part)
		if err != nil || doc.Root() == nil {
			continue // a story that does not parse holds no anchor we could reach
		}
		doc.Root().Walk(func(n *tree.Node) bool {
			switch {
			case (model.IsW(n, "commentRangeStart") || model.IsW(n, "commentRangeEnd")) && ids[model.AttrOr(n, "id")]:
				out = append(out, anchor{part, n})
			case model.IsW(n, "r"):
				for _, c := range n.Elements() {
					if model.IsW(c, "commentReference") && ids[model.AttrOr(c, "id")] {
						out = append(out, anchor{part, n})
						return false
					}
				}
			}
			return true
		})
	}
	return out, nil
}

// editor returns a handle that may change the content of the unit
// holding n (its nearest enclosing paragraph, cell, body or part root).
func (env *Env) editor(part string, n *tree.Node) *core.Handle {
	u := n.Parent()
	for u.Parent() != nil && u.Parent().Kind() != tree.DocumentNode && !docxedit.IsUnit(u) {
		u = u.Parent()
	}
	return env.WS.Target(part, "the comment's anchor", u)
}
