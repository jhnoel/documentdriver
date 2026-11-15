package ops

// BookmarkAdd is bookmark.add (docxdriver's bookmark_add).
type BookmarkAdd struct {
	At         ParagraphAddress `json:"at"`
	Name       string           `json:"name"`
	Select     *string          `json:"select" default:"null"`
	Occurrence *uint32          `json:"occurrence" default:"null"`
}

// opBookmarkAdd is bookmark.add's entry in the operation table.
var opBookmarkAdd = OpDef{
	Name: "bookmark.add", Oracle: "bookmark_add", New: func() Spec { return &BookmarkAdd{} },
	Rules: bookmarkAddRules, Track: Untracked, Direct: Same,
}

// Run adds a bookmark around a paragraph or a span of its final view
// (bookmarks.rs add), in any story: a name no bookmark uses yet (ignoring
// case), the next bookmark id of the document, and markers beside the
// span's first and last run, outside every wrapper whose edge is the
// span's (the bookmarked text is the same either way).
func (o *BookmarkAdd) Run(env *Env) error {
	part, p, pid, err := env.textParagraph(o.At)
	if err != nil {
		return err
	}
	known, err := env.bookmarks()
	if err != nil {
		return err
	}
	if err := unusedName(known, o.Name, "name", nil); err != nil {
		return err
	}
	id, err := env.WS.BookmarkID()
	if err != nil {
		return err
	}
	if err := env.placeBookmark(part, p, pid, id, o.Name, o.Select, o.Occurrence); err != nil {
		return err
	}
	return nil
}
