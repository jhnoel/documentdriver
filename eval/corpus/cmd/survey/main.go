// Command survey scans a corpus directory of cloned repos for PowerPoint
// decks and writes a JSON-lines manifest. It is eval tooling only: unlike the
// product packages it uses encoding/xml token scanning, and it never writes
// to the decks.
//
// Usage: survey -corpus <repo>/corpus [-out <corpus>/manifest.jsonl]
package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/jhnoel/documentdriver/internal/repo"
)

const (
	nsP  = "http://schemas.openxmlformats.org/presentationml/2006/main"
	nsA  = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsMC = "http://schemas.openxmlformats.org/markup-compatibility/2006"
)

// repoInfo is static metadata per surveyed repo (dir name under repos/).
var repoInfo = map[string]struct{ URL, SPDX string }{
	"libreoffice-core":    {"https://github.com/LibreOffice/core", "MPL-2.0"},
	"apache-poi":          {"https://github.com/apache/poi", "Apache-2.0"},
	"open-xml-sdk":        {"https://github.com/dotnet/Open-XML-SDK", "MIT"},
	"python-pptx":         {"https://github.com/scanny/python-pptx", "MIT"},
	"apache-tika":         {"https://github.com/apache/tika", "Apache-2.0"},
	"shapecrawler":        {"https://github.com/ShapeCrawler/ShapeCrawler", "MIT"},
	"pptx-automizer":      {"https://github.com/singerla/pptx-automizer", "MIT"},
	"markitdown":          {"https://github.com/microsoft/markitdown", "MIT"},
	"pandoc":              {"https://github.com/jgm/pandoc", "GPL-2.0-or-later"},
	"open-xml-powertools": {"https://github.com/OfficeDev/Open-XML-PowerTools", "MIT"},
}

// Stats are structural counts, summed over slides (depth is a max).
type Stats struct {
	Sp           int `json:"sp"`
	NonPhSp      int `json:"sp_non_placeholder"`
	CxnSp        int `json:"cxnSp"`
	CxnGlued     int `json:"cxn_glued"`
	GrpSp        int `json:"grpSp"`
	MaxGrpDepth  int `json:"grp_max_depth"`
	CustGeom     int `json:"custGeom"`
	Pic          int `json:"pic"`
	GraphicFrame int `json:"graphicFrame"`
	GFChart      int `json:"gf_chart"`
	GFDgm        int `json:"gf_smartart"`
	GFTable      int `json:"gf_table"`
	GFOle        int `json:"gf_ole"`
	GFOther      int `json:"gf_other"`
	AltContent   int `json:"alternateContent"`
	RotFlipGrp   int `json:"rot_or_flip_grp"`
	PhNoXfrm     int `json:"placeholder_no_xfrm"`
	DupIDs       int `json:"dup_shape_ids"`
}

// add adds o's counts to s, keeping the maximum group depth.
func (s *Stats) add(o Stats) {
	s.Sp += o.Sp
	s.NonPhSp += o.NonPhSp
	s.CxnSp += o.CxnSp
	s.CxnGlued += o.CxnGlued
	s.GrpSp += o.GrpSp
	if o.MaxGrpDepth > s.MaxGrpDepth {
		s.MaxGrpDepth = o.MaxGrpDepth
	}
	s.CustGeom += o.CustGeom
	s.Pic += o.Pic
	s.GraphicFrame += o.GraphicFrame
	s.GFChart += o.GFChart
	s.GFDgm += o.GFDgm
	s.GFTable += o.GFTable
	s.GFOle += o.GFOle
	s.GFOther += o.GFOther
	s.AltContent += o.AltContent
	s.RotFlipGrp += o.RotFlipGrp
	s.PhNoXfrm += o.PhNoXfrm
	s.DupIDs += o.DupIDs
}

