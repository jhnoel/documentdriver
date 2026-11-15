package main

import (
	"encoding/json"
	"fmt"
	"github.com/jhnoel/documentdriver/internal/docx/integrity"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// docFacts is what plan generation knows of a source, all read through
// the oracle (read and find in the markup view, the bookmark and
// revision listings).
type docFacts struct {
	set, file string
	size      int
	paras     []paraFact
	tables    []tableFact
	sections  []sectionFact
	equations []eqFact
	comments  []commentFact
	bookmarks []bookmarkFact
	revisions []revFact
	// bodyAuthors are the authors of every revision element of the main
	// document, the ones the listing leaves out (cell, row, table and
	// section changes) included, "" for one without an author, but for
	// w:tblGridChange, which has none by schema. Read with the Go model,
	// not the oracle.
	bodyAuthors map[string]bool
	repairs     int // idRepairsNeeded: paragraphs without a usable paraId
	// findings counts the integrity and schema findings the input already
	// has (internal/docx/integrity): a dirty source when not zero.
	findings int
}

type paraFact struct {
	id, text, selectable, style string
	listNum                     string
	inTable, hasBreak, hasMath  bool
	hasRevision                 bool // markup shows ins/del inside the paragraph
}

type tableFact struct {
	handle string
	rows   []int // cells per row
	merged bool
	// trackedRows marks rows with a row insertion or deletion.
	trackedRows int
}

type sectionFact struct {
	index     int
	handle    string
	firstPage bool
	stories   []string // "hdr/default", "ftr/first", ...
	explicit  []string // stories the section references itself
}

type eqFact struct {
	at      string
	n       int
	display bool
}

type commentFact struct {
	id      string
	replies []string
	status  string
}

type bookmarkFact struct{ name, part, start string }

// revFact is one revision of the oracle's listing; para is the id of
// the body paragraph it is listed under ("" for a row, or a header or
// footer revision), row whether it is a table row's insertion or
// deletion.
type revFact struct {
	id, kind, author, part, para string
	row                          bool
}

// readJSON runs an oracle read in the source's directory and decodes the
// result member.
func readJSON(oracle, dir, file string, args []string, into any) error {
	r, err := runIn(dir, oracle, append([]string{"--json"}, append([]string{args[0], file}, args[1:]...)...)...)
	if err != nil {
		return err
	}
	if r.exit != 0 {
		return fmt.Errorf("%s %v: exit %d: %s", file, args, r.exit, firstLine(r.stderr))
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(r.stdout, &env); err != nil {
		return fmt.Errorf("%s %v: %w", file, args, err)
	}
	return json.Unmarshal(env.Result, into)
}

// firstLine returns the first line of b.
func firstLine(b []byte) string {
	s, _, _ := strings.Cut(string(b), "\n")
	return s
}

var (
	reParaOpen = regexp.MustCompile(`<p id="([0-9A-F]{8})"`)
	reTag      = regexp.MustCompile(`<(/?)(table|p|header|footer)[ >]`)
)

// gatherFacts reads one source with the oracle.
func gatherFacts(oracle string, in input) (*docFacts, error) {
	full := filepath.Join(in.dir, filepath.FromSlash(in.path))
	dir, file := filepath.Dir(full), filepath.Base(full)
	st, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	f := &docFacts{set: in.set, file: in.path, size: int(st.Size())}
	if src, err := os.ReadFile(full); err == nil {
		f.findings = integrity.Check(src).Len()
		f.bodyAuthors = bodyAuthors(src)
	}
	var doc struct {
		Markup     string `json:"markup"`
		Repairs    int    `json:"idRepairsNeeded"`
		Selections struct {
			Paragraphs []struct {
				At   string `json:"at"`
				Text string `json:"selectable_text"`
			} `json:"paragraphs"`
		} `json:"selections"`
		Sections []struct {
			Index     int    `json:"index"`
			Handle    string `json:"handle"`
			FirstPage bool   `json:"first_page"`
			Stories   []struct {
				Scope    string `json:"scope"`
				Kind     string `json:"kind"`
				Explicit bool   `json:"explicit"`
			} `json:"stories"`
		} `json:"sections"`
		Tables []struct {
			At   string `json:"at"`
			Rows []struct {
				Cells []struct {
					GridSpan int    `json:"grid_span"`
					VMerge   string `json:"vertical_merge"`
				} `json:"cells"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"rows"`
		} `json:"tables"`
		Equations []struct {
			At       string `json:"at"`
			Equation int    `json:"equation"`
			Display  bool   `json:"display"`
		} `json:"equations"`
		Comments []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Replies []struct {
				ID string `json:"id"`
			} `json:"replies"`
		} `json:"comments"`
	}
	if err := readJSON(oracle, dir, file, []string{"read"}, &doc); err != nil {
		return nil, err
	}
	f.repairs = doc.Repairs
	var find struct {
		Matches []struct {
			ID         string `json:"id"`
			Text       string `json:"text"`
			Selectable string `json:"selectable_text"`
			Style      string `json:"style"`
			List       *struct {
				NumID string `json:"num_id"`
			} `json:"list"`
		} `json:"matches"`
	}
	if err := readJSON(oracle, dir, file, []string{"find", ""}, &find); err != nil {
		return nil, err
	}
	inTable, flags := scanMarkup(doc.Markup)
	for _, m := range find.Matches {
		p := paraFact{id: m.ID, text: m.Text, selectable: m.Selectable, style: m.Style, inTable: inTable[m.ID]}
		if m.List != nil {
			p.listNum = m.List.NumID
		}
		fl := flags[m.ID]
		p.hasBreak, p.hasMath, p.hasRevision = strings.Contains(fl, "br"), strings.Contains(fl, "math"), strings.Contains(fl, "rev")
		f.paras = append(f.paras, p)
	}
	for _, s := range doc.Sections {
		sf := sectionFact{index: s.Index, handle: s.Handle, firstPage: s.FirstPage}
		for _, st := range s.Stories {
			sf.stories = append(sf.stories, st.Scope+"/"+st.Kind)
			if st.Explicit {
				sf.explicit = append(sf.explicit, st.Scope+"/"+st.Kind)
			}
		}
		f.sections = append(f.sections, sf)
	}
	for _, t := range doc.Tables {
		tf := tableFact{handle: t.At}
		for _, r := range t.Rows {
			tf.rows = append(tf.rows, len(r.Cells))
			for _, c := range r.Cells {
				if c.GridSpan > 1 || c.VMerge != "" {
					tf.merged = true
				}
			}
			if r.Properties["ins"] != nil || r.Properties["del"] != nil {
				tf.trackedRows++
			}
		}
		f.tables = append(f.tables, tf)
	}
	for _, e := range doc.Equations {
		f.equations = append(f.equations, eqFact{e.At, e.Equation, e.Display})
	}
	for _, c := range doc.Comments {
		cf := commentFact{id: c.ID, status: c.Status}
		for _, r := range c.Replies {
			cf.replies = append(cf.replies, r.ID)
		}
		f.comments = append(f.comments, cf)
	}
	var bms struct {
		Bookmarks []struct {
			Name   string `json:"name"`
			Part   string `json:"part"`
			Start  string `json:"start"`
			Hidden bool   `json:"hidden"`
		} `json:"bookmarks"`
	}
	if err := readJSON(oracle, dir, file, []string{"read", "--kind", "bookmarks"}, &bms); err != nil {
		return nil, err
	}
	for _, b := range bms.Bookmarks {
		if !b.Hidden {
			f.bookmarks = append(f.bookmarks, bookmarkFact{b.Name, b.Part, b.Start})
		}
	}
	var revs struct {
		Revisions []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Author string `json:"author"`
			Part   string `json:"part"`
			// Paragraph is the 1-based index of the body paragraph, in
			// the order find lists them; 0 for a row.
			Paragraph int    `json:"paragraphIndex"`
			Locator   string `json:"locator"`
		} `json:"revisions"`
	}
	if err := readJSON(oracle, dir, file, []string{"read", "--kind", "revisions"}, &revs); err != nil {
		// The oracle panics listing some documents' revisions; the source
		// is still usable without them.
		revs.Revisions = nil
	}
	for _, r := range revs.Revisions {
		para := ""
		if r.Part == "" && r.Locator != "row" && r.Paragraph > 0 && r.Paragraph <= len(f.paras) {
			para = f.paras[r.Paragraph-1].id
		}
		f.revisions = append(f.revisions, revFact{r.ID, r.Kind, r.Author, r.Part, para, r.Locator == "row"})
	}
	return f, nil
}

// scanMarkup finds, per paragraph id, whether it sits in a table and
// what its markup holds (a break, an equation, a revision).
func scanMarkup(markup string) (inTable map[string]bool, flags map[string]string) {
	inTable, flags = map[string]bool{}, map[string]string{}
	depth := 0
	for _, loc := range reTag.FindAllStringSubmatchIndex(markup, -1) {
		closing, tag := markup[loc[2]:loc[3]] == "/", markup[loc[4]:loc[5]]
		if tag == "table" {
			if closing {
				depth--
			} else {
				depth++
			}
			continue
		}
		if tag != "p" || closing {
			continue
		}
		m := reParaOpen.FindStringSubmatch(markup[loc[0]:min(len(markup), loc[0]+20)])
		if m == nil {
			continue
		}
		id := m[1]
		inTable[id] = depth > 0
		end := strings.Index(markup[loc[0]:], "</p>")
		if end < 0 {
			continue
		}
		body := markup[loc[0] : loc[0]+end]
		var fl []string
		if strings.Contains(body, "<br") {
			fl = append(fl, "br")
		}
		if strings.Contains(body, "<math") || strings.Contains(body, "<equation") {
			fl = append(fl, "math")
		}
		if strings.Contains(body, "<ins") || strings.Contains(body, "<del") || strings.Contains(body, "data-docx-break-") || strings.Contains(body, "data-docx-format-change") {
			fl = append(fl, "rev")
		}
		flags[id] = strings.Join(fl, ",")
	}
	return inTable, flags
}

// features names what a source offers the generators, for sources.tsv
// and corpus selection.
func (f *docFacts) features() []string {
	var out []string
	add := func(ok bool, name string) {
		if ok {
			out = append(out, name)
		}
	}
	add(len(f.plain(10)) >= 3, "text")
	add(len(f.tables) > 0, "tables")
	add(slices.ContainsFunc(f.tables, func(t tableFact) bool { return t.merged }), "merged-cells")
	add(slices.ContainsFunc(f.tables, func(t tableFact) bool { return t.trackedRows > 0 }), "tracked-rows")
	add(len(f.sections) >= 2, "sections")
	add(slices.ContainsFunc(f.sections, func(s sectionFact) bool {
		return slices.ContainsFunc(s.stories, func(k string) bool { return strings.HasSuffix(k, "/first") || strings.HasSuffix(k, "/even") })
	}), "first-even-stories")
	add(slices.ContainsFunc(f.sections, func(s sectionFact) bool { return len(s.stories) > 0 }), "headers")
	add(len(f.equations) > 0, "equations")
	add(slices.ContainsFunc(f.equations, func(e eqFact) bool { return e.display }), "display-equations")
	add(len(f.comments) > 0, "comments")
	add(slices.ContainsFunc(f.comments, func(c commentFact) bool { return len(c.replies) > 0 }), "comment-replies")
	add(len(f.bodyBookmarks()) > 0, "bookmarks")
	add(len(f.revisions) > 0, "revisions")
	add(f.authors() >= 2, "revision-authors")
	add(slices.ContainsFunc(f.paras, func(p paraFact) bool { return p.listNum != "" }), "lists")
	add(f.repairs > 0, "missing-paraids")
	add(f.findings > 0, "dirty")
	return out
}

// authors returns the number of distinct revision authors in f.
func (f *docFacts) authors() int {
	seen := map[string]bool{}
	for _, r := range f.revisions {
		seen[r.author] = true
	}
	return len(seen)
}

// bodyBookmarks returns f's bookmarks in the main document part.
func (f *docFacts) bodyBookmarks() []bookmarkFact {
	var out []bookmarkFact
	for _, b := range f.bookmarks {
		if b.part == "word/document.xml" {
			out = append(out, b)
		}
	}
	return out
}

// plain returns the body paragraphs span operations can target without
// meeting the selection differences docxdriver has (breaks and equations
// count as no characters there, B8): outside tables, no break, no
// equation, no revision, at least minLen selectable characters.
func (f *docFacts) plain(minLen int) []paraFact {
	var out []paraFact
	for _, p := range f.paras {
		if p.inTable || p.hasBreak || p.hasMath || p.hasRevision || len([]rune(p.selectable)) < minLen || p.selectable != p.text {
			continue
		}
		out = append(out, p)
	}
	return out
}

var reWord = regexp.MustCompile(`[\p{L}\p{N}]{3,}`)

// uniqueWord returns a word of text that occurs exactly once in it as a
// substring, preferring longer words.
func uniqueWord(text string) string {
	best := ""
	for _, w := range reWord.FindAllString(text, -1) {
		if strings.Count(text, w) == 1 && len(w) > len(best) {
			best = w
		}
	}
	return best
}

// repeatedWord returns a word that occurs at least twice as a substring.
func repeatedWord(text string) string {
	for _, w := range reWord.FindAllString(text, -1) {
		if strings.Count(text, w) >= 2 {
			return w
		}
	}
	return ""
}

// uniqueWords returns up to n distinct unique words, in text order.
func uniqueWords(text string, n int) []string {
	var out []string
	for _, w := range reWord.FindAllString(text, -1) {
		if strings.Count(text, w) == 1 && !slices.Contains(out, w) {
			ok := true
			for _, o := range out {
				if strings.Contains(o, w) || strings.Contains(w, o) {
					ok = false
				}
			}
			if ok {
				out = append(out, w)
			}
		}
		if len(out) == n {
			break
		}
	}
	return out
}

// bodyAuthors lists the authors of the main document's revision
// elements (docFacts.bodyAuthors); nil when it does not open.
func bodyAuthors(src []byte) map[string]bool {
	d, err := model.OpenDocument(src)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, n := range d.XML.RevisionElements() {
		if !model.IsW(n, "tblGridChange") {
			out[model.AttrOr(n, "author")] = true
		}
	}
	return out
}
