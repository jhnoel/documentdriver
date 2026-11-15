package shift

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

func TestGuardChunk(t *testing.T) {
	e := formula.Edit{Kind: formula.InsertRows, At: 3, Count: 1}
	cases := []struct {
		name string
		p    Parts
		want bool
	}{
		{"comment before", Parts{Comments: `<comments><comment ref="A2"/></comments>`}, false},
		{"comment at", Parts{ThreadedComments: `<comments><threadedComment ref="A3"/></comments>`}, true},
		{"pivot before", Parts{PivotCaches: []string{`<cache><worksheetSource sheet="budget" ref="A1:B2"/></cache>`}}, false},
		{"pivot crossing", Parts{PivotCaches: []string{`<cache><worksheetSource sheet="budget" ref="A1:B3"/></cache>`}}, true},
		{"external", Parts{ExternalLinks: []string{`<external><sheetNames><sheetName val="BUDGET"/></sheetNames></external>`}}, true},
		{"vml", Parts{VML: `<xml><Anchor>0, 0, 0, 0, 1, 0, 2, 0</Anchor></xml>`}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Guard(tc.p, "Budget", e)
			var refused *Refused
			if errors.As(err, &refused) != tc.want || (err != nil && refused == nil) {
				t.Fatalf("Guard: %v", err)
			}
		})
	}
}
