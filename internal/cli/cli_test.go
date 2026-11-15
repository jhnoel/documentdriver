package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestParseJSON(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{`{"a":1,"a":2}`, `duplicate key "a"`},
		{`{"a":01}`, "leading zero"},
		{`{"a":1} x`, "unexpected text"},
		{`{"a":"\ud800"}`, "unpaired surrogate"},
		{`[1,]`, "expected a JSON value"},
		{strings.Repeat("[", MaxDepth+1), "nested more than"},
		{"\xff", "not valid UTF-8"},
		{`{"a":tru}`, "expected a JSON value"},
	} {
		_, err := ParseJSON([]byte(c.text))
		if err == nil || err.Code != CodeInvalidJSON || !strings.Contains(err.Message, c.want) {
			t.Errorf("%q: got %v, want invalid_json %q", c.text, err, c.want)
		}
	}
	v, err := ParseJSON([]byte(`{"s":"aé😀\n","n":-1.5e3,"l":[true,null]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Get("s").Str; got != "aé😀\n" {
		t.Errorf("string = %q", got)
	}
	if got := v.Get("n").Num; got != "-1.5e3" {
		t.Errorf("number = %q", got)
	}
	// The grammar agrees with encoding/json on what is valid.
	for _, s := range []string{`1e5`, `"x"`, `{"a":{"b":[1,2,{"c":null}]}}`, `-0`, `0.0`, `1.`, `.5`, `+1`, `"\x"`, `{"a" 1}`, `{,}`, `nul`} {
		_, err := ParseJSON([]byte(s))
		if json.Valid([]byte(s)) != (err == nil) {
			t.Errorf("%q: valid=%v, ParseJSON err=%v", s, json.Valid([]byte(s)), err)
		}
	}
}

type opt[T any] struct {
	V   T
	Set bool
}

func (o *opt[T]) Slot() any { return &o.V }
func (o *opt[T]) MarkSet()  { o.Set = true }

type inner struct {
	W float64 `json:"w"`
}

type embedded struct {
	Box opt[[2]float64] `json:"box"`
}

type spec struct {
	At   string      `json:"at"`
	N    uint8       `json:"n" plan:"optional"`
	Name opt[string] `json:"name"`
	embedded
	In   *inner          `json:"in"`
	List []inner         `json:"list" plan:"optional"`
	Any  map[string]any  `json:"any" plan:"optional"`
	Raw  json.RawMessage `json:"raw" plan:"optional"`
}

func TestDecodeCollectsErrorsWithPaths(t *testing.T) {
	text := []byte(`{"op":"x","n":300,"name":null,"box":[1],"in":{"w":"a","z":1},"list":[{"w":1},{}],"bogus":1}`)
	v, perr := ParseJSON(text)
	if perr != nil {
		t.Fatal(perr)
	}
	d := NewDecoder(text)
	var s spec
	d.Struct(v, "ops[0]", &s, "op")
	var got []string
	for _, e := range d.Errs() {
		if e.Code != CodeInvalidPlan || e.Span == nil {
			t.Errorf("%v: want invalid_plan with a span", e)
		}
		got = append(got, e.Path)
	}
	want := "ops[0].n ops[0].name ops[0].box ops[0].in.w ops[0].in.z ops[0].list[1].w ops[0].bogus ops[0].at"
	if strings.Join(got, " ") != want {
		t.Errorf("paths:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
}

func TestDecodeValues(t *testing.T) {
	text := []byte(`{"at":"a","name":"n","box":[1,2.5],"in":{"w":3},"any":{"k":[1,"x",true]},"raw":{"a": 1}}`)
	v, _ := ParseJSON(text)
	d := NewDecoder(text)
	var s spec
	d.Struct(v, "", &s)
	if err := d.Err(); err != nil {
		t.Fatal(err)
	}
	if s.At != "a" || !s.Name.Set || s.Name.V != "n" || !s.Box.Set || s.Box.V != [2]float64{1, 2.5} || s.In.W != 3 {
		t.Errorf("decoded %+v", s)
	}
	if string(s.Raw) != `{"a": 1}` {
		t.Errorf("raw = %s", s.Raw)
	}
	if l := s.Any["k"].([]any); len(l) != 3 || l[1] != "x" {
		t.Errorf("any = %v", s.Any)
	}
}

func TestParsePlan(t *testing.T) {
	base := "sha256:" + strings.Repeat("0", 64)
	env, d := ParsePlan([]byte(`{"base":"`+base+`","contract":1,"units":"pt","ops":[{"op":"a.b"},{"op":"c.d","x":1}]}`), "units")
	if err := d.Err(); err != nil {
		t.Fatal(err)
	}
	if env.Base != base || len(env.Ops) != 2 || env.Names[1] != "c.d" || env.Settings["units"].Str != "pt" {
		t.Errorf("envelope %+v", env)
	}
	for _, c := range []struct{ plan, path string }{
		{`{"ops":[]}`, "ops"},
		{`{}`, "ops"},
		{`{"ops":[{"op":"a"}],"slide":"s"}`, "slide"},
		{`{"ops":[{"op":"a"}],"base":"abc"}`, "base"},
		{`{"ops":[{"op":"a"}],"contract":2}`, "contract"},
		{`{"ops":[{}]}`, "ops[0].op"},
		{`{"ops":[1]}`, "ops[0]"},
		{`[]`, ""},
	} {
		_, d := ParsePlan([]byte(c.plan), "units")
		es := d.Errs()
		if len(es) != 1 || es[0].Code != CodeInvalidPlan || es[0].Path != c.path {
			t.Errorf("%s: got %v, want invalid_plan at %q", c.plan, es, c.path)
		}
	}
	if e := CheckBase(base, []byte("x")); e == nil || e.Code != CodeStaleBase {
		t.Errorf("CheckBase = %v", e)
	}
	if e := CheckBase(Hash([]byte("x")), []byte("x")); e != nil {
		t.Errorf("CheckBase on the same bytes = %v", e)
	}
}

func testApp() *App {
	read := &Command{Name: "read", Args: []string{"file"}, JSON: true, Summary: "read it",
		Flags: []Flag{{Name: "view", Value: "view", Enum: []string{"a", "b"}, Help: "the view"}, {Name: "deep", Help: "more"}, OutFlag()}}
	return &App{Tool: "tool", Version: "0.1.0", Summary: "A tool.", Commands: []*Command{read},
		Handlers: map[string]Handler{"read": func(p *Parsed, w io.Writer) error {
			if p.Args[0] == "missing" {
				return Errorf(CodeIO, "", "no such file")
			}
			return WriteJSON(w, []byte(`{"view":"`+p.Get("view")+`","out":"`+p.Get("out")+`"}`), p.Has("pretty"))
		}},
		Topics: map[string]func() string{"ops": func() string { return "the ops\n" }}}
}

func TestAppRun(t *testing.T) {
	for _, c := range []struct {
		args         []string
		exit         int
		stdout, code string
		path         string
	}{
		{[]string{"read", "f", "--view", "a", "-o", "x"}, 0, `{"view":"a","out":"x"}` + "\n", "", ""},
		{[]string{"read", "--view=b", "f"}, 0, `{"view":"b","out":""}` + "\n", "", ""},
		{[]string{"read", "f", "--pretty"}, 0, "{\n  \"view\": \"\",\n  \"out\": \"\"\n}\n", "", ""},
		{[]string{"read", "--", "-f"}, 0, `{"view":"","out":""}` + "\n", "", ""},
		{[]string{"read", "f", "--view", "c"}, 2, "", "usage", "--view"},
		{[]string{"read", "f", "--nope"}, 2, "", "usage", "--nope"},
		{[]string{"read", "f", "--deep=1"}, 2, "", "usage", "--deep"},
		{[]string{"read", "f", "--view"}, 2, "", "usage", "--view"},
		{[]string{"read", "f", "--deep", "--deep"}, 2, "", "usage", "--deep"},
		{[]string{"read"}, 2, "", "usage", ""},
		{[]string{"read", "a", "b"}, 2, "", "usage", ""},
		{[]string{"nope"}, 2, "", "usage", ""},
		{nil, 2, "", "usage", ""},
		{[]string{"read", "missing"}, 1, "", "io", ""},
		{[]string{"--version"}, 0, `{"tool":"tool","version":"0.1.0","contract":1}` + "\n", "", ""},
		{[]string{"help", "ops"}, 0, "the ops\n", "", ""},
		{[]string{"help", "zzz"}, 2, "", "usage", ""},
	} {
		var stdout, stderr bytes.Buffer
		exit := testApp().Run(c.args, &stdout, &stderr)
		if exit != c.exit || stdout.String() != c.stdout {
			t.Errorf("%v: exit %d stdout %q, want %d %q (stderr %s)", c.args, exit, stdout.String(), c.exit, c.stdout, stderr.String())
		}
		if c.code == "" {
			if stderr.Len() != 0 {
				t.Errorf("%v: stderr %q on success", c.args, stderr.String())
			}
			continue
		}
		var got struct {
			Errors []Error `json:"errors"`
		}
		if err := json.Unmarshal(stderr.Bytes(), &got); err != nil || len(got.Errors) != 1 {
			t.Errorf("%v: stderr is not one errors object: %q", c.args, stderr.String())
			continue
		}
		if got.Errors[0].Code != c.code || got.Errors[0].Path != c.path {
			t.Errorf("%v: error %+v, want %s at %q", c.args, got.Errors[0], c.code, c.path)
		}
	}
	for _, args := range [][]string{{"help"}, {"--help"}, {"read", "--help"}, {"help", "read"}} {
		var stdout, stderr bytes.Buffer
		if exit := testApp().Run(args, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "usage: tool") || stderr.Len() != 0 {
			t.Errorf("%v: exit %d, stdout %q", args, exit, stdout.String())
		}
	}
}

func FuzzParseJSON(f *testing.F) {
	for _, s := range []string{`{"ops":[{"op":"a","x":[1,2.5e3,"sé"]}]}`, `[`, `{"a":1,"a":2}`, `"😀"`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := ParseJSON([]byte(s))
		if (err == nil) != (v != nil) {
			t.Fatalf("value and error disagree")
		}
		if err == nil && !json.Valid([]byte(s)) {
			t.Fatalf("accepted JSON that encoding/json refuses: %q", s)
		}
	})
}

func TestSnake(t *testing.T) {
	for _, c := range [][2]string{
		{"flipH", "flip_h"}, {"txBox", "tx_box"}, {"paraId", "para_id"}, {"lnSpcReduction", "ln_spc_reduction"},
		{"forceAA", "force_aa"}, {"noChangeAspect", "no_change_aspect"}, {"rtlCol", "rtl_col"}, {"anchor", "anchor"},
		{"spcFirstLastPara", "spc_first_last_para"}, {"fromWordArt", "from_word_art"}, {"lumMod", "lum_mod"},
		{"svgMedia", "svg_media"}, {"text.lvl1.marL", "text.lvl1.mar_l"}, {"HTMLParser", "html_parser"}, {"adj1", "adj1"},
	} {
		if got := Snake(c[0]); got != c[1] {
			t.Errorf("Snake(%q) = %q, want %q", c[0], got, c[1])
		}
	}
	if got := Camel("no_change_aspect"); got != "noChangeAspect" {
		t.Errorf("Camel = %q", got)
	}
	if n, ok := Unsnake("no_grp", []string{"noRot", "noGrp"}); !ok || n != "noGrp" {
		t.Errorf("Unsnake = %q, %v", n, ok)
	}
}
