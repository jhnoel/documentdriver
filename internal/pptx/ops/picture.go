package ops

import (
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
)

// Pictures: media parts and relationships behind the image and svg
// fields, and the order the picture fields are written in.

// writePicture writes a picture's fields: the image and svg parts, then
// crop, fit and fill rectangle, then locks. Each goes through its codec,
// so a value written as shown is a no-op that touches no XML.
func (t *Tx) writePicture(h *edit.Handle, p PictureFields) error {
	if h.Kind() != "pic" {
		return fmt.Errorf("image, svg, crop, fit, fillRect and locks are for pictures, not a %s", h.Kind())
	}
	if p.Image.Set || p.SVG.Set {
		if err := t.writeMedia(h, p); err != nil {
			return err
		}
	}
	if p.Crop.Set || p.Fit.Set || p.FillRect.Set {
		c := prop.PicFill()
		cur := c.Read(h, 0)
		want, ok := cur.Get()
		if !ok {
			return fmt.Errorf("crop: %w: %s", prop.ErrNotDerived, cur.Reason())
		}
		if p.Crop.Set {
			r, err := percents(p.Crop.V)
			if err != nil {
				return fmt.Errorf("crop: %w", err)
			}
			want.Crop = &r
		}
		if p.Fit.Set {
			// A new stretch is written as PowerPoint writes an uncropped
			// picture's: with an empty a:fillRect, unless one is given.
			if p.Fit.V == "stretch" && want.Fit != "stretch" {
				want.FillRect = &[4]int64{}
			}
			want.Fit = p.Fit.V
		}
		if p.FillRect.Set {
			r, err := percents(p.FillRect.V)
			if err != nil {
				return fmt.Errorf("fill_rect: %w", err)
			}
			want.FillRect = &r
		}
		if err := prop.Set(h, c, want); err != nil {
			return err
		}
	}
	if p.Locks.Set {
		c := prop.Locks()
		cur := c.Read(h, 0)
		have, ok := cur.Get()
		if !ok {
			return fmt.Errorf("locks: %w: %s", prop.ErrNotDerived, cur.Reason())
		}
		on := map[string]bool{}
		for _, n := range have {
			on[n] = true
		}
		for k, v := range p.Locks.V {
			if name, ok := lockName(k); ok {
				on[name] = v
			}
		}
		if err := prop.Set(h, c, derive.TrueLocks(on)); err != nil {
			return err
		}
	}
	return nil
}

// writeMedia sets the image and svg parts. A value equal to what is shown
// changes nothing. Otherwise it names a part of the deck or a local file,
// whose bytes go into a media part (an existing part with the same bytes
// and content type, else a new one); the blip then embeds that part
// through a relationship from the slide, and a relationship or media part
// the picture no longer uses is removed when nothing else refers to it.
func (t *Tx) writeMedia(h *edit.Handle, p PictureFields) error {
	slide := h.Ref().Part()
	refs := mediaRefs{t, slide}
	img, svg := prop.Image(refs), prop.SVG(refs)
	curSVG, svgOK := svg.Read(h, 0).Get()
	if p.Image.Set && !p.SVG.Set && svgOK && curSVG != "" {
		if cur, ok := img.Read(h, 0).Get(); !ok || cur != p.Image.V {
			return fmt.Errorf("image: the picture shows svg %s, whose fallback image is replaced; give svg too (a new SVG, %s to keep it, or none)", curSVG, curSVG)
		}
	}
	old := t.embedded(h)
	for _, f := range []struct {
		field  Opt[string]
		c      prop.Codec[string]
		format string
	}{{p.Image, img, "raster"}, {p.SVG, svg, "svg"}} {
		if !f.field.Set {
			continue
		}
		want := f.field.V
		if f.format == "svg" && want == "none" {
			want = ""
		}
		if cur, ok := f.c.Read(h, 0).Get(); ok && f.c.Show(cur) == f.c.Show(want) {
			continue
		}
		if want != "" {
			part, err := t.mediaPart(want, f.format)
			if err != nil {
				return fmt.Errorf("%s: %w", f.c.Name, err)
			}
			want = part
		}
		if err := prop.Set(h, f.c, want); err != nil {
			return err
		}
	}
	return t.release(slide, old)
}