// score weights the things diagram tasks are made of: glued connectors most,
// then groups, freeforms, plain connectors and non-placeholder shapes.
func (s Stats) score() float64 {
	return 3*float64(s.CxnGlued) + float64(s.CxnSp-s.CxnGlued) +
		2*float64(s.GrpSp) + 1.5*float64(s.CustGeom) + 0.5*float64(s.NonPhSp)
}

// Record is one manifest line.
type Record struct {
	SHA256      string   `json:"sha256"`
	Size        int64    `json:"size"`
	Source      string   `json:"source"`
	SourceURL   string   `json:"source_url"`
	Commit      string   `json:"commit"`
	Path        string   `json:"path"`
	AlsoAt      []string `json:"also_at,omitempty"`
	License     string   `json:"license"`
	Application string   `json:"application"`
	AppVersion  string   `json:"app_version"`
	Creator     string   `json:"-"` // read for the Suspect check only; personal data, never written
	LastModBy   string   `json:"-"`
	OpensZip    bool     `json:"opens_zip"`
	HasPresXML  bool     `json:"has_presentation_xml"`
	Error       string   `json:"error,omitempty"`
	Slides      int      `json:"slides"`
	ParseErrs   int      `json:"slide_parse_errors,omitempty"`
	Stats
	PowerPoint bool     `json:"powerpoint_authored"`
	Native     bool     `json:"powerpoint_native"`
	Suspect    string   `json:"authorship_caveat,omitempty"`
	Score      float64  `json:"diagram_score"`
	ScoreTotal float64  `json:"diagram_score_total"`
	BestSlide  string   `json:"best_slide,omitempty"`
	BestScore  float64  `json:"best_slide_score"`
	HardCases  []string `json:"hard_cases,omitempty"`
}

// main parses flags, runs the survey, and exits with status 1 on error.
func main() {
	corpus := flag.String("corpus", "", "corpus dir (default <repo>/corpus)")
	out := flag.String("out", "", "manifest path (default <corpus>/manifest.jsonl; the manifest stays with the corpus, out of git)")
	webDir := flag.String("web", "", "dir of directly downloaded decks (default <corpus>/web)")
	webTSV := flag.String("web-sources", "eval/corpus/web-sources.tsv", "web-sources.tsv with licence and source URL per slug")
	noRepos := flag.Bool("no-repos", false, "skip the repos/ clones (used to triage web candidates)")
	flag.Parse()
	if *corpus == "" {
		c, err := repo.Corpus()
		if err != nil {
			fmt.Fprintln(os.Stderr, "survey: corpus dir:", err, "(pass -corpus)")
			os.Exit(1)
		}
		*corpus = c
	}
	if *out == "" {
		*out = filepath.Join(*corpus, "manifest.jsonl")
	}
	if *webDir == "" {
		*webDir = filepath.Join(*corpus, "web")
	}
	if err := run(*corpus, *out, *webDir, *webTSV, *noRepos); err != nil {
		fmt.Fprintln(os.Stderr, "survey:", err)
		os.Exit(1)
	}
}

type file struct{ repo, rel, abs, url, license string }

// loadWebSources reads web-sources.tsv: slug -> {source page URL, licence}.
// Columns: slug, category, source_url, download_url, sha256, size, licence, ...
func loadWebSources(path string) map[string][2]string {
	m := map[string][2]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for i, line := range strings.Split(string(b), "\n") {
		c := strings.Split(line, "\t")
		if i == 0 || len(c) < 7 {
			continue
		}
		m[c[0]] = [2]string{c[2], c[6]}
	}
	return m
}

