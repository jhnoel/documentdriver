package ops

import (
	"errors"
)

// BookmarkMove is bookmark.move (docxdriver's bookmark_move).
type BookmarkMove struct {
	At         BookmarkAddress  `json:"at"`
	To         ParagraphAddress `json:"to" doc:"The paragraph the bookmark moves to."`
	Select     *string          `json:"select" default:"null"`
	Occurrence *uint32          `json:"occurrence" default:"null"`
}

// opBookmarkMove is bookmark.move's entry in the operation table.
var opBookmarkMove = OpDef{
	Name: "bookmark.move", Oracle: "bookmark_move", New: func() Spec { return &BookmarkMove{} },
	Moved: "`at` ← `name`; `to` ← `at`",
	Rules: bookmarkMoveRules, Track: Untracked, Direct: Same,
}

// Run moves a bookmark to a paragraph or a span of one, in any story
// (bookmarks.rs move_to): its markers are removed and placed anew with its
// id and name, as bookmark.add places them.
func (o *BookmarkMove) Run(env *Env) error {
	name, _ := o.At.Name()
	b, _, err := env.findBookmark(name)
	if err != nil {
		return err
	}
	part, p, pid, err := env.textParagraph(o.To)
	if err != nil {
		var r *Rejection
		if errors.As(err, &r) && r.Field == "at" {
			r.Field = "to"
		}
		return err
	}
	h, err := env.storyHandle(b.part)
	if err != nil {
		return err
	}
	if err := removeBookmark(h, b); err != nil {
		return err
	}
	if err := env.placeBookmark(part, p, pid, b.id, b.name, o.Select, o.Occurrence); err != nil {
		return err
	}
	return nil
}
