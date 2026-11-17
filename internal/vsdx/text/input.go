package text

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/htmlx"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ParseHTML reads the HTML of a text.set or object.add (the grammar
// Render writes, through htmlx.ParseHTML in strict mode and
// htmlx.StyleDecls) into a body and the formats its runs and paragraphs
// set, one Character row per distinct CharFormat and one Paragraph row
// per distinct ParaFormat, numbered from 0 in first use; Terminated is
// true (Visio ends text with a line feed). A field placeholder is kept
// by its data-ref. A span's data-color accepts only #RRGGBB (any case,
// stored upper-case) and, with color present, must name the same colour
// as it; alone it sets the colour. Anything else outside the grammar is
// an error naming the element or attribute. Render(ParseHTML(h)) == h
// for every h Render writes. Chunk K5, amended by TXF.
func ParseHTML(h string) (Body, Formats, error) {
	root, err := htmlx.ParseHTML(h, htmlx.Options{
		Strict:      true,
		SelfClosing: map[string]bool{"opaque": true},
	})
	if err != nil {
		return Body{}, Formats{}, fmt.Errorf("text: %w", err)
	}
	kids := root.Kids
	block := false
	for _, k := range kids {
		if k.Tag == "p" || k.Tag == "ul" {
			block = true
			break
		}
	}
	if !block {
		if len(kids) == 0 {
			return Body{Terminated: true}, Formats{}, nil
		}
		runs, err := walkInline(kids, CharFormat{})
		if err != nil {
			return Body{}, Formats{}, err
		}
		b, f := buildBody([]paraTmp{{runs: runs}})
		return b, f, nil
	}
	var paras []paraTmp
	for _, k := range kids {
		switch k.Tag {
		case "":
			if strings.TrimSpace(k.Text) != "" {
				return Body{}, Formats{}, fmt.Errorf("text: text beside block elements; put it in a <p>")
			}
		case "p":
			p, err := parsePara(k, false)
			if err != nil {
				return Body{}, Formats{}, err
			}
			paras = append(paras, p)
		case "ul":
			if err := checkAttrs(k, nil); err != nil {
				return Body{}, Formats{}, err
			}
			n := 0
			for _, li := range k.Kids {
				if li.Tag == "" {
					if strings.TrimSpace(li.Text) != "" {
						return Body{}, Formats{}, fmt.Errorf("text: text directly in <ul>; put it in an <li>")
					}
					continue
				}
				if li.Tag != "li" {
					return Body{}, Formats{}, fmt.Errorf("text: <%s> in <ul>: only <li> is allowed", li.Tag)
				}
				p, err := parsePara(li, true)
				if err != nil {
					return Body{}, Formats{}, err
				}
				paras = append(paras, p)
				n++
			}
			if n == 0 {
				return Body{}, Formats{}, fmt.Errorf("text: empty <ul>")
			}
		default:
			return Body{}, Formats{}, fmt.Errorf("text: element <%s> is not in the text grammar", k.Tag)
		}
	}
	b, f := buildBody(paras)
	return b, f, nil
}

// paraTmp is one parsed paragraph before its rows are assigned: its
// paragraph format and its runs with the character format in force.
type paraTmp struct {
	fmt  ParaFormat
	runs []runTmp
}

// runTmp is one parsed run before its Character row is assigned.
type runTmp struct {
	fmt CharFormat
	run Run
}

// parsePara reads a <p> (list false) or an <li> in a <ul> (list true).
func parsePara(n *htmlx.Node, list bool) (paraTmp, error) {
	var out paraTmp
	allowed := map[string]bool{"style": true}
	if list {
		allowed["data-char"] = true
	}
	if err := checkAttrs(n, allowed); err != nil {
		return out, err
	}
	if v, ok := n.Attr("style"); ok {
		decls, err := htmlx.StyleDecls(v)
		if err != nil {
			return out, fmt.Errorf("text: <%s> style: %w", n.Tag, err)
		}
		for _, d := range decls {
			if err := paraDecl(&out.fmt, d); err != nil {
				return out, err
			}
		}
	}
	if list {
		out.fmt.Bullet = true
		if v, ok := n.Attr("data-char"); ok {
			if v == "" {
				return out, fmt.Errorf("text: empty data-char on <li>")
			}
			out.fmt.BulletChar = v
		}
	}
	runs, err := walkInline(n.Kids, CharFormat{})
	if err != nil {
		return out, err
	}
	out.runs = runs
	return out, nil
}

