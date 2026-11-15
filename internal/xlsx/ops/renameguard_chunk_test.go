package ops

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

func TestRenameGuardChunk(t *testing.T) {
	cases := []struct {
		name  string
		parts RenameParts
		want  bool
	}{
		{"quoted chart", RenameParts{Charts: []string{`<chart><f>'Q1 Budget'!$A$1</f></chart>`}}, true},
		{"chart string", RenameParts{Charts: []string{`<chart><f>"Q1 Budget!A1"</f></chart>`}}, false},
		{"external workbook", RenameParts{Charts: []string{`<chart><f>'[Book]Q1 Budget'!A1</f></chart>`}}, false},
		{"pivot entity", RenameParts{PivotCaches: []string{`<pivot><worksheetSource sheet="Q1&#32;Budget"/></pivot>`}}, true},
		{"external sheet", RenameParts{ExternalLinks: []string{`<externalLink><sheetNames><sheetName val="Q1 Budget"/></sheetNames></externalLink>`}}, true},
		{"unrelated sheet", RenameParts{ExternalLinks: []string{`<externalLink><sheetName val="Q1 Budget"/></externalLink>`}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := RenameGuard(tc.parts, "q1 budget")
			var r *Rejection
			if tc.want {
				if !errors.As(err, &r) || r.Code != cli.CodeOperationFailed || r.Detail == "" {
					t.Fatalf("got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := RenameGuard(RenameParts{Charts: []string{`<chart>`}}, "x"); err == nil {
		t.Fatal("malformed XML accepted")
	}
}
