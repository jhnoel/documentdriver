package opc

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// TestCorpus runs the package-level guarantees against every .pptx under
// $PPTX_CORPUS:
//
//   - every XML part round-trips byte for byte through its tree;
//   - a no-op save (after parsing every part) writes nothing;
//   - setting one attribute on one shape changes only those bytes of that
//     part, and every other member's stored entry is identical.
//
// Files archive/zip cannot open (encrypted decks are OLE files, fuzzer
// crash cases) are counted and skipped. Parts that are not well formed are
// counted, not failed: they are never edited and are copied verbatim.
func TestCorpus(t *testing.T) {
	files := testdeck.Corpus(t)
	// File by file in parallel, each into tallies of its own.
	type tallies struct{ stats, localDiffs map[string]int }
	stats := map[string]int{}
	localDiffs := map[string]int{}
	shard := corpustest.Files(files, os.Getenv("PPTX_CORPUS"))
	for _, r := range corpustest.Run(t, shard, func(t *testing.T, f corpustest.File, c *tallies) {
		c.stats, c.localDiffs = map[string]int{}, map[string]int{}
		if err := corpusFile(t, f.Path, c.stats, c.localDiffs); err != nil {
			t.Errorf("%s: %v", f.Path, err)
			c.stats["failed"]++
		}
	}) {
		for k, v := range r.Value.stats {
			stats[k] += v
		}
		for k, v := range r.Value.localDiffs {
			localDiffs[k] += v
		}
	}
	t.Logf("corpus: %d files; %s", len(corpustest.Shard(t, shard)), fmtStats(stats))
	if len(localDiffs) > 0 {
		t.Logf("untouched members whose local header bytes changed (archive/zip rewrites them from the central directory): %s", fmtStats(localDiffs))
	}
}

func fmtStats(m map[string]int) string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%d ", k, m[k])
	}
	return b.String()
}

func corpusFile(t *testing.T, src string, stats, localDiffs map[string]int) error {
	p, err := Open(src)
	if errors.Is(err, ErrLimit) {
		return fmt.Errorf("rejected by the default limits: %w", err)
	}
	if err != nil {
		stats["unopenable"]++
		return nil
	}
	stats["files"]++
	// Every part, XML or not, must read within the default limits.
	for _, name := range p.Names() {
		pt, _ := p.Part(name)
		orig, err := pt.Original()
		if errors.Is(err, ErrLimit) {
			p.Close()
			return fmt.Errorf("%s: rejected by the default limits: %w", name, err)
		}
		if err != nil {
			stats["unreadable parts"]++
			continue
		}
		if !p.IsXML(name) {
			continue
		}
		stats["xml parts"]++
		doc, err := pt.Tree()
		if err != nil {
			stats["not well formed parts"]++
			continue
		}
		if !bytes.Equal(doc.Bytes(), orig) {
			return fmt.Errorf("%s: serialisation differs", name)
		}
	}
	p.Close()

	// No-op save after parsing everything.
	path := testdeck.Copy(t, src)
	ageFile(t, path)
	s := snap(t, path)
	p, err = Open(path)
	if err != nil {
		return err
	}
	// A part that cannot be read or parsed (counted above) fails only
	// at the part: the save neither reads nor fails on it.
	for _, name := range p.Names() {
		if pt, _ := p.Part(name); p.IsXML(name) {
			pt.Tree() //nolint:errcheck // defects were counted above; what matters is that the save below ignores them
		}
	}
	if err := p.Save(); err != nil {
		return fmt.Errorf("no-op save: %w", err)
	}
	p.Close()
	if now := snap(t, path); !bytes.Equal(now.data, s.data) || !now.mtime.Equal(s.mtime) {
		return fmt.Errorf("no-op save wrote the file")
	}

	// One attribute on one shape.
	p, err = Open(path)
	if err != nil {
		return err
	}
	defer p.Close()
	part, off, attrName, old := firstEditable(p)
	if off == nil {
		stats["no slide attribute to edit"]++
		return nil
	}
	pt, _ := p.Part(part)
	orig, _ := pt.Original()
	start, end, _ := off.Span()
	newVal := "1234567"
	if old == newVal {
		newVal = "7654321"
	}
	if err := off.SetAttr(attrName, newVal); err != nil {
		return err
	}
	before, comment := rawEntries(t, path)
	if err := p.Save(); err != nil {
		stats["edit save failed"]++
		return fmt.Errorf("edit save: %w", err)
	}
	after, comment2 := rawEntries(t, path)
	if comment != comment2 || len(before) != len(after) {
		return fmt.Errorf("archive comment or member count changed")
	}
	for i := range before {
		b, a := before[i], after[i]
		if b.hdr.Name != a.hdr.Name {
			return fmt.Errorf("member order changed at %d", i)
		}
		if PartKey("/"+b.hdr.Name) == PartKey(part) {
			continue
		}
		if d := sameEntry(b, a); d != "" {
			return fmt.Errorf("untouched member %s changed: %s", b.hdr.Name, d)
		}
		if !bytes.Equal(b.local, a.local) {
			localDiffs[localDiffKind(b.local, a.local)]++
		}
	}
	p2, err := Open(path)
	if err != nil {
		return err
	}
	defer p2.Close()
	pt2, _ := p2.Part(part)
	got, err := pt2.Bytes()
	if err != nil {
		return err
	}
	// Only the edited element may differ, and only in that attribute's
	// value: same name, "=" spacing and quote.
	if !bytes.Equal(got[:start], orig[:start]) || !bytes.HasSuffix(got, orig[end:]) {
		return fmt.Errorf("%s: bytes outside the edited element changed", part)
	}
	el := got[start : len(got)-len(orig[end:])]
	re := regexp.MustCompile(`(\s` + regexp.QuoteMeta(attrName) + `\s*=\s*)(?:"` + regexp.QuoteMeta(old) + `"|'` + regexp.QuoteMeta(old) + `')`)
	loc := re.FindSubmatchIndex(orig[start:end])
	if loc == nil {
		return fmt.Errorf("%s: attribute %s not found in original element", part, attrName)
	}
	o := orig[start:end]
	q := o[loc[3]]
	want := slices.Concat(o[:loc[3]], []byte{q}, []byte(newVal), []byte{q}, o[loc[1]:])
	if !bytes.Equal(el, want) {
		return fmt.Errorf("%s: edited element is %q, want %q", part, trunc(el), trunc(want))
	}
	stats["attribute edits verified"]++
	return nil
}

