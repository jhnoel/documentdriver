package laws

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/pptx/ops"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// The cross-feature laws: the text, path and group features compose on
// one slide. Text on a freeform, text.set on a shape inside a group the
// same plan created, a shape added into that group, a path on a
// picture, hyperlinks surviving group.create and group.ungroup, and the
// duplicate of a group holding a picture and linked text, a picture
// added into a group with a crop and a path, and deleting the groups
// releasing their media while other links stay. Each result is read
// back through the views; untargeted objects keep their bytes.

const (
	crossLinkA = "https://example.invalid/cross-a"
	crossLinkB = "https://example.invalid/cross-b"
	crossRels  = "/ppt/slides/_rels/slide1.xml.rels"
)

// crossObjs reads slide1 of path into objects by id and the objects by
// name (first of each name, in document order).
func crossObjs(t *testing.T, path string) (map[string]rawObj, []rawObj) {
	t.Helper()
	s, err := readRawOnly(path, "slide1")
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 1 {
		t.Fatalf("%d slides read", len(s))
	}
	byID := map[string]rawObj{}
	for _, o := range s[0].objects {
		byID[o.str("id")] = o
	}
	return byID, s[0].objects
}

func normal(t *testing.T, h string) string {
	t.Helper()
	n, err := text.Normalise(h)
	if err != nil {
		t.Fatalf("normalise %q: %v", h, err)
	}
	return n
}

// hyperlinkRels counts the hyperlink relationships of the slide.
func hyperlinkRels(b []byte) int {
	return bytes.Count(b, []byte(`relationships/hyperlink"`))
}

