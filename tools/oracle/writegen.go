package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// raw is JSON text a plan builder inserts as is.
type raw string

// obj builds a JSON object in the order its key/value pairs are given;
// a nil value leaves the member out.
func obj(kv ...any) string {
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == nil {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(jsonString(kv[i].(string)))
		b.WriteByte(':')
		b.WriteString(jsonValue(kv[i+1]))
	}
	b.WriteByte('}')
	return b.String()
}

// jsonValue renders v as JSON text for obj, panicking on unsupported types.
func jsonValue(v any) string {
	switch x := v.(type) {
	case string:
		return jsonString(x)
	case raw:
		return string(x)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = jsonString(s)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	panic(fmt.Sprintf("obj: unsupported value %T", v)) // a bug in a builder, not input
}

// arr joins JSON item texts into a JSON array raw value.
func arr(items ...string) raw { return raw("[" + strings.Join(items, ",") + "]") }

// gen accumulates plans.
type gen struct {
	plans []*WritePlan
	n     map[string]int
}

// add files a plan of ops (JSON objects) for op on source f.
func (g *gen) add(op, kind string, f *docFacts, note string, ops ...string) *WritePlan {
	tag := f.set + ":" + strings.ReplaceAll(strings.TrimSuffix(f.file, ".docx"), "/", ".")
	// Cases are keyed by op, kind, source and what they cover, not by
	// generation order or plan bytes, so ids survive regeneration.
	id := op + "/" + kind + "/" + tag + "/" + slug(note)
	g.n[id]++
	if n := g.n[id]; n > 1 {
		id += "-" + strconv.Itoa(n)
	}
	p := &WritePlan{ID: id, Op: op, Kind: kind, Set: f.set, File: f.file, Note: note,
		Ops: json.RawMessage(mustUp("[" + strings.Join(ops, ",") + "]"))}
	g.plans = append(g.plans, p)
	return p
}

// sourcePath is the file of a source.
func (c *config) sourcePath(set, file string) string {
	var dir string
	switch set {
	case setFixtures:
		dir = c.docxdriver
	case setAuthored:
		dir = filepath.Join(c.root, "testdata", "docx")
	case setCorpus:
		dir = c.corpus
	case setWrite:
		dir = filepath.Join(c.root, filepath.FromSlash(writeFixtureDir))
	}
	return filepath.Join(dir, filepath.FromSlash(file))
}

// sourcesPath returns the write sources TSV path.
func (c *config) sourcesPath() string { return filepath.Join(c.writeDir(), "sources.tsv") }

// writeGen picks the sources, reads them with the oracle and writes the
// plans files and sources.tsv.
func writeGen(c *config, oracle string, perFeature int) error {
	var ins []input
	for _, name := range writeFixtureNames {
		ins = append(ins, input{set: setWrite, dir: filepath.Join(c.root, filepath.FromSlash(writeFixtureDir)), path: name})
	}
	ins = append(ins, input{set: setAuthored, dir: filepath.Join(c.root, "testdata", "docx"), path: "rich.docx"})
	fx, err := c.inputs(setFixtures)
	if err != nil {
		return err
	}
	ins = append(ins, fx...)
	facts, err := gatherAll(oracle, ins, c.jobs)
	if err != nil {
		return err
	}
	for _, f := range facts {
		// badroot.docx's defect (a content-types root that is not Types)
		// is not an integrity finding, but it belongs to the dirty family.
		if f.set == setWrite && f.file == "badroot.docx" && f.findings == 0 {
			f.findings = 1
		}
	}
	if !slices.ContainsFunc(facts, func(f *docFacts) bool { return f.file == "lazy.docx" }) {
		facts = append(facts, lazyFacts())
	}
	corpus, sweep, dirty, err := selectCorpus(c, oracle, perFeature)
	if err != nil {
		return err
	}
	facts = append(facts, corpus...)
	for _, f := range dirty {
		if !slices.Contains(facts, f) {
			facts = append(facts, f)
		}
	}
	g := &gen{n: map[string]int{}}
	// The revision_settle sweep (P4c's live differential, folded in):
	// every small corpus file with revisions, all and the first id, accept
	// and reject, in both modes.
	var sweepOnly []*docFacts
	for _, f := range sweep {
		if !slices.Contains(corpus, f) {
			sweepOnly = append(sweepOnly, f)
			settleByPlans(g, f, kindPositive)
		}
		for _, target := range []string{"all", f.revisions[0].id} {
			for _, action := range []string{"accept", "reject"} {
				kind := "all"
				if target != "all" {
					kind = "first id"
				}
				g.add("revision_settle", kindPositive, f, "sweep "+kind+" "+action, obj("op", "revision_settle", "target", target, "action", action))
			}
		}
	}
	for _, f := range facts {
		switch f.set {
		case setWrite:
			fixturePlans(g, f)
			sitePlans(g, f)
		case setAuthored:
			sitePlans(g, f)
		}
		kind := kindPositive
		if f.findings > 0 {
			kind = kindDirty
		}
		positivePlans(g, f, kind)
		if kind == kindPositive {
			settleByPlans(g, f, kind)
		}
		negativePlans(g, f)
	}
	// Rule and schema plans run on the mixed fixture: validation comes
	// before the document is looked at, so one source is enough.
	for _, f := range facts {
		if f.set == setWrite && f.file == "mixed.docx" {
			rulePlans(g, f)
			schemaPlans(g, f)
		}
	}
	facts = append(facts, sweepOnly...)
	// rich-bare-text.docx makes the oracle panic (a w:t outside a w:r):
	// its read fails, so its one case is written without facts.
	bare := &docFacts{set: setAuthored, file: "rich-bare-text.docx"}
	g.add("revision_settle", kindPositive, bare, "sweep all accept", obj("op", "revision_settle", "target", "all", "action", "accept"))
	facts = append(facts, bare)
	if err := saveParaIDKnown(c, facts); err != nil {
		return err
	}
	if err := checkPlanIDs(g.plans); err != nil {
		return err
	}
	if err := c.savePlans(g.plans); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# set\tfile\tsha256\tfeatures (sources of the write baseline; corpus-lo files picked by `oracle write gen`)\n")
	for _, f := range facts {
		src, err := os.ReadFile(c.sourcePath(f.set, f.file))
		if err != nil {
			return err
		}
		s := sha256.Sum256(src)
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", f.set, f.file, hex.EncodeToString(s[:]), strings.Join(f.features(), ","))
	}
	if err := os.WriteFile(c.sourcesPath(), []byte(b.String()), 0o644); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, p := range g.plans {
		counts[p.Kind]++
	}
	fmt.Printf("write gen: %d sources, %d plans (%v)\n", len(facts), len(g.plans), counts)
	return nil
}

// gatherAll reads every input's facts in parallel; inputs the oracle
// cannot read are skipped.
func gatherAll(oracle string, ins []input, jobs int) ([]*docFacts, error) {
	out := make([]*docFacts, len(ins))
	var wg sync.WaitGroup
	sem := make(chan struct{}, jobs)
	for i, in := range ins {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			f, err := gatherFacts(oracle, in)
			if err != nil {
				fmt.Fprintf(os.Stderr, "write gen: skip %s/%s: %v\n", in.set, in.path, err)
				return
			}
			out[i] = f
		})
	}
	wg.Wait()
	return slices.DeleteFunc(out, func(f *docFacts) bool { return f == nil }), nil
}