// embedded lists the relationship ids the object's pictures embed (the
// blip and the SVG blip of every p:pic in it, itself, the children of a
// group and every mc:AlternateContent branch included), in every branch.
func (t *Tx) embedded(h *edit.Handle) []string {
	var out []string
	for i := range h.Branches() {
		h.Elem(i).Walk(func(n *tree.Node) bool {
			if !derive.Is(n, derive.PML, "pic") {
				return true
			}
			blip := derive.Child(derive.Child(n, derive.PML, "blipFill"), derive.DML, "blip")
			if blip == nil {
				return true
			}
			if rid, ok := derive.AttrNS(blip, derive.REL, "embed"); ok {
				out = append(out, rid)
			}
			if sb := derive.SVGBlip(blip); sb != nil {
				if rid, ok := derive.AttrNS(sb, derive.REL, "embed"); ok {
					out = append(out, rid)
				}
			}
			return true
		})
	}
	return out
}

// mediaRefs resolves relationships of one slide for the picture codecs.
type mediaRefs struct {
	t     *Tx
	slide string
}

// Target implements prop.MediaRefs.
func (m mediaRefs) Target(rid string) (string, bool) {
	rels, err := m.t.pkg.Rels(m.slide)
	if err != nil {
		return "", false
	}
	r, ok := rels.Get(rid)
	if !ok || r.External() {
		return "", false
	}
	part, err := rels.TargetPart(r)
	return part, err == nil
}

// Embed implements prop.MediaRefs: the slide's image relationship to part
// if it has one (as PowerPoint shares one between pictures of the same
// image), else a new one with the next free id, of the image
// relationship type the slide's own relationships use.
func (m mediaRefs) Embed(part string) (string, error) {
	if !m.t.pkg.Has(part) {
		return "", fmt.Errorf("no part %s", part)
	}
	rels, err := m.t.pkg.Rels(m.slide)
	if err != nil {
		return "", err
	}
	for _, r := range rels.All() {
		if r.External() || !ctypes.Is(r.Type, ctypes.RelImage) {
			continue
		}
		if tp, err := rels.TargetPart(r); err == nil && strings.EqualFold(tp, part) {
			return r.ID, nil
		}
	}
	typ, err := imageRelType(rels)
	if err != nil {
		return "", err
	}
	m.t.ws.DeclarePart(rels.Name())
	m.t.ws.DeclarePart(opc.ContentTypesName)
	return rels.Add(typ, relTarget(m.slide, part), false)
}

// imageRelType returns the image relationship type in the slide's own
// namespace (transitional or strict): that of an image relationship the
// slide has, else its layout relationship's with image for slideLayout.
func imageRelType(rels *opc.Rels) (string, error) {
	for _, r := range rels.All() {
		if ctypes.Is(r.Type, ctypes.RelImage) {
			return r.Type, nil
		}
	}
	for _, r := range rels.All() {
		if ctypes.Is(r.Type, ctypes.RelSlideLayout) {
			if typ, ok := ctypes.Rebase(r.Type, ctypes.RelImage); ok {
				return typ, nil
			}
		}
	}
	return "", errors.New("the image relationship type is not derivable: the slide has no image or layout relationship")
}

// relTarget is part relative to the slide's folder, as PowerPoint writes
// targets ("../media/image3.png").
func relTarget(slide, part string) string {
	var from []string
	if dir := strings.Trim(path.Dir(slide), "/"); dir != "" {
		from = strings.Split(dir, "/")
	}
	to := strings.Split(strings.TrimPrefix(part, "/"), "/")
	i := 0
	for i < len(from) && i < len(to)-1 && from[i] == to[i] {
		i++
	}
	return strings.Repeat("../", len(from)-i) + strings.Join(to[i:], "/")
}