func TestCrossFeature(t *testing.T) {
	src := testdeck.Minimal(t)
	dir := t.TempDir()
	before, _ := crossObjs(t, src)
	for _, id := range []string{"s2", "s3", "s4", "s5"} {
		if before[id] == nil {
			t.Fatalf("fixture lacks %s", id)
		}
	}
	linked := `<p><a href="` + crossLinkA + `">API</a> Gateway</p>`
	linked2 := `<p><a href="` + crossLinkA + `">API</a> Gateway <s>v2</s></p>`
	ffHTML := `<p>free <a href="` + crossLinkB + `">form</a></p>`

	// One plan: link text, a freeform with linked text and body
	// settings, a path on the picture, a group of the linked shape and
	// the picture, text.set and a new shape inside that group, and a
	// duplicate of the group.
	out := filepath.Join(dir, "cross.pptx")
	err := ops.ApplyTo(src, out, plan("slide1",
		map[string]any{"op": "text.set", "at": "s2", "html": linked},
		map[string]any{"op": "object.add", "kind": "shape", "name": "FF", "as": "ff", "box": []float64{100, 300, 200, 100},
			"path": "M0 0 L200 0 L100 100 Z", "html": ffHTML, "text": map[string]any{"anchor": "b", "wrap": "none"}},
		map[string]any{"op": "object.set", "at": "s5", "path": "M0 0 L36 0 L18 36 Z"},
		map[string]any{"op": "group.create", "at": []string{"s2", "s5"}, "name": "G", "as": "g"},
		map[string]any{"op": "text.set", "at": "s2", "html": linked2},
		map[string]any{"op": "object.add", "kind": "shape", "name": "In", "at": "$g", "geom": "rect",
			"box": []float64{80, 150, 50, 20}, "html": "<p>in group</p>", "descr": "inner"},
		map[string]any{"op": "object.duplicate", "at": "$g", "as": "g2", "dy": 200},
	)...)
	if err != nil {
		t.Fatalf("cross plan: %v", err)
	}
	after, objs := crossObjs(t, out)

	// Untargeted: s3 and the connector keep their bytes; every part but
	// the slide and its relationships is byte-identical.
	sb, _ := readRawOnly(src, "slide1")
	sa, _ := readRawOnly(out, "slide1")
	for _, id := range []string{"s3", "s4"} {
		if !bytes.Equal(sb[0].model[id].ElementXML(), sa[0].model[id].ElementXML()) {
			t.Errorf("untargeted %s changed", id)
		}
	}
	ps, pd := parts(t, src), parts(t, out)
	for n, b := range ps {
		if strings.EqualFold(n, "/ppt/slides/slide1.xml") || strings.EqualFold(n, crossRels) {
			continue
		}
		if !bytes.Equal(b, pd[n]) {
			t.Errorf("part %s changed", n)
		}
	}
	// Two links, two relationships: the second text.set shares the first's.
	if got, was := hyperlinkRels(pd[crossRels]), hyperlinkRels(ps[crossRels]); got != was+2 {
		t.Errorf("hyperlink relationships %d -> %d, want +2", was, got)
	}

	// Text on a freeform: path, html and body settings read back.
	var ff, inner rawObj
	for _, o := range objs {
		switch o.str("name") {
		case "FF":
			ff = o
		case "In":
			if inner == nil {
				inner = o
			}
		}
	}
	if ff == nil || inner == nil {
		t.Fatalf("added objects missing: FF %v, In %v", ff != nil, inner != nil)
	}
	if ff.str("geom") != "custom" || ff.str("path") == "" {
		t.Errorf("freeform: geom %q path %q", ff.str("geom"), ff.str("path"))
	}
	if got, want := ff.str("html"), normal(t, ffHTML); got != want {
		t.Errorf("freeform html %q, want %q", got, want)
	}
	slide := pd["/ppt/slides/slide1.xml"]
	if !regexp.MustCompile(`name="FF".*?<a:custGeom>.*?<a:bodyPr[^>]*\banchor="b"`).Match(slide) ||
		!regexp.MustCompile(`name="FF".*?<a:bodyPr[^>]*\bwrap="none"`).Match(slide) {
		t.Errorf("freeform body settings not written")
	}

	// The group, its children and the text set inside it.
	g := after["s2"].str("parent")
	if g == "" || after[g] == nil || after[g].str("kind") != "group" {
		t.Fatalf("s2 parent %q is not a group", g)
	}
	if after["s5"].str("parent") != g || inner.str("parent") != g {
		t.Errorf("group children: s5 in %q, In in %q, want %q", after["s5"].str("parent"), inner.str("parent"), g)
	}
	if got, want := after["s2"].str("html"), normal(t, linked2); got != want {
		t.Errorf("text.set inside the new group: %q, want %q", got, want)
	}
	if inner.str("html") != "<p>in group</p>" || inner.str("descr") != "inner" {
		t.Errorf("shape added into the group: html %q descr %q", inner.str("html"), inner.str("descr"))
	}
	// A path on a picture keeps its image.
	if p := after["s5"]; p.str("geom") != "custom" || p.str("path") == "" || p.str("image") != before["s5"].str("image") {
		t.Errorf("picture path: geom %q path %q image %q", p.str("geom"), p.str("path"), p.str("image"))
	}

	// The duplicate: a second group with the same children, the
	// picture sharing its media and the text keeping its link.
	var dup string
	for _, o := range objs {
		if o.str("kind") == "group" && o.str("id") != g {
			dup = o.str("id")
		}
	}
	if dup == "" {
		t.Fatal("no duplicated group")
	}
	kids := map[string]rawObj{}
	for _, o := range objs {
		if o.str("parent") == dup {
			kids[o.str("kind")+":"+o.str("name")] = o
		}
	}
	if len(kids) != 3 {
		t.Errorf("duplicate holds %d children, want 3", len(kids))
	}
	if p := kids["picture:Logo"]; p == nil || p.str("image") != after["s5"].str("image") || p.str("path") != after["s5"].str("path") {
		t.Errorf("duplicated picture: %v", p)
	}
	if s := kids["shape:API"]; s == nil || s.str("html") != after["s2"].str("html") {
		t.Errorf("duplicated linked text: %v", s)
	}

	// Hyperlinks survive ungroup, and create then ungroup of the linked
	// freeform with another shape; a path set on the linked shape keeps
	// its text. The relationships part is untouched.
	out2 := filepath.Join(dir, "cross2.pptx")
	err = ops.ApplyTo(out, out2, plan("slide1",
		map[string]any{"op": "group.ungroup", "at": g},
		map[string]any{"op": "group.create", "at": []string{ff.str("id"), "s3"}, "name": "H", "as": "h"},
		map[string]any{"op": "group.ungroup", "at": "$h"},
		map[string]any{"op": "object.set", "at": "s2", "path": "M0 0 L180 0 L180 90 L0 90 Z"},
	)...)
	if err != nil {
		t.Fatalf("ungroup plan: %v", err)
	}
	after2, _ := crossObjs(t, out2)
	if p := after2["s2"].str("parent"); p != "" {
		t.Errorf("s2 still in %q after ungroup", p)
	}
	if after2["s2"].str("html") != after["s2"].str("html") || after2["s2"].str("geom") != "custom" {
		t.Errorf("s2 after ungroup and path: html %q geom %q", after2["s2"].str("html"), after2["s2"].str("geom"))
	}
	if after2[ff.str("id")].str("html") != ff.str("html") {
		t.Errorf("freeform link lost through create+ungroup: %q", after2[ff.str("id")].str("html"))
	}
	if !bytes.Equal(parts(t, out2)[crossRels], pd[crossRels]) {
		t.Errorf("relationships changed by ungroup")
	}

	// Pictures with the rest: a picture added into the group, cropped
	// and given a path, then the group duplicated; the copies share the
	// media. Deleting both groups releases the media but keeps the
	// freeform's link.
	png := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(png, pd["/ppt/media/image1.png"], 0o644); err != nil {
		t.Fatal(err)
	}
	out3 := filepath.Join(dir, "cross3.pptx")
	err = ops.ApplyTo(out, out3, plan("slide1",
		map[string]any{"op": "object.add", "kind": "picture", "name": "Pic2", "as": "p", "at": g,
			"box": []float64{150, 40, 40, 40}, "image": png, "crop": []float64{10, 0, 10, 0}, "descr": "logo"},
		map[string]any{"op": "object.set", "at": "$p", "path": "M0 0 L40 0 L20 40 Z"},
		map[string]any{"op": "object.duplicate", "at": g, "as": "g3", "dy": 100},
	)...)
	if err != nil {
		t.Fatalf("picture plan: %v", err)
	}
	_, objs3 := crossObjs(t, out3)
	var pics []rawObj
	for _, o := range objs3 {
		if o.str("name") == "Pic2" {
			pics = append(pics, o)
		}
	}
	if len(pics) != 2 {
		t.Fatalf("%d Pic2 pictures, want the added one and its copy", len(pics))
	}
	for _, p := range pics {
		if p.str("geom") != "custom" || p.str("path") == "" || p.str("descr") != "logo" || len(p["crop"]) == 0 {
			t.Errorf("picture in group: geom %q path %q descr %q crop %s", p.str("geom"), p.str("path"), p.str("descr"), p["crop"])
		}
		if p.str("image") != after["s5"].str("image") {
			t.Errorf("identical media not shared: %q vs %q", p.str("image"), after["s5"].str("image"))
		}
	}
	if pics[0].str("parent") != g || pics[1].str("parent") == g || pics[1].str("parent") == "" {
		t.Errorf("picture parents %q, %q; want %q and the copy's group", pics[0].str("parent"), pics[1].str("parent"), g)
	}
	out4 := filepath.Join(dir, "cross4.pptx")
	err = ops.ApplyTo(out, out4, plan("slide1",
		map[string]any{"op": "object.delete", "at": g},
		map[string]any{"op": "object.delete", "at": dup},
	)...)
	if err != nil {
		t.Fatalf("delete groups: %v", err)
	}
	p4 := parts(t, out4)
	if _, ok := p4["/ppt/media/image1.png"]; ok {
		t.Errorf("media kept after its last pictures were deleted")
	}
	if bytes.Contains(p4[crossRels], []byte("media/image1.png")) || hyperlinkRels(p4[crossRels]) < 1 {
		t.Errorf("relationships after delete: %s", p4[crossRels])
	}
	after4, _ := crossObjs(t, out4)
	if after4[ff.str("id")].str("html") != ff.str("html") {
		t.Errorf("freeform link lost after deleting the groups")
	}

	// Atomicity: the same plan followed by a failing operation writes
	// nothing.
	st, _ := os.Stat(out)
	err = ops.ApplyTo(out, out, plan("slide1",
		map[string]any{"op": "group.ungroup", "at": g},
		map[string]any{"op": "text.set", "at": "s2", "html": linked},
		map[string]any{"op": "object.delete", "at": "s999"},
	)...)
	if err == nil {
		t.Fatal("failing plan succeeded")
	}
	if st2, _ := os.Stat(out); !st2.ModTime().Equal(st.ModTime()) || !bytes.Equal(parts(t, out)["/ppt/slides/slide1.xml"], slide) {
		t.Error("failing plan changed the file")
	}
}
