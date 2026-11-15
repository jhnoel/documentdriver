package create

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// scopeRefusals are an input refused at each path Scope lists.
var scopeRefusals = map[string]string{
	"html<img>":           `<p><img src="data:image/png;base64,iVBORw0KGgo="></p>`,
	"html<footnote>":      `<p>a<footnote>n</footnote></p>`,
	"html<endnote>":       `<p>a<endnote>n</endnote></p>`,
	"html<comment-start>": `<p>a<comment-start id="1"/>b</p>`,
	"html@class":          `<p class="NoSuchStyle">x</p>`,
	"html@style":          `<table style="color:#FF0000"><tr><td>x</td></tr></table>`,
	"html":                `<p>a<ins>b</ins></p>`,
}

// scopeBuilds are an input of each family Scope says create builds.
var scopeBuilds = []string{
	`<h2>h</h2><p class="Title" style="text-align:center;margin-top:6pt;text-indent:12pt;line-height:1.5">t</p>`,
	`<p><b>b</b> <a href="https://e.com/">l</a><br>&#9;<equation display="inline">x</equation> <page-number/> <page-count/></p>`,
	`<ol start="2" type="a"><li>a<ul><li>b</li></ul></li></ol>`,
	`<section first-page/><header first><p>h</p></header><header even><p>e</p></header><footer><p>f</p></footer><p>a</p><section/><p>b</p>`,
	`<table style="width:50%"><tr><th colspan="2">a</th></tr><tr><td style="padding-left:2pt"><table><tr><td>n</td></tr></table></td><td>b</td></tr></table>`,
	`<equation display="block">a+b</equation>`,
}

var scopePath = regexp.MustCompile(`html[<@]?[a-z-]*>?`)

// TestScope: help create's scope is what create does: each family it
// lists builds, and each path it lists is where a refusal is reported,
// with no path unlisted.
func TestScope(t *testing.T) {
	_, refused, ok := strings.Cut(Scope, "\nRefused")
	if !ok {
		t.Fatal("Scope lists no refusals")
	}
	var listed []string
	for line := range strings.SplitSeq(refused, "\n") {
		if !strings.HasPrefix(line, "  html") {
			continue
		}
		head, _, _ := strings.Cut(strings.TrimSpace(line), "  ")
		listed = append(listed, scopePath.FindAllString(head, -1)...)
	}
	for path, in := range scopeRefusals {
		if !slices.Contains(listed, path) {
			t.Errorf("%s is not listed in Scope", path)
		}
		_, err := Build(in)
		var e *Error
		if !errors.As(err, &e) || e.Code != "invalid_operation" || e.Path != path {
			t.Errorf("%s: %v (%+v), want invalid_operation at %s", in, err, e, path)
		}
	}
	for _, path := range listed {
		if _, ok := scopeRefusals[path]; !ok {
			t.Errorf("Scope lists %s, which no example is refused at", path)
		}
	}
	for _, in := range scopeBuilds {
		created(t, in)
	}
}
