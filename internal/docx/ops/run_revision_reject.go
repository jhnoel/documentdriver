package ops

// RejectRevisions is revision.reject (docxdriver's revision_settle with
// action reject).
type RejectRevisions struct {
	At     *RevisionScope  "json:\"at\" default:\"null\" doc:\"One revision (`revision:7`), or the body paragraphs whose revisions are settled. Not with `all`.\""
	All    bool            `json:"all,omitzero" default:"false" doc:"Every revision of the document."`
	Author *RevisionAuthor `json:"author" default:"null" doc:"Only this author's revisions (with all or paragraphs)."`
}

// opRevisionReject is revision.reject's entry in the operation table.
var opRevisionReject = OpDef{
	Name: "revision.reject", Oracle: "revision_settle", New: func() Spec { return &RejectRevisions{} },
	Moved: "`at` / `all` / `author` ← `target`",
	Fixed: map[string]string{"action": `"reject"`}, Rules: revisionRules(func(a *RejectRevisions) (*RevisionScope, bool, *RevisionAuthor) { return a.At, a.All, a.Author }, "revision.reject"),
	Track: Untracked, Direct: Same,
}

// Run rejects the revisions (the shared settle is in
// run_revision_accept.go).
func (o *RejectRevisions) Run(env *Env) error { return env.settle(o.At, o.Author, false) }
