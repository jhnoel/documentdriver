package tree

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// TestNodeSize holds a node to 64 bytes on 64-bit platforms (on wasip1
// pointers are 32-bit, and sizes are not gated).
func TestNodeSize(t *testing.T) {
	if size := reflect.TypeFor[Node]().Size(); strconv.IntSize == 64 && size > 64 {
		t.Fatalf("Node is %d bytes, over 64", size)
	}
}

// TestFarFields checks the elements whose name, prefix or start tag is
// too long for the packed fields, untouched, rebuilt and renamed.
func TestFarFields(t *testing.T) {
	long := strings.Repeat("n", 1<<16+3)
	pfx := strings.Repeat("p", 300)
	in := `<r xmlns:` + pfx + `="urn:p"><` + long + ` a="1">x</` + long + ` ><` + pfx + `:e b="` +
		strings.Repeat("v", 1<<17) + `"  /><` + pfx + `:f>y</` + pfx + `:f></r>`
	doc := mustParse(t, in)
	if got := string(doc.Bytes()); got != in {
		t.Fatal("untouched round trip differs")
	}
	kids := doc.Root().Elements()
	if kids[0].Name() != long || kids[1].Local() != "e" || kids[1].Prefix() != pfx || kids[1].Space() != "urn:p" {
		t.Fatal("names of far fields read wrong")
	}
	if a, _ := kids[0].Attr("a"); a != "1" || string(kids[1].StartTag()) != in[strings.Index(in, "<"+pfx+":e"):strings.Index(in, "/>")+2] {
		t.Fatal("attributes or start tag of far fields read wrong")
	}
	rebuildAll(doc)
	if got := string(doc.Bytes()); got != in {
		t.Fatal("rebuilt round trip differs")
	}
	if err := kids[2].Rename("g"); err != nil {
		t.Fatal(err)
	}
	if err := kids[0].Rename("short"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(strings.Replace(in, `<`+long+` a="1">x</`+long+` >`, `<short a="1">x</short>`, 1), `:f>y</`+pfx+`:f>`, `:g>y</`+pfx+`:g>`, 1)
	if got := string(doc.Bytes()); got != want {
		t.Fatal("renamed far fields written wrong")
	}
}

// TestFarPrefix checks a prefix too long for the packed local offset.
func TestFarPrefix(t *testing.T) {
	pfx := strings.Repeat("q", 1<<16+5)
	in := `<` + pfx + `:a xmlns:` + pfx + `="urn:q"><` + pfx + `:b/></` + pfx + `:a>`
	doc := mustParse(t, in)
	for _, n := range []*Node{doc.Root(), doc.Root().FirstChild()} {
		if n.Prefix() != pfx || n.Space() != "urn:q" || (n.Local() != "a" && n.Local() != "b") {
			t.Fatalf("far prefix read wrong: local %q", n.Local())
		}
	}
	if err := doc.Root().Rename("c"); err != nil {
		t.Fatal(err)
	}
	if want := `<` + pfx + `:c xmlns:` + pfx + `="urn:q"><` + pfx + `:b/></` + pfx + `:c>`; string(doc.Bytes()) != want {
		t.Fatal("renamed far prefix written wrong")
	}
}