// run surveys corpus decks, dedupes them by hash, and writes the manifest.
func run(corpus, out, webDir, webTSV string, noRepos bool) error {
	var repos []os.DirEntry
	if !noRepos {
		var err error
		repos, err = os.ReadDir(filepath.Join(corpus, "repos"))
		if err != nil {
			return err
		}
	}
	var files []file
	commits := map[string]string{}
	for _, r := range repos {
		if !r.IsDir() {
			continue
		}
		root := filepath.Join(corpus, "repos", r.Name())
		b, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
		if err == nil {
			commits[r.Name()] = strings.TrimSpace(string(b))
		}
		if err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".pptx", ".potx", ".ppsx", ".pptm":
				rel, _ := filepath.Rel(root, p)
				files = append(files, file{repo: r.Name(), rel: filepath.ToSlash(rel), abs: p})
			}
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "survey: walk %s: %v\n", root, err)
		}
	}
	// Directly downloaded web decks: source "web", licence from the TSV.
	web := loadWebSources(webTSV)
	if ents, err := os.ReadDir(webDir); err == nil {
		for _, e := range ents {
			if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".pptx" {
				continue
			}
			slug := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			w := web[slug]
			files = append(files, file{repo: "web", rel: e.Name(), abs: filepath.Join(webDir, e.Name()), url: w[0], license: w[1]})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].repo != files[j].repo {
			return files[i].repo < files[j].repo
		}
		return files[i].rel < files[j].rel
	})

	recs := make([]*Record, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rec := survey(f)
			rec.Commit = commits[f.repo]
			recs[i] = rec
		}()
	}
	wg.Wait()

	// Dedupe by sha256: first path in sorted order wins.
	bySHA := map[string]*Record{}
	var uniq []*Record
	for _, r := range recs {
		if r.SHA256 == "" {
			continue
		}
		if prev, ok := bySHA[r.SHA256]; ok {
			prev.AlsoAt = append(prev.AlsoAt, r.Source+":"+r.Path)
			continue
		}
		bySHA[r.SHA256] = r
		uniq = append(uniq, r)
	}
	sort.Slice(uniq, func(i, j int) bool { return uniq[i].SHA256 < uniq[j].SHA256 })

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, r := range uniq {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "survey: %d files, %d unique -> %s\n", len(files), len(uniq), out)
	return nil
}

// survey builds a Record for one deck, hashing, scoring and flagging hard cases.
func survey(f file) *Record {
	info := repoInfo[f.repo]
	if f.repo == "web" {
		info.URL, info.SPDX = f.url, f.license
	}
	rec := &Record{Source: f.repo, SourceURL: info.URL, License: info.SPDX, Path: f.rel}
	data, err := os.ReadFile(f.abs)
	if err != nil {
		rec.Error = err.Error()
		return rec
	}
	sum := sha256.Sum256(data)
	rec.SHA256 = hex.EncodeToString(sum[:])
	rec.Size = int64(len(data))
	zr, err := zip.NewReader(bytesReaderAt(data), int64(len(data)))
	if err != nil {
		rec.Error = "zip: " + err.Error()
		return rec
	}
	rec.OpensZip = true
	var slides []*zip.File
	declBad, declTotal := 0, 0
	checkDecl := func(zf *zip.File) {
		declTotal++
		if !strings.HasPrefix(readHead(zf, 60), `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`) {
			declBad++
		}
	}
	for _, zf := range zr.File {
		switch {
		case zf.Name == "ppt/presentation.xml":
			rec.HasPresXML = true
			checkDecl(zf)
		case zf.Name == "docProps/app.xml":
			s := readPart(zf)
			rec.Application = tagText(s, "Application")
			rec.AppVersion = tagText(s, "AppVersion")
		case zf.Name == "docProps/core.xml":
			s := readPart(zf)
			rec.Creator = tagText(s, "dc:creator")
			rec.LastModBy = tagText(s, "cp:lastModifiedBy")
		case slideRe.MatchString(zf.Name):
			slides = append(slides, zf)
			checkDecl(zf)
		}
	}
	sort.Slice(slides, func(i, j int) bool { return natLess(slides[i].Name, slides[j].Name) })
	rec.Slides = len(slides)
	var top []float64
	for _, zf := range slides {
		st, err := scanSlide(zf)
		if err != nil {
			rec.ParseErrs++
		}
		rec.add(st)
		sc := st.score()
		rec.ScoreTotal += sc
		top = append(top, sc)
		if sc > rec.BestScore {
			rec.BestScore = sc
			rec.BestSlide = strings.TrimSuffix(strings.TrimPrefix(zf.Name, "ppt/slides/"), ".xml")
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(top)))
	for i := 0; i < len(top) && i < 3; i++ {
		rec.Score += top[i]
	}
	app := strings.ToLower(rec.Application)
	rec.PowerPoint = rec.HasPresXML && strings.Contains(app, "microsoft") && strings.Contains(app, "powerpoint")
	if rec.PowerPoint {
		meta := strings.ToLower(rec.Creator + " " + rec.LastModBy)
		switch {
		case strings.Contains(meta, "python-pptx") || strings.Contains(meta, "aspose"):
			rec.Suspect = "metadata names a non-PowerPoint generator"
		case declBad > 0:
			rec.Suspect = fmt.Sprintf("%d of %d presentation/slide parts have a non-PowerPoint XML declaration (rewritten by another tool, e.g. lxml or the Open XML SDK)", declBad, declTotal)
		}
	}
	rec.Native = rec.PowerPoint && rec.Suspect == ""
	if rec.AltContentHard() {
		rec.HardCases = append(rec.HardCases, "alternate_content")
	}
	if rec.RotFlipGrp > 0 {
		rec.HardCases = append(rec.HardCases, "rotated_or_flipped_group")
	}
	if rec.DupIDs > 0 {
		rec.HardCases = append(rec.HardCases, "duplicate_ids")
	}
	if rec.PhNoXfrm > 0 {
		rec.HardCases = append(rec.HardCases, "placeholder_no_xfrm")
	}
	if rec.GFDgm > 0 {
		rec.HardCases = append(rec.HardCases, "smartart")
	}
	if rec.GFChart > 0 {
		rec.HardCases = append(rec.HardCases, "chart")
	}
	if rec.GFOle > 0 {
		rec.HardCases = append(rec.HardCases, "ole")
	}
	return rec
}

