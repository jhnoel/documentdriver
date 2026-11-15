package span

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// apply rebuilds new from old and the changes, checking they are ordered
// and disjoint.
func applyChanges(t *testing.T, old, repl []rune, cs []Change) string {
	t.Helper()
	var b strings.Builder
	at := 0
	for _, c := range cs {
		if c.A < at || c.B < c.A || c.D < c.C {
			t.Fatalf("bad change %+v after %d", c, at)
		}
		b.WriteString(string(old[at:c.A]))
		b.WriteString(string(repl[c.C:c.D]))
		at = c.B
	}
	b.WriteString(string(old[at:]))
	return b.String()
}

// oldTokens is the pre-W1 tokens, kept as the baseline: it grows each
// token one rune at a time.
func oldTokens(text []rune) (toks []string, ends []int) {
	ends = []int{0}
	prev := -1
	for i, r := range text {
		k := tokenKind(r)
		if prev == k && k != 2 {
			toks[len(toks)-1] += string(r)
			ends[len(ends)-1] = i + 1
		} else {
			toks = append(toks, string(r))
			ends = append(ends, i+1)
		}
		prev = k
	}
	return toks, ends
}

// TestTokensMatchBaseline checks tokens and ends against the pre-W1
// baseline over empty, word, space, punctuation and mixed inputs,
// including a long word (the quadratic case the fix removes).
func TestTokensMatchBaseline(t *testing.T) {
	for _, s := range []string{"", "a", "hello world", "one  two", "a,b!!c", "  leading and trailing  ", "café naïve", "a ￼ b", "one\nline\ttab", "!!!", "123 456", strings.Repeat("w", 5000), strings.Repeat("ab ", 1000), strings.Repeat(" ", 100), strings.Repeat("!", 100)} {
		text := []rune(s)
		gotToks, gotEnds := tokens(text)
		wantToks, wantEnds := oldTokens(text)
		if !slices.Equal(gotToks, wantToks) || !slices.Equal(gotEnds, wantEnds) {
			t.Errorf("%q: tokens %q ends %v, baseline %q %v", s, gotToks, gotEnds, wantToks, wantEnds)
		}
	}
}

// TestWordDiff ports docxdriver's auto-diff cases (typed_api.rs
// auto_diff_*): the changes rebuild the new text, an unchanged text has
// none, and rewritten phrases group.
func TestWordDiff(t *testing.T) {
	for _, c := range []struct {
		old, new string
		changes  int
	}{
		{"Payment is due within thirty days.", "Payment is due within sixty days.", 1},
		{"one two three four five", "one TWO three FOUR five", 2},
		{"hello world", "hello brave world", 1},
		{"hello brave world", "hello world", 1},
		{"hello", "hello!", 1},
		{"hello!", "hello", 1},
		{"café naïve résumé", "café wise résumé", 1},
		{"a a b a", "a b a a", -1},
		{"one  two", "one two", 1},
		{"same words", "same words", 0},
		{"delete everything", "", 1},
		{"", "new text", 1},
		{"old red car", "new blue truck", 1},
		{"old, red car", "new, blue truck", 1},
		{"old red car stays here", "new blue truck stays there", 2},
		{"a ￼ b", "x ￼ y", 2},
		{"one\nline", "1\nLINE", 1},
	} {
		old, repl := []rune(c.old), []rune(c.new)
		cs, err := WordDiff(old, repl)
		if err != nil {
			t.Fatal(err)
		}
		if got := applyChanges(t, old, repl, cs); got != c.new {
			t.Errorf("%q -> %q: rebuilt %q from %+v", c.old, c.new, got, cs)
		}
		if c.changes >= 0 && len(cs) != c.changes {
			t.Errorf("%q -> %q: %d changes %+v, want %d", c.old, c.new, len(cs), cs, c.changes)
		}
		for _, ch := range cs {
			if strings.ContainsRune(string(old[ch.A:ch.B]), Object) {
				t.Errorf("%q -> %q: change %+v removes the object", c.old, c.new, ch)
			}
		}
	}
	big := []rune(strings.Repeat("w ", 1500))
	if _, err := WordDiff(big, []rune(strings.Repeat("v ", 1500))); !errors.Is(err, ErrDiffTooLarge) {
		t.Errorf("a large diff: %v", err)
	}
}
