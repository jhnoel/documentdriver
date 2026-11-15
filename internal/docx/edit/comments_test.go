package edit

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

func TestCommentPartsCreatedTogether(t *testing.T) {
	w, _ := openIDs(t, idDoc, nil)
	cp, err := w.CommentParts()
	if err != nil || cp != (CommentParts{}) {
		t.Fatalf("CommentParts = %+v %v", cp, err)
	}
	add := func(parent string) NewComment {
		id, err := w.CommentID()
		if err != nil {
			t.Fatal(err)
		}
		pid, err := w.CommentParaID()
		if err != nil {
			t.Fatal(err)
		}
		dur, err := w.DurableID(pid, "Ann Lee", " note ")
		if err != nil {
			t.Fatal(err)
		}
		c := NewComment{ID: id, ParaID: pid, ParentParaID: parent, Author: "Ann Lee", Initials: "AL", Date: "2000-01-01T00:00:00Z", Text: " note ", DurableID: dur}
		if err := w.AddCommentRecords(c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	first := add("")
	reply := add(first.ParaID)
	if first.ID != "0" || reply.ID != "1" || first.ParaID == reply.ParaID || first.DurableID == reply.DurableID {
		t.Fatalf("ids %+v %+v", first, reply)
	}
	for _, p := range find(t, w, docPart, "p") {
		if id, _ := w.ParaID(p); id.ID == first.ParaID || id.ID == reply.ParaID {
			t.Fatalf("comment paraId %s clashes with a document paragraph", id.ID)
		}
	}
	cp, err = w.CommentParts()
	if err != nil {
		t.Fatal(err)
	}
	want := CommentParts{"/word/comments.xml", "/word/commentsExtended.xml", "/word/commentsIds.xml", "/word/commentsExtensible.xml"}
	if cp != want {
		t.Fatalf("CommentParts = %+v", cp)
	}
	ct, _ := w.Package().ContentTypes()
	for i, k := range commentKinds {
		if got, _ := ct.Lookup(*cp.slot(i)); got != k.ct {
			t.Errorf("%s content type %s", *cp.slot(i), got)
		}
	}
	doc, _ := w.Tree(cp.Comments)
	if got := string(doc.Bytes()); !strings.Contains(got, `<w:t xml:space="preserve"> note </w:t>`) || !strings.Contains(got, `mc:Ignorable="w14"`) {
		t.Fatalf("comments part %s", got)
	}
	ext, _ := w.Tree(cp.Extended)
	if got := string(ext.Bytes()); !strings.Contains(got, `w15:paraIdParent="`+first.ParaID+`"`) || strings.Contains(got, "Ignorable") {
		t.Fatalf("extended part %s", got)
	}
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
	removed, err := w.RemoveCommentRecords([]string{first.ParaID, reply.ParaID})
	if err != nil || strings.Join(removed, ",") != "0,1" {
		t.Fatalf("removed %v %v", removed, err)
	}
	for i := range commentKinds {
		d, _ := w.Tree(*cp.slot(i))
		if n := len(d.Root().Elements()); n != 0 {
			t.Errorf("%s keeps %d records", *cp.slot(i), n)
		}
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestCommentPartsFollowRelationships(t *testing.T) {
	comments := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:comments` + nsDecl + w14Decl + `><w:comment w:id="7"><w:p w14:paraId="00000ABC"/></w:comment></w:comments>`
	w, _ := openIDs(t, idDoc, map[string]string{
		"word/notes/c.xml": comments,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId3" Type="` + ctypes.RelComments + `" Target="notes/c.xml"/></Relationships>`,
	})
	if id, _ := w.CommentID(); id != "8" {
		t.Fatalf("CommentID = %s", id)
	}
	cp, err := w.EnsureCommentParts()
	if err != nil {
		t.Fatal(err)
	}
	if cp.Comments != "/word/notes/c.xml" || cp.IDs != "/word/commentsIds.xml" {
		t.Fatalf("parts %+v", cp)
	}
	rels, _ := w.Package().Rels(docPart)
	if n := len(rels.All()); n != 4 {
		t.Fatalf("%d relationships", n)
	}
	if id, err := w.CommentParaID(); err != nil || !model.IsValidParaID(id) || id == "00000ABC" {
		t.Fatalf("CommentParaID = %s, %v", id, err)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}
