package text

import "testing"

// TestRender checks the chunk T2 rows: Render of a body under the
// given formats for shape "page0/s5" (unless a row says otherwise).
func TestRender(t *testing.T) {
	tests := []struct {
		name    string
		body    Body
		formats Formats
		shape   string
		want    string
	}{
		{
			name: "plain",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "Hello", Field: -1}}}}},
			want: `<p>Hello</p>`,
		},
		{
			name:    "bold cp0",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: 0, Text: "Hello", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "on"}}},
			want:    `<p><b>Hello</b></p>`,
		},
		{
			name:    "bold row0 before any cp",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "on"}}},
			want:    `<p><b>x</b></p>`,
		},
		{
			name:    "bold italic size color",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: 0, Text: "Hi", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "on", Italic: "on", Size: "12", Color: "#FF0000"}}},
			want:    `<p><b><i><span style="font-size:12pt;color:#FF0000" data-color="#FF0000">Hi</span></i></b></p>`,
		},
		{
			name:    "color alone",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Color: "#00FF00"}}},
			want:    `<p><span style="color:#00FF00" data-color="#00FF00">x</span></p>`,
		},
		{
			name:    "bold off",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "off"}}},
			want:    `<p><span style="font-weight:normal">x</span></p>`,
		},
		{
			name:    "para align pp0",
			body:    Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{0: {Align: "center"}}},
			want:    `<p style="text-align:center">x</p>`,
		},
		{
			name:    "para align pp minus1",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{0: {Align: "center"}}},
			want:    `<p style="text-align:center">x</p>`,
		},
		{
			name: "bullet second paragraph",
			body: Body{Paras: []Para{
				{PP: -1, Runs: []Run{{CP: -1, Text: "A", Field: -1}}},
				{PP: 1, Runs: []Run{{CP: -1, Text: "B", Field: -1}}},
			}},
			formats: Formats{Para: map[int]ParaFormat{1: {Bullet: true}}},
			want:    `<p>A</p><ul><li>B</li></ul>`,
		},
		{
			name:    "bullet char",
			body:    Body{Paras: []Para{{PP: 1, Runs: []Run{{CP: -1, Text: "B", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{1: {Bullet: true, BulletChar: "•"}}},
			want:    `<ul><li data-char="•">B</li></ul>`,
		},
		{
			name: "break",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: -1, Text: "a", Field: -1},
				{CP: -1, Br: true, Field: -1},
				{CP: -1, Text: "b", Field: -1},
			}}}},
			want: `<p>a<br/>b</p>`,
		},
		{
			name: "escaped text",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "a<b&c", Field: -1}}}}},
			want: `<p>a&lt;b&amp;c</p>`,
		},
		{
			name: "field",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: -1, Text: "Page ", Field: -1},
				{CP: -1, Text: "3", Field: 0},
			}}}},
			want: `<p>Page <opaque data-ref="page0/s5/field:1" data-kind="field" data-text="3"></opaque></p>`,
		},
		{
			name: "second field",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: -1, Text: "3", Field: 0},
				{CP: -1, Text: "4", Field: 1},
			}}}},
			want: `<p><opaque data-ref="page0/s5/field:1" data-kind="field" data-text="3"></opaque><opaque data-ref="page0/s5/field:2" data-kind="field" data-text="4"></opaque></p>`,
		},
		{
			name: "empty body",
			body: Body{},
			want: "",
		},
		{
			name:    "font and size",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Font: "Arial", Size: "10.5"}}},
			want:    `<p><span style="font-size:10.5pt;font-family:'Arial'">x</span></p>`,
		},
		{
			name: "equal formats merge",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: 0, Text: "a", Field: -1},
				{CP: 1, Text: "b", Field: -1},
			}}}},
			formats: Formats{Char: map[int]CharFormat{0: {}, 1: {}}},
			want:    `<p>ab</p>`,
		},
		{
			name:    "para margins",
			body:    Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{0: {MarginLeft: "18pt", Indent: "-18pt", SpaceBefore: "6pt"}}},
			want:    `<p style="margin-top:6pt;margin-left:18pt;text-indent:-18pt">x</p>`,
		},
		{
			name:    "underline strike",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Underline: "on", Strike: "on"}}},
			want:    `<p><u><s>x</s></u></p>`,
		},
		{
			name:    "font quote",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Font: "O'Neil"}}},
			want:    `<p><span style="font-family:'O\'Neil'">x</span></p>`,
		},
		{
			name:    "italic off",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Italic: "off"}}},
			want:    `<p><span style="font-style:normal">x</span></p>`,
		},
		{
			name:    "underline off",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Underline: "off"}}},
			want:    `<p><span style="text-decoration:none">x</span></p>`,
		},
		{
			name:    "strike off",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Strike: "off"}}},
			want:    `<p><span style="text-decoration:none">x</span></p>`,
		},
		{
			name: "equal formatted runs merge",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: 0, Text: "a", Field: -1},
				{CP: 1, Text: "b", Field: -1},
			}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "on"}, 1: {Bold: "on"}}},
			want:    `<p><b>ab</b></p>`,
		},
		{
			name: "consecutive bullets group",
			body: Body{Paras: []Para{
				{PP: 1, Runs: []Run{{CP: -1, Text: "A", Field: -1}}},
				{PP: 1, Runs: []Run{{CP: -1, Text: "B", Field: -1}}},
				{PP: -1, Runs: []Run{{CP: -1, Text: "C", Field: -1}}},
			}},
			formats: Formats{Para: map[int]ParaFormat{1: {Bullet: true}}},
			want:    `<ul><li>A</li><li>B</li></ul><p>C</p>`,
		},
		{
			name: "empty paragraph",
			body: Body{Paras: []Para{
				{PP: -1, Runs: []Run{{CP: -1, Text: "A", Field: -1}}},
				{PP: -1},
			}},
			want: `<p>A</p><p></p>`,
		},
		{
			name:    "para styles order",
			body:    Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{0: {Align: "right", LineHeight: "120%", SpaceBefore: "6pt", SpaceAfter: "3pt", MarginLeft: "18pt", MarginRight: "9pt", Indent: "-18pt"}}},
			want:    `<p style="text-align:right;line-height:120%;margin-top:6pt;margin-bottom:3pt;margin-left:18pt;margin-right:9pt;text-indent:-18pt">x</p>`,
		},
		{
			name:    "char styles order",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "off", Italic: "off", Underline: "off", Size: "12", Color: "#00FF00", Font: "Arial"}}},
			want:    `<p><span style="font-weight:normal;font-style:normal;text-decoration:none;font-size:12pt;color:#00FF00;font-family:'Arial'" data-color="#00FF00">x</span></p>`,
		},
		{
			name:    "field is bare under a format",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: 0, Text: "3", Field: 0}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Size: "10", Font: "Arial"}}},
			want:    `<p><opaque data-ref="page0/s5/field:1" data-kind="field" data-text="3"></opaque></p>`,
		},
		{
			name:    "font backslash",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Font: `a\b`}}},
			want:    `<p><span style="font-family:'a\\b'">x</span></p>`,
		},
		{
			name:  "v7 golden",
			shape: "page0/s1",
			body: Body{Paras: []Para{
				{PP: 0, Runs: []Run{
					{CP: 0, Text: "Title", Field: -1},
					{CP: 1, Text: " red", Field: -1},
				}},
				{PP: 1, Runs: []Run{
					{CP: 2, Text: "item", Field: -1},
					{CP: 2, Br: true, Field: -1},
					{CP: 2, Text: "two\tend ", Field: -1},
					{CP: 2, Text: "3", Field: 0},
				}},
			}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {}, 1: {Bold: "on", Color: "#FF0000"}, 2: {Size: "10", Font: "Arial"}},
				Para: map[int]ParaFormat{0: {Align: "left"}, 1: {Bullet: true, BulletChar: "\u2022", MarginLeft: "18pt", Indent: "-18pt"}},
			},
			want: "<p style=\"text-align:left\">Title<b><span style=\"color:#FF0000\" data-color=\"#FF0000\"> red</span></b></p><ul><li style=\"margin-left:18pt;text-indent:-18pt\" data-char=\"\u2022\"><span style=\"font-size:10pt;font-family:'Arial'\">item</span><br/><span style=\"font-size:10pt;font-family:'Arial'\">two\tend </span><opaque data-ref=\"page0/s1/field:1\" data-kind=\"field\" data-text=\"3\"></opaque></li></ul>",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shape := tc.shape
			if shape == "" {
				shape = "page0/s5"
			}
			if got := Render(tc.body, tc.formats, shape); got != tc.want {
				t.Errorf("Render(%#v, %#v, %q) = %q; want %q", tc.body, tc.formats, shape, got, tc.want)
			}
		})
	}
}
