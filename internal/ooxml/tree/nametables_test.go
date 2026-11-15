package tree

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// The old* functions below are verbatim copies of the tables that lived
// in tree/chars.go and splice/writer.go before consolidation S1b, kept
// as the reference TestNameTablesEquivalent and FuzzNameTables prove
// the shared xmlscan tables equivalent to. They are test-only copies:
// production code calls xmlscan.
func oldIsNameStart(r rune) bool {
	switch {
	case r == ':' || r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z'):
		return true
	case r < 0xC0:
		return false
	}
	return (0xC0 <= r && r <= 0xD6) || (0xD8 <= r && r <= 0xF6) ||
		(0xF8 <= r && r <= 0x2FF) || (0x370 <= r && r <= 0x37D) ||
		(0x37F <= r && r <= 0x1FFF) || (0x200C <= r && r <= 0x200D) ||
		(0x2070 <= r && r <= 0x218F) || (0x2C00 <= r && r <= 0x2FEF) ||
		(0x3001 <= r && r <= 0xD7FF) || (0xF900 <= r && r <= 0xFDCF) ||
		(0xFDF0 <= r && r <= 0xFFFD) || (0x10000 <= r && r <= 0xEFFFF)
}

func oldIsNameChar(r rune) bool {
	return oldIsNameStart(r) || r == '-' || r == '.' || ('0' <= r && r <= '9') ||
		r == 0xB7 || (0x300 <= r && r <= 0x36F) || (0x203F <= r && r <= 0x2040)
}

func oldIsXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || (0x20 <= r && r <= 0xD7FF) ||
		(0xE000 <= r && r <= 0xFFFD) || (0x10000 <= r && r <= 0x10FFFF)
}

// oldTreeValidName is tree's validName before S1b: it does not check
// UTF-8 validity, which xmlscan.ValidName does. The two agree on valid
// UTF-8; on invalid UTF-8 tree's range loop reads each bad byte as
// U+FFFD, a name-start character, and accepts it. Parse never sees the
// difference: it runs checkChars over the whole source first.
func oldTreeValidName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !oldIsNameStart(r) {
				return false
			}
		} else if !oldIsNameChar(r) {
			return false
		}
	}
	return true
}

func oldTreeValidQName(s string) bool {
	if !oldTreeValidName(s) {
		return false
	}
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return true
	}
	return i > 0 && i < len(s)-1 && strings.IndexByte(s[i+1:], ':') < 0
}

func oldSpliceValidNCName(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for i, r := range s {
		if r == ':' || (i == 0 && !oldIsNameStart(r)) || !oldIsNameChar(r) {
			return false
		}
	}
	return true
}

func oldSpliceValidQName(s string) bool {
	prefix, local, found := strings.Cut(s, ":")
	if !found {
		return oldSpliceValidNCName(s)
	}
	return oldSpliceValidNCName(prefix) && oldSpliceValidNCName(local)
}

// oldSpliceValidChars is splice's validChars before S1b.
func oldSpliceValidChars(s string) bool {
	for i, r := range s {
		if r == utf8.RuneError {
			if _, n := utf8.DecodeRuneInString(s[i:]); n <= 1 {
				return false
			}
		}
		if !oldIsXMLChar(r) {
			return false
		}
	}
	return true
}

// qnameTable returns valid UTF-8 names with colons at every position,
// empty parts and non-ASCII starts.
func qnameTable() []string {
	parts := []string{"a", "b", "", "0", "-", ".", "é", "×", "÷", "ÿ", "α", "_", "x-y", "x.y", "x_2"}
	var out []string
	for _, p := range parts {
		out = append(out, p)
		for _, q := range parts {
			out = append(out, p+":"+q)
			for _, r := range parts[:5] {
				out = append(out, p+":"+q+":"+r)
			}
		}
	}
	out = append(out, "", ":", "::", "a::b", ":a", "a:", "xml:space", "xmlns", "a b", "a/b", "x\x00y", "a:b:c:d")
	return out
}

