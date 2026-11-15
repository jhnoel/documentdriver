package edit

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

const slide1 = "/ppt/slides/slide1.xml"

func workspace(t *testing.T, path string) *Workspace {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return New(p)
}

func TestResolve(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	cases := []struct {
		addr, want string
		err        error
	}{
		{"slide1#10", "slide1#10", nil},
		{"slide1#s10", "slide1#10", nil},
		{"slide1#40~2", "slide1#40~2", nil},
		{"slide1.xml#21", "slide1#21", nil},
		{"/ppt/slides/slide1.xml#30", "slide1#30", nil},
		{"slide1#40~3", "", ErrNoObject},
		{"slide1#999", "", ErrNoObject},
		{"slide2#10", "", ErrBadAddress},
		{"slide1#10~0", "", ErrBadAddress},
		{"slide1", "", ErrBadAddress},
	}
	for _, c := range cases {
		r, err := w.Resolve(c.addr)
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Errorf("%s: err = %v, want %v", c.addr, err, c.err)
			}
			continue
		}
		if err != nil || r.String() != c.want || r.Part() != slide1 {
			t.Errorf("%s: %v, %v", c.addr, r, err)
		}
	}
	if r, _ := w.Resolve("slide1#40~2"); r.Short() != "s40~2" || r.ID() != "40" || r.Ord() != 2 {
		t.Errorf("Short = %s", r.Short())
	}
}

func TestAliases(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	if _, err := w.ResolveIn(slide1, "$cache"); !errors.Is(err, ErrNoObject) {
		t.Fatalf("unbound alias: %v", err)
	}
	r, _ := w.Resolve("slide1#11")
	a, ok := ParseAlias("$cache")
	if !ok {
		t.Fatal("ParseAlias")
	}
	if err := w.Bind(a, r); err != nil {
		t.Fatal(err)
	}
	if err := w.Bind(a, r); err == nil {
		t.Fatal("alias bound twice")
	}
	if got, err := w.ResolveIn(slide1, "$cache"); err != nil || got != r {
		t.Fatalf("ResolveIn($cache) = %v, %v", got, err)
	}
	for _, bad := range []string{"cache", "$", "$a b"} {
		if _, ok := ParseAlias(bad); ok {
			t.Errorf("ParseAlias(%q) accepted", bad)
		}
	}
}

func TestResolveGlueIsExplicit(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	if r, err := w.ResolveGlue(slide1, "10"); err != nil || r.Short() != "s10" {
		t.Fatalf("ResolveGlue(10) = %v, %v", r, err)
	}
	if _, err := w.ResolveGlue(slide1, "40"); !errors.Is(err, ErrAmbiguous) || err.Error() != "edit: ambiguous glue id: id 40 is shared by s40, s40~2" {
		t.Fatalf("ResolveGlue(40) err = %v", err)
	}
	if _, err := w.ResolveGlue(slide1, "999"); !errors.Is(err, ErrNoObject) {
		t.Fatalf("ResolveGlue(999) err = %v", err)
	}
}

func TestNextIDScansEveryBranch(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	// The largest cNvPr id is 71, but a dangling connector is glued to id
	// 999: a new object must not take it. Ids in groups and in both
	// branches of mc:AlternateContent count too.
	for _, want := range []string{"1000", "1001"} {
		got, err := w.NextID(slide1)
		if err != nil || got != want {
			t.Fatalf("NextID = %s, %v; want %s", got, err, want)
		}
	}
}
