package ops

import (
	"math"
	"slices"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/span"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// FormatText is text.format (docxdriver's format_text).
type FormatText struct {
	At          ParagraphAddress `json:"at"`
	Select      string           `json:"select"`
	Occurrence  *uint32          `json:"occurrence" default:"null"`
	Bold        *bool            `json:"bold" default:"null"`
	Italic      *bool            `json:"italic" default:"null"`
	Underline   *bool            `json:"underline" default:"null"`
	Strike      *bool            `json:"strike" default:"null"`
	Superscript *bool            `json:"superscript" default:"null"`
	Subscript   *bool            `json:"subscript" default:"null"`
	Color       *string          `json:"color" default:"null"`
	Highlight   *HighlightColor  `json:"highlight" default:"null"`
	FontSize    *Points          `json:"font_size" default:"null"`
	Clear       []string         `json:"clear" default:"[]" enum:"bold,italic,underline,strike,superscript,subscript,highlight,color,font_size"`
}

// opTextFormat is text.format's entry in the operation table.
var opTextFormat = OpDef{
	Name: "text.format", Oracle: "format_text", New: func() Spec { return &FormatText{} },
	Rules: formatTextRules,
}

// runProp is one requested direct run property: its w:rPr child and its
// w:val, or clear (removed, so styles supply it again).
type runProp struct {
	name, val string
	clear     bool
}

// requested is the properties the operation asks for (typed.rs
// char_format_properties): false writes an explicit off value, and clear
// wins over a value set in the same operation; superscript and subscript
// share w:vertAlign, the later winning.
func (o *FormatText) requested() []runProp {
	var out []runProp
	put := func(p runProp) {
		for i := range out {
			if out[i].name == p.name {
				out[i] = p
				return
			}
		}
		out = append(out, p)
	}
	for _, b := range []struct {
		name    string
		v       *bool
		on, off string
	}{
		{"b", o.Bold, "1", "0"}, {"i", o.Italic, "1", "0"}, {"u", o.Underline, "single", "none"},
		{"strike", o.Strike, "1", "0"}, {"vertAlign", o.Superscript, "superscript", "baseline"},
		{"vertAlign", o.Subscript, "subscript", "baseline"},
	} {
		if b.v != nil {
			v := b.off
			if *b.v {
				v = b.on
			}
			put(runProp{name: b.name, val: v})
		}
	}
	if o.Highlight != nil {
		put(runProp{name: "highlight", val: string(*o.Highlight)})
	}
	if o.Color != nil {
		put(runProp{name: "color", val: strings.ToUpper(strings.TrimPrefix(*o.Color, "#"))})
	}
	if o.FontSize != nil {
		put(runProp{name: "sz", val: strconv.FormatFloat(math.Max(math.Round(o.FontSize.Float()*2), 1), 'f', -1, 64)})
	}
	tags := map[string]string{"bold": "b", "italic": "i", "underline": "u", "strike": "strike", "superscript": "vertAlign",
		"subscript": "vertAlign", "highlight": "highlight", "color": "color", "font_size": "sz"}
	for _, c := range o.Clear {
		if tag, ok := tags[c]; ok {
			put(runProp{name: tag, clear: true})
		}
	}
	return out
}

// changes returns the requested properties run's direct properties do
// not already hold (typed.rs changed_run_properties): a property is held
// when it is absent and cleared, or present once with the requested
// value and no other attribute or content (a themed colour is rewritten).
func changes(run *tree.Node, want []runProp) []runProp {
	rpr := model.ChildW(run, "rPr")
	var out []runProp
	for _, p := range want {
		var nodes []*tree.Node
		if rpr != nil {
			for _, c := range rpr.Elements() {
				if model.IsW(c, p.name) {
					nodes = append(nodes, c)
				}
			}
		}
		held := false
		switch {
		case len(nodes) == 0:
			held = p.clear
		case len(nodes) == 1 && !p.clear:
			n := nodes[0]
			v, has := model.Attr(n, "val")
			same := has && v == p.val
			if p.name == "b" || p.name == "i" || p.name == "strike" {
				on := !has || v == "1" || v == "true" || v == "on"
				off := has && (v == "0" || v == "false" || v == "off")
				same = (p.val == "1" && on) || (p.val == "0" && off)
			}
			attrs := 0
			for _, a := range n.Attrs() {
				if !strings.HasPrefix(a.Name, "xmlns") {
					attrs++
				}
			}
			held = same && (attrs == 0 || (attrs == 1 && has)) && len(n.Children()) == 0
		}
		if !held {
			out = append(out, p)
		}
	}
	return out
}

// Run applies character formatting to the selected text (typed.rs
// format_text_typed): the span is isolated, and every run in it whose
// direct properties change gets them, recorded as a w:rPrChange with the
// properties before (Reviser.Property; direct mode accepts it). A run
// whose properties already record a pending change keeps that record,
// whose snapshot holds the properties before any pending change (Word's
// rule; docxdriver refuses the operation).
func (o *FormatText) Run(env *Env) error {
	t, err := env.textTargetOf(o.At)
	if err != nil {
		return err
	}
	cs, ce, _, err := selectOne(t.p, t.id, o.Select, o.Occurrence)
	if err != nil {
		return err
	}
	s, err := span.Read(t.p).Select(model.Current, cs, ce)
	if err != nil {
		return err
	}
	want := o.requested()
	// The runs whose properties change, found before anything is split.
	var touched bool
	for _, g := range span.Read(t.p).Segs {
		if g.Start < s.End && g.End > s.Start && g.Run != nil && len(changes(g.Run, want)) > 0 {
			touched = true
			break
		}
	}
	if !touched {
		return nil
	}
	if _, err := t.isolate(s, "select"); err != nil {
		return err
	}
	var runs []*tree.Node
	for _, g := range span.Read(t.p).Segs {
		if g.Start < s.End && g.End > s.Start && g.Run != nil && !slices.Contains(runs, g.Run) {
			runs = append(runs, g.Run)
		}
	}
	for _, run := range runs {
		ch := changes(run, want)
		if len(ch) == 0 {
			continue
		}
		rpr := model.ChildW(run, "rPr")
		if rpr == nil {
			if rpr, err = t.h.EnsureIn(run, docxedit.W("rPr")); err != nil {
				return err
			}
		}
		if _, err := env.Rev.Property(t.h, rpr, func() error { return writeRunProps(t, rpr, ch) }); err != nil {
			return err
		}
	}
	return nil
}

// writeRunProps sets each property in rpr (typed.rs write_run_properties):
// existing elements of its name go, and a set value is placed where the
// schema puts it.
func writeRunProps(t *textTarget, rpr *tree.Node, ch []runProp) error {
	for _, p := range ch {
		for _, c := range slices.Clone(rpr.Elements()) {
			if model.IsW(c, p.name) {
				if err := t.h.RemoveNode(c); err != nil {
					return err
				}
			}
		}
		if p.clear {
			continue
		}
		n, err := t.h.EnsureIn(rpr, docxedit.W(p.name))
		if err != nil {
			return err
		}
		if err := t.h.SetNodeAttr(n, qualified(rpr, "val"), p.val); err != nil {
			return err
		}
	}
	return nil
}

// qualified is local in the WordprocessingML namespace as n's scope
// spells it.
func qualified(n *tree.Node, local string) string {
	if p, _, ok := strings.Cut(n.Name(), ":"); ok {
		return p + ":" + local
	}
	return local
}
