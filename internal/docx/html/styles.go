package html

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/htmlx"
)

// The items of docxdriver's html/styles.rs the input path needs: the
// preview stylesheet, CSS declaration parsing, inline run CSS and
// paragraph CSS. The style cascade (Styles) depends on the document model
// and is ported with the read side.

// CSS is the scoped preview stylesheet reads emit. Document input may carry
// it in <head><style>; any other stylesheet is rejected.
const CSS = `.docxdriver { font-family: Calibri, Arial, sans-serif; line-height: 1.2; color: #111; }
.docxdriver p, .docxdriver h1, .docxdriver h2, .docxdriver h3, .docxdriver h4, .docxdriver h5, .docxdriver h6 { white-space: pre-wrap; tab-size: 4; min-height: 1em; }
.docxdriver p { margin: 0 0 .6em; }
.docxdriver table { border-collapse: collapse; max-width: 100%; }
.docxdriver td, .docxdriver th { border: 1px solid #aaa; padding: .3em .5em; vertical-align: top; }
.docxdriver img { max-width: 100%; }
.docxdriver ins { color: #067a35; text-decoration: underline; }
.docxdriver del { color: #b32323; text-decoration: line-through; }
.docxdriver [data-format-change] { background: #fff1b8; }
.docxdriver [data-field] { background: #f2f4f7; }
.docxdriver ol, .docxdriver ul { padding-left: 2em; }
.docxdriver li > p { margin-bottom: .25em; }
.docxdriver [data-marker]::marker { content: attr(data-marker) " "; }
.docxdriver aside { border-top: 1px solid #ddd; margin-top: 1em; font-size: .9em; }
.docxdriver [data-break="page"] { break-after: page; }
.docxdriver [data-break="column"] { break-after: column; }
.docxdriver header, .docxdriver footer { color: #555; }
`

// ParseFontFamily validates a CSS font family: surrounding whitespace and
// quotes are removed; the result is at most 127 bytes with no control
// characters or ; < > { } \.
func ParseFontFamily(value string) (FontFamily, error) {
	value = strings.Trim(strings.TrimSpace(value), `'"`)
	if len(value) > 127 || strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || strings.ContainsRune(";<>{}\\", r)
	}) {
		return "", errorf("invalid font family")
	}
	return FontFamily(value), nil
}

// Decl is one CSS declaration.
type Decl = htmlx.StyleDecl

// Declarations parses an inline style; it forwards to htmlx.StyleDecls.
func Declarations(css string) ([]Decl, error) { return htmlx.StyleDecls(css) }

// signedPoints parses a CSS point value that may carry a leading minus.
func signedPoints(value string) (float64, error) {
	if v, ok := strings.CutPrefix(strings.TrimSpace(value), "-"); ok {
		p, err := Points(v)
		return -p, err
	}
	return Points(value)
}

// Points converts a CSS length in pt or px (or a bare 0) to points, within
// 0..100000.
func Points(value string) (float64, error) {
	value = strings.TrimSpace(value)
	var number string
	scale := 1.0
	switch {
	case strings.HasSuffix(value, "pt"):
		number = strings.TrimSuffix(value, "pt")
	case strings.HasSuffix(value, "px"):
		number, scale = strings.TrimSuffix(value, "px"), 0.75
	case value == "0":
		number = value
	default:
		return 0, errorf("lengths require pt or px")
	}
	n, ok := parseF64(number)
	if !ok {
		return 0, errorf("invalid CSS length")
	}
	n *= scale
	if math.IsInf(n, 0) || math.IsNaN(n) || n < 0 || n > 100000 {
		return 0, errorf("CSS length out of range")
	}
	return n, nil
}

