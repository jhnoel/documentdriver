package render

import (
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/geom"
)

// TextBox is text to lay out in a block: Frame maps the block's space
// (points, y down, origin at its top-left corner, W by H) to the page;
// Margin is left, top, right and bottom; VAlign "top", "middle" or
// "bottom"; Background "#RRGGBB" ("" for none), filled behind the
// text at BgOpacity (0 is read as 1).
type TextBox struct {
	Frame      geom.Affine
	W, H       float64
	Margin     [4]float64
	VAlign     string
	Paras      []TextPara
	Background string
	BgOpacity  float64
}

// TextPara is one paragraph: Align "left", "center", "right" or
// "justify" (laid out as left), Bullet a bullet text ("" for none),
// LineSpacing the line height as a multiple of the largest font size
// on the line (0 for the default, 1.2) and its runs.
type TextPara struct {
	Align       string
	Bullet      string
	LineSpacing float64
	Runs        []TextRun
}

// TextRun is a stretch of text in one style, or a line break (Br):
// Font is the face name ("" for the default face), Size in points (0
// for DefaultTextSize), Color "#RRGGBB" ("" for black).
type TextRun struct {
	Text                            string
	Br                              bool
	Font                            string
	Size                            float64
	Color                           string
	Bold, Italic, Underline, Strike bool
}

// DefaultTextSize is the size of a run whose size is not derived:
// 12 pt, the size of Visio's default text style.
const DefaultTextSize = 12

// defaultLineSpacing is the line height as a multiple of the font
// size when a paragraph sets none: Visio's SpLine of -120%.
const defaultLineSpacing = 1.2

// MaxLines bounds the lines one text box lays out.
const MaxLines = 4096

// piece is a run of text on a line, measured.
type piece struct {
	run   *TextRun
	text  string
	width float64
}

// textLine is one laid-out line.
type textLine struct {
	pieces []piece
	width  float64
	size   float64
	para   *TextPara
}

// runSize returns a run's size, DefaultTextSize when not set.
func runSize(r *TextRun) float64 {
	if r.Size > 0 {
		return r.Size
	}
	return DefaultTextSize
}

// LayoutText lays b out: each paragraph is wrapped greedily at spaces
// (a word wider than the line is cut between characters) to the
// block's width less its margins, using the width tables (Carlito,
// Liberation Sans or Liberation Serif metrics: an estimate of the
// viewer's real font, so a line may end a word early or late), then
// aligned in the block. It returns the text items and the strokes of
// underlines and strikes, in page space, and notes on what it cut.
func LayoutText(b TextBox) (items []Item, notes []string) {
	avail := b.W - b.Margin[0] - b.Margin[2]
	var lines []textLine
	cut := false
	for pi := range b.Paras {
		p := &b.Paras[pi]
		ls := wrapPara(p, avail)
		if len(lines)+len(ls) > MaxLines {
			ls = ls[:max(MaxLines-len(lines), 0)]
			cut = true
		}
		lines = append(lines, ls...)
		if cut {
			break
		}
	}
	if cut {
		notes = append(notes, "text cut at 4096 lines")
	}
	if len(lines) == 0 {
		return nil, notes
	}
	total := 0.0
	for _, l := range lines {
		total += lineHeight(l)
	}
	top := b.Margin[1]
	inner := b.H - b.Margin[1] - b.Margin[3]
	switch b.VAlign {
	case "middle", "":
		top += (inner - total) / 2
	case "bottom":
		top += inner - total
	}
	if b.Background != "" {
		items = append(items, textBackground(b, lines, avail, top, total))
	}
	y := top
	for _, l := range lines {
		h := lineHeight(l)
		base := y + (h-l.size)/2 + 0.8*l.size
		y += h
		if len(l.pieces) == 0 {
			continue
		}
		x, anchor, start := lineStart(b, l, avail)
		tl := &TextLine{Frame: b.Frame, X: x, Y: base, Anchor: anchor}
		px := start
		for _, pc := range l.pieces {
			r := pc.run
			size := runSize(r)
			color := r.Color
			if color == "" {
				color = "#000000"
			}
			tl.Spans = append(tl.Spans, Span{Text: pc.text, Family: Family(r.Font), Size: size, Color: color, Bold: r.Bold, Italic: r.Italic})
			for _, deco := range [2]struct {
				on bool
				dy float64
			}{{r.Underline, 0.12 * size}, {r.Strike, -0.28 * size}} {
				if !deco.on || pc.width <= 0 {
					continue
				}
				a := b.Frame.Apply(geom.Pt{X: px, Y: base + deco.dy})
				e := b.Frame.Apply(geom.Pt{X: px + pc.width, Y: base + deco.dy})
				items = append(items, Item{Kind: StrokeItem, Path: Path{}.MoveTo(a).LineTo(e), Color: color, Width: size / 16})
			}
			px += pc.width
		}
		items = append(items, Item{Kind: TextItem, Text: tl})
	}
	return items, notes
}

