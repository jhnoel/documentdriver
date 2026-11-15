package model

import "testing"

func TestNameID(t *testing.T) {
	for _, tc := range []struct{ sheet, name, want string }{{"", "Tax", "name:Tax"}, {"Budget", "Local", "name:Budget!Local"}, {"Bob's", "Local", "name:'Bob''s'!Local"}} {
		got := NameID(tc.sheet, tc.name)
		if got != tc.want {
			t.Errorf("NameID = %q", got)
		}
		ref, err := ParseRef(got)
		if err != nil || ref.Sheet != tc.sheet || ref.Name != tc.name {
			t.Errorf("ParseRef(%q) = %+v, %v", got, ref, err)
		}
	}
}
