package main

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Revision, comment and bookmark ids in a projection are allocated ids:
// their values are the allocating engine's order, an artefact the plan
// contract does not fix. relabel renames them by order of first
// appearance in the projection's text (document order for the document
// reads, listing order for the listings), separately per id space, so two
// engines that allocate in different orders but write the same document
// project the same text. The same function runs on the oracle's and the
// Go CLI's outputs.

type idPattern struct {
	space string // r (revision), c (comment), b (bookmark)
	re    *regexp.Regexp
}

// Markup is embedded in JSON strings, so its quotes are escaped.
var (
	markupIDs = []idPattern{
		{"r", regexp.MustCompile(`data-docx-revision=\\"(\d+)\\"`)},
		{"r", regexp.MustCompile(`data-docx-break-(?:ins|del)=\\"(\d+)\\"`)},
		{"r", regexp.MustCompile(`data-docx-format-change=\\"(\d+)\\"`)},
		{"c", regexp.MustCompile(`data-docx-comment-(?:start|end)=\\"(\d+)\\"`)},
		// Table row and cell revision properties in the document read.
		{"r", regexp.MustCompile(`"(?:ins|del|cellIns|cellDel|cellMerge)":\{[^{}]*?"id":"(\d+)"`)},
	}
	jsonID = regexp.MustCompile(`"(?:id|ooxmlId|ooxmlCommentId|parentId|threadId)":"(\d+)"`)
)

// relabel renames the allocated ids of one projection (or report).
func relabel(projection, text string) string {
	pats := slices.Clone(markupIDs)
	switch {
	case strings.HasSuffix(projection, "revisions"):
		pats = append(pats, idPattern{"r", jsonID})
	case strings.HasSuffix(projection, "bookmarks"):
		pats = append(pats, idPattern{"b", jsonID})
	case strings.HasSuffix(projection, "comments"), strings.HasSuffix(projection, "doc"),
		strings.HasSuffix(projection, "final"), strings.HasSuffix(projection, "original"):
		// The document read lists comments (and their replies) by id.
		pats = append(pats, idPattern{"c", jsonID})
	}
	type hit struct {
		start, end int // the id digits
		space      string
	}
	var hits []hit
	covered := func(s, e int) bool {
		for _, h := range hits {
			if s < h.end && h.start < e {
				return true
			}
		}
		return false
	}
	for _, p := range pats {
		for _, m := range p.re.FindAllStringSubmatchIndex(text, -1) {
			if !covered(m[2], m[3]) {
				hits = append(hits, hit{m[2], m[3], p.space})
			}
		}
	}
	if len(hits) == 0 {
		return text
	}
	slices.SortFunc(hits, func(a, b hit) int { return a.start - b.start })
	labels := map[string]string{}
	next := map[string]int{}
	var b strings.Builder
	last := 0
	for _, h := range hits {
		k := h.space + text[h.start:h.end]
		l, ok := labels[k]
		if !ok {
			next[h.space]++
			l = h.space + strconv.Itoa(next[h.space])
			labels[k] = l
		}
		b.WriteString(text[last:h.start])
		b.WriteString(l)
		last = h.end
	}
	b.WriteString(text[last:])
	return b.String()
}

// quotedID is an eight-hex-digit paragraph id in quotes, in plain JSON or
// in markup embedded in a JSON string.
var quotedID = regexp.MustCompile(`(\\?")([0-9A-F]{8})(\\?")`)

// relabelNewIDs renames every paragraph id the source document does not
// have (P4a, 2026-11-13: ids of inserted and copied paragraphs, and of new
// comment paragraphs, are the engine's derivation from source, op and
// slot; docxdriver's also include the plan's canonical bytes, so they
// differ between modes) by order of first appearance: n1, n2, ...
// Existing ids, the repaired ones included, stay as they are.
func relabelNewIDs(text string, known map[string]bool) string {
	labels := map[string]string{}
	return quotedID.ReplaceAllStringFunc(text, func(m string) string {
		sub := quotedID.FindStringSubmatch(m)
		if known[sub[2]] {
			return m
		}
		l, ok := labels[sub[2]]
		if !ok {
			l = "n" + strconv.Itoa(len(labels)+1)
			labels[sub[2]] = l
		}
		return sub[1] + l + sub[3]
	})
}
