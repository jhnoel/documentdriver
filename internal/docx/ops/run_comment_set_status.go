package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/opc"
)

// CommentSetStatus is comment.set_status (docxdriver's comment_set_status).
type CommentSetStatus struct {
	At     CommentAddress `json:"at" doc:"The comment (comment:N)."`
	Status CommentStatus  `json:"status"`
}

// opCommentSetStatus is comment.set_status's entry in the operation table.
var opCommentSetStatus = OpDef{
	Name: "comment.set_status", Oracle: "comment_set_status", New: func() Spec { return &CommentSetStatus{} },
	Moved: "`at` ← `comment_id`",
	Rules: commentAtRules[CommentSetStatus](`{"op":"comment.set_status","at":"comment:x1","status":"open"}`), Track: Untracked, Direct: Same,
}

// Run resolves or reopens a comment (comments.rs resolve_comment_typed):
// w15:done on its commentsExtended record, written where it has none,
// and the commentsExtended part added where the document has none
// (docxdriver registers the comment parts and writes none of them, W1).
// A comment already so writes nothing; one without a paragraph id gets
// one.
func (o *CommentSetStatus) Run(env *Env) error {
	if err := env.existingCommentParts(); err != nil {
		return err
	}
	c, err := env.comment(o.At)
	if err != nil {
		return err
	}
	want := o.Status == "resolved"
	done, _, err := env.WS.CommentDone(c)
	if err != nil {
		return err
	}
	if done == want {
		return nil
	}
	// A comment without a paragraph id gets one, so that a
	// commentsExtended record can carry its status (docxdriver writes
	// a record naming the empty id, which its gate refuses).
	if c, err = env.WS.EnsureCommentParaID(c); err != nil {
		return err
	}
	if c.ParaID == "" {
		return reject("operation_failed", "at", "comment %s has no paragraph, so no commentsExtended record can carry its status", c.ID)
	}
	err = env.WS.SetCommentDone(c, want)
	if errors.Is(err, opc.ErrContentTypes) {
		return &Rejection{Code: "unreadable_target", Msg: "a commentsExtended part must be added, but " + err.Error()}
	}
	if err != nil {
		return err
	}
	return nil
}