// paraDecl applies one paragraph style declaration to f.
func paraDecl(f *ParaFormat, d htmlx.StyleDecl) error {
	switch d.Name {
	case "text-align":
		switch d.Value {
		case "left", "center", "right", "justify":
			f.Align = d.Value
		default:
			return fmt.Errorf("text: text-align:%s: want left, center, right or justify", d.Value)
		}
	case "line-height":
		_, pt := htmlx.Measure(d.Value, "pt", false)
		_, pct := htmlx.Measure(d.Value, "%", false)
		if !pt && !pct {
			return fmt.Errorf("text: line-height:%s: want points (14pt) or a percentage (120%%)", d.Value)
		}
		f.LineHeight = d.Value
	case "margin-top":
		if _, ok := htmlx.Measure(d.Value, "pt", false); !ok {
			return fmt.Errorf("text: margin-top:%s: want points, such as 6pt", d.Value)
		}
		f.SpaceBefore = d.Value
	case "margin-bottom":
		if _, ok := htmlx.Measure(d.Value, "pt", false); !ok {
			return fmt.Errorf("text: margin-bottom:%s: want points, such as 3pt", d.Value)
		}
		f.SpaceAfter = d.Value
	case "margin-left":
		if _, ok := htmlx.Measure(d.Value, "pt", false); !ok {
			return fmt.Errorf("text: margin-left:%s: want points, such as 18pt", d.Value)
		}
		f.MarginLeft = d.Value
	case "margin-right":
		if _, ok := htmlx.Measure(d.Value, "pt", false); !ok {
			return fmt.Errorf("text: margin-right:%s: want points, such as 9pt", d.Value)
		}
		f.MarginRight = d.Value
	case "text-indent":
		if _, ok := htmlx.Measure(d.Value, "pt", true); !ok {
			return fmt.Errorf("text: text-indent:%s: want points, such as -18pt", d.Value)
		}
		f.Indent = d.Value
	default:
		return fmt.Errorf("text: paragraph style property %s is not in the text grammar", d.Name)
	}
	return nil
}

// walkInline reads inline content under the character format ctx.
func walkInline(kids []*htmlx.Node, ctx CharFormat) ([]runTmp, error) {
	var out []runTmp
	for _, k := range kids {
		if k.Tag == "" {
			if err := checkStorable(k.Text); err != nil {
				return nil, err
			}
			out = appendText(out, ctx, k.Text)
			continue
		}
		switch k.Tag {
		case "br":
			if err := checkAttrs(k, nil); err != nil {
				return nil, err
			}
			out = append(out, runTmp{fmt: ctx, run: Run{Br: true, Field: -1}})
		case "opaque":
			run, err := fieldRun(k)
			if err != nil {
				return nil, err
			}
			out = append(out, runTmp{fmt: ctx, run: run})
		case "b", "i", "u", "s", "span":
			next, err := inlineFormat(k, ctx)
			if err != nil {
				return nil, err
			}
			if len(k.Kids) == 0 {
				if next != (CharFormat{}) {
					out = append(out, runTmp{fmt: next, run: Run{Field: -1}})
				}
				continue
			}
			sub, err := walkInline(k.Kids, next)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		default:
			return nil, fmt.Errorf("text: element <%s> is not in the text grammar", k.Tag)
		}
	}
	return out, nil
}

