package ops

// SetFooter is footer.set (docxdriver's set_footer).
type SetFooter struct {
	At     *SectionAddress `json:"at" default:"null"`
	Kind   *ChromeKind     `json:"kind" default:"null"`
	HTML   string          `json:"html"`
	Delete []string        "json:\"delete\" default:\"[]\" doc:\"Addresses of the equations and placeholders of the footer that `html` leaves out on purpose.\""
}

// opFooterSet is footer.set's entry in the operation table.
var opFooterSet = OpDef{
	Name: "footer.set", Oracle: "set_footer", New: func() Spec { return &SetFooter{} },
	Moved: "`at` ← `section`; `html` ← `with`",
	Rules: setFooterRules, Track: ComplexField, Direct: OwnPath,
	TrackGuards: storySetGuards,
}

// Run sets the section's footer of a kind to html's paragraphs (setStory).
func (o *SetFooter) Run(env *Env) error {
	return env.setStory(footerStory, o.At, o.Kind, o.HTML, o.Delete)
}