// ApplyInlineCSS applies a run's inline style to fmt.
func ApplyInlineCSS(fmt Fmt, css string) (Fmt, error) {
	decls, err := Declarations(css)
	if err != nil {
		return fmt, err
	}
	for _, d := range decls {
		value := d.Value
		switch d.Name {
		case "--docx-highlight":
			h, err := ParseHighlightColor(value)
			if err != nil {
				return fmt, err
			}
			fmt.Highlight = Some(h)
		case "background-color":
			h, err := HighlightFromCSS(value)
			if err != nil {
				return fmt, err
			}
			fmt.Highlight = Some(h)
		case "font-weight":
			fmt.Resets |= 1
			switch value {
			case "bold", "bolder", "600", "700", "800", "900":
				fmt.Bold = true
			case "normal", "400":
				fmt.Bold = false
			default:
				return fmt, errorf("unsupported font-weight")
			}
		case "font-style":
			fmt.Resets |= 2
			switch value {
			case "italic", "oblique":
				fmt.Italic = true
			case "normal":
				fmt.Italic = false
			default:
				return fmt, errorf("unsupported font-style")
			}
		case "text-decoration", "text-decoration-line":
			fmt.Resets |= 4 | 8
			words := strings.Fields(value)
			fmt.Underline, fmt.Strike = false, false
			for _, w := range words {
				fmt.Underline = fmt.Underline || w == "underline"
				fmt.Strike = fmt.Strike || w == "line-through"
			}
			for _, w := range words {
				if w != "none" && w != "underline" && w != "line-through" {
					return fmt, errorf("unsupported text-decoration")
				}
			}
		case "vertical-align":
			fmt.Resets |= 16
			switch value {
			case "super":
				fmt.VertAlign = Some(Superscript)
			case "sub":
				fmt.VertAlign = Some(Subscript)
			case "baseline":
				fmt.VertAlign = Opt[VertAlign]{}
			default:
				return fmt, errorf("unsupported vertical-align")
			}
		case "color":
			hex, ok := strings.CutPrefix(value, "#")
			if !ok {
				return fmt, errorf("colors require #RGB or #RRGGBB")
			}
			if len(hex) == 3 {
				var b strings.Builder
				for _, r := range hex {
					b.WriteRune(r)
					b.WriteRune(r)
				}
				hex = b.String()
			}
			if len(hex) != 6 {
				return fmt, errorf("invalid color")
			}
			c, ok := parseHexU32(hex)
			if !ok {
				return fmt, errorf("invalid color")
			}
			fmt.Color = Some(c)
		case "font-size":
			p, err := Points(value)
			if err != nil {
				return fmt, err
			}
			size := p * 2
			if _, frac := math.Modf(size); math.Abs(frac) > 0.00001 || size < 1 {
				return fmt, errorf("font-size must be a positive multiple of .5pt")
			}
			fmt.FontSize = Some(uint32(size))
		case "font-family":
			f, err := ParseFontFamily(value)
			if err != nil {
				return fmt, err
			}
			fmt.FontFamily = Some(f)
		case "white-space":
			if value != "pre-wrap" && value != "normal" && value != "pre" {
				return fmt, errorf("unsupported inline CSS property %s", d.Name)
			}
		default:
			return fmt, errorf("unsupported inline CSS property %s", d.Name)
		}
	}
	return fmt, nil
}

// roundTwips converts points to twips by rounding points times twenty.
func roundTwips(points float64) int64 { return int64(math.Round(points * 20)) }

// ParagraphProperties validates a paragraph's inline style and returns its
// w:pPr children (alignment, spacing, indentation) as XML. Run properties
// in the style are accepted and left to the paragraph's content.
func ParagraphProperties(css string) (string, error) {
	decls, err := Declarations(css)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	spacing := map[string]int64{}
	indent := map[string]int64{}
	lineExact := false
	for _, d := range decls {
		value := d.Value
		switch d.Name {
		case "text-align":
			if value != "left" && value != "right" && value != "center" && value != "justify" {
				return "", errorf("unsupported paragraph CSS property %s", d.Name)
			}
			if value == "justify" {
				value = "both"
			}
			out.WriteString(`<w:jc w:val="` + value + `"/>`)
		case "margin-top", "margin-bottom":
			p, err := Points(value)
			if err != nil {
				return "", err
			}
			key := "before"
			if d.Name == "margin-bottom" {
				key = "after"
			}
			spacing[key] = roundTwips(p)
		case "margin-left", "margin-right":
			p, err := signedPoints(value)
			if err != nil {
				return "", err
			}
			indent[strings.TrimPrefix(d.Name, "margin-")] = roundTwips(p)
		case "text-indent":
			p, err := signedPoints(value)
			if err != nil {
				return "", err
			}
			offset := roundTwips(p)
			delete(indent, "firstLine")
			delete(indent, "hanging")
			if offset < 0 {
				indent["hanging"] = -offset
			} else {
				indent["firstLine"] = offset
			}
		case "line-height":
			if pt, ok := strings.CutSuffix(value, "pt"); ok {
				// An exact line height, as read writes w:spacing w:line
				// with a lineRule other than auto.
				p, err := Points(value)
				if err != nil || pt == "" {
					return "", errorf("invalid line-height")
				}
				spacing["line"] = roundTwips(p)
				lineExact = true
				continue
			}
			multiple, ok := parseF64(value)
			if !ok {
				return "", errorf("line-height must be a positive number")
			}
			if math.IsInf(multiple, 0) || math.IsNaN(multiple) || multiple < 0.1 || multiple > 100 {
				return "", errorf("invalid line-height")
			}
			spacing["line"] = int64(math.Round(multiple * 240))
			lineExact = false
		case "color", "font-size", "font-family", "font-weight", "font-style", "text-decoration", "vertical-align":
		case "white-space":
			if value != "pre-wrap" && value != "normal" && value != "pre" {
				return "", errorf("unsupported paragraph CSS property %s", d.Name)
			}
		default:
			return "", errorf("unsupported paragraph CSS property %s", d.Name)
		}
	}
	for _, group := range []struct {
		tag    string
		values map[string]int64
	}{{"spacing", spacing}, {"ind", indent}} {
		if len(group.values) == 0 {
			continue
		}
		keys := slices.Sorted(maps.Keys(group.values))
		out.WriteString("<w:" + group.tag)
		for _, k := range keys {
			out.WriteString(" w:" + k + `="` + strconv.FormatInt(group.values[k], 10) + `"`)
			if group.tag == "spacing" && k == "line" && lineExact {
				out.WriteString(` w:lineRule="exact"`)
			}
		}
		out.WriteString("/>")
	}
	return out.String(), nil
}
