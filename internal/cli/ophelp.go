package cli

// FindOpHelp returns OpHelp of the entry of docs named name, and false when none is.
func FindOpHelp(docs []OpDoc, name string) (string, bool) {
	for _, d := range docs {
		if d.Name == name {
			return OpHelp(d), true
		}
	}
	return "", false
}
