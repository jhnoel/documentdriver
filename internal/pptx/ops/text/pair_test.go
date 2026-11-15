package text

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPairGap(t *testing.T) {
	cases := []struct {
		olds, news []string
		want       string
	}{
		{[]string{"a", "b"}, []string{"x", "y"}, "[[0 0] [1 1]]"},
		// Inserted after the first and every paragraph edited: aligned by
		// text.
		{[]string{"Notification", "Staff", "Data"}, []string{"Notification x", "new", "Staff x", "Data x"}, "[[0 0] [1 2] [2 3]]"},
		// One deleted.
		{[]string{"alpha", "beta", "gamma"}, []string{"alpha!", "gamma!"}, "[[0 0] [2 1]]"},
		// Nothing alike: no pairs.
		{[]string{"aaa"}, []string{"xyz", "qrs"}, "[]"},
	}
	for _, c := range cases {
		if got := fmt.Sprint(pairGap(c.olds, c.news)); got != c.want {
			t.Errorf("pairGap(%q, %q) = %s, want %s", c.olds, c.news, got, c.want)
		}
	}
}

func TestSetRefusesDeletingHiddenContent(t *testing.T) {
	f := newFake(t, sp(`<p:txBody><a:bodyPr/><a:p><mc:AlternateContent><mc:Choice Requires="a14"><a14:m/></mc:Choice><mc:Fallback><a:r><a:t>x²</a:t></a:r></mc:Fallback></mc:AlternateContent></a:p><a:p><a:r><a:t>a</a:t></a:r></a:p></p:txBody>`))
	if err := Set(f, "<p>a</p>"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("deleting the equation paragraph: %v", err)
	}
	mustSet(t, f, "<p></p><p>b</p><p>c</p>")
	if !strings.Contains(f.xml(0), "<a14:m/>") {
		t.Errorf("equation lost: %s", f.xml(0))
	}
}

func TestSetSystemColourKeepsLastClr(t *testing.T) {
	f := newFake(t, sp(`<p:txBody><a:bodyPr/><a:p><a:r><a:t>x</a:t></a:r></a:p></p:txBody>`))
	mustSet(t, f, `<span style="color:#000000" data-color="sys:windowText #000000">x</span>`)
	if !strings.Contains(f.xml(0), `<a:sysClr val="windowText" lastClr="000000"/>`) {
		t.Errorf("%s", f.xml(0))
	}
}
