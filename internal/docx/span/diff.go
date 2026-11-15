package span

import (
	"errors"
	"unicode"
)

// Object is the character an object (an equation, a symbol, a drawing, a
// note reference) counts as in a paragraph's text.
const Object = '￼'

// ErrDiffTooLarge is a word diff whose table would pass the bound
// docxdriver sets (4,000,000 cells).
var ErrDiffTooLarge = errors.New("span: word-level diff selection is too large")

// Change is one difference a word diff found: old characters [A, B)
// become new characters [C, D). A == B is an insertion, C == D a
// deletion.
type Change struct{ A, B, C, D int }

// tokenKind is a character's class for the diff: word characters, white
// space and everything else (each its own token).
func tokenKind(r rune) int {
	switch {
	case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_':
		return 0
	case unicode.IsSpace(r):
		return 1
	}
	return 2
}

// tokens splits text into diff tokens (typed.rs diff_tokens): runs of word
// characters, runs of white space, and every other character alone. ends
// holds each token's end offset, after a leading 0. Each token is sliced
// from text once, when the next token starts: appending a rune at a time
// costs quadratic bytes on a long word.
func tokens(text []rune) (toks []string, ends []int) {
	ends = []int{0}
	prev := -1
	start := 0
	for i, r := range text {
		k := tokenKind(r)
		if prev != -1 && (k != prev || k == 2) {
			toks = append(toks, string(text[start:i]))
			ends = append(ends, i)
			start = i
		}
		prev = k
	}
	if len(text) > 0 {
		toks = append(toks, string(text[start:]))
		ends = append(ends, len(text))
	}
	return toks, ends
}

// WordDiff returns the changes that turn old into repl, word by word
// (typed.rs word_diff): the unmatched spans around a deterministic token
// longest common subsequence, with changes separated only by separators
// joined into one, so a rewritten phrase is one deletion and one
// insertion rather than one per word. A change is never joined across an
// object (U+FFFC), which is content the diff keeps, not a separator.
// It returns ErrDiffTooLarge past docxdriver's bound.
func WordDiff(old, repl []rune) ([]Change, error) {
	if string(old) == string(repl) {
		return nil, nil
	}
	a, ao := tokens(old)
	b, bo := tokens(repl)
	width := len(b) + 1
	cells := (len(a) + 1) * width
	if cells > 4_000_000 {
		return nil, ErrDiffTooLarge
	}
	lcs := make([]uint32, cells)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i*width+j] = 1 + lcs[(i+1)*width+j+1]
			} else {
				lcs[i*width+j] = max(lcs[(i+1)*width+j], lcs[i*width+j+1])
			}
		}
	}
	var spans []Change
	i, j, ai, bj := 0, 0, 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			if ai != i || bj != j {
				spans = append(spans, Change{ao[ai], ao[i], bo[bj], bo[j]})
			}
			i++
			j++
			ai, bj = i, j
		case lcs[(i+1)*width+j] >= lcs[i*width+j+1]:
			i++
		default:
			j++
		}
	}
	if ai != len(a) || bj != len(b) {
		spans = append(spans, Change{ao[ai], ao[len(a)], bo[bj], bo[len(b)]})
	}
	var grouped []Change
	for _, s := range spans {
		if n := len(grouped); n > 0 && separators(old[grouped[n-1].B:s.A]) {
			grouped[n-1].B, grouped[n-1].D = s.B, s.D
			continue
		}
		grouped = append(grouped, s)
	}
	return grouped, nil
}

// separators reports whether rs holds no word character and no object.
func separators(rs []rune) bool {
	for _, r := range rs {
		if tokenKind(r) == 0 || r == Object {
			return false
		}
	}
	return true
}
