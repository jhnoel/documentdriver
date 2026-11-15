package edit

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// Comment parts. A comment lives in four parts that Word reads together:
// comments (the content), commentsExtended (w15: resolution and reply
// threading), commentsIds (w16cid: durable ids) and commentsExtensible
// (w16cex: durable id and UTC date). Word recovers a package as corrupt
// when commentsIds exists without commentsExtensible, when a durable id
// has its high bit set, or when the extended parts carry the short
// vnd.ms-word content types. CommentParts keeps the four consistent:
// adding a comment writes its record to each, creating missing parts with
// their content type and relationship; removing a thread removes its
// records from each existing part.
//
// docxdriver always uses word/comments.xml and its siblings; here the
// parts the main document's relationships name are used, and the
// default names only for parts that do not exist yet. docxdriver's
// delete also writes empty commentsExtended, commentsIds and
// commentsExtensible parts into packages that had none (unregistered);
// here removal touches only existing parts. Both are known differences.

// Comment-part namespace families.
var (
	W15    = core.NS{URIs: []string{"http://schemas.microsoft.com/office/word/2012/wordml"}, Prefix: "w15"}
	W16CID = core.NS{URIs: []string{"http://schemas.microsoft.com/office/word/2016/wordml/cid"}, Prefix: "w16cid"}
	W16CEX = core.NS{URIs: []string{"http://schemas.microsoft.com/office/word/2018/wordml/cex"}, Prefix: "w16cex"}
)

// commentKind is one of the four comment parts.
type commentKind struct {
	rel, ct, file string
	empty         string
}

