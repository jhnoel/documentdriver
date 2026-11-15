package htmlx

import (
	"errors"
	"strings"
	"testing"
)

func parseDump(t *testing.T, in string, opts Options) string {
	t.Helper()
	root, err := ParseHTML(in, opts)
	if err != nil {
		t.Fatalf("ParseHTML(%q): %v", in, err)
	}
	return Dump(root)
}

func TestOptionalEndTags(t *testing.T) {
	for in, want := range map[string]string{
		"<p>a<p>b":                      "<p>\n  \"a\"\n<p>\n  \"b\"\n",
		"<ul><li>a<li>b</ul>":           "<ul>\n  <li>\n    \"a\"\n  <li>\n    \"b\"\n",
		"<table><td>a<td>b</table>":     "<table>\n  <tbody>\n    <tr>\n      <td>\n        \"a\"\n      <td>\n        \"b\"\n",
		"<h1>a<h2>b":                    "<h1>\n  \"a\"\n<h2>\n  \"b\"\n",
		"<p>a</p></p>":                  "<p>\n  \"a\"\n<p>\n",
		"<p>a</br>":                     "<p>\n  \"a\"\n  <br>\n",
		"<p><image src=x></image>":      "<p>\n  <img>\n    src=\"x\"\n",
		"<p><span>a</p>b":               "<p>\n  <span>\n    \"a\"\n\"b\"\n",
		"<p>a &amp; &nbsp;&notin; AT&T": "<p>\n  \"a & \u00a0∉ AT&T\"\n",
	} {
		if got := parseDump(t, in, Options{}); got != want {
			t.Errorf("%q:\n%s\nwant:\n%s", in, got, want)
		}
	}
}

func TestDocument(t *testing.T) {
	got := parseDump(t, "<!DOCTYPE html><html><head><title>t</title><style>li > p {}</style></head>\n<body><p>x</p></body></html>\n", Options{Document: true})
	want := "<head>\n  <title>\n    \"t\"\n  <style>\n    \"li > p {}\"\n\"\\n\"\n<body>\n  <p>\n    \"x\"\n  \"\\n\"\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSelfClosing(t *testing.T) {
	opts := Options{SelfClosing: map[string]bool{"marker": true}}
	if got := parseDump(t, "<p><marker/>a<span/>b</p>", opts); got != "<p>\n  <marker>\n  \"a\"\n  <span>\n    \"b\"\n" {
		t.Errorf("got:\n%s", got)
	}
}

func TestRejected(t *testing.T) {
	for in, want := range map[string]string{
		"<p>a<b>b<p>c":               "unsupported HTML structure: <p> closes the open <p> while <b> is still open",
		"<table>x</table>":           "unsupported HTML structure: text directly inside <table> (docxdriver moves it before the table)",
		"<table><p>x</p></table>":    "unsupported HTML structure: <p> directly inside <table> (docxdriver moves it before the table)",
		"<p><b><i>x</b></i></p>":     "unsupported HTML structure: </b> does not close the innermost open element <i>",
		"<a href=1>x<a href=2>y":     "unsupported HTML structure: <a> inside an open <a>",
		"<p><math><mi>x</mi></math>": "unsupported HTML element <math>",
		"<select></select>":          "unsupported HTML element <select>",
		`<p a="1" a="2">x</p>`:       "duplicate HTML attribute a; write it once",
		"<p>x\xff</p>":               "HTML input is not valid UTF-8",
		"<p>x<!-->":                  `ParseDiagnostic { severity: Error, message: "abrupt-closing-of-empty-comment", location: SourceLocation { line: 1, column: 10, byte_offset: 9 } }`,
		"<p>\r\n&bogus;":             `ParseDiagnostic { severity: Error, message: "unknown-named-character-reference", location: SourceLocation { line: 2, column: 8, byte_offset: 12 } }`,
		strings.Repeat("<b>", 200):   "HTML nesting exceeds 128 elements",
	} {
		_, err := ParseHTML(in, Options{})
		if err == nil || err.Error() != want {
			t.Errorf("ParseHTML(%q) = %v, want %q", in, err, want)
		}
	}
	var u *UnsupportedError
	if _, err := ParseHTML("<select>", Options{}); !errors.As(err, &u) {
		t.Errorf("want an UnsupportedError, got %T", err)
	}
}

func FuzzParseHTML(f *testing.F) {
	for _, s := range []string{
		"<p>a<b>b</b></p>", "<table><tr><td>x</table>", "<ul><li>a<li>b</ul>", "<!DOCTYPE html><html><head><style>x</style></head><body><p>x</p></body></html>",
		"<p>&amp;&nbsp;&#x41;</p><!-- c -->", "<p title='a&b'>x</p>",
	} {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, in string, document bool) {
		root, err := ParseHTML(in, Options{Document: document, SelfClosing: map[string]bool{"section": true}})
		if err != nil {
			return
		}
		if d := depthOf(root); d > MaxDepth+1 {
			t.Fatalf("tree %d deep", d)
		}
	})
}

func depthOf(n *Node) int {
	d := 0
	for _, k := range n.Kids {
		d = max(d, depthOf(k))
	}
	return d + 1
}
