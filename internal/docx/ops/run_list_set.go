package ops

import (
	"errors"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/docx/build"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// SetList is list.set (docxdriver's set_list).
type SetList struct {
	Levels *[]ListLevel       `json:"levels" default:"null"`
	At     []ParagraphAddress `json:"at"`
	NumID  *uint32            `json:"num_id" default:"null"`
	Format *string            `json:"format" default:"null" enum:"bullet,decimal,lowerLetter,upperLetter,lowerRoman,upperRoman" schema:"w:ST_NumberFormat"`
	Level  uint8              `json:"level" default:"0"`
	Start  *uint32            `json:"start" default:"null"`
	Remove bool               `json:"remove" default:"false"`
}

// opListSet is list.set's entry in the operation table.
var opListSet = OpDef{
	Name: "list.set", Oracle: "set_list", New: func() Spec { return &SetList{} },
	Moved: "direct equals accepted track up to the relabelling of new list ids",
	Rules: setListRules,
}

// Run sets the list membership of paragraphs of the main document
// (lists.rs set_list): into an existing list instance (num_id, at a
// level it defines), into a new definition (format or levels), or out of
// any list (remove: w:numId 0, which ends inherited membership too).
// Definitions are only appended, never changed: a start, or a tracked
// change joining an instance nothing refers to yet, gets a clone of the
// instance (with a w:startOverride for a start), so rejecting takes back
// only what the change added. Each paragraph's change is a tracked
// paragraph property change; direct mode accepts it.
func (o *SetList) Run(env *Env) error {
	part, x, err := env.Main()
	if err != nil {
		return err
	}
	var targets []*tree.Node
	var ids []string
	seen := map[*tree.Node]bool{}
	for i, at := range o.At {
		p, id, err := env.paragraph(at)
		if err != nil {
			var r *Rejection
			if errors.As(err, &r) {
				r.Field = "at[" + strconv.Itoa(i) + "]"
			}
			return err
		}
		if seen[p] {
			return reject("invalid_operation", "at["+strconv.Itoa(i)+"]", "list.set names paragraph %s twice", id)
		}
		seen[p] = true
		if ppr := model.ChildW(p, "pPr"); ppr != nil && model.ChildW(ppr, "pPrChange") != nil {
			return reject("operation_failed", "at["+strconv.Itoa(i)+"]", "paragraph %s has a pending paragraph property revision; settle it first", id)
		}
		targets = append(targets, p)
		ids = append(ids, id)
	}
	assigned := "0"
	if !o.Remove {
		if _, ok, err := env.WS.NumberingPart(); err == nil && !ok && o.NumID != nil {
			// An instance is looked up, never made: no part to add.
			return reject("not_found", "num_id", "list.set num_id %d does not exist (the document has no numbering part)", *o.NumID)
		}
		numbering, nh, root, err := env.numberingTree()
		if err != nil {
			return err
		}
		assigned, err = env.listInstance(o, x, part, numbering, nh, root)
		if err != nil {
			return err
		}
	}
	for i, p := range targets {
		h, err := env.WS.Core().Range(part, "list.set at "+ids[i], p, p)
		if err != nil {
			return err
		}
		ppr, err := h.EnsureIn(p, docxedit.W("pPr"))
		if err != nil {
			return err
		}
		if _, err := env.Rev.Property(h, ppr, func() error { return membership(h, ppr, assigned, o.Level, o.Remove) }); err != nil {
			return err
		}
	}
	return nil
}

// membership replaces only the list membership in w:pPr ppr: w:ilvl and
// w:numId (no w:ilvl when the paragraph leaves its list); its other
// properties stay.
func membership(h *core.Handle, ppr *tree.Node, numID string, level uint8, remove bool) error {
	np, err := h.EnsureIn(ppr, docxedit.W("numPr"))
	if err != nil {
		return err
	}
	if remove {
		if l := model.ChildW(np, "ilvl"); l != nil {
			if err := h.RemoveNode(l); err != nil {
				return err
			}
		}
	} else if err := setVal(h, np, "ilvl", strconv.Itoa(int(level))); err != nil {
		return err
	}
	return setVal(h, np, "numId", numID)
}

// setVal gives element e's child w:local the w:val value.
func setVal(h *core.Handle, e *tree.Node, local, value string) error {
	c, err := h.EnsureIn(e, docxedit.W(local))
	if err != nil {
		return err
	}
	return h.SetNodeAttr(c, qname(c, "val"), value)
}

// numberingTree is the numbering part, added empty when there is none; a
// part that cannot be read is the op's unreadable target (W10).
func (env *Env) numberingTree() (string, *core.Handle, *tree.Node, error) {
	part, h, root, err := build.Numbering(env.WS)
	if err != nil {
		var r *Rejection
		if errors.As(err, &r) {
			return "", nil, nil, err
		}
		return "", nil, nil, reject("unreadable_target", "", "the numbering part cannot be read: %s", err.Error())
	}
	if root == nil {
		return "", nil, nil, reject("unreadable_target", "", "the numbering part has no root element")
	}
	return part, h, root, nil
}

// listInstance returns the w:numId the paragraphs join, writing what it
// needs into the numbering part.
func (env *Env) listInstance(o *SetList, x *model.XML, mainPart, numbering string, h *core.Handle, root *tree.Node) (string, error) {
	nums := map[string]*tree.Node{}
	abstracts := map[string]*tree.Node{}
	for _, c := range root.Elements() {
		switch {
		case model.IsW(c, "num"):
			nums[model.AttrOr(c, "numId")] = c
		case model.IsW(c, "abstractNum"):
			abstracts[model.AttrOr(c, "abstractNumId")] = c
		}
	}
	level := strconv.Itoa(int(o.Level))
	if o.NumID == nil {
		num, err := env.WS.NumID(numbering)
		if err != nil {
			return "", err
		}
		abs, err := env.WS.AbstractNumID(numbering)
		if err != nil {
			return "", err
		}
		return num, build.NewList(h, root, num, abs, o.newLevels())
	}
	id := strconv.FormatUint(uint64(*o.NumID), 10)
	existing, ok := nums[id]
	if !ok {
		return "", reject("not_found", "num_id", "list.set num_id %s does not exist", id)
	}
	defined := false
	if a := model.ChildW(existing, "abstractNumId"); a != nil {
		if def, ok := abstracts[model.AttrOr(a, "val")]; ok {
			for _, l := range def.Elements() {
				if model.IsW(l, "lvl") && model.AttrOr(l, "ilvl") == level {
					defined = true
				}
			}
		}
	}
	for _, ov := range existing.Elements() {
		if model.IsW(ov, "lvlOverride") && model.AttrOr(ov, "ilvl") == level && model.ChildW(ov, "lvl") != nil {
			defined = true
		}
	}
	if !defined {
		return "", reject("operation_failed", "level", "level %s is not defined by list %s", level, id)
	}
	ownInstance := false
	if env.Mode == ModeTrack && o.Start == nil {
		// A tracked change joining an instance nothing refers to yet gets
		// its own clone: rejecting takes back only what the change added.
		refs := revise.ReferencedNumIDs(append([]*tree.Node{x.Root()}, env.wordParts(mainPart, numbering)...))
		ownInstance = !refs[id]
	}
	if o.Start == nil && !ownInstance {
		return id, nil
	}
	// Clone the instance, never change a shared definition.
	num, err := env.WS.NumID(numbering)
	if err != nil {
		return "", err
	}
	clone, err := h.Workspace().Copy(existing)
	if err != nil {
		return "", err
	}
	var cleanup *tree.Node
	for _, c := range root.Elements() {
		if model.IsW(c, "numIdMacAtCleanup") {
			cleanup = c
		}
	}
	if err := h.PlaceAt(root, clone, cleanup); err != nil {
		return "", err
	}
	if err := h.SetNodeAttr(clone, qname(clone, "numId"), num); err != nil {
		return "", err
	}
	if o.Start == nil {
		return num, nil
	}
	var over *tree.Node
	for _, c := range clone.Elements() {
		if model.IsW(c, "lvlOverride") && model.AttrOr(c, "ilvl") == level {
			over = c
		}
	}
	if over == nil {
		nodes, err := h.Workspace().Parse(clone, []byte(`<w:lvlOverride w:ilvl="`+level+`"/>`), map[string]string{"w": model.NSW})
		if err != nil || len(nodes) != 1 {
			return "", errors.Join(errors.New("ops: a level override did not parse"), err)
		}
		if err := h.PlaceAt(clone, nodes[0], nil); err != nil {
			return "", err
		}
		over = nodes[0]
	}
	return num, setVal(h, over, "startOverride", strconv.FormatUint(uint64(*o.Start), 10))
}

// newLevels are the levels of a new definition (lists.rs): levels as
// given, or nine of format (decimal by default) labelled "•" or "%N.",
// the selected level starting at start.
func (o *SetList) newLevels() []build.ListLevel {
	if o.Levels != nil {
		out := make([]build.ListLevel, len(*o.Levels))
		for i, l := range *o.Levels {
			b := build.ListLevel{Format: l.Format, Label: l.Label, RestartAfter: -1}
			if l.Start != nil {
				b.Start = *l.Start
			} else if i == int(o.Level) && o.Start != nil {
				b.Start = *o.Start
			}
			if l.RestartAfter != nil {
				b.RestartAfter = int(*l.RestartAfter)
			}
			for _, v := range []struct {
				src *SignedPoints
				dst **int64
			}{{l.IndentLeft, &b.IndentLeft}, {l.IndentRight, &b.IndentRight}, {l.FirstLineIndent, &b.FirstLine}} {
				if v.src != nil {
					n := int64(*v.src)
					*v.dst = &n
				}
			}
			b.MarkerBold, b.MarkerItalic = l.MarkerBold, l.MarkerItalic
			if l.MarkerFont != nil {
				b.MarkerFont = *l.MarkerFont
			}
			if l.MarkerColor != nil {
				b.MarkerColor = *l.MarkerColor
			}
			out[i] = b
		}
		return out
	}
	format := "decimal"
	if o.Format != nil {
		format = *o.Format
	}
	out := make([]build.ListLevel, 9)
	for i := range out {
		label := "%" + strconv.Itoa(i+1) + "."
		if format == "bullet" {
			label = "•"
		}
		out[i] = build.ListLevel{Format: format, Label: label, RestartAfter: -1}
		if i == int(o.Level) && o.Start != nil {
			out[i].Start = *o.Start
		}
	}
	return out
}
