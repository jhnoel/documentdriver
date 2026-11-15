package edit

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

func TestCellTextEscapeRoundTripChunk(t *testing.T) {
	for _, text := range []string{"_x0041__x005F_", "_xAbCd_\x00\r\ufffe\uffff", "😀<&\t\n", "_X0041_ _x00zz_"} {
		if got := sml.UnescapeX(sml.EscapeX(text)); got != text {
			t.Errorf("round trip %q = %q", text, got)
		}
	}
}

func TestRenderCellInvalidChunk(t *testing.T) {
	for _, tc := range []struct {
		row, col, style int
		c               Content
	}{
		{0, 1, 0, Content{Kind: ContentBool}},
		{1, formula.MaxCols + 1, 0, Content{Kind: ContentBool}},
		{1, 1, -1, Content{Kind: ContentBool}},
		{1, 1, 0, Content{Kind: 255}},
		{1, 1, 0, Content{Kind: ContentString, Text: "\xff"}},
		{1, 1, 0, Content{Kind: ContentString, Text: strings.Repeat("😀", model.MaxText/2+1)}},
		{1, 1, 0, Content{Kind: ContentFormula, Formula: "\x01"}},
	} {
		if got, err := RenderCell("", tc.row, tc.col, tc.style, tc.c); err == nil {
			t.Errorf("invalid input %+v returned %q", tc, got)
		}
	}
}