// lineStart returns where line l of b is anchored: the x of its
// anchor, the anchor ("start", "middle" or "end") and the x its text
// starts at, for a line avail wide between the margins.
func lineStart(b TextBox, l textLine, avail float64) (x float64, anchor string, start float64) {
	x, anchor, start = b.Margin[0], "start", b.Margin[0]
	switch l.para.Align {
	case "center":
		x, anchor = b.Margin[0]+avail/2, "middle"
		start = x - l.width/2
	case "right":
		x, anchor = b.W-b.Margin[2], "end"
		start = x - l.width
	}
	return x, anchor, start
}

// textBackground returns the fill of b's text background: the box
// around its laid-out lines (from top, total high, each line from its
// start to its width) grown by the block's margins and kept inside
// the block, in b.Background at b.BgOpacity, marked data-role
// text-background. Visio fills the text's own box with the margins
// around it, not the whole block (docs/vsdx-design.md, Text blocks).
func textBackground(b TextBox, lines []textLine, avail, top, total float64) Item {
	x0, x1 := b.W, 0.0
	for _, l := range lines {
		if len(l.pieces) == 0 {
			continue
		}
		_, _, start := lineStart(b, l, avail)
		x0, x1 = min(x0, start), max(x1, start+l.width)
	}
	if x1 < x0 {
		x0, x1 = b.Margin[0], b.Margin[0]
	}
	x0, x1 = max(x0-b.Margin[0], 0), min(x1+b.Margin[2], b.W)
	y0, y1 := max(top-b.Margin[1], 0), min(top+total+b.Margin[3], b.H)
	f := b.Frame
	return Item{Kind: FillItem, Color: b.Background, Opacity: b.BgOpacity,
		Path:  Polygon(f.Apply(geom.Pt{X: x0, Y: y0}), f.Apply(geom.Pt{X: x1, Y: y0}), f.Apply(geom.Pt{X: x1, Y: y1}), f.Apply(geom.Pt{X: x0, Y: y1})),
		Attrs: [][2]string{{"data-role", "text-background"}}}
}

// lineHeight returns a line's height: its largest size times the
// paragraph's line spacing.
func lineHeight(l textLine) float64 {
	ls := l.para.LineSpacing
	if !(ls > 0) {
		ls = defaultLineSpacing
	}
	return l.size * ls
}

// word is a breakable unit: a word and the spaces after it, possibly
// in several runs.
type word struct {
	pieces []piece
	width  float64 // without trailing spaces
	space  float64 // width of the trailing spaces
}

