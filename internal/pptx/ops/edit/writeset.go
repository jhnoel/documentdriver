package edit

// DeclarePart adds a part the plan may change as a whole (relationships,
// content types, media). Slide parts are declared through Target and
// Container instead, which also say which objects may change.
func (w *Workspace) DeclarePart(name string) { w.core.DeclarePart(name) }
