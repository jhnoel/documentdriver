package main

import (
	"encoding/json"
	"slices"
	"strings"
)

// settleByPlans files the contract's settle-by cases on a source with
// revisions: revision.accept and revision.reject of the first listed
// author's revisions (all with author), of the first and of the last
// body paragraph owning a listed revision (at a paragraph list), and of
// that author's revisions in both paragraphs. docxdriver's
// revision_settle takes one id or all, so each case is recorded through
// an equivalent: settling the same revisions one id at a time, the
// listing's paragraph and story revisions last first, then its rows in
// order. Last first settles a revision nested in another (a deletion
// inside an insertion, a property change inside a deleted run) before
// the one whose settling would remove it, and rows go last for the same
// reason; the result is the one-pass settle's. A source whose listing
// repeats an id is skipped (an id names the first revision that has it),
// and so is a case that selects no listed revision.
func settleByPlans(g *gen, f *docFacts, kind string) {
	if len(f.revisions) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, r := range f.revisions {
		if seen[r.id] {
			return
		}
		seen[r.id] = true
	}
	author := f.revisions[0].author
	var paras []string
	for _, r := range f.revisions {
		if r.para != "" && (len(paras) == 0 || paras[len(paras)-1] != r.para) && !strings.Contains(r.para, "~") {
			paras = append(paras, r.para)
		}
	}
	type sel struct {
		note   string
		author string
		at     []string
	}
	var sels []sel
	if author != "" {
		sels = append(sels, sel{note: "author", author: author})
	}
	if len(paras) > 0 {
		sels = append(sels, sel{note: "first paragraph", at: paras[:1]})
		if len(paras) > 1 {
			last := paras[len(paras)-1]
			sels = append(sels, sel{note: "last paragraph", at: []string{last}})
			if author != "" {
				sels = append(sels, sel{note: "author in two paragraphs", author: author, at: []string{paras[0], last}})
			}
		}
	}
	for _, s := range sels {
		in := map[string]bool{}
		for _, p := range s.at {
			in[p] = true
		}
		var ids, rows []string
		for _, r := range slices.Backward(f.revisions) {
			if (s.author == "" || r.author == s.author) && (s.at == nil || in[r.para]) {
				if r.row {
					rows = append([]string{r.id}, rows...)
				} else {
					ids = append(ids, r.id)
				}
			}
		}
		ids = append(ids, rows...)
		if len(ids) == 0 {
			continue
		}
		for _, action := range []string{"accept", "reject"} {
			op := "revision." + action
			var o string
			switch {
			case s.at == nil:
				o = obj("op", op, "all", true, "author", s.author)
			case s.author != "":
				o = obj("op", op, "at", s.at, "author", s.author)
			default:
				o = obj("op", op, "at", s.at)
			}
			p := g.addContract("revision_settle", kind, f, "settle by "+s.note+" "+action, o)
			if s.at == nil && f.onlyAuthor(s.author) {
				p.Equivalent = json.RawMessage("[" + obj("op", op, "all", true) + "]")
				continue
			}
			eq := make([]string, len(ids))
			for i, id := range ids {
				eq[i] = obj("op", op, "at", "revision:"+id)
			}
			p.Equivalent = json.RawMessage("[" + strings.Join(eq, ",") + "]")
		}
	}
}

// onlyAuthor reports whether every revision of the source is author's:
// each one the listing shows (headers and footers included), and each
// revision element of the main document, those the listing leaves out
// included, but for a w:tblGridChange, which has no author and goes with
// the table's changes. Settling the author's revisions is then settling
// all of them, which the oracle can do: its equivalent is all.
func (f *docFacts) onlyAuthor(author string) bool {
	if f.bodyAuthors == nil {
		return false
	}
	for _, r := range f.revisions {
		if r.author != author {
			return false
		}
	}
	for a := range f.bodyAuthors {
		if a != author {
			return false
		}
	}
	return true
}
