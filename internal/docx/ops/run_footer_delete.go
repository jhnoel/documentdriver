package ops

// ClearFooter is footer.delete (docxdriver's clear_footer).
type ClearFooter struct {
	At   *SectionAddress `json:"at" default:"null"`
	Kind *ChromeKind     `json:"kind" default:"null"`
}

// opFooterDelete is footer.delete's entry in the operation table.
var opFooterDelete = OpDef{
	Name: "footer.delete", Oracle: "clear_footer", New: func() Spec { return &ClearFooter{} },
	Moved: "`at` ← `section`",
	Rules: sectionRules[ClearFooter](`{"op":"footer.delete","at":"section:0"}`), Track: EmptiesStory, Direct: OwnPath,
	TrackGuards: storyDeleteGuards,
}

// Run deletes the section's footer of a kind (deleteStory).
func (o *ClearFooter) Run(env *Env) error {
	return env.deleteStory(footerStory, o.At, o.Kind)
}