// mediaPart returns the part holding the image a plan names: a part of
// the deck (its name as the views show it) whose bytes are in the wanted
// format, or a local file whose bytes are, which is stored as a media
// part. format is "raster" (PNG, JPEG or GIF) or "svg".
func (t *Tx) mediaPart(v, format string) (string, error) {
	name := v
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	var data []byte
	inDeck := strings.HasPrefix(v, "/") && t.pkg.Has(name)
	if inDeck {
		if _, err := os.Stat(v); err == nil {
			return "", fmt.Errorf("%q names both a part of the deck and a local file", v)
		}
		pt, err := t.pkg.Part(name)
		if err != nil {
			return "", err
		}
		if data, err = pt.Bytes(); err != nil {
			return "", err
		}
	} else {
		var err error
		if data, err = os.ReadFile(v); err != nil { //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
			return "", fmt.Errorf("not a part of the deck, and not a readable file: %w", err)
		}
	}
	f := derive.SniffImage(data)
	switch {
	case format == "svg" && f != "svg":
		return "", fmt.Errorf("%s is not an SVG document", v)
	case format == "raster" && f == "svg":
		return "", fmt.Errorf("%s is an SVG: give it as svg, with a PNG, JPEG or GIF fallback as image", v)
	case format == "raster" && f != "png" && f != "jpeg" && f != "gif":
		return "", fmt.Errorf("%s is not a PNG, JPEG or GIF image", v)
	}
	if f != "svg" {
		if m := derive.ReadMedia(data); !m.HasPx {
			return "", fmt.Errorf("%s: %s", v, m.PxReason)
		}
	}
	if inDeck {
		pt, err := t.pkg.Part(name)
		if err != nil {
			return "", err
		}
		return pt.Name(), nil
	}
	return t.storeMedia(data, f)
}

// storeMedia returns a media part holding data: an existing part under
// /ppt/media/ with the same bytes and the format's content type, else a
// new part /ppt/media/imageN.ext (N one more than the largest in use),
// with the content type Default for its extension added when the deck
// has none.
func (t *Tx) storeMedia(data []byte, format string) (string, error) {
	mf := derive.MediaFormats[format]
	idx, err := t.mediaIndex()
	if err != nil {
		return "", err
	}
	sum := derive.ReadMedia(data).SHA256
	if p, ok := idx[sum]; ok {
		if pt, err := t.pkg.Part(p); err == nil {
			if ct, _ := pt.ContentType(); ct == mf.ContentType {
				return p, nil
			}
		}
	}
	// The largest N of an imageN part, or of an imageN that a relationship
	// targets although the part is missing (a dangling relationship must
	// not come to point at the new part).
	targets, err := t.relTargets()
	if err != nil {
		return "", err
	}
	n := 0
	for _, name := range append(t.pkg.Names(), targets...) {
		if m := mediaName.FindStringSubmatch(name); m != nil {
			if k, err := strconv.Atoi(m[1]); err == nil && k > n {
				n = k
			}
		}
	}
	name := "/ppt/media/image" + strconv.Itoa(n+1) + "." + mf.Ext
	ct, err := t.pkg.ContentTypes()
	if err != nil {
		return "", err
	}
	t.ws.DeclarePart(opc.ContentTypesName)
	if _, ok := ct.Default(mf.Ext); !ok {
		if err := ct.SetDefault(mf.Ext, mf.ContentType); err != nil {
			return "", err
		}
		if t.addedDefaults == nil {
			t.addedDefaults = map[string]bool{}
		}
		t.addedDefaults[strings.ToLower(mf.Ext)] = true
	}
	if _, err := t.pkg.AddPart(name, data, mf.ContentType); err != nil {
		return "", err
	}
	t.ws.DeclarePart(name)
	idx[sum] = name
	return name, nil
}

var mediaName = regexp.MustCompile(`(?i)^/ppt/media/image([0-9]+)\.[^/]+$`)

// mediaIndex maps the SHA-256 of each part under /ppt/media/ to the part,
// listed once per transaction (the first part in zip order for equal
// bytes).
func (t *Tx) mediaIndex() (map[string]string, error) {
	if t.media != nil {
		return t.media, nil
	}
	idx := map[string]string{}
	for _, name := range t.pkg.Names() {
		if !strings.HasPrefix(strings.ToLower(name), "/ppt/media/") {
			continue
		}
		pt, err := t.pkg.Part(name)
		if err != nil {
			return nil, err
		}
		b, err := pt.Bytes()
		if err != nil {
			return nil, err
		}
		if sum := derive.ReadMedia(b).SHA256; idx[sum] == "" {
			idx[sum] = name
		}
	}
	t.media = idx
	return idx, nil
}

