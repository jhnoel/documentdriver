package ops

// CommentDelete is comment.delete (docxdriver's comment_delete).
type CommentDelete struct {
	At CommentAddress `json:"at" doc:"The comment (comment:N): it goes with every reply in its thread."`
}

// opCommentDelete is comment.delete's entry in the operation table.
var opCommentDelete = OpDef{
	Name: "comment.delete", Oracle: "comment_delete", New: func() Spec { return &CommentDelete{} },
	Moved: "`at` ← `comment_id`",
	Rules: commentAtRules[CommentDelete](`{"op":"comment.delete","at":"comment:"}`), Track: Untracked, Direct: Same,
}

// Run deletes a comment thread (comments.rs delete_comment_typed): the
// comment and every reply chained to it, from each comment part that
// exists, and their range markers and reference runs from every story
// (docxdriver strips the body's only). Comments are not tracked, so both
// change modes write the same.
func (o *CommentDelete) Run(env *Env) error {
	if err := env.existingCommentParts(); err != nil {
		return err
	}
	root, err := env.comment(o.At)
	if err != nil {
		return err
	}
	thread, err := env.WS.CommentThread(root)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, c := range thread {
		ids[c.ID] = true
	}
	anchors, err := env.anchors(ids)
	if err != nil {
		return err
	}
	for _, a := range anchors {
		if err := env.editor(a.part, a.n).RemoveNode(a.n); err != nil {
			return err
		}
	}
	if err := env.WS.RemoveComments(thread); err != nil {
		return err
	}
	return nil
}
