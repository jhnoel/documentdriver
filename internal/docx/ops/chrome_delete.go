package ops

// deleteStory is header.delete and footer.delete (headers.rs
// clear_chrome_typed): the section's explicit story of kind goes.
// Directly, its reference is removed (the section then inherits the
// previous section's story of that kind, as Word shows it). Word tracks
// a story's content, never its reference, so track mode deletes the
// story's content as tracked changes and leaves the story, empty once
// accepted (EmptiesStory); that is what removing it shows only in the
// first section, so a later section's is refused in track mode.
func (env *Env) deleteStory(c chrome, at *SectionAddress, k *ChromeKind) error {
	kind := kindOf(k)
	sect, n, err := env.storySection(at)
	if err != nil {
		return err
	}
	ref := storyRef(sect, c, kind)
	if ref == nil {
		return reject("not_found", "at", "section %d has no explicit %s %s to delete", n, kind, c.word)
	}
	if env.Mode == ModeTrack {
		if n != 1 {
			return reject("refused_in_mode", "at", "%s cannot be tracked in section %d: removing a later section's %s %s makes it inherit the previous section's, and Word cannot track a header or footer reference; use a direct plan", c.word+".delete", n, kind, c.word)
		}
		part, err := env.storyPart(ref)
		if err != nil {
			return err
		}
		if part == "" {
			return reject("unreadable_target", "at", "section %d's %s %s reference does not resolve to a part of the package", n, kind, c.word)
		}
		root, err := env.storyRoot(part)
		if err != nil {
			return reject("unreadable_target", "at", "%s: %s", part, err.Error())
		}
		for _, ch := range root.Elements() {
			if !isParagraphOrMarker(ch) {
				return reject("refused_in_mode", "at", "%s cannot be tracked here: the story holds a %s, and only paragraphs are tracked in a header or footer; use a direct plan", c.word+".delete", ch.Local())
			}
		}
		h := env.Story(part, root)
		if err := env.trackStory(h, root.Elements(), nil, nil, nil); err != nil {
			return err
		}
		return nil
	}
	h, err := env.sectionHandle(sect)
	if err != nil {
		return err
	}
	if err := h.RemoveNode(ref); err != nil {
		return err
	}
	return nil
}