// wrapPara breaks a paragraph into lines no wider than avail.
func wrapPara(p *TextPara, avail float64) []textLine {
	var lines []textLine
	cur := textLine{para: p}
	flush := func() {
		if cur.size == 0 {
			cur.size = DefaultTextSize
		}
		// Trailing spaces do not count toward a line's width.
		if n := len(cur.pieces); n > 0 {
			last := &cur.pieces[n-1]
			trimmed := strings.TrimRightFunc(last.text, unicode.IsSpace)
			if trimmed != last.text {
				w := TextWidth(last.run.Font, last.run.Bold, last.run.Italic, runSize(last.run), trimmed)
				cur.width -= last.width - w
				last.text, last.width = trimmed, w
			}
		}
		lines = append(lines, cur)
		cur = textLine{para: p}
	}
	add := func(w word) {
		for _, pc := range w.pieces {
			if n := len(cur.pieces); n > 0 && cur.pieces[n-1].run == pc.run {
				cur.pieces[n-1].text += pc.text
				cur.pieces[n-1].width += pc.width
			} else {
				cur.pieces = append(cur.pieces, pc)
			}
			cur.size = max(cur.size, runSize(pc.run))
		}
		cur.width += w.width + w.space
	}
	words, breaks := splitWords(p)
	if p.Bullet != "" && len(words) > 0 {
		r := words[0].pieces[0].run
		bw := TextWidth(r.Font, r.Bold, r.Italic, runSize(r), p.Bullet+" ")
		words[0].pieces = append([]piece{{run: r, text: p.Bullet + " ", width: bw}}, words[0].pieces...)
		words[0].width += bw
	}
	for i, w := range words {
		if breaks[i] {
			flush()
		}
		if len(w.pieces) == 0 {
			continue
		}
		if len(cur.pieces) > 0 && cur.width+w.width > avail+1e-6 {
			flush()
		}
		if len(cur.pieces) == 0 && w.width > avail+1e-6 {
			for _, part := range splitLong(w, avail) {
				if len(cur.pieces) > 0 {
					flush()
				}
				add(part)
			}
			continue
		}
		add(w)
	}
	if len(cur.pieces) > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// splitWords cuts a paragraph's runs into words (each with its
// trailing spaces); breaks[i] is true when a line break comes before
// word i.
func splitWords(p *TextPara) (words []word, breaks []bool) {
	var cur word
	pendingBreak := false
	push := func() {
		words = append(words, cur)
		breaks = append(breaks, pendingBreak)
		cur, pendingBreak = word{}, false
	}
	for ri := range p.Runs {
		r := &p.Runs[ri]
		if r.Br {
			push()
			pendingBreak = true
			continue
		}
		size := runSize(r)
		text := strings.ReplaceAll(r.Text, "\r", "")
		for _, seg := range strings.SplitAfter(text, "\n") {
			nl := strings.HasSuffix(seg, "\n")
			seg = strings.TrimSuffix(seg, "\n")
			start := 0
			inSpace := false
			for i, ch := range seg {
				sp := ch == ' ' || ch == '\t'
				if inSpace && !sp {
					t := seg[start:i]
					w := TextWidth(r.Font, r.Bold, r.Italic, size, t)
					cur.pieces = append(cur.pieces, piece{run: r, text: t, width: w})
					cur.space += w
					push()
					start = i
				}
				if sp && !inSpace {
					t := seg[start:i]
					if t != "" {
						w := TextWidth(r.Font, r.Bold, r.Italic, size, t)
						cur.pieces = append(cur.pieces, piece{run: r, text: t, width: w})
						cur.width += cur.space + w
						cur.space = 0
					}
					start = i
				}
				inSpace = sp
			}
			if t := seg[start:]; t != "" {
				w := TextWidth(r.Font, r.Bold, r.Italic, size, t)
				cur.pieces = append(cur.pieces, piece{run: r, text: t, width: w})
				if inSpace {
					cur.space += w
				} else {
					cur.width += cur.space + w
					cur.space = 0
				}
			}
			if nl {
				push()
				pendingBreak = true
			}
		}
	}
	push()
	return words, breaks
}

// splitLong cuts a word wider than avail into words that fit, between
// characters (at least one character each).
func splitLong(w word, avail float64) []word {
	var out []word
	var cur word
	for _, pc := range w.pieces {
		r := pc.run
		size := runSize(r)
		start := 0
		acc := 0.0
		for i, ch := range pc.text {
			cw := RuneWidth(r.Font, r.Bold, r.Italic, ch) * size
			if i > start && cur.width+acc+cw > avail+1e-6 {
				cur.pieces = append(cur.pieces, piece{run: r, text: pc.text[start:i], width: acc})
				cur.width += acc
				out = append(out, cur)
				cur, start, acc = word{}, i, 0
			}
			acc += cw
		}
		if start < len(pc.text) {
			cur.pieces = append(cur.pieces, piece{run: r, text: pc.text[start:], width: acc})
			cur.width += acc
		}
	}
	cur.space = w.space
	cur.width -= w.space
	if len(cur.pieces) > 0 {
		out = append(out, cur)
	}
	return out
}
