package formula

import (
	"errors"
	"testing"
)

func TestStoreAtChunk(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"@5+@TRUE", "_xlfn.SINGLE((5))+_xlfn.SINGLE((TRUE))"},
		{"@SUM(@A1,Tbl[@X])", "_xlfn.SINGLE(SUM(_xlfn.SINGLE(A1),Tbl[[#This Row],[X]]))"},
		{"@(A1+B1)", "_xlfn.SINGLE((A1+B1))"},
		{"@{1,2}", "_xlfn.SINGLE({1,2})"},
		{"Tbl['@Col]+\"@\"", "Tbl['@Col]+\"@\""},
	} {
		got, err := StoreAt(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"@", "@  ", "@+", "@)"} {
		_, err := StoreAt(in)
		var refusal *Refusal
		if !errors.As(err, &refusal) {
			t.Errorf("%q: got %v, want Refusal", in, err)
		}
	}
}