// corpusFeatures are the features corpus files are picked for, each by
// the perFeature smallest files that have it and enough plain text.
var corpusFeatures = []string{"merged-cells", "tracked-rows", "sections", "first-even-stories", "equations",
	"display-equations", "comment-replies", "comments", "bookmarks", "revision-authors", "lists", "missing-paraids", "tables"}

// dirtyCorpus is how many corpus files the dirty family takes.
const dirtyCorpus = 8

// selectCorpus reads the corpus files under 60 KB and picks sources by
// feature, deterministically (smallest first, then path).
func selectCorpus(c *config, oracle string, perFeature int) (picked, withRevisions, dirty []*docFacts, err error) {
	all, err := c.inputs(setCorpus)
	if err != nil {
		return nil, nil, nil, err
	}
	var small []input
	for _, in := range all {
		st, err := os.Stat(filepath.Join(in.dir, filepath.FromSlash(in.path)))
		if err == nil && st.Size() < 60<<10 {
			small = append(small, in)
		}
	}
	facts, err := gatherAll(oracle, small, c.jobs)
	if err != nil {
		return nil, nil, nil, err
	}
	slices.SortFunc(facts, func(a, b *docFacts) int {
		if a.size != b.size {
			return a.size - b.size
		}
		return strings.Compare(a.file, b.file)
	})
	chosen := map[string]bool{}
	var out []*docFacts
	for _, feat := range corpusFeatures {
		n := 0
		for _, f := range facts {
			if n == perFeature {
				break
			}
			fs := f.features()
			if !slices.Contains(fs, feat) || !slices.Contains(fs, "text") {
				continue
			}
			n++
			if !chosen[f.file] {
				chosen[f.file] = true
				out = append(out, f)
			}
		}
	}
	slices.SortFunc(out, func(a, b *docFacts) int { return strings.Compare(a.file, b.file) })
	for _, f := range facts {
		if len(f.revisions) > 0 {
			withRevisions = append(withRevisions, f)
		}
	}
	slices.SortFunc(withRevisions, func(a, b *docFacts) int { return strings.Compare(a.file, b.file) })
	// The dirty family: the smallest files whose input already has
	// integrity or schema findings, one per distinct finding count so
	// different defects are likelier.
	seen := map[int]bool{}
	for _, f := range facts {
		if f.findings > 0 && !seen[f.findings] && slices.Contains(f.features(), "text") && len(dirty) < dirtyCorpus {
			seen[f.findings] = true
			dirty = append(dirty, f)
		}
	}
	return out, withRevisions, dirty, nil
}

