package laws

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/ops"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
)

// The text operations end to end, as single-operation plans through
// ops and edit.Handle.ReplaceParagraphs (internal/pptx/laws/text_test.go
// checks the builder by splicing bytes, without the write path):
//
//   - untargeted bytes: text.set (an agent edit of the shown HTML, or a
//     new body on a shape without one) and text.replace leave every
//     other part and every other object's own bytes identical, and the
//     read model reads the set body back as Normalise(h);
//   - atomicity: a text operation followed by a failing one leaves the
//     file's bytes and mtime unchanged.

// textRefusal reports whether err is one the text operations give for
// content they cannot write (rather than a bug), and names it.
func textRefusal(err error) (string, bool) {
	for _, e := range []error{text.ErrInexpressible, text.ErrUnsupported, text.ErrCrossing, text.ErrNoMatch} {
		if errors.Is(err, e) {
			return e.Error(), true
		}
	}
	return "", false
}

// replaceProbe returns a substring of the body's first run with text,
// at most four bytes long and cut at a rune boundary, or "".
func replaceProbe(b text.Body) string {
	for _, p := range b.Paras {
		for _, r := range p.Runs {
			if r.Br || r.Text == "" {
				continue
			}
			s := ""
			for _, c := range r.Text {
				if len(s)+len(string(c)) > 4 {
					break
				}
				s += string(c)
			}
			return s
		}
	}
	return ""
}

// lawURL is the target of the hyperlink the link law adds; no deck holds
// it, so the relationship is always new.
const lawURL = "https://example.invalid/pptx-law"

// linkLaw: wrapping the body's first text run in a new external link
// reads back as Normalise(h), adds exactly one relationship to the
// slide's relationships part and changes no other part; writing the
// shown HTML back over that removes the link and its relationship, so
// the relationships part is byte-identical to the source again.
func linkLaw(t *testing.T, c *m3counts, path, dir, slide, id, shown string, b text.Body) {
	t.Helper()
	l := b
	l.Paras = append([]text.Para(nil), b.Paras...)
	found := false
	for i := range l.Paras {
		if found {
			break
		}
		p := l.Paras[i]
		p.Runs = append([]text.Run(nil), p.Runs...)
		for k, r := range p.Runs {
			if !r.Br && r.Text != "" && r.Fmt.Link == (text.Link{}) {
				p.Runs[k].Fmt.Link = text.Link{Href: lawURL}
				found = true
				break
			}
		}
		l.Paras[i] = p
	}
	if !found {
		return
	}
	want, err := text.Normalise(text.Render(l))
	if err != nil {
		t.Errorf("%s %s: linked html: %v", slide, id, err)
		return
	}
	rels := "/ppt/slides/_rels/" + slide + ".xml.rels"
	dst := filepath.Join(dir, "link.pptx")
	back := filepath.Join(dir, "unlink.pptx")
	os.Remove(dst)
	os.Remove(back)
	err = ops.ApplyTo(path, dst, plan(slide, map[string]any{"op": "text.set", "at": id, "html": want})...)
	if why, ok := textRefusal(err); ok {
		c.textRefused["text-link: "+why]++
		return
	}
	if err != nil {
		t.Errorf("%s %s link: %v", slide, id, err)
		return
	}
	ps, pd := parts(t, path), parts(t, dst)
	for n, x := range ps {
		if strings.EqualFold(n, "/ppt/slides/"+slide+".xml") || strings.EqualFold(n, rels) {
			continue
		}
		if !bytes.Equal(x, pd[n]) {
			t.Errorf("%s %s link: part %s changed", slide, id, n)
		}
	}
	if got, was := bytes.Count(pd[rels], []byte("<Relationship ")), bytes.Count(ps[rels], []byte("<Relationship ")); got != was+1 || !bytes.Contains(pd[rels], []byte(lawURL)) {
		t.Errorf("%s %s link: %d relationships, want %d with the link's", slide, id, got, was+1)
	}
	if html := shownHTML(t, dst, slide, id); html != want {
		t.Errorf("%s %s link: reads back\n%s\nwant\n%s", slide, id, html, want)
		return
	}
	c.opPlans["text-link"]++
	if err := ops.ApplyTo(dst, back, plan(slide, map[string]any{"op": "text.set", "at": id, "html": shown})...); err != nil {
		t.Errorf("%s %s unlink: %v", slide, id, err)
		return
	}
	if html := shownHTML(t, back, slide, id); html != shown {
		t.Errorf("%s %s unlink: reads back\n%s\nwant\n%s", slide, id, html, shown)
	}
	if !bytes.Equal(parts(t, back)[rels], ps[rels]) {
		t.Errorf("%s %s unlink: the link's relationship was not removed", slide, id)
		return
	}
	c.opPlans["text-unlink"]++
}