func trunc(b []byte) []byte {
	if len(b) > 200 {
		return b[:200]
	}
	return b
}

// firstEditable picks an attribute to edit on a slide: the x of the first
// shape offset, else the first attribute (not a namespace declaration)
// whose value is written without references.
func firstEditable(p *Package) (part string, el *tree.Node, attr, old string) {
	var slides []*tree.Document
	var names []string
	for _, name := range p.Names() {
		l := strings.ToLower(name)
		if !strings.HasPrefix(l, "/ppt/slides/slide") || !strings.HasSuffix(l, ".xml") {
			continue
		}
		pt, _ := p.Part(name)
		doc, err := pt.Tree()
		if err != nil {
			continue
		}
		slides, names = append(slides, doc), append(names, name)
	}
	plain := func(n *tree.Node, a string) (string, bool) {
		v, ok := n.Attr(a)
		raw, _ := n.AttrRaw(a)
		return v, ok && string(raw) == v && v != ""
	}
	for i, doc := range slides {
		for _, off := range doc.Root().FindAll(nsA, "off") {
			if off.Parent().Parent().Local() != "spPr" {
				continue
			}
			if x, ok := plain(off, "x"); ok {
				return names[i], off, "x", x
			}
		}
	}
	for i, doc := range slides {
		var found *tree.Node
		var fa, fv string
		doc.Root().Walk(func(n *tree.Node) bool {
			if found != nil || n.Kind() != tree.ElementNode {
				return found == nil
			}
			for _, a := range n.Attrs() {
				if a.Name == "xmlns" || strings.HasPrefix(a.Name, "xmlns:") {
					continue
				}
				if v, ok := plain(n, a.Name); ok {
					found, fa, fv = n, a.Name, v
					return false
				}
			}
			return true
		})
		if found != nil {
			return names[i], found, fa, fv
		}
	}
	return "", nil, "", ""
}

// localDiffKind classifies how a copied member's local header changed.
func localDiffKind(a, b []byte) string {
	if a == nil || b == nil {
		return "not located"
	}
	if len(a) < 30 || len(b) < 30 {
		return "short"
	}
	switch {
	case !bytes.Equal(a[4:6], b[4:6]):
		return "version needed"
	case !bytes.Equal(a[6:8], b[6:8]):
		return "flags"
	case !bytes.Equal(a[14:26], b[14:26]):
		return "crc/sizes (data descriptor)"
	case !bytes.Equal(a[28:30], b[28:30]) || !bytes.Equal(a[30:], b[30:]):
		return "extra field"
	}
	return "other"
}