var commentKinds = [4]commentKind{
	{ctypes.RelComments, ctypes.CTWordprocessingmlComments, "comments.xml",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:comments xmlns:w="` + "http://schemas.openxmlformats.org/wordprocessingml/2006/main" + `" xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"></w:comments>`},
	{ctypes.RelCommentsExtended, ctypes.CTWordprocessingmlCommentsExtended, "commentsExtended.xml",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w15:commentsEx xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml"></w15:commentsEx>`},
	{ctypes.RelCommentsIds, ctypes.CTWordprocessingmlCommentsIds, "commentsIds.xml",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w16cid:commentsIds xmlns:w16cid="http://schemas.microsoft.com/office/word/2016/wordml/cid"></w16cid:commentsIds>`},
	{ctypes.RelCommentsExtensible, ctypes.CTWordprocessingmlCommentsExtensible, "commentsExtensible.xml",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w16cex:commentsExtensible xmlns:w16cex="http://schemas.microsoft.com/office/word/2018/wordml/cex"></w16cex:commentsExtensible>`},
}

// CommentParts names the main document's comment parts; a name is ""
// for a part the package does not have.
type CommentParts struct {
	Comments, Extended, IDs, Extensible string
}

// slot returns a pointer to the i-th comment part name.
func (c *CommentParts) slot(i int) *string {
	return [4]*string{&c.Comments, &c.Extended, &c.IDs, &c.Extensible}[i]
}

// CommentParts returns the comment parts the main document's
// relationships name.
func (w *Workspace) CommentParts() (CommentParts, error) {
	var out CommentParts
	for i, k := range commentKinds {
		parts, err := w.relatedParts(k.rel)
		if err != nil {
			return out, err
		}
		if len(parts) > 0 {
			*out.slot(i) = parts[0]
		}
	}
	return out, nil
}

// EnsureCommentParts makes sure all four comment parts exist, are related
// from the main document and carry the content type Word writes (an
// existing Override with another type is corrected), and returns them.
// It declares what it changes.
func (w *Workspace) EnsureCommentParts() (CommentParts, error) {
	cp, err := w.CommentParts()
	if err != nil {
		return cp, err
	}
	if cp.Comments == "" || cp.Extended == "" || cp.IDs == "" || cp.Extensible == "" {
		// Parts must be added: refuse up front, before any change, when
		// the content types stream cannot register them.
		if err := w.pkg.RegistersTypes(); err != nil {
			return cp, err
		}
	}
	dir := path.Dir(w.doc)
	for i, k := range commentKinds {
		name := *cp.slot(i)
		if name == "" {
			name = path.Join(dir, k.file)
			if w.pkg.Has(name) {
				return cp, fmt.Errorf("edit: %s exists but the document does not relate to it as %s", name, ctypes.Last(k.rel))
			}
			if _, err := w.AddPart(k.rel, k.ct, name, k.empty); err != nil {
				return cp, err
			}
			*cp.slot(i) = name
			continue
		}
		ct, err := w.pkg.ContentTypes()
		if err != nil {
			return cp, err
		}
		if got, _ := ct.Lookup(name); got != k.ct && w.pkg.RegistersTypes() == nil {
			// A wrong Override is corrected where the stream can be
			// written; an unwritable stream is left as it is.
			w.core.DeclarePart(opc.ContentTypesName)
			if err := ct.SetOverride(name, k.ct); err != nil {
				return cp, err
			}
		}
	}
	return cp, nil
}

// commentRecords returns the elements of part named local, in order: the
// comments part's w:comment records when local is "comment", the
// commentsExtended part's w15:commentEx records when local is "commentEx".
func (w *Workspace) commentRecords(part, local string) ([]*tree.Node, error) {
	if part == "" {
		return nil, nil
	}
	doc, err := w.Tree(part)
	if err != nil {
		return nil, err
	}
	var out []*tree.Node
	if r := doc.Root(); r != nil {
		for _, c := range r.Elements() {
			if model.IsLocal(c, local) {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// recordParaID is the w14:paraId of a comment's first paragraph, or "".
func recordParaID(c *tree.Node) string {
	var id string
	c.Walk(func(n *tree.Node) bool {
		if id != "" {
			return false
		}
		if model.IsLocal(n, "p") {
			id = model.AttrOr(n, "paraId")
			return false
		}
		return true
	})
	return id
}

// CommentID returns a fresh comment id: one more than the largest decimal
// w:id of the comments part's comments, or 0 (docxdriver's
// next_comment_id).
func (w *Workspace) CommentID() (string, error) {
	cp, err := w.CommentParts()
	if err != nil {
		return "", err
	}
	recs, err := w.commentRecords(cp.Comments, "comment")
	if err != nil {
		return "", err
	}
	next := uint64(0)
	for _, c := range recs {
		if v, err := strconv.ParseUint(model.AttrOr(c, "id"), 10, 64); err == nil && v+1 > next {
			next = v + 1
		}
	}
	return strconv.FormatUint(next, 10), nil
}

// CommentParaID returns a fresh paragraph id for a comment's paragraph,
// unique across the whole package (docxdriver's next_para_id): past
// every paraId and paraIdParent attribute of every XML part, every main
// document paragraph's id (repairs not yet written included) and every
// comment record's: one more than the largest, wrapping to 1 at
// 80000000, probing forward.
func (w *Workspace) CommentParaID() (string, error) {
	occupied := map[uint64]bool{}
	for _, id := range w.ids {
		if v, err := strconv.ParseUint(id.ID, 16, 64); err == nil {
			occupied[v] = true
		}
	}
	for _, name := range w.pkg.Names() {
		if !strings.HasSuffix(strings.ToLower(name), ".xml") || !w.pkg.IsXML(name) {
			continue
		}
		doc, err := w.Tree(name)
		if err != nil {
			continue // a part that does not parse holds no ids we could clash with
		}
		doc.Node().Walk(func(n *tree.Node) bool {
			if n.Kind() != tree.ElementNode {
				return true
			}
			for _, a := range n.Attrs() {
				if l := a.Name[strings.LastIndexByte(a.Name, ':')+1:]; (l == "paraId" || l == "paraIdParent") && len(a.Value) == 8 {
					if v, err := strconv.ParseUint(a.Value, 16, 64); err == nil {
						occupied[v] = true
					}
				}
			}
			return true
		})
	}
	cand := uint64(1)
	for v := range occupied {
		cand = max(cand, v+1)
	}
	for {
		if cand == 0 || cand >= 0x8000_0000 {
			cand = 1
		}
		if !occupied[cand] {
			return model.FormatParaID(uint32(cand)), nil
		}
		cand++
	}
}

// DurableID returns a durable comment id for a new comment (docxdriver's
// durable_id): FNV-1a over its paragraph id, author and text, mapped into
// 00000001..7FFFFFFE, probing forward past the durable ids the commentsIds
// part holds.
func (w *Workspace) DurableID(paraID, author, text string) (string, error) {
	cp, err := w.CommentParts()
	if err != nil {
		return "", err
	}
	occupied := map[uint32]bool{}
	if cp.IDs != "" {
		doc, err := w.Tree(cp.IDs)
		if err != nil {
			return "", err
		}
		if r := doc.Root(); r != nil {
			for _, c := range r.Elements() {
				if v, err := strconv.ParseUint(model.AttrOr(c, "durableId"), 16, 32); err == nil && model.IsLocal(c, "commentId") {
					occupied[uint32(v)] = true
				}
			}
		}
	}
	h := uint32(0x811c_9dc5)
	for _, s := range []string{paraID, author, text} {
		for i := range len(s) {
			h ^= uint32(s[i])
			h *= 0x0100_0193
		}
	}
	c := h%0x7FFF_FFFE + 1
	for range 0x7FFF_FFFE {
		if !occupied[c] {
			break
		}
		c++
		if c >= 0x7FFF_FFFF {
			c = 1
		}
	}
	return fmt.Sprintf("%08X", c), nil
}

// NewComment is one comment's records.
type NewComment struct {
	ID, ParaID, ParentParaID string // ParentParaID "" for a thread's root
	Author, Initials, Date   string
	Text                     string
	DurableID                string
}

// AddCommentRecords appends a comment's records to all four parts
// (creating and registering missing ones), as docxdriver's
// append_comment_record writes them: the w:comment with one CommentText
// paragraph (an annotation-reference run, then the text run), its
// w15:commentEx (done 0, threaded under ParentParaID), its
// w16cid:commentId and its w16cex:commentExtensible. Each part's root
// declares the prefix it uses. Anchoring the comment in a story is the
// operation's.
func (w *Workspace) AddCommentRecords(c NewComment) error {
	cp, err := w.EnsureCommentParts()
	if err != nil {
		return err
	}
	space := ""
	if strings.HasPrefix(c.Text, " ") || strings.HasSuffix(c.Text, " ") {
		space = ` xml:space="preserve"`
	}
	frags := [4]string{
		`<w:comment w:id="` + esc(c.ID) + `" w:author="` + esc(c.Author) + `" w:date="` + esc(c.Date) + `" w:initials="` + esc(c.Initials) + `">` +
			`<w:p w14:paraId="` + esc(c.ParaID) + `"><w:pPr><w:pStyle w:val="CommentText"/></w:pPr>` +
			`<w:r><w:rPr><w:rStyle w:val="CommentReference"/></w:rPr><w:annotationRef/></w:r>` +
			`<w:r><w:t` + space + `>` + string(tree.EscapeText(c.Text)) + `</w:t></w:r></w:p></w:comment>`,
		`<w15:commentEx w15:paraId="` + esc(c.ParaID) + `"` + parentAttr(c.ParentParaID) + ` w15:done="0"/>`,
		`<w16cid:commentId w16cid:paraId="` + esc(c.ParaID) + `" w16cid:durableId="` + esc(c.DurableID) + `"/>`,
		`<w16cex:commentExtensible w16cex:durableId="` + esc(c.DurableID) + `" w16cex:dateUtc="` + esc(c.Date) + `"/>`,
	}
	ns := [4]core.NS{W14, W15, W16CID, W16CEX}
	for i := range commentKinds {
		part := *cp.slot(i)
		// The comments part needs w14 for the paragraph id; the others'
		// roots are in their own namespace, never ignorable.
		if err := w.core.EnsureRootNamespace(part, ns[i].Prefix, ns[i].URIs[0], true); err != nil {
			return err
		}
		doc, err := w.Tree(part)
		if err != nil {
			return err
		}
		root := doc.Root()
		nodes, err := w.core.Parse(root, []byte(frags[i]), map[string]string{
			"w": WML.URIs[0], "w14": W14.URIs[0], "w15": W15.URIs[0], "w16cid": W16CID.URIs[0], "w16cex": W16CEX.URIs[0],
		})
		if err != nil {
			return err
		}
		if err := w.core.Insert(part, core.Into(root), nodes[0], nil); err != nil {
			return err
		}
	}
	return nil
}

// parentAttr returns the w15:paraIdParent attribute markup for p, or empty when p is empty.
func parentAttr(p string) string {
	if p == "" {
		return ""
	}
	return ` w15:paraIdParent="` + esc(p) + `"`
}

// esc escapes &, < and quote in s for XML attribute values.
func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;")
	return r.Replace(s)
}

// RemoveCommentRecords removes the records of the comments whose
// paragraph ids are listed (a thread: its root and every reply) from
// each comment part that exists: the w:comment, its commentEx, its
// commentId, and the commentExtensible of its durable id. It declares
// what it changes and returns the comment ids removed.
func (w *Workspace) RemoveCommentRecords(paraIDs []string) ([]string, error) {
	cp, err := w.CommentParts()
	if err != nil {
		return nil, err
	}
	in := map[string]bool{}
	for _, p := range paraIDs {
		in[strings.ToUpper(p)] = true
	}
	var removed []string
	durable := map[string]bool{}
	drop := func(part string, pick func(*tree.Node) bool) error {
		if part == "" {
			return nil
		}
		doc, err := w.Tree(part)
		if err != nil {
			return err
		}
		if doc.Root() == nil {
			return nil
		}
		for _, c := range doc.Root().Elements() {
			if pick(c) {
				if err := w.core.Delete(part, c); err != nil {
					return err
				}
			}
		}
		return nil
	}
	steps := []struct {
		part string
		pick func(*tree.Node) bool
	}{
		{cp.Comments, func(c *tree.Node) bool {
			if model.IsLocal(c, "comment") && in[strings.ToUpper(recordParaID(c))] {
				removed = append(removed, model.AttrOr(c, "id"))
				return true
			}
			return false
		}},
		{cp.Extended, func(c *tree.Node) bool {
			return model.IsLocal(c, "commentEx") && in[strings.ToUpper(model.AttrOr(c, "paraId"))]
		}},
		{cp.IDs, func(c *tree.Node) bool {
			if model.IsLocal(c, "commentId") && in[strings.ToUpper(model.AttrOr(c, "paraId"))] {
				durable[model.AttrOr(c, "durableId")] = true
				return true
			}
			return false
		}},
		{cp.Extensible, func(c *tree.Node) bool {
			return model.IsLocal(c, "commentExtensible") && durable[model.AttrOr(c, "durableId")]
		}},
	}
	for _, s := range steps {
		if err := drop(s.part, s.pick); err != nil {
			return nil, err
		}
	}
	return removed, nil
}