// inlineFormat returns ctx with the formatting of element n added.
func inlineFormat(n *htmlx.Node, ctx CharFormat) (CharFormat, error) {
	f := ctx
	switch n.Tag {
	case "b", "i", "u", "s":
		if err := checkAttrs(n, nil); err != nil {
			return f, err
		}
		switch n.Tag {
		case "b":
			if f.Bold == "off" {
				return f, fmt.Errorf("text: <b> inside a span that sets it normal")
			}
			f.Bold = "on"
		case "i":
			if f.Italic == "off" {
				return f, fmt.Errorf("text: <i> inside a span that sets it normal")
			}
			f.Italic = "on"
		case "u":
			if f.Underline == "off" {
				return f, fmt.Errorf("text: <u> inside a span that sets it none")
			}
			f.Underline = "on"
		case "s":
			if f.Strike == "off" {
				return f, fmt.Errorf("text: <s> inside a span that sets it none")
			}
			f.Strike = "on"
		}
		return f, nil
	case "span":
		if err := checkAttrs(n, map[string]bool{"style": true, "data-color": true}); err != nil {
			return f, err
		}
		if v, ok := n.Attr("style"); ok {
			decls, err := htmlx.StyleDecls(v)
			if err != nil {
				return f, fmt.Errorf("text: <span> style: %w", err)
			}
			for _, d := range decls {
				if err := charDecl(&f, d); err != nil {
					return f, err
				}
			}
		}
		if v, ok := n.Attr("data-color"); ok {
			hex, valid := vsd.Color(v)
			if !valid {
				return f, fmt.Errorf("text: data-color:%s: want #RRGGBB; Visio text has no theme colour references", v)
			}
			if f.Color != "" && f.Color != hex {
				return f, fmt.Errorf("text: data-color:%s and the colour in force %s disagree; the two must name the same colour", v, f.Color)
			}
			f.Color = hex
		}
		return f, nil
	}
	return f, fmt.Errorf("text: element <%s> is not in the text grammar", n.Tag)
}

// charDecl applies one span style declaration to f.
func charDecl(f *CharFormat, d htmlx.StyleDecl) error {
	switch d.Name {
	case "font-weight":
		if d.Value != "normal" {
			return fmt.Errorf("text: font-weight:%s: only normal (use <b> for the other)", d.Value)
		}
		if f.Bold == "on" {
			return fmt.Errorf("text: font-weight:normal inside <b>")
		}
		f.Bold = "off"
	case "font-style":
		if d.Value != "normal" {
			return fmt.Errorf("text: font-style:%s: only normal (use <i> for the other)", d.Value)
		}
		if f.Italic == "on" {
			return fmt.Errorf("text: font-style:normal inside <i>")
		}
		f.Italic = "off"
	case "text-decoration":
		if d.Value != "none" {
			return fmt.Errorf("text: text-decoration:%s: only none (use <u> for underlines)", d.Value)
		}
		if f.Underline == "on" || f.Strike == "on" {
			return fmt.Errorf("text: text-decoration:none inside <u> or <s>")
		}
		f.Underline, f.Strike = "off", "off"
	case "font-size":
		if _, ok := htmlx.Measure(d.Value, "pt", false); !ok {
			return fmt.Errorf("text: font-size:%s: want points, such as 12pt", d.Value)
		}
		size := strings.TrimSuffix(d.Value, "pt")
		if f.Size != "" && f.Size != size {
			return fmt.Errorf("text: font-size:%s contradicts an enclosing %spt", d.Value, f.Size)
		}
		f.Size = size
	case "color":
		hex, ok := vsd.Color(d.Value)
		if !ok {
			return fmt.Errorf("text: color:%s: want #RRGGBB", d.Value)
		}
		if f.Color != "" && f.Color != hex {
			return fmt.Errorf("text: color:%s contradicts an enclosing %s", d.Value, f.Color)
		}
		f.Color = hex
	case "font-family":
		font, err := htmlx.QuotedName(d.Value)
		if err != nil {
			return fmt.Errorf("text: font-family:%s: %w", d.Value, err)
		}
		if f.Font != "" && f.Font != font {
			return fmt.Errorf("text: font-family contradicts an enclosing one")
		}
		f.Font = font
	default:
		return fmt.Errorf("text: <span> style property %s is not in the text grammar", d.Name)
	}
	return nil
}

// fieldRun reads an <opaque> field placeholder into a field run with
// Ref set to its data-ref as written and Field -1: the writing op
// checks the ref's shape part and maps its number to the N-th fld
// (the K5F ruling: the N-1 remap is wrong).
func fieldRun(n *htmlx.Node) (Run, error) {
	if err := checkAttrs(n, map[string]bool{"data-ref": true, "data-kind": true, "data-text": true}); err != nil {
		return Run{}, err
	}
	ref, ok := n.Attr("data-ref")
	if !ok {
		return Run{}, fmt.Errorf("text: <opaque> needs data-ref")
	}
	addr, err := vsd.Parse(ref)
	if err != nil || addr.Kind != vsd.KindField {
		return Run{}, fmt.Errorf("text: data-ref=%q on <opaque>: want a field address", ref)
	}
	if kind, ok := n.Attr("data-kind"); ok && kind != "field" {
		return Run{}, fmt.Errorf("text: data-kind=%q on <opaque>: want field", kind)
	}
	if len(n.Kids) != 0 {
		return Run{}, fmt.Errorf("text: <opaque> takes no content")
	}
	cached, _ := n.Attr("data-text")
	if err := checkStorable(cached); err != nil {
		return Run{}, err
	}
	return Run{Text: cached, Field: -1, Ref: ref}, nil
}