// writeGenCmd is `oracle write gen`.
func writeGenCmd(args []string) error {
	fs := flag.NewFlagSet("write gen", flag.ExitOnError)
	var c config
	c.flags(fs)
	oracle := oracleFlag(fs)
	per := fs.Int("per-feature", 2, "corpus files picked per feature")
	fs.Parse(args)
	if err := c.resolve(); err != nil {
		return err
	}
	return writeGen(&c, c.oracleBin(*oracle), *per)
}

// slug is a case's note as an id component: lower-case words joined by
// hyphens.
func slug(note string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(note) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	s := b.String()
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	return s
}

// saveParaIDKnown writes known/write-paraid.tsv, P4a's intended
// difference: the Go engine binds repaired paraIds (paragraphs without a
// w14:paraId) to their nodes when the document is opened, content-derived
// from source, plan seed, op and slot (docx edit.New with Options{Source,
// PlanSeed}); docxdriver re-derives them at the first paragraph
// resolution and again at emit, so after a table operation its ids drift
// and a copied paragraph can take its original's id. Every table
// structure case on a source with repaired ids may project other ids.
func saveParaIDKnown(c *config, facts []*docFacts) error {
	var b strings.Builder
	b.WriteString("# Generated by `oracle write gen` (P4a, 2026-11-13): repaired paraIds are bound at open in the Go engine;\n")
	b.WriteString("# docxdriver re-derives them at first resolution and at emit, so table operations on sources with\n")
	b.WriteString("# repaired ids may project different paragraph ids. plan id\tmode\tfield\treason\n")
	for _, f := range facts {
		if f.repairs == 0 {
			continue
		}
		tag := f.set + ":" + strings.ReplaceAll(strings.TrimSuffix(f.file, ".docx"), "/", ".")
		for _, op := range []string{"edit_table", "insert_table"} {
			for _, kind := range []string{kindPositive, kindNegative} {
				fmt.Fprintf(&b, "%s/%s/%s/*\t*\tproj:*\tparaId repair: Go binds repaired ids at open, docxdriver re-derives them after table edits (P4a)\n", op, kind, tag)
			}
		}
	}
	dir := filepath.Join(c.baseline, "known")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "write-paraid.tsv"), []byte(b.String()), 0o644)
}

// mustUp translates docxdriver-shaped ops written by a generator into the
// contract form; a failure is a bug in a generator or the translator.
func mustUp(ops string) string {
	out, err := upOps(ops)
	if err != nil {
		panic(fmt.Sprintf("upOps %s: %v", ops, err))
	}
	return out
}

// addContract files a case written directly in the contract form (for
// what docxdriver's shapes cannot say: text.replace's all, text.insert,
// the delete field).
func (g *gen) addContract(op, kind string, f *docFacts, note string, ops ...string) *WritePlan {
	p := g.add(op, kind, f, note)
	p.Ops = json.RawMessage("[" + strings.Join(ops, ",") + "]")
	return p
}

// lazyFacts describes lazy.docx, which docxdriver cannot open (it reads
// every member, and one is corrupt), from its builder.
func lazyFacts() *docFacts {
	p := func(id, text, style string) paraFact {
		return paraFact{id: id, text: text, selectable: text, style: style}
	}
	return &docFacts{set: setWrite, file: "lazy.docx", findings: 1,
		paras: []paraFact{
			p("60000001", "Lazy fixture", "Heading1"),
			p("60000002", "Only this paragraph and its neighbours are read.", ""),
			p("60000003", "The last paragraph.", ""),
		},
		sections: []sectionFact{{index: 1, handle: "section:final"}},
	}
}
