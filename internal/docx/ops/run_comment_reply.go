package ops

import (
	"errors"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// CommentReply is comment.reply (docxdriver's comment_reply).
type CommentReply struct {
	At   CommentAddress `json:"at" doc:"The comment replied to (comment:N)."`
	Text string         `json:"text"`
}

// opCommentReply is comment.reply's entry in the operation table.
var opCommentReply = OpDef{
	Name: "comment.reply", Oracle: "comment_reply", New: func() Spec { return &CommentReply{} },
	Moved: "`at` `comment:n` ← `comment_id`",
	Rules: commentReplyRules, Track: Untracked, Direct: Same,
}

// Run replies to a comment (comments.rs reply_comment_typed): the reply's
// records in all four comment parts, threaded under the parent's
// paragraph id, and its anchor where Word puts one: its range markers
// right after the parent's and its reference run right after the
// parent's, in whichever story the parent is anchored. A parent without
// a commentsExtended record gets one, so the reply's names it.
// docxdriver anchors only a parent whose three anchors are all in the
// body, and leaves the reply unanchored otherwise (W2). A parent with no
// reference run is refused; one without a paragraph id gets one.
// Comments are not tracked, so both change modes write the same.
func (o *CommentReply) Run(env *Env) error {
	if err := env.existingCommentParts(); err != nil {
		return err
	}
	parent, err := env.comment(o.At)
	if err != nil {
		return err
	}
	// The reply's records go to all four parts: a missing one must be
	// registrable, checked before any change.
	if err := env.commentParts(); err != nil {
		return err
	}
	anchors, err := env.anchors(map[string]bool{parent.ID: true})
	if err != nil {
		return err
	}
	var start, end, ref *anchor
	for i := range anchors {
		a := &anchors[i]
		switch {
		case model.IsW(a.n, "commentRangeStart") && start == nil:
			start = a
		case model.IsW(a.n, "commentRangeEnd") && end == nil:
			end = a
		case model.IsW(a.n, "r") && ref == nil:
			ref = a
		}
	}
	if ref == nil {
		return reject("operation_failed", "at", "comment %s has no reference in the document, so a reply has nowhere to be anchored", parent.ID)
	}
	// A parent without a paragraph id gets one, and one without a
	// commentsExtended record gets one, so the reply's paraIdParent
	// names it.
	if parent, err = env.WS.EnsureCommentParaID(parent); err != nil {
		return err
	}
	if parent.ParaID == "" {
		return reject("operation_failed", "at", "comment %s has no paragraph, so a reply cannot be threaded under it", parent.ID)
	}
	if _, found, err := env.WS.CommentDone(parent); err != nil {
		return err
	} else if !found {
		if err := env.WS.SetCommentDone(parent, false); err != nil {
			return err
		}
	}
	id, err := env.WS.CommentID()
	if err != nil {
		return err
	}
	place := func(a *anchor, frag string) error {
		if a == nil {
			return nil
		}
		h := env.editor(a.part, a.n)
		nodes, err := h.Workspace().Parse(a.n.Parent(), []byte(frag), map[string]string{"w": model.NSW})
		if err != nil {
			return err
		}
		return h.PlaceAt(a.n.Parent(), nodes[0], model.NextElement(a.n))
	}
	w := `w:id="` + id + `"`
	// A range needs both markers: the reply gets one only where the
	// parent has both.
	if start != nil && end != nil {
		if err := place(start, `<w:commentRangeStart `+w+`/>`); err != nil {
			return err
		}
		if err := place(end, `<w:commentRangeEnd `+w+`/>`); err != nil {
			return err
		}
	}
	if err := place(ref, `<w:r><w:rPr><w:rStyle w:val="CommentReference"/></w:rPr><w:commentReference `+w+`/></w:r>`); err != nil {
		return err
	}
	paraID, err := env.WS.CommentParaID()
	if err != nil {
		return err
	}
	durable, err := env.WS.DurableID(paraID, env.Author, o.Text)
	if err != nil {
		return err
	}
	err = env.WS.AddCommentRecords(docxedit.NewComment{
		ID: id, ParaID: paraID, ParentParaID: parent.ParaID, Author: env.Author, Initials: initials(env.Author),
		Date: env.Now.UTC().Format(revise.DateLayout), Text: o.Text, DurableID: durable,
	})
	if errors.Is(err, opc.ErrContentTypes) {
		return &Rejection{Code: "unreadable_target", Msg: err.Error()}
	}
	if err != nil {
		return err
	}
	return nil
}
