package text

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// parseText parses src as a document and returns Parse of its root.
func parseText(t *testing.T, src string) (Body, int) {
	t.Helper()
	doc, err := tree.ParseString(src)
	if err != nil {
		t.Fatalf("ParseString(%q): %v", src, err)
	}
	root := doc.Root()
	if root == nil {
		t.Fatalf("ParseString(%q) has no root", src)
	}
	return Parse(root)
}

// TestParsePlain checks the chunk T1 rows: Parse of a Text element
// source, its Plain text, and its skipped count.
func TestParsePlain(t *testing.T) {
	big := strings.Repeat("a", 2<<20)
	full := strings.Repeat("a", MaxText)
	tests := []struct {
		name     string
		src      string
		want     Body
		plain    string
		wantSkip int
	}{
		{
			name:  "plain",
			src:   "<Text>Hello</Text>",
			want:  Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "Hello", Field: -1}}}}},
			plain: "Hello",
		},
		{
			name: "markers and field",
			src:  "<Text><cp IX='0'/>Title<cp IX='1'/> red\n<pp IX='1'/>second <fld IX='0'>3</fld></Text>",
			want: Body{Paras: []Para{
				{PP: -1, Runs: []Run{
					{CP: 0, Text: "Title", Field: -1},
					{CP: 1, Text: " red", Field: -1},
				}},
				{PP: 1, Runs: []Run{
					{CP: 1, Text: "second ", Field: -1},
					{CP: 1, Text: "3", Field: 0},
				}},
			}},
			plain: "Title red\nsecond 3",
		},
		{
			name:  "terminated",
			src:   "<Text>A\n</Text>",
			want:  Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "A", Field: -1}}}}, Terminated: true},
			plain: "A",
		},
		{
			name: "terminated with empty paragraph",
			src:  "<Text>A\n\n</Text>",
			want: Body{Paras: []Para{
				{PP: -1, Runs: []Run{{CP: -1, Text: "A", Field: -1}}},
				{PP: -1},
			}, Terminated: true},
			plain: "A\n",
		},
		{
			name:  "terminated with cp",
			src:   "<Text><cp IX='0'/>Hello\n</Text>",
			want:  Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: 0, Text: "Hello", Field: -1}}}}, Terminated: true},
			plain: "Hello",
		},
		{
			name: "line break",
			src:  "<Text>a\u2028b</Text>",
			want: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: -1, Text: "a", Field: -1},
				{CP: -1, Br: true, Field: -1},
				{CP: -1, Text: "b", Field: -1},
			}}}},
			plain: "a\nb",
		},
		{
			name:  "tab",
			src:   "<Text>a\tb</Text>",
			want:  Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "a\tb", Field: -1}}}}},
			plain: "a\tb",
		},
		{
			name:  "empty",
			src:   "<Text/>",
			want:  Body{},
			plain: "",
		},
		{
			name:  "paragraph row",
			src:   "<Text><pp IX='2'/>x</Text>",
			want:  Body{Paras: []Para{{PP: 2, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			plain: "x",
		},
		{
			name:     "bad ix and unknown element",
			src:      "<Text><cp IX='x'/>a<foo/>b</Text>",
			want:     Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "ab", Field: -1}}}}},
			plain:    "ab",
			wantSkip: 2,
		},
		{
			name:  "tab marker is not a boundary",
			src:   "<Text><tp IX='0'/>a</Text>",
			want:  Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "a", Field: -1}}}}},
			plain: "a",
		},
		{
			name:     "cut at MaxText",
			src:      "<Text>" + big + "</Text>",
			want:     Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: full, Field: -1}}}}},
			plain:    full,
			wantSkip: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, skipped := parseText(t, tc.src)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q) = %#v; want %#v", tc.src, got, tc.want)
			}
			if plain := Plain(got); plain != tc.plain {
				t.Errorf("Plain(Parse(%q)) = %q; want %q", tc.src, plain, tc.plain)
			}
			if skipped != tc.wantSkip {
				t.Errorf("Parse(%q) skipped = %d; want %d", tc.src, skipped, tc.wantSkip)
			}
		})
	}
}

// FuzzText seeds the corpus with the chunk T1 rows and checks that
// Parse, Plain and Render never panic on any well-formed fragment.
func FuzzText(f *testing.F) {
	seeds := []string{
		"<Text>Hello</Text>",
		"<Text><cp IX='0'/>Title<cp IX='1'/> red\n<pp IX='1'/>second <fld IX='0'>3</fld></Text>",
		"<Text>A\n</Text>",
		"<Text>A\n\n</Text>",
		"<Text><cp IX='0'/>Hello\n</Text>",
		"<Text>a\u2028b</Text>",
		"<Text>a\tb</Text>",
		"<Text/>",
		"<Text><pp IX='2'/>x</Text>",
		"<Text><cp IX='x'/>a<foo/>b</Text>",
		"<Text><tp IX='0'/>a</Text>",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, src string) {
		doc, err := tree.ParseString(src)
		if err != nil {
			return
		}
		root := doc.Root()
		if root == nil {
			return
		}
		b, _ := Parse(root)
		_ = Plain(b)
		_ = Render(b, Formats{}, "page0/s1")
	})
}
