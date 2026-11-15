package view

import (
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// The CSS reading the projection's run formatting is defined by: the
// cascade written as CSS and read back (docxdriver's
// styles::declarations and apply_inline_css). The renderer computes the
// same formatting on typed values (runProps.format, inheritable); these
// are the reference the tests hold it to.

func highlightFromCSS(v string) (string, bool) {
	for _, h := range highlights {
		if strings.EqualFold(h.css, v) {
			return h.name, true
		}
	}
	return "", false
}

var errCSS = errors.New("unsupported CSS")

// declarations parses "name:value;" CSS into a map, as docxdriver's
// styles::declarations: names are ASCII-lowercased, unsafe values are an
// error and "unset" values are dropped.
func declarations(css string) (map[string]string, error) {
	out := map[string]string{}
	for part := range strings.SplitSeq(css, ";") {
		part = rustTrim(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, ":")
		if !ok {
			return nil, errCSS
		}
		name = ascii.Lower(rustTrim(name))
		value = rustTrim(value)
		if value == "" || strings.ContainsFunc(value, func(r rune) bool {
			return isControl(r) || r == '<' || r == '>' || r == '{' || r == '}'
		}) || strings.Contains(ascii.Lower(value), "url(") || strings.Contains(value, "!") {
			return nil, errCSS
		}
		if strings.EqualFold(value, "unset") {
			continue
		}
		out[name] = value
	}
	return out, nil
}

// points parses a CSS length in pt or px (or a bare 0) into points.
func points(value string) (float64, error) {
	value = rustTrim(value)
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
		return 0, errCSS
	}
	n, ok := parseRustF64(number)
	if !ok {
		return 0, errCSS
	}
	n *= scale
	if n != n || n < 0 || n > 100000 {
		return 0, errCSS
	}
	return n, nil
}

// applyInlineCSS applies CSS declarations to f, as docxdriver's
// apply_inline_css; any unsupported declaration is an error.
func applyInlineCSS(f fmtState, css string) (fmtState, error) {
	decls, err := declarations(css)
	if err != nil {
		return f, err
	}
	for _, name := range slices.Sorted(maps.Keys(decls)) {
		value := decls[name]
		switch name {
		case "--docx-highlight":
			if !isHighlight(value) {
				return f, errCSS
			}
			f.highlight = value
		case "background-color":
			h, ok := highlightFromCSS(value)
			if !ok {
				return f, errCSS
			}
			f.highlight = h
		case "font-weight":
			f.resets |= resetBold
			switch value {
			case "bold", "bolder", "600", "700", "800", "900":
				f.bold = true
			case "normal", "400":
				f.bold = false
			default:
				return f, errCSS
			}
		case "font-style":
			f.resets |= resetItalic
			switch value {
			case "italic", "oblique":
				f.italic = true
			case "normal":
				f.italic = false
			default:
				return f, errCSS
			}
		case "text-decoration", "text-decoration-line":
			f.resets |= resetUnderline | resetStrike
			words := strings.FieldsFunc(value, unicode.IsSpace)
			f.underline = slices.Contains(words, "underline")
			f.strike = slices.Contains(words, "line-through")
			for _, w := range words {
				if w != "none" && w != "underline" && w != "line-through" {
					return f, errCSS
				}
			}
		case "vertical-align":
			f.resets |= resetBaseline
			switch value {
			case "super":
				f.vert = superscript
			case "sub":
				f.vert = subscript
			case "baseline":
				f.vert = baseline
			default:
				return f, errCSS
			}
		case "color":
			hex, ok := strings.CutPrefix(value, "#")
			if !ok {
				return f, errCSS
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
				return f, errCSS
			}
			v, err := strconv.ParseUint(strings.TrimPrefix(hex, "+"), 16, 32)
			if err != nil {
				return f, errCSS
			}
			f.color, f.hasColor = uint32(v), true
		case "font-size":
			p, err := points(value)
			if err != nil {
				return f, err
			}
			size := p * 2
			frac := size - float64(int64(size))
			if frac < 0 {
				frac = -frac
			}
			if frac > 0.00001 || size < 1 {
				return f, errCSS
			}
			f.size, f.hasSize = uint32(size), true
		case "font-family":
			font, ok := parseFontFamily(value)
			if !ok {
				return f, errCSS
			}
			f.font, f.hasFont = font, true
		case "white-space":
			if value != "pre-wrap" && value != "normal" && value != "pre" {
				return f, errCSS
			}
		default:
			return f, errCSS
		}
	}
	return f, nil
}

var inheritableRunCSS = map[string]bool{
	"color": true, "font-family": true, "font-size": true, "font-style": true, "font-weight": true,
}
