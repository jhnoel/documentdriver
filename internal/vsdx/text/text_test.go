package text

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// parseRoot parses src as a document and returns its root element.
func parseRoot(t *testing.T, src string) *tree.Node {
	t.Helper()
	doc, err := tree.ParseString(src)
	if err != nil {
		t.Fatalf("ParseString(%q): %v", src, err)
	}
	root := doc.Root()
	if root == nil {
		t.Fatalf("ParseString(%q) has no root", src)
	}
	return root
}

// parseText parses src as a document and returns Parse of its root.
func parseText(t *testing.T, src string) (Body, int) {
	t.Helper()
	return Parse(parseRoot(t, src))
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
			name: "newlines and line breaks",
			src:  "<Text>a\nb\u2028c\n\u2028</Text>",
			want: Body{Paras: []Para{
				{PP: -1, Runs: []Run{{CP: -1, Text: "a", Field: -1}}},
				{PP: -1, Runs: []Run{
					{CP: -1, Text: "b", Field: -1},
					{CP: -1, Br: true, Field: -1},
					{CP: -1, Text: "c", Field: -1},
				}},
				{PP: -1, Runs: []Run{{CP: -1, Br: true, Field: -1}}},
			}},
			plain: "a\nb\nc\n\n",
		},
		{
			name:  "three newlines",
			src:   "<Text>\n\n\n</Text>",
			want:  Body{Paras: []Para{{PP: -1}, {PP: -1}, {PP: -1}}, Terminated: true},
			plain: "\n\n",
		},
		{
			name:  "two line breaks",
			src:   "<Text>\u2028\u2028</Text>",
			want:  Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Br: true, Field: -1}, {CP: -1, Br: true, Field: -1}}}}},
			plain: "\n\n",
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

// TestParseScaling checks chunk T1F rows 4 and 5: Parse of a Text
// holding N = 50,000 and 4N = 200,000 separators (newlines, U+2028
// line separators, and alternating "x\n" pairs) costs, at four
// times the units, at most 5.5 times the bytes and allocations
// (laws.Measure), and the time through laws.TimeScales (50 separators
// per unit, so laws.Small and laws.Large units are 50,000 and 200,000);
// a quadratic scan for U+2028 after each newline would cost about 16
// times the time.
func TestParseScaling(t *testing.T) {
	cases := []struct {
		name string
		unit string
	}{
		{"newlines", "\n"},
		{"line separators", "\u2028"},
		{"alternating x and newlines", "x\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var costs [2]laws.Cost
			for i, n := range []int{50000, 200000} {
				root := parseRoot(t, "<Text>"+strings.Repeat(tc.unit, n)+"</Text>")
				run := func() error {
					_, _ = Parse(root)
					return nil
				}
				if err := run(); err != nil { // warm: first-use caches
					t.Fatalf("%s at %d: %v", tc.name, n, err)
				}
				cost, err := laws.Measure(5, run)
				if err != nil {
					t.Fatalf("%s at %d: %v", tc.name, n, err)
				}
				costs[i] = cost
			}
			bytes := float64(costs[1].Bytes) / float64(max(costs[0].Bytes, 1))
			mallocs := float64(costs[1].Mallocs) / float64(max(costs[0].Mallocs, 1))
			t.Logf("%s: 50000 units %d bytes %d allocs, 200000 units %d bytes %d allocs: ratio bytes %.2f, allocs %.2f",
				tc.name, costs[0].Bytes, costs[0].Mallocs, costs[1].Bytes, costs[1].Mallocs, bytes, mallocs)
			if bytes > 5.5 {
				t.Errorf("%s allocates %.2f times the bytes for 4 times the input (budget 5.5): super-linear", tc.name, bytes)
			}
			if mallocs > 5.5 {
				t.Errorf("%s makes %.2f times the allocations for 4 times the input (budget 5.5): super-linear", tc.name, mallocs)
			}
			laws.TimeScales(t, tc.name, func(n int) (func() error, error) {
				root := parseRoot(t, "<Text>"+strings.Repeat(tc.unit, n*50)+"</Text>")
				return func() error {
					_, _ = Parse(root)
					return nil
				}, nil
			})
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
