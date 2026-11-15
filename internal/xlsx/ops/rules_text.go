package ops

// text.replace's rules (designer-owned).
var textReplaceRules = []Rule{
	rule("select must not be empty", "select",
		func(o *TextReplace) bool { return o.Select == "" },
		`{"op":"text.replace","at":"sheet:Budget","select":"","with":"x"}`),
	rule("occurrence and all cannot both be given", "occurrence",
		func(o *TextReplace) bool { return o.Occurrence != nil && o.All },
		`{"op":"text.replace","at":"sheet:Budget","select":"a","with":"b","occurrence":1,"all":true}`),
	rule("occurrence counts from 1", "occurrence",
		func(o *TextReplace) bool { return o.Occurrence != nil && *o.Occurrence == 0 },
		`{"op":"text.replace","at":"sheet:Budget","select":"a","with":"b","occurrence":0}`),
}