// AltContentHard reports whether the deck contains alternate content.
func (r *Record) AltContentHard() bool { return r.AltContent > 0 }

var (
	slideRe = regexp.MustCompile(`^ppt/slides/[^/]+\.xml$`)
	numRe   = regexp.MustCompile(`\d+`)
)

// natLess reports whether a sorts before b by its first number, then lexically.
func natLess(a, b string) bool {
	na, nb := numRe.FindString(a), numRe.FindString(b)
	if len(na) != len(nb) {
		return len(na) < len(nb)
	}
	return a < b
}

// tagText returns the first tag element's trimmed text in s, or empty.
func tagText(s, tag string) string {
	re := regexp.MustCompile(`<` + regexp.QuoteMeta(tag) + `(?:\s[^>]*)?>([^<]*)</`)
	if m := re.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// readPart returns up to 8 MiB of a zip part as string, or empty on open error.
func readPart(zf *zip.File) string {
	rc, err := zf.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, 8<<20))
	return string(b)
}

// readHead returns the first n bytes of a zip part, stripping any BOM.
func readHead(zf *zip.File, n int) string {
	rc, err := zf.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	b := make([]byte, n)
	m, _ := io.ReadFull(rc, b)
	return strings.TrimPrefix(string(b[:m]), "\ufeff")
}

type bytesReaderAt []byte

