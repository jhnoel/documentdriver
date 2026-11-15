package shift

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

func TestTableChunk(t *testing.T) {
	src := `<x:table ref='C3:E8'><x:autoFilter ref="C3:E8"><x:sortState ref="C4:E8"><x:sortCondition ref="D4:D8"/></x:sortState></x:autoFilter><!-- <table ref="A1"/> --><ext><sortState ref="A1"/></ext></x:table>`
	cases := []struct {
		name      string
		kind      formula.EditKind
		at, count int
		want      string
		refuse    bool
	}{
		{"grow", formula.InsertRows, 5, 2, `<x:table ref='C3:E10'><x:autoFilter ref="C3:E10"><x:sortState ref="C4:E10"><x:sortCondition ref="D4:D10"/></x:sortState></x:autoFilter><!-- <table ref="A1"/> --><ext><sortState ref="A1"/></ext></x:table>`, false},
		{"left", formula.InsertCols, 3, 1, `<x:table ref='D3:F8'><x:autoFilter ref="D3:F8"><x:sortState ref="D4:F8"><x:sortCondition ref="E4:E8"/></x:sortState></x:autoFilter><!-- <table ref="A1"/> --><ext><sortState ref="A1"/></ext></x:table>`, false},
		{"right", formula.DeleteCols, 6, 1, src, false},
		{"cross", formula.DeleteCols, 2, 2, "", true},
		{"header", formula.DeleteRows, 3, 1, "", true},
		{"data", formula.DeleteRows, 4, 5, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Table(src, formula.Edit{Sheet: "S", Kind: tc.kind, At: tc.at, Count: tc.count})
			var refused *Refused
			if tc.refuse {
				if !errors.As(err, &refused) {
					t.Fatalf("want refusal: %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %s, %v", got, err)
			}
		})
	}
}
