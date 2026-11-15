package ops

// SetHeader is header.set (docxdriver's set_header).
type SetHeader struct {
	At     *SectionAddress `json:"at" default:"null"`
	Kind   *ChromeKind     `json:"kind" default:"null"`
	HTML   string          `json:"html"`
	Delete []string        "json:\"delete\" default:\"[]\" doc:\"Addresses of the equations and placeholders of the header that `html` leaves out on purpose.\""
}

// opHeaderSet is header.set's entry in the operation table.
var opHeaderSet = OpDef{
	Name: "header.set", Oracle: "set_header", New: func() Spec { return &SetHeader{} },
	Moved: "`at` ← `section`; `html` ← `with`",
	Rules: setHeaderRules, Track: KeepsAdditions, Direct: OwnPath,
	TrackGuards: storySetGuards,
}

// Run sets the section's header of a kind to html's paragraphs (setStory).
func (o *SetHeader) Run(env *Env) error {
	return env.setStory(headerStory, o.At, o.Kind, o.HTML, o.Delete)
}