// release removes, for each relationship id the picture embedded before
// the write, the slide's relationship when no attribute in the slide
// refers to the id any more, and then its target part when no
// relationship in the package targets it. Nothing else is removed: a part
// another slide, layout or picture uses stays.
func (t *Tx) release(slide string, rids []string) error {
	if len(rids) == 0 {
		return nil
	}
	pt, err := t.pkg.Part(slide)
	if err != nil {
		return err
	}
	doc, err := pt.Tree()
	if err != nil {
		return err
	}
	used := map[string]bool{}
	doc.Root().Walk(func(n *tree.Node) bool {
		if n.Kind() == tree.ElementNode {
			for _, a := range n.Attrs() {
				used[a.Value] = true
			}
		}
		return true
	})
	rels, err := t.pkg.Rels(slide)
	if err != nil {
		return err
	}
	for _, rid := range rids {
		if used[rid] {
			continue
		}
		r, ok := rels.Get(rid)
		if !ok {
			continue
		}
		t.ws.DeclarePart(rels.Name())
		rels.Remove(rid)
		if r.External() {
			continue
		}
		target, err := rels.TargetPart(r)
		if err != nil || !t.pkg.Has(target) {
			continue
		}
		if ref, err := t.referenced(target); err != nil || ref {
			continue
		}
		t.ws.DeclarePart(target)
		t.ws.DeclarePart(opc.ContentTypesName)
		if err := t.pkg.DeletePart(target); err != nil {
			return err
		}
		for sum, p := range t.media {
			if p == target {
				delete(t.media, sum)
			}
		}
		if err := t.dropAddedDefault(target); err != nil {
			return err
		}
	}
	return nil
}

// relTargets lists the part names every internal relationship of the
// package targets.
func (t *Tx) relTargets() ([]string, error) {
	var out []string
	for _, name := range t.pkg.Names() {
		src, ok := relsSource(name)
		if !ok {
			continue
		}
		rels, err := t.pkg.Rels(src)
		if err != nil {
			return nil, err
		}
		for _, r := range rels.All() {
			if r.External() {
				continue
			}
			if tp, err := rels.TargetPart(r); err == nil {
				out = append(out, tp)
			}
		}
	}
	return out, nil
}

// referenced reports whether any internal relationship of any part of
// the package targets part.
func (t *Tx) referenced(part string) (bool, error) {
	for _, name := range t.pkg.Names() {
		src, ok := relsSource(name)
		if !ok {
			continue
		}
		rels, err := t.pkg.Rels(src)
		if err != nil {
			return false, err
		}
		for _, r := range rels.All() {
			if r.External() {
				continue
			}
			if tp, err := rels.TargetPart(r); err == nil && strings.EqualFold(tp, part) {
				return true, nil
			}
		}
	}
	return false, nil
}

// dropAddedDefault removes the content-type Default for a deleted media
// part's extension when this plan added that Default and no part with
// the extension is left, so that adding and removing media in one plan
// leaves the content types as they were. A Default the deck had before
// the plan is never removed: it is the deck's, whatever it serves.
func (t *Tx) dropAddedDefault(deleted string) error {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(deleted), "."))
	if ext == "" || !t.addedDefaults[ext] {
		return nil
	}
	for _, n := range t.pkg.Names() {
		if strings.EqualFold(strings.TrimPrefix(path.Ext(n), "."), ext) {
			return nil
		}
	}
	ct, err := t.pkg.ContentTypes()
	if err != nil {
		return err
	}
	t.ws.DeclarePart(opc.ContentTypesName)
	ct.RemoveDefault(ext)
	delete(t.addedDefaults, ext)
	return nil
}

// relsSource returns the source part of a relationships part
// (/ppt/slides/_rels/slide1.xml.rels is /ppt/slides/slide1.xml; the
// package's /_rels/.rels is "/").
func relsSource(name string) (string, bool) {
	dir, file := path.Split(name)
	if !strings.HasSuffix(dir, "/_rels/") || !strings.HasSuffix(file, ".rels") {
		return "", false
	}
	src := strings.TrimSuffix(dir, "_rels/") + strings.TrimSuffix(file, ".rels")
	if src == "/" {
		return "/", true
	}
	return src, true
}
