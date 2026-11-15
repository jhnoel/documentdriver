package ops

import "testing"

func TestRenameElementChunk(t *testing.T) {
	for _, tc := range []struct {
		local, in, want string
		count           int
	}{
		{"hyperlinks", `<hyperlinks><hyperlink location="Budget!A1"/><hyperlink location='Other!A1'/></hyperlinks>`, `<hyperlinks><hyperlink location="'Q1 Budget'!A1"/><hyperlink location='Other!A1'/></hyperlinks>`, 1},
		{"dataValidations", `<dataValidations><dataValidation><formula1>Budget!A1</formula1><formula2>Budget!B1</formula2></dataValidation></dataValidations>`, `<dataValidations><dataValidation><formula1>'Q1 Budget'!A1</formula1><formula2>'Q1 Budget'!B1</formula2></dataValidation></dataValidations>`, 2},
		{"conditionalFormatting", `<conditionalFormatting><cfRule><formula>Budget!A1&gt;0</formula></cfRule></conditionalFormatting>`, `<conditionalFormatting><cfRule><formula>'Q1 Budget'!A1&gt;0</formula></cfRule></conditionalFormatting>`, 1},
		{"other", `not xml`, `not xml`, 0},
	} {
		got, count, err := RenameElement(tc.local, tc.in, "Budget", "Q1 Budget")
		if err != nil || got != tc.want || count != tc.count {
			t.Errorf("%s: got %q, %d, %v; want %q, %d", tc.local, got, count, err, tc.want, tc.count)
		}
	}
	if _, _, err := RenameElement("hyperlinks", "<hyperlinks>", "x", "y"); err == nil {
		t.Error("malformed XML accepted")
	}
}
