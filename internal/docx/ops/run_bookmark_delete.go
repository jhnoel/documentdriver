package ops

// BookmarkDelete is bookmark.delete (docxdriver's bookmark_delete).
type BookmarkDelete struct {
	At BookmarkAddress `json:"at"`
}

// opBookmarkDelete is bookmark.delete's entry in the operation table.
var opBookmarkDelete = OpDef{
	Name: "bookmark.delete", Oracle: "bookmark_delete", New: func() Spec { return &BookmarkDelete{} },
	Moved: "`at` ← `name`",
	Rules: bookmarkDeleteRules, Track: Untracked, Direct: Same,
}

// Run removes a bookmark of any story, its start and every end with its
// id (bookmarks.rs delete); the text is unchanged.
func (o *BookmarkDelete) Run(env *Env) error {
	name, _ := o.At.Name()
	b, _, err := env.findBookmark(name)
	if err != nil {
		return err
	}
	h, err := env.storyHandle(b.part)
	if err != nil {
		return err
	}
	if err := removeBookmark(h, b); err != nil {
		return err
	}
	return nil
}