// ReadAt reads p from the byte slice at off, returning EOF when short.
func (b bytesReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

type shapeState struct{ ph, xfrm bool }

// scanSlide token-scans one slide part. Content inside mc:Fallback is
// skipped so only the branch PowerPoint reads is counted.
func scanSlide(zf *zip.File) (Stats, error) {
	var st Stats
	rc, err := zf.Open()
	if err != nil {
		return st, err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	dec.Strict = false
	var stack []xml.Name
	var fallback, grpDepth int
	var sp, pic *shapeState
	var inCxn, cxnGlue, inGF bool
	ids := map[string]int{}
	is := func(n xml.Name, space, local string) bool { return n.Space == space && n.Local == local }
	parent := func() xml.Name {
		if len(stack) == 0 {
			return xml.Name{}
		}
		return stack[len(stack)-1]
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return st, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := t.Name
			par := parent()
			stack = append(stack, n)
			if is(n, nsMC, "Fallback") {
				fallback++
				continue
			}
			if fallback > 0 {
				continue
			}
			attr := func(name string) string {
				for _, a := range t.Attr {
					if a.Name.Local == name && (a.Name.Space == "") {
						return a.Value
					}
				}
				return ""
			}
			switch {
			case is(n, nsMC, "AlternateContent"):
				st.AltContent++
			case is(n, nsP, "sp"):
				st.Sp++
				sp = &shapeState{}
			case is(n, nsP, "pic"):
				st.Pic++
				pic = &shapeState{}
			case is(n, nsP, "cxnSp"):
				st.CxnSp++
				inCxn, cxnGlue = true, false
			case is(n, nsA, "stCxn"), is(n, nsA, "endCxn"):
				if inCxn {
					cxnGlue = true
				}
			case is(n, nsP, "grpSp"):
				st.GrpSp++
				grpDepth++
				if grpDepth > st.MaxGrpDepth {
					st.MaxGrpDepth = grpDepth
				}
			case is(n, nsA, "xfrm"):
				switch {
				case is(par, nsP, "grpSpPr"):
					rot := attr("rot")
					if (rot != "" && rot != "0") || truthy(attr("flipH")) || truthy(attr("flipV")) {
						st.RotFlipGrp++
					}
				case is(par, nsP, "spPr"):
					if sp != nil {
						sp.xfrm = true
					}
					if pic != nil {
						pic.xfrm = true
					}
				}
			case is(n, nsP, "ph"):
				if is(par, nsP, "nvPr") {
					if sp != nil {
						sp.ph = true
					}
					if pic != nil {
						pic.ph = true
					}
				}
			case is(n, nsA, "custGeom"):
				st.CustGeom++
			case is(n, nsP, "graphicFrame"):
				st.GraphicFrame++
				inGF = true
			case is(n, nsA, "graphicData"):
				if inGF {
					uri := attr("uri")
					switch {
					case strings.HasSuffix(uri, "/chart"):
						st.GFChart++
					case strings.HasSuffix(uri, "/diagram"):
						st.GFDgm++
					case strings.HasSuffix(uri, "/table"):
						st.GFTable++
					case strings.HasSuffix(uri, "/ole"):
						st.GFOle++
					default:
						st.GFOther++
					}
				}
			case is(n, nsP, "cNvPr"):
				if par.Space == nsP && strings.HasPrefix(par.Local, "nv") {
					ids[attr("id")]++
				}
			}
		case xml.EndElement:
			n := t.Name
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if is(n, nsMC, "Fallback") {
				fallback--
				continue
			}
			if fallback > 0 {
				continue
			}
			switch {
			case is(n, nsP, "sp") && sp != nil:
				if sp.ph {
					if !sp.xfrm {
						st.PhNoXfrm++
					}
				} else {
					st.NonPhSp++
				}
				sp = nil
			case is(n, nsP, "pic") && pic != nil:
				if pic.ph && !pic.xfrm {
					st.PhNoXfrm++
				}
				pic = nil
			case is(n, nsP, "cxnSp"):
				if cxnGlue {
					st.CxnGlued++
				}
				inCxn = false
			case is(n, nsP, "grpSp"):
				grpDepth--
			case is(n, nsP, "graphicFrame"):
				inGF = false
			}
		}
	}
	for _, c := range ids {
		if c > 1 {
			st.DupIDs += c - 1
		}
	}
	return st, nil
}

// truthy reports whether s is the XML boolean true, "1" or "true".
func truthy(s string) bool { return s == "1" || s == "true" }
