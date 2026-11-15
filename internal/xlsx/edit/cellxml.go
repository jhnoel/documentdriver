package edit

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// ContentKind is the kind of content a new cell holds.
type ContentKind uint8

// The content kinds.
const (
	// ContentEmpty is a style-only cell (<c r s/>).
	ContentEmpty ContentKind = iota
	// ContentNumber is a number: <v> with no t (Num, finite).
	ContentNumber
	// ContentString is text, written inline (decision 11): t="inlineStr"
	// and <is><t>, never a shared string.
	ContentString
	// ContentBool is t="b" and <v>1</v> or <v>0</v>.
	ContentBool
	// ContentError is t="e" and the error literal in <v> (Text).
	ContentError
	// ContentFormula is <f> with the stored formula (Formula) and no <v>
	// and no t (E1): the tool never writes a value it computed.
	ContentFormula
)

// Content is what a new cell holds.
type Content struct {
	Kind ContentKind
	// Num is a ContentNumber's value.
	Num float64
	// Text is a ContentString's text (unescaped: the renderer applies the
	// _xHHHH_ escapes) or a ContentError's literal.
	Text string
	// Bool is a ContentBool's value.
	Bool bool
	// Formula is a ContentFormula's stored text (formula.Store's output;
	// no leading "=").
	Formula string
}

// RenderCell returns the <c> element for (row, col) with style s (0
// writes no s) holding c, written with prefix (Sheet.Prefix: "" or
// "x:"), for SetCell. It is pure.
//
// Rules (chunk K2):
//   - r is always written (A1, no "$"); attributes in the order r, s, t.
//   - Numbers: <v> holds model.FormatNumber(Num) (the shortest
//     round-trip form); NaN and infinities are an error.
//   - Strings: t="inlineStr", <is><t>text</t></is>; xml:space="preserve"
//     on <t> when the text starts or ends with whitespace (space, tab,
//     newline) or holds a newline; C0 controls other than tab and
//     newline (carriage return included, as Excel writes it) and
//     U+FFFE, U+FFFF are written _xHHHH_ (upper-case hex), and a literal "_xHHHH_" in the text is
//     written with its underscore escaped (_x005F_xHHHH_), so that
//     sheet.UnescapeX gives the text back. Text longer than
//     model.MaxText UTF-16 units is an error.
//   - Booleans: t="b", <v>1</v> or <v>0</v>. Errors: t="e", <v>#N/A</v>
//     (Text must be one of Excel's error literals: an error otherwise).
//   - Formulas: <f>text</f> only: no <v>, no t (E1); Formula longer than
//     model.MaxFormula is an error.
//   - ContentEmpty: <c r s/>; with s 0 it is an error (no cell to write:
//     the caller deletes the cell instead).
//
// Every byte comes from one splice.Writer, so the result is well formed
// by construction; SetCell scans it again.
func RenderCell(prefix string, row, col, style int, c Content) (string, error) {
	if row < 1 || row > formula.MaxRows || col < 1 || col > formula.MaxCols || style < 0 {
		return "", fmt.Errorf("invalid cell coordinates or style")
	}
	var w splice.Writer
	w.Start(prefix + "c")
	w.Attr("r", model.CellKey(row, col))
	if style != 0 {
		w.Attr("s", strconv.Itoa(style))
	}
	switch c.Kind {
	case ContentEmpty:
		if style == 0 {
			return "", fmt.Errorf("empty cell has no style")
		}
	case ContentNumber:
		if math.IsNaN(c.Num) || math.IsInf(c.Num, 0) {
			return "", fmt.Errorf("cell number is not finite")
		}
		w.Start(prefix + "v")
		w.Text(model.FormatNumber(c.Num))
		w.End()
	case ContentString:
		if !utf8.ValidString(c.Text) || model.TextLen(c.Text, model.MaxText) > model.MaxText {
			return "", fmt.Errorf("invalid or overlong cell text")
		}
		w.Attr("t", "inlineStr")
		w.Start(prefix + "is")
		w.Start(prefix + "t")
		if strings.Contains(c.Text, "\n") || (len(c.Text) > 0 && (strings.ContainsRune(" \t\n", rune(c.Text[0])) || strings.ContainsRune(" \t\n", rune(c.Text[len(c.Text)-1])))) {
			w.Attr("xml:space", "preserve")
		}
		w.Text(sml.EscapeX(c.Text))
		w.End()
		w.End()
	case ContentBool:
		w.Attr("t", "b")
		w.Start(prefix + "v")
		if c.Bool {
			w.Text("1")
		} else {
			w.Text("0")
		}
		w.End()
	case ContentError:
		switch c.Text {
		case "#NULL!", "#DIV/0!", "#VALUE!", "#REF!", "#NAME?", "#NUM!", "#N/A", "#GETTING_DATA":
		default:
			return "", fmt.Errorf("invalid cell error %q", c.Text)
		}
		w.Attr("t", "e")
		w.Start(prefix + "v")
		w.Text(c.Text)
		w.End()
	case ContentFormula:
		if model.TextLen(c.Formula, model.MaxFormula) > model.MaxFormula {
			return "", fmt.Errorf("formula is too long")
		}
		w.Start(prefix + "f")
		w.Text(c.Formula)
		w.End()
	default:
		return "", fmt.Errorf("unknown cell content kind %d", c.Kind)
	}
	w.End()
	return w.String()
}