// appendText appends s as a text run under f, merging it into a
// preceding text run of the same format.
func appendText(out []runTmp, f CharFormat, s string) []runTmp {
	if s == "" {
		return out
	}
	if len(out) > 0 {
		last := &out[len(out)-1]
		if !last.run.Br && last.run.Field < 0 && last.run.Ref == "" && last.fmt == f {
			last.run.Text += s
			return out
		}
	}
	return append(out, runTmp{fmt: f, run: Run{Text: s, Field: -1}})
}

// checkAttrs refuses every attribute of n outside allowed, naming it.
func checkAttrs(n *htmlx.Node, allowed map[string]bool) error {
	for _, a := range n.Attrs {
		if !allowed[a.Name] {
			return fmt.Errorf("text: attribute %s on <%s> is not in the text grammar", a.Name, n.Tag)
		}
		if err := checkStorable(a.Value); err != nil {
			return err
		}
	}
	return nil
}

// checkStorable refuses a character XML cannot store.
func checkStorable(s string) error {
	for _, c := range s {
		if !htmlx.Storable(c) {
			return fmt.Errorf("text: character %U cannot be stored in XML", c)
		}
	}
	return nil
}

// buildBody assigns one Character row per distinct character format
// and one Paragraph row per distinct paragraph format, numbered from
// 0 in first use: when unformatted and formatted formats both occur,
// the empty format is row 0 and the formatted rows follow it.
func buildBody(paras []paraTmp) (Body, Formats) {
	if len(paras) == 0 {
		return Body{Terminated: true}, Formats{}
	}
	charEmpty, charFormatted, paraEmpty, paraFormatted := false, false, false, false
	for _, p := range paras {
		if p.fmt == (ParaFormat{}) {
			paraEmpty = true
		} else {
			paraFormatted = true
		}
		for _, r := range p.runs {
			if r.fmt == (CharFormat{}) {
				charEmpty = true
			} else {
				charFormatted = true
			}
		}
	}
	charIndex := map[CharFormat]int{}
	var charRows []CharFormat
	nextChar := 0
	if charEmpty && charFormatted {
		charIndex[CharFormat{}] = 0
		charRows = append(charRows, CharFormat{})
		nextChar = 1
	}
	charRow := func(f CharFormat) int {
		if idx, ok := charIndex[f]; ok {
			return idx
		}
		idx := nextChar
		nextChar++
		charIndex[f] = idx
		charRows = append(charRows, f)
		return idx
	}
	paraIndex := map[ParaFormat]int{}
	var paraRows []ParaFormat
	nextPara := 0
	if paraEmpty && paraFormatted {
		paraIndex[ParaFormat{}] = 0
		paraRows = append(paraRows, ParaFormat{})
		nextPara = 1
	}
	paraRow := func(f ParaFormat) int {
		if idx, ok := paraIndex[f]; ok {
			return idx
		}
		idx := nextPara
		nextPara++
		paraIndex[f] = idx
		paraRows = append(paraRows, f)
		return idx
	}
	body := Body{Terminated: true, Paras: make([]Para, len(paras))}
	for i, p := range paras {
		para := Para{PP: paraRow(p.fmt)}
		for _, r := range p.runs {
			para.Runs = append(para.Runs, Run{
				CP:    charRow(r.fmt),
				Text:  r.run.Text,
				Br:    r.run.Br,
				Field: r.run.Field,
				Ref:   r.run.Ref,
			})
		}
		body.Paras[i] = para
	}
	var formats Formats
	if len(charRows) > 0 {
		formats.Char = make(map[int]CharFormat, len(charRows))
		for i, f := range charRows {
			formats.Char[i] = f
		}
	}
	if len(paraRows) > 0 {
		formats.Para = make(map[int]ParaFormat, len(paraRows))
		for i, f := range paraRows {
			formats.Para[i] = f
		}
	}
	return body, formats
}
