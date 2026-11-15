package ops

// ClearHeader is header.delete (docxdriver's clear_header).
type ClearHeader struct {
	At   *SectionAddress `json:"at" default:"null"`
	Kind *ChromeKind     `json:"kind" default:"null"`
}

// opHeaderDelete is header.delete's entry in the operation table.
var opHeaderDelete = OpDef{
	Name: "header.delete", Oracle: "clear_header", New: func() Spec { return &ClearHeader{} },
	Moved: "`at` ← `section`",
	Rules: sectionRules[ClearHeader](`{"op":"header.delete","at":"section:0"}`), Track: EmptiesStory, Direct: OwnPath,
	TrackGuards: storyDeleteGuards,
}

// Run deletes the section's header of a kind (deleteStory).
func (o *ClearHeader) Run(env *Env) error {
	return env.deleteStory(headerStory, o.At, o.Kind)
}
