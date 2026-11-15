package htmlx

import (
	"errors"
	"strings"
	"testing"
)

// TestStrictMode holds the contract's input policy (docs/contract.md,
// section 7): HTML5 syntax, strict meaning. Strict input parses to the
// same tree as lenient input wherever it is accepted at all, and refuses
// what the lenient builder would ignore or invent.
func TestStrictMode(t *testing.T) {
	strict := Options{Strict: true, SelfClosing: map[string]bool{"span": true, "p": true}}
	for _, in := range []string{
		"<p>a</p><p>b</p>",
		"<p>a<p>b",                  // optional end tags
		"<ul><li>a<li>b</ul>",       //
		"<P CLASS=x>a</P>",          // names fold to lower case
		"<p>a<br>b<br/>c</p>",       // void elements, with or without the slash
		"<p>R&D &amp; &nbsp;&#x41;", // references and a bare ampersand
		"<p data-x=1 data-y='2' data-z=\"3\">x</p>",
		"<p><span/>x</p><p/>", // <x/> for the elements read writes empty
		"<p><b><i>x</i></b></p>",
		"<table><tr><td>a<td>b<tr><td>c</table>",
		"text only",
		"",
	} {
		got, err := ParseHTML(in, strict)
		if err != nil {
			t.Errorf("strict refuses %q: %v", in, err)
			continue
		}
		lenient, err := ParseHTML(in, Options{SelfClosing: strict.SelfClosing})
		if err != nil {
			t.Fatalf("lenient refuses %q: %v", in, err)
		}
		if Dump(got) != Dump(lenient) {
			t.Errorf("%q: strict tree\n%s\nlenient tree\n%s", in, Dump(got), Dump(lenient))
		}
	}
	for _, c := range []struct{ in, want string }{
		{"<p>a</b>", "</b> has no open <b>"},     // lenient: ignored
		{"a</p>", "</p> has no open <p>"},        // lenient: an empty paragraph
		{"a</br>b", "</br> has no open <br>"},    // lenient: a line break
		{"<p>a</p></p>", "</p> has no open <p>"}, //
		{"</span>", "</span> has no open <span>"},
		{"<b>a", "<b> is not closed"}, // lenient: closed at the end
		{"<p><span>a</p>", "<span>"},  // lenient too: an open element across a block end
		{"<ul><li>a</ul></li>", "</li> has no open <li>"},
		{"<!-- note --><p>a</p>", "comment"}, // lenient: dropped
		{"<p>a<!--x--></p>", "comment"},
		{"<p><b><i>x</b></i></p>", "</b>"}, // misnested
	} {
		_, err := ParseHTML(c.in, strict)
		var u *UnsupportedError
		if !errors.As(err, &u) || !strings.Contains(u.Msg, c.want) {
			t.Errorf("strict %q: %v, want an UnsupportedError mentioning %q", c.in, err, c.want)
			continue
		}
		if u.Off <= 0 || u.Off > len(c.in) {
			t.Errorf("strict %q: offset %d outside the input", c.in, u.Off)
		}
	}
}

// TestTokenizerErrorsRefuse: a tokenizer parse error refuses the input in
// both modes, with the WHATWG error name and the place.
func TestTokenizerErrorsRefuse(t *testing.T) {
	for _, c := range []struct {
		in, code string
		offset   int
	}{
		{"a < b", "invalid-first-character-of-tag-name", 4},
		{"<p>a</ >", "invalid-first-character-of-tag-name", 7},
		{"<p", "eof-in-tag", 2},
		{"<p title=\"x", "eof-in-tag", 11},
		{"<p>&#1;</p>", "control-character-reference", 7},
		{"<p>&#xD800;</p>", "surrogate-character-reference", 11},
		{"<p>&#x110000;</p>", "character-reference-outside-unicode-range", 13},
		{"<p>&#;</p>", "absence-of-digits-in-numeric-character-reference", 5},
		{"<p a=\"1\"b=2>", "missing-whitespace-between-attributes", -1},
		{"<p =x>", "unexpected-equals-sign-before-attribute-name", -1},
		{"<p a=>", "missing-attribute-value", -1},
		{"<p a=x\"y>", "unexpected-character-in-unquoted-attribute-value", -1},
		{"<!x>", "incorrectly-opened-comment", -1},
		{"<?xml?>", "unexpected-question-mark-instead-of-tag-name", -1},
		{"<p>\x00</p>", "unexpected-null-character", -1},
		{"<!--x", "eof-in-comment", -1},
	} {
		for _, o := range []Options{{}, {Strict: true}} {
			_, err := ParseHTML(c.in, o)
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Errorf("%q (strict %v): %v, want a SyntaxError %s", c.in, o.Strict, err, c.code)
				continue
			}
			if se.Code != c.code {
				t.Errorf("%q: code %s, want %s", c.in, se.Code, c.code)
			}
			if c.offset >= 0 && se.Offset != c.offset {
				t.Errorf("%q: offset %d, want %d", c.in, se.Offset, c.offset)
			}
			if se.Offset < 0 || se.Offset > len(c.in) || se.Line < 1 || se.Column < 1 {
				t.Errorf("%q: position line %d column %d offset %d outside the input", c.in, se.Line, se.Column, se.Offset)
			}
		}
	}
}
