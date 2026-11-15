package xmlscan_test

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

func TestNames(t *testing.T) {
	for _, tc := range []struct {
		s           string
		name, qname bool
	}{
		{"a", true, true},
		{"x:row", true, true},
		{"_a-b.c9", true, true},
		{"é", true, true},
		{":a", true, false},
		{"a:", true, false},
		{"a:b:c", true, false},
		{"", false, false},
		{"1a", false, false},
		{"-a", false, false},
		{"a b", false, false},
		{"\xff", false, false},
	} {
		if got := xmlscan.ValidName(tc.s); got != tc.name {
			t.Errorf("ValidName(%q) = %v", tc.s, got)
		}
		if got := xmlscan.ValidQName(tc.s); got != tc.qname {
			t.Errorf("ValidQName(%q) = %v", tc.s, got)
		}
	}
	for _, tc := range []struct {
		s  string
		ok bool
	}{
		{"plain\t\n\r", true}, {"é\U0001F600", true}, {"\x00", false}, {"\x1f", false},
		{"\xff", false}, {"￾", false}, {"￿", false}, {"\xed\xa0\x80", false},
	} {
		if got := xmlscan.ValidChars(tc.s); got != tc.ok {
			t.Errorf("ValidChars(%q) = %v", tc.s, got)
		}
	}
	for _, c := range []byte(" \t\n\r") {
		if !xmlscan.IsSpace(c) {
			t.Errorf("IsSpace(%q) = false", c)
		}
	}
	if xmlscan.IsSpace('\f') || xmlscan.IsNameStart('-') || !xmlscan.IsNameChar('-') || !xmlscan.IsNameStart(0x10000) {
		t.Error("character classes")
	}
}

// FuzzNames: an element name tree accepts is a ValidQName.
func FuzzNames(f *testing.F) {
	for _, s := range []string{"a", "x:a", ":a", "a:b:c", "é", "1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if doc, err := tree.ParseString("<" + s + "/>"); err == nil && doc.Root().Name() == s && !xmlscan.ValidQName(s) {
			t.Fatalf("tree accepts element name %q, ValidQName refuses it", s)
		}
	})
}
