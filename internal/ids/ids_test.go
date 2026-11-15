package ids

import (
	"regexp"
	"testing"
	"time"
)

var guidRE = regexp.MustCompile(`^\{[0-9A-F]{8}-[0-9A-F]{4}-4[0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}\}$`)

func TestFixedIsReproducible(t *testing.T) {
	clock := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	a, b := Fixed(7, clock), Fixed(7, clock)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		x, y := a.GUID(), b.GUID()
		if x != y {
			t.Fatalf("call %d: %s != %s", i, x, y)
		}
		if !guidRE.MatchString(x) {
			t.Fatalf("bad GUID %q", x)
		}
		if seen[x] {
			t.Fatalf("repeated GUID %s", x)
		}
		seen[x] = true
	}
	if !a.Now().Equal(clock) || !a.Now().Equal(b.Now()) {
		t.Errorf("clock moved: %v", a.Now())
	}
	if Fixed(8, clock).GUID() == Fixed(7, clock).GUID() {
		t.Error("different seeds gave the same first GUID")
	}
}

func TestDefault(t *testing.T) {
	s := Default()
	g1, g2 := s.GUID(), s.GUID()
	if !guidRE.MatchString(g1) || g1 == g2 {
		t.Errorf("GUIDs %q %q", g1, g2)
	}
	if d := time.Since(s.Now()); d < 0 || d > time.Minute {
		t.Errorf("clock off by %v", d)
	}
}