// shownHTML reads an object's HTML through the read model.
func shownHTML(t *testing.T, path, slide, id string) string {
	t.Helper()
	after, err := readRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range after {
		if s.name == slide {
			if o := s.model[id]; o != nil && o.Text != nil {
				return o.Text.HTML
			}
		}
	}
	return ""
}

// textOps runs the text laws on one object of a private deck copy.
func textOps(t *testing.T, path, slide string, o rawObj, c *m3counts) {
	id := o.str("id")
	dir := t.TempDir()
	only := func(_, _ map[string]*model.Object) map[string]bool { return map[string]bool{id: true} }
	run := func(name string, spec map[string]any, want string) {
		dst := filepath.Join(dir, name+".pptx")
		os.Remove(dst)
		err := ops.ApplyTo(path, dst, plan(slide, spec)...)
		if why, ok := textRefusal(err); ok {
			c.textRefused[name+": "+why]++
			return
		}
		if err != nil {
			t.Errorf("%s %s %s: %v", slide, id, name, err)
			return
		}
		c.opPlans[name]++
		checkUntargeted(t, path, dst, slide, only)
		if want == "" {
			return
		}
		after, err := readRaw(dst)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range after {
			if s.name != slide {
				continue
			}
			got := s.model[id]
			switch {
			case got == nil || got.Text == nil:
				t.Errorf("%s %s %s: no text after the plan", slide, id, name)
			case got.Text.HTML != want:
				t.Errorf("%s %s %s: reads back\n%s\nwant\n%s", slide, id, name, got.Text.HTML, want)
			default:
				c.textReadBack++
			}
		}
	}
	html := o.str("html")
	kind := o.str("kind")
	switch {
	case html != "":
		b, err := text.Parse(html)
		if err != nil {
			t.Errorf("%s %s: shown html does not parse: %v", slide, id, err)
			return
		}
		e := agentEdit(b)
		run("text-set", map[string]any{"op": "text.set", "at": id, "html": text.Render(e)}, text.Render(e))
		linkLaw(t, c, path, dir, slide, id, html, b)
		if find := replaceProbe(b); find != "" {
			run("text-replace", map[string]any{"op": "text.replace", "at": id, "select": find, "with": "«law»", "all": true}, "")
		}
		// Atomicity: the set followed by a failing operation.
		b0, _ := os.ReadFile(path)
		i0, _ := os.Stat(path)
		err = ops.Apply(path, plan(slide,
			map[string]any{"op": "text.set", "at": id, "html": text.Render(e)},
			map[string]any{"op": "object.delete", "at": "s99999999"})...)
		b1, _ := os.ReadFile(path)
		i1, _ := os.Stat(path)
		switch {
		case err == nil:
			t.Errorf("%s %s: failing plan succeeded", slide, id)
		case !bytes.Equal(b0, b1) || !i0.ModTime().Equal(i1.ModTime()):
			t.Errorf("%s %s: failed text plan changed the file", slide, id)
		default:
			c.textAtomic++
		}
	case kind == "shape":
		run("text-set-new-body", map[string]any{"op": "object.set", "at": id, "html": "<p><b>law</b> text</p>"}, "<p><b>law</b> text</p>")
	}
}
