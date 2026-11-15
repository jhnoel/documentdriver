package laws

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/ops"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// The picture laws: the picture codecs (image, svg, crop, fit, fillRect,
// locks) agree with the view, write back as shown without a change,
// read back as set, and the media behind them is shared and collected
// as the design says.

// testImages writes small images authored here: PNG, JPEG and GIF of
// distinct sizes, a second PNG, and an SVG.
func testImages(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	mk := func(w, h int, c color.RGBA) image.Image {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				img.Set(x, y, c)
			}
		}
		return img
	}
	out := map[string]string{}
	write := func(name string, enc func(f *os.File) error) {
		p := filepath.Join(dir, name)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := enc(f); err != nil {
			t.Fatal(err)
		}
		f.Close()
		out[name] = p
	}
	write("a.png", func(f *os.File) error { return png.Encode(f, mk(4, 3, color.RGBA{0x44, 0x72, 0xC4, 0xFF})) })
	write("b.png", func(f *os.File) error { return png.Encode(f, mk(5, 2, color.RGBA{0xED, 0x7D, 0x31, 0xFF})) })
	write("c.jpeg", func(f *os.File) error { return jpeg.Encode(f, mk(8, 6, color.RGBA{0x70, 0xAD, 0x47, 0xFF}), nil) })
	write("d.gif", func(f *os.File) error { return gif.Encode(f, mk(3, 3, color.RGBA{0xFF, 0xC0, 0x00, 0xFF}), nil) })
	write("e.svg", func(f *os.File) error {
		_, err := f.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="30" viewBox="0 0 40 30"><rect width="40" height="30" fill="#4472C4"/></svg>`)
		return err
	})
	return out
}

// pkgRefs resolves a slide's relationships for reading the codecs
// outside a transaction.
type pkgRefs struct {
	p     *opc.Package
	slide string
}

func (r pkgRefs) Target(rid string) (string, bool) {
	rels, err := r.p.Rels(r.slide)
	if err != nil {
		return "", false
	}
	rel, ok := rels.Get(rid)
	if !ok || rel.External() {
		return "", false
	}
	t, err := rels.TargetPart(rel)
	return t, err == nil
}

func (pkgRefs) Embed(string) (string, error) { return "", errors.New("read only") }

// picCodecShows is what the picture codecs show of a picture, in the
// view's JSON form, by view key.
func picCodecShows(h *edit.Handle, refs prop.MediaRefs) map[string]string {
	out := map[string]string{}
	if v, ok := prop.Image(refs).Read(h, 0).Get(); ok {
		out["image"] = v
	}
	if v, ok := prop.SVG(refs).Read(h, 0).Get(); ok && v != "" {
		out["svg"] = v
	}
	if f, ok := prop.PicFill().Read(h, 0).Get(); ok {
		if f.Crop != nil {
			out["crop"] = string(show.Rect(*f.Crop))
		}
		if f.Fit != "" {
			out["fit"] = f.Fit
		}
		if f.FillRect != nil {
			out["fill_rect"] = string(show.Rect(*f.FillRect))
		}
	}
	if l, ok := prop.Locks().Read(h, 0).Get(); ok && len(l) > 0 {
		out["locks"] = string(show.Locks(l))
	}
	return out
}

// picViewShows is what the view shows of the same.
func picViewShows(o rawObj) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"image", "svg", "fit"} {
		if _, ok := o[k]; ok {
			out[k] = o.str(k)
		}
	}
	for _, k := range []string{"crop", "fill_rect", "locks"} {
		if v, ok := o[k]; ok {
			out[k] = compact(v)
		}
	}
	return out
}

// pictureKeys are the view keys object.set takes back for a picture.
var pictureKeys = []string{"image", "svg", "crop", "fit", "fill_rect", "locks"}

type picCounts struct {
	decks, pictures, paired, identity, inverse, inexpressible int
	replaced, shared, collected, kept, added, atomic          int
	deleted, delCollected, delKept, addDeleteNoop             int
}

func (c *picCounts) String() string {
	return fmt.Sprintf("decks %d, pictures %d; codec=view fields %d; identity fields %d; inverse sets %d (+%d inexpressible or branch-refused); replace plans %d (media shared after duplicate %d, old media collected %d, kept because referenced %d); deletes %d (media collected %d, kept because referenced %d); adds %d; add+delete byte-identical %d; atomic %d",
		c.decks, c.pictures, c.paired, c.identity, c.inverse, c.inexpressible, c.replaced, c.shared, c.collected, c.kept, c.deleted, c.delCollected, c.delKept, c.added, c.addDeleteNoop, c.atomic)
}

// releasedMedia compares a plan's slide relationships before and after:
// nil when they are unchanged; otherwise every relationship after must
// have been there before, and the result holds the parts (lower case)
// that went from the package, each of which must be media no
// relationship targets any more.
func releasedMedia(t *testing.T, src, dst, slidePart string) map[string]bool {
	t.Helper()
	ps, pd := parts(t, src), parts(t, dst)
	rn := opc.RelsName(slidePart)
	if bytes.Equal(ps[rn], pd[rn]) {
		for n := range ps {
			if _, ok := pd[n]; !ok {
				t.Errorf("part %s removed, the slide's relationships unchanged", n)
			}
		}
		return nil
	}
	rels := func(path string) map[string]opc.Relationship {
		p, err := opc.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		r, _ := p.Rels(slidePart)
		out := map[string]opc.Relationship{}
		for _, x := range r.All() {
			out[x.ID] = x
		}
		return out
	}
	before, after := rels(src), rels(dst)
	for id, r := range after {
		if before[id] != r {
			t.Errorf("relationship %s %v is new or changed", id, r)
		}
	}
	out := map[string]bool{}
	for n := range ps {
		if _, ok := pd[n]; ok {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(n), "/ppt/media/") || anyTargets(t, dst, n) {
			t.Errorf("part %s removed", n)
		}
		out[strings.ToLower(n)] = true
	}
	return out
}

// anyTargets reports whether a relationship of any part targets part.
func anyTargets(t *testing.T, path, part string) bool {
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, name := range p.Names() {
		dir, file := filepath.Split(name)
		if !strings.HasSuffix(dir, "/_rels/") {
			continue
		}
		rels, err := p.Rels(strings.TrimSuffix(dir, "_rels/") + strings.TrimSuffix(file, ".rels"))
		if err != nil {
			continue
		}
		for _, r := range rels.All() {
			if tp, err := rels.TargetPart(r); err == nil && !r.External() && strings.EqualFold(tp, part) {
				return true
			}
		}
	}
	return false
}

// checkPictureDeck runs the picture laws on one deck; budget bounds the
// file-writing plans per deck.
func checkPictureDeck(t *testing.T, src string, imgs map[string]string, budget int, c *picCounts) {
	path := private(t, src)
	slides, err := readRaw(path)
	if err != nil {
		t.Logf("unreadable: %v", err)
		return
	}
	type pic struct {
		slide, part string
		o           rawObj
	}
	var pics []pic
	for _, s := range slides {
		for _, o := range s.objects {
			if o.str("kind") == "picture" {
				pics = append(pics, pic{s.name, s.part, o})
			}
		}
	}
	c.decks++
	c.pictures += len(pics)

	// Pairing: each codec shows what the view shows.
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w := edit.New(p)
	var targetable []pic
	for _, pc := range pics {
		ref, err := w.Resolve(pc.slide + "#" + strings.TrimPrefix(pc.o.str("id"), "s"))
		if err != nil {
			continue
		}
		h, err := w.Target(ref)
		if err != nil {
			continue
		}
		cs, vs := picCodecShows(h, pkgRefs{p, pc.part}), picViewShows(pc.o)
		for k, v := range vs {
			if cs[k] != v {
				t.Errorf("%s %s %s: codec shows %q, view shows %q", pc.slide, pc.o.str("id"), k, cs[k], v)
				continue
			}
			c.paired++
		}
		for k, v := range cs {
			if _, ok := vs[k]; !ok && k != "image" { // a linked image is shown by its URL, not read by the codec
				t.Errorf("%s %s %s: codec shows %q, the view shows nothing", pc.slide, pc.o.str("id"), k, v)
			}
		}
		targetable = append(targetable, pc)
	}
	p.Close()

	// Identity: every picture field written back as shown, all in one
	// transaction: bytes and mtime unchanged.
	before, _ := os.ReadFile(path)
	info0, _ := os.Stat(path)
	tx, err := ops.Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, pc := range targetable {
		spec := map[string]any{"op": "object.set", "at": pc.o.str("id")}
		for _, k := range pictureKeys {
			if v, ok := pc.o[k]; ok {
				spec[k] = v
				c.identity++
			}
		}
		if _, ok := pc.o["image"]; ok {
			if _, ok := pc.o["svg"]; !ok {
				spec["svg"] = "none"
			}
		}
		for _, op := range plan(pc.slide, spec) {
			if err := tx.Run(op); err != nil {
				t.Errorf("%s %s: identity write: %v", pc.slide, pc.o.str("id"), err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("identity plan: %v", err)
	}
	after, _ := os.ReadFile(path)
	info1, _ := os.Stat(path)
	if !bytes.Equal(before, after) || !info0.ModTime().Equal(info1.ModTime()) {
		t.Errorf("identity plan changed the file")
	}

	if len(targetable) > budget {
		targetable = targetable[:budget]
	}
	for _, pc := range targetable {
		pictureOps(t, path, pc.slide, pc.part, pc.o, imgs, c)
	}
	if len(slides) > 0 {
		addPictures(t, path, slides[0].name, slides[0].part, imgs, c)
	}
}

// pictureOps runs the per-picture plans: inverse sets, image replace
// with media sharing and collection, and atomicity.
func pictureOps(t *testing.T, path, slide, part string, o rawObj, imgs map[string]string, c *picCounts) {
	id := o.str("id")
	dir := t.TempDir()
	refused := func(err error) bool {
		return errors.Is(err, prop.ErrInexpressible) || errors.Is(err, edit.ErrBranch) || errors.Is(err, prop.ErrReadOnly) || errors.Is(err, prop.ErrNotDerived)
	}

	// Inverse: new crop, fit and fill rectangle and locks read back as set.
	locks := map[string]any{"no_grp": true, "no_change_aspect": !strings.Contains(compact(o["locks"]), "no_change_aspect")}
	spec := map[string]any{"op": "object.set", "at": id, "crop": []float64{5, 6.5, 7, 8.125}, "fit": "stretch", "fill_rect": []float64{1, 2, 3, 4}, "locks": locks}
	dst := filepath.Join(dir, "inverse.pptx")
	err := ops.ApplyTo(path, dst, plan(slide, spec)...)
	switch {
	case refused(err):
		c.inexpressible++
	case err != nil:
		t.Errorf("%s %s inverse: %v", slide, id, err)
	default:
		got := viewObj(t, dst, slide, id)
		wantLocks := `"no_grp":true`
		for k, want := range map[string]string{"crop": "[5,6.5,7,8.125]", "fit": `"stretch"`, "fill_rect": "[1,2,3,4]"} {
			if g := compact(got[k]); g != want {
				t.Errorf("%s %s inverse %s: view shows %s, want %s", slide, id, k, g, want)
			}
		}
		if g := compact(got["locks"]); !strings.Contains(g, wantLocks) || strings.Contains(g, "no_change_aspect") != locks["no_change_aspect"].(bool) {
			t.Errorf("%s %s inverse locks: view shows %s, want %v", slide, id, g, locks)
		}
		checkUntargeted(t, path, dst, slide, func(_, _ map[string]*model.Object) map[string]bool { return map[string]bool{id: true} })
		c.inverse++
	}

	img := o.str("image")
	if img == "" || !strings.HasPrefix(img, "/") || !hasPart(t, path, img) {
		return
	}

	// Delete: the picture's media goes exactly when nothing else refers
	// to it; nothing else changes.
	dst = filepath.Join(dir, "delete.pptx")
	err = ops.ApplyTo(path, dst, plan(slide, map[string]any{"op": "object.delete", "at": id})...)
	switch {
	case refused(err):
		c.inexpressible++
	case err != nil:
		t.Errorf("%s %s delete: %v", slide, id, err)
	default:
		c.deleted++
		stillUsed := referencedAfter(t, path, slide, id, img)
		switch has := hasPart(t, dst, img); {
		case has && !stillUsed:
			t.Errorf("%s %s delete: %s is no longer referenced but was kept", slide, id, img)
		case !has && stillUsed:
			t.Errorf("%s %s delete: %s is still referenced but was removed", slide, id, img)
		case has:
			c.delKept++
		default:
			c.delCollected++
		}
		checkUntargeted(t, path, dst, slide, func(_, _ map[string]*model.Object) map[string]bool { return map[string]bool{id: true} })
	}

	// Replace: duplicate, replace the copy's image; the original keeps its
	// image, the copy shows the new one, the old media part stays.
	newImg := map[string]any{"image": imgs["b.png"]}
	if _, ok := o["svg"]; ok {
		newImg["svg"] = imgs["e.svg"]
	}
	dst = filepath.Join(dir, "replace-copy.pptx")
	spec = map[string]any{"op": "object.set", "at": "$c"}
	maps.Copy(spec, newImg)
	err = ops.ApplyTo(path, dst, plan(slide, map[string]any{"op": "object.duplicate", "at": id, "as": "c"}, spec)...)
	switch {
	case refused(err):
		c.inexpressible++
	case err != nil:
		t.Errorf("%s %s duplicate+replace: %v", slide, id, err)
	default:
		if got := viewObj(t, dst, slide, id); got.str("image") != img {
			t.Errorf("%s %s: replacing the copy's image changed the original's to %s", slide, id, got.str("image"))
		}
		if !hasPart(t, dst, img) {
			t.Errorf("%s %s: the shared media %s was removed", slide, id, img)
		} else {
			c.shared++
		}
	}

	// Replace in place: the old media part goes exactly when nothing else
	// refers to it; every other part and object is unchanged.
	dst = filepath.Join(dir, "replace.pptx")
	spec = map[string]any{"op": "object.set", "at": id}
	maps.Copy(spec, newImg)
	err = ops.ApplyTo(path, dst, plan(slide, spec)...)
	switch {
	case refused(err):
		c.inexpressible++
		return
	case err != nil:
		t.Errorf("%s %s replace: %v", slide, id, err)
		return
	}
	c.replaced++
	got := viewObj(t, dst, slide, id)
	if gi := got.str("image"); gi == img || !strings.HasPrefix(gi, "/ppt/media/") {
		t.Errorf("%s %s replace: view shows image %s", slide, id, gi)
	}
	stillUsed := referencedAfter(t, path, slide, id, img)
	switch has := hasPart(t, dst, img); {
	case has && !stillUsed:
		t.Errorf("%s %s replace: %s is no longer referenced but was kept", slide, id, img)
	case !has && stillUsed:
		t.Errorf("%s %s replace: %s is still referenced but was removed", slide, id, img)
	case has:
		c.kept++
	default:
		c.collected++
	}
	checkMediaUntargeted(t, path, dst, slide, part, id, img, o.str("svg"))

	// Atomicity: the replace followed by a failing operation writes
	// nothing.
	b0, _ := os.ReadFile(path)
	st0, _ := os.Stat(path)
	if err := ops.Apply(path, append(plan(slide, spec), plan(slide, map[string]any{"op": "object.move", "at": "s4294967295", "dx": 1})...)...); err == nil {
		t.Errorf("%s %s: failing plan succeeded", slide, id)
	}
	b1, _ := os.ReadFile(path)
	st1, _ := os.Stat(path)
	if !bytes.Equal(b0, b1) || !st0.ModTime().Equal(st1.ModTime()) {
		t.Errorf("%s %s: failed picture plan changed the file", slide, id)
	} else {
		c.atomic++
	}
}

// addPictures adds a PNG, the same PNG again, a JPEG, a GIF and an SVG
// with its PNG fallback, and checks the media parts, relationships and
// content types they leave.
func addPictures(t *testing.T, path, slide, part string, imgs map[string]string, c *picCounts) {
	dst := filepath.Join(t.TempDir(), "add.pptx")
	add := func(alias, image string, extra map[string]any) map[string]any {
		m := map[string]any{"op": "object.add", "kind": "picture", "as": alias, "name": alias, "box": []float64{10, 10, 40, 30}, "image": image, "fit": "stretch", "geom": "rect"}
		maps.Copy(m, extra)
		return m
	}
	err := ops.ApplyTo(path, dst, plan(slide,
		add("p1", imgs["a.png"], nil),
		add("p2", imgs["a.png"], map[string]any{"locks": map[string]any{"no_change_aspect": true}}),
		add("p3", imgs["c.jpeg"], map[string]any{"crop": []float64{10, 0, 0, 0}}),
		add("p4", imgs["d.gif"], nil),
		add("p5", imgs["b.png"], map[string]any{"svg": imgs["e.svg"]}),
	)...)
	if errors.Is(err, edit.ErrAmbiguousPlace) {
		return // a shape tree holding elements the schema table does not know
	}
	if err != nil {
		t.Errorf("%s add pictures: %v", slide, err)
		return
	}
	// Add then delete in one plan (the five pictures, deleted in another
	// order, one through a duplicate too): the file is untouched, no media,
	// relationship or content-type Default left behind.
	b0, _ := os.ReadFile(path)
	st0, _ := os.Stat(path)
	if err := ops.Apply(path, plan(slide,
		add("p1", imgs["a.png"], nil),
		add("p2", imgs["a.png"], nil),
		add("p3", imgs["c.jpeg"], nil),
		add("p4", imgs["d.gif"], nil),
		add("p5", imgs["b.png"], map[string]any{"svg": imgs["e.svg"]}),
		map[string]any{"op": "object.duplicate", "at": "$p5", "as": "p6"},
		map[string]any{"op": "object.delete", "at": "$p5"},
		map[string]any{"op": "object.delete", "at": "$p1"},
		map[string]any{"op": "object.delete", "at": "$p4"},
		map[string]any{"op": "object.delete", "at": "$p6"},
		map[string]any{"op": "object.delete", "at": "$p2"},
		map[string]any{"op": "object.delete", "at": "$p3"},
	)...); err != nil {
		t.Errorf("%s add+delete pictures: %v", slide, err)
	}
	b1, _ := os.ReadFile(path)
	st1, _ := os.Stat(path)
	if !bytes.Equal(b0, b1) || !st0.ModTime().Equal(st1.ModTime()) {
		t.Errorf("%s: adding and deleting pictures in one plan changed the file", slide)
	} else {
		c.addDeleteNoop++
	}
	c.added++
	slides, err := readRaw(dst)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]rawObj{}
	for _, s := range slides {
		if s.name == slide {
			for _, o := range s.objects {
				by[o.str("name")] = o
			}
		}
	}
	if by["p1"].str("image") == "" || by["p1"].str("image") != by["p2"].str("image") {
		t.Errorf("%s: the same PNG added twice is %s and %s, want one shared part", slide, by["p1"].str("image"), by["p2"].str("image"))
	}
	for name, want := range map[string]string{"p1": `"image/png"`, "p3": `"image/jpeg"`, "p4": `"image/gif"`, "p5": `"image/png"`} {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(by[name]["media"], &m)
		if string(m["type"]) != want || m["px"] == nil {
			t.Errorf("%s %s: media %s, want type %s with px", slide, name, compact(by[name]["media"]), want)
		}
	}
	if got := by["p5"].str("svg"); !strings.HasSuffix(got, ".svg") {
		t.Errorf("%s p5: svg %q", slide, got)
	}
	if got := compact(by["p2"]["locks"]); got != `{"no_change_aspect":true}` {
		t.Errorf("%s p2: locks %s", slide, got)
	}
	if got := compact(by["p3"]["crop"]); got != "[10,0,0,0]" {
		t.Errorf("%s p3: crop %s", slide, got)
	}
	// The plan adds at most one image relationship per media part, and
	// none to a part the slide already had one to.
	imageRels := func(path string) map[string][]string {
		pk, err := opc.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer pk.Close()
		rels, _ := pk.Rels(part)
		out := map[string][]string{}
		for _, r := range rels.All() {
			if strings.HasSuffix(r.Type, "/image") {
				out[r.Target] = append(out[r.Target], r.ID)
			}
		}
		return out
	}
	before, after := imageRels(path), imageRels(dst)
	for tg, ids := range after {
		if n := len(ids) - len(before[tg]); n > 1 || n == 1 && len(before[tg]) > 0 {
			t.Errorf("%s: the plan added %d image relationships to %s, which had %d", slide, n, tg, len(before[tg]))
		}
	}
	pk, err := opc.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer pk.Close()
	ct, _ := pk.ContentTypes()
	for ext, typ := range map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "svg": "image/svg+xml"} {
		if got, ok := ct.Default(ext); !ok || got != typ {
			t.Errorf("content type Default %s = %q, want %q", ext, got, typ)
		}
	}
}

// viewObj reads one object of the default JSON view.
func viewObj(t *testing.T, path, slide, id string) rawObj {
	t.Helper()
	slides, err := readRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slides {
		if s.name == slide {
			for _, o := range s.objects {
				if o.str("id") == id {
					return o
				}
			}
		}
	}
	t.Fatalf("%s %s not in the view", slide, id)
	return nil
}

func hasPart(t *testing.T, path, name string) bool {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	return p.Has(name)
}

// referencedAfter reports whether anything but the picture's own blip
// refers to the media part in src: a relationship from another part, or,
// on the picture's slide, an attribute other than the picture's own
// r:embed naming a relationship to it.
func referencedAfter(t *testing.T, src, slide, id, media string) bool {
	t.Helper()
	p, err := opc.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	slidePart := "/ppt/slides/" + slide + ".xml"
	var own []string // the slide's relationship ids to media
	for _, name := range p.Names() {
		dir, file := filepath.Split(name)
		if !strings.HasSuffix(dir, "/_rels/") {
			continue
		}
		srcPart := strings.TrimSuffix(dir, "_rels/") + strings.TrimSuffix(file, ".rels")
		rels, err := p.Rels(srcPart)
		if err != nil {
			continue
		}
		for _, r := range rels.All() {
			if tp, err := rels.TargetPart(r); err == nil && !r.External() && strings.EqualFold(tp, media) {
				if !strings.EqualFold(srcPart, slidePart) {
					return true
				}
				own = append(own, r.ID)
			}
		}
	}
	slides, _ := readRaw(src)
	var pic []byte
	for _, s := range slides {
		if s.name == slide {
			pic = s.model[id].XML()
		}
	}
	pt, _ := p.Part(slidePart)
	b, _ := pt.Bytes()
	rest := bytes.Replace(b, pic, nil, 1)
	for _, rid := range own {
		if bytes.Contains(rest, []byte(`"`+rid+`"`)) {
			return true
		}
	}
	return false
}

// checkMediaUntargeted checks a single-picture media plan: parts other
// than the slide, its relationships, the content types, the new media
// part and the collected old one are byte-identical, and every other
// object on the slide keeps its own XML.
func checkMediaUntargeted(t *testing.T, src, dst, slide, part, id string, old ...string) {
	t.Helper()
	ps, pd := parts(t, src), parts(t, dst)
	skip := map[string]bool{strings.ToLower(part): true, strings.ToLower(opc.RelsName(part)): true, strings.ToLower(opc.ContentTypesName): true}
	for n, b := range ps {
		if skip[strings.ToLower(n)] {
			continue
		}
		if a, ok := pd[n]; !ok {
			if !slices.ContainsFunc(old, func(o string) bool { return strings.EqualFold(n, o) }) {
				t.Errorf("part %s removed", n)
			}
		} else if !bytes.Equal(a, b) {
			t.Errorf("part %s changed", n)
		}
	}
	var added []string
	for n := range pd {
		if _, ok := ps[n]; !ok {
			added = append(added, n)
		}
	}
	sort.Strings(added)
	if len(added) > 2 || slices.ContainsFunc(added, func(a string) bool { return !strings.HasPrefix(a, "/ppt/media/") }) {
		t.Errorf("parts added: %v", added)
	}
	checkUntargeted2(t, src, dst, slide, id)
}

// checkUntargeted2 checks that every object on the slide other than id
// keeps its own XML.
func checkUntargeted2(t *testing.T, src, dst, slide, id string) {
	t.Helper()
	sb, _ := readRaw(src)
	sa, _ := readRaw(dst)
	for i := range sb {
		if sb[i].name != slide {
			continue
		}
		for oid, o := range sb[i].model {
			if oid == id {
				continue
			}
			a := sa[i].model[oid]
			if a == nil {
				t.Errorf("%s: untargeted %s removed", slide, oid)
				continue
			}
			if !bytes.Equal(own(sb[i].model, o), own(sa[i].model, a)) && !ancestorIn(sb[i].model, o, map[string]bool{id: true}) {
				t.Errorf("%s: untargeted %s changed", slide, oid)
			}
		}
	}
}

func TestPictureLawsFixtures(t *testing.T) {
	imgs := testImages(t)
	c := &picCounts{}
	all := testdeck.All(t)
	for _, n := range []string{"minimal", "quirky", "soffice", "diagram"} {
		t.Run(n, func(t *testing.T) { checkPictureDeck(t, all[n], imgs, 1000, c) })
	}
	t.Log(c)
	if c.pictures == 0 || c.identity == 0 || c.inverse == 0 || c.replaced == 0 || c.added == 0 || c.atomic == 0 || c.shared == 0 || c.deleted == 0 || c.addDeleteNoop == 0 {
		t.Fatalf("laws exercised nothing: %v", c)
	}
}

func TestPictureLawsCorpus(t *testing.T) {
	t.Parallel() // beside the other corpus suites (corpus_test.go)
	files, _ := nativeFiles(t)
	imgs := testImages(t)
	c := &picCounts{}
	for _, r := range corpustest.Run(t, files, func(t *testing.T, f corpustest.File, d *picCounts) {
		checkPictureDeck(t, f.Path, imgs, 3, d)
	}) {
		c.add(r.Value)
	}
	t.Log(c)
}