// checkQName asserts xmlscan.ValidQName matches the old tree validQName
// on valid UTF-8, and pins where the old splice validQName differed:
// splice required each NCName part to start with a name-start
// character, so it rejected a local part starting with a digit, '-' or
// '.' (a:0) that tree and xmlscan accept. Splice therefore widens
// its Writer acceptance for exactly that class; no bytes it wrote
// before change.
func checkQName(t *testing.T, s string) {
	t.Helper()
	want := oldTreeValidQName(s)
	if got := xmlscan.ValidQName(s); got != want {
		t.Fatalf("ValidQName(%q) = %v, want %v", s, got, want)
	}
	got := oldSpliceValidQName(s)
	if got == want {
		return
	}
	i := strings.IndexByte(s, ':')
	if i <= 0 || !want || got {
		t.Fatalf("ValidQName(%q): old tree %v, old splice %v (unexpected disagreement)", s, want, got)
	}
	r, _ := utf8.DecodeRuneInString(s[i+1:])
	if oldIsNameStart(r) || !oldIsNameChar(r) {
		t.Fatalf("ValidQName(%q): old tree %v, old splice %v (not the recorded local-start difference)", s, want, got)
	}
}

// TestNameTablesEquivalent proves the xmlscan name and character tables
// equivalent to the tree and splice tables S1b deleted, on valid
// UTF-8; the one recorded difference is tree's old validName accepting
// invalid UTF-8 (see oldTreeValidName).
func TestNameTablesEquivalent(t *testing.T) {
	for r := rune(0); r <= 0x10FFFF; r++ {
		if got, want := xmlscan.IsNameStart(r), oldIsNameStart(r); got != want {
			t.Fatalf("IsNameStart(%#U) = %v, want %v", r, got, want)
		}
		if got, want := xmlscan.IsNameChar(r), oldIsNameChar(r); got != want {
			t.Fatalf("IsNameChar(%#U) = %v, want %v", r, got, want)
		}
	}
	for _, s := range qnameTable() {
		if !utf8.ValidString(s) {
			continue
		}
		checkQName(t, s)
		if got := xmlscan.ValidChars(s); got != oldSpliceValidChars(s) {
			t.Fatalf("ValidChars(%q) = %v, want %v", s, got, oldSpliceValidChars(s))
		}
	}
	// Single-rune ValidChars at every range boundary.
	for r := range rune(0x110000) {
		if 0xD800 <= r && r <= 0xDFFF {
			continue // surrogates have no string form of their own
		}
		s := string(r)
		if got := xmlscan.ValidChars(s); got != oldSpliceValidChars(s) {
			t.Fatalf("ValidChars(%#U) = %v, want %v", r, got, oldSpliceValidChars(s))
		}
	}
	// The recorded difference: invalid UTF-8 is not a name to xmlscan,
	// but tree's old validName read each bad byte as U+FFFD and accepted
	// it. Parse runs checkChars first, so it never reached a name check.
	for _, s := range []string{"\xff", "a\xffb", "\x80:x"} {
		if xmlscan.ValidQName(s) {
			t.Fatalf("ValidQName(%q) = true, want false", s)
		}
		if !oldTreeValidQName(s) {
			t.Fatalf("recorded difference gone: old tree rejects %q", s)
		}
		if oldSpliceValidQName(s) {
			t.Fatalf("old splice accepts %q, want false", s)
		}
	}
}

// FuzzNameTables checks that xmlscan's name and character predicates
// agree with the pre-consolidation tables on random strings: ValidQName
// on valid UTF-8 (the recorded invalid-UTF-8 difference above) and
// ValidChars everywhere.
func FuzzNameTables(f *testing.F) {
	f.Add("a:b")
	f.Add("é:α")
	f.Add("")
	f.Add(":")
	f.Add("\xff")
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := xmlscan.ValidChars(s), oldSpliceValidChars(s); got != want {
			t.Fatalf("ValidChars(%q) = %v, want %v", s, got, want)
		}
		if !utf8.ValidString(s) {
			return
		}
		checkQName(t, s)
	})
}
