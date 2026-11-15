package ops

// BookmarkRename is bookmark.rename (docxdriver's bookmark_rename).
type BookmarkRename struct {
	At   BookmarkAddress `json:"at"`
	Name string          `json:"name" doc:"The new name."`
}

// opBookmarkRename is bookmark.rename's entry in the operation table.
var opBookmarkRename = OpDef{
	Name: "bookmark.rename", Oracle: "bookmark_rename", New: func() Spec { return &BookmarkRename{} },
	Moved: "`at` `bookmark:Name` ← `name`; `name` ← `new_name`",
	Rules: bookmarkRenameRules, Track: Untracked, Direct: Same,
}

// Run renames a bookmark of any story (bookmarks.rs rename): the new name
// may be the old one in another case, but no other bookmark's. Renaming
// to the name it has writes nothing.
func (o *BookmarkRename) Run(env *Env) error {
	name, _ := o.At.Name()
	b, known, err := env.findBookmark(name)
	if err != nil {
		return err
	}
	if err := unusedName(known, o.Name, "name", b.start); err != nil {
		return err
	}
	if b.name == o.Name {
		return nil
	}
	h, err := env.storyHandle(b.part)
	if err != nil {
		return err
	}
	return h.SetNodeAttr(b.start, qualified(b.start, "name"), o.Name)
}
