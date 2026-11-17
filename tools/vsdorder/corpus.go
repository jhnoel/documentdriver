package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// pairKey is one ordered pair of child names under one parent.
type pairKey struct {
	parent, first, second string
}

// parentChildKey is one corpus parent/child pair.
type parentChildKey struct {
	parent, child string
}

// corpusStats is what one pass over the corpus records: per parent
// the ordered child occurrence pairs (counts and distinct file
// counts), per parent/child the occurrence, parent-instance and file
// counts, and the global first-appearance order of each parent's
// children (for corpus-only parents such as an undeclared element).
type corpusStats struct {
	pairCount    map[pairKey]int
	pairFiles    map[pairKey]map[string]bool
	pcCount      map[parentChildKey]int
	pcParents    map[parentChildKey]int
	pcFiles      map[parentChildKey]map[string]bool
	childCount   map[string]int // child name occurrences anywhere (Visio parts)
	parentFirst  map[string][]string
	parentMaxRep map[parentChildKey]int
}

// newCorpusStats builds empty corpus statistics.
func newCorpusStats() *corpusStats {
	return &corpusStats{
		pairCount: map[pairKey]int{}, pairFiles: map[pairKey]map[string]bool{},
		pcCount: map[parentChildKey]int{}, pcParents: map[parentChildKey]int{},
		pcFiles:    map[parentChildKey]map[string]bool{},
		childCount: map[string]int{}, parentFirst: map[string][]string{},
		parentMaxRep: map[parentChildKey]int{},
	}
}

// isVisio reports whether uri is a Visio main namespace (2011 or 2012).
func isVisio(uri string) bool {
	return vsd.IsMain(uri)
}

// listCorpusFiles returns the Visio drawing files under root (the
// corpus passed as repos/<source>/ by the caller), sorted by their
// slash-separated path relative to root.
func listCorpusFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".vsdx", ".vsdm", ".vstx", ".vstm", ".vssx", ".vssm":
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("vsdorder: list corpus %s: %w", root, err)
	}
	slices.Sort(out)
	return out, nil
}

// scanCorpusFile opens the package at path, reports whether its
// docProps/app.xml names Microsoft Visio, and, when it does, scans
// the Visio parts of it into st under the file-name fileID. A file
// that cannot be opened is an unaccepted file, never an error.
func scanCorpusFile(path, fileID string, st *corpusStats) (bool, error) {
	pkg, err := opc.Open(path)
	if err != nil {
		return false, nil
	}
	defer func() { _ = pkg.Close() }()
	accepted, err := isVisioAuthored(pkg)
	if err != nil || !accepted {
		return accepted, err
	}
	names := slices.Clone(pkg.Names())
	slices.Sort(names)
	for _, name := range names {
		if !strings.HasPrefix(strings.ToLower(name), "/visio/") || !strings.HasSuffix(strings.ToLower(name), ".xml") {
			continue
		}
		part, err := pkg.Part(name)
		if err != nil {
			continue
		}
		src, ok, err := part.OriginalString()
		if err != nil {
			return false, fmt.Errorf("vsdorder: %s %s: %w", path, name, err)
		}
		if !ok {
			continue
		}
		v := &partVisitor{st: st, fileID: fileID}
		if err := tree.ScanString(src, v); err != nil {
			return false, fmt.Errorf("vsdorder: scan %s %s: %w", path, name, err)
		}
		part.Forget()
	}
	return true, nil
}

// isVisioAuthored reports whether pkg's docProps/app.xml names
// Microsoft Visio as its Application.
func isVisioAuthored(pkg *opc.Package) (bool, error) {
	if !pkg.Has("/docProps/app.xml") {
		return false, nil
	}
	part, err := pkg.Part("/docProps/app.xml")
	if err != nil {
		return false, nil
	}
	src, ok, err := part.OriginalString()
	if err != nil || !ok {
		return false, err
	}
	doc, err := tree.ParseString(src)
	if err != nil {
		return false, nil
	}
	for _, e := range doc.Root().Elements() {
		if e.Local() != "Application" {
			continue
		}
		if e.Space() != "http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" {
			continue
		}
		return strings.TrimSpace(e.Text()) == "Microsoft Visio", nil
	}
	return false, nil
}

// parentFrame is one open element while a Visio part is scanned.
type parentFrame struct {
	name   string
	visio  bool
	counts map[string]int
}

// partVisitor is one tree.Scan visitor over a Visio part.
type partVisitor struct {
	st     *corpusStats
	fileID string
	ns     tree.NSStack
	frames []parentFrame
}

// Start records the child's ordered pairs against its Visio parent
// and pushes a frame for it.
func (v *partVisitor) Start(n *tree.Node) error {
	v.ns.Push(n)
	local := n.Local()
	visio := isVisio(v.ns.Space(n))
	if len(v.frames) > 0 {
		top := &v.frames[len(v.frames)-1]
		if top.visio && visio {
			v.record(top.name, local)
		}
	}
	// The frame's child counts map is allocated on first use: most
	// elements (every Cell and Row) have no Visio children to pair.
	v.frames = append(v.frames, parentFrame{name: local, visio: visio})
	return nil
}

// End finalises the top frame's per-child repetition maxima and pops
// it.
func (v *partVisitor) End(_ *tree.Node) error {
	if len(v.frames) > 0 {
		top := v.frames[len(v.frames)-1]
		if top.visio {
			for child, c := range top.counts {
				k := parentChildKey{parent: top.name, child: child}
				if c > v.st.parentMaxRep[k] {
					v.st.parentMaxRep[k] = c
				}
			}
		}
		v.frames = v.frames[:len(v.frames)-1]
	}
	v.ns.Pop()
	return nil
}

// Leaf ignores non-element nodes.
func (v *partVisitor) Leaf(_ *tree.Node) error { return nil }

// record folds one child occurrence named child into the open
// parent frame parent of record: ordered occurrence pairs against
// every prior distinct child, parent/child occurrence and instance
// counts, file sets and global first-appearance order.
func (v *partVisitor) record(parent, child string) {
	top := &v.frames[len(v.frames)-1]
	if top.counts == nil {
		top.counts = map[string]int{}
	}
	st := v.st
	seen := slices.Contains(st.parentFirst[parent], child)
	if !seen {
		st.parentFirst[parent] = append(st.parentFirst[parent], child)
	}
	pc := parentChildKey{parent: parent, child: child}
	st.pcCount[pc]++
	if top.counts[child] == 0 {
		st.pcParents[pc]++
	}
	if st.pcFiles[pc] == nil {
		st.pcFiles[pc] = map[string]bool{}
	}
	st.pcFiles[pc][v.fileID] = true
	st.childCount[child]++
	for prev, c := range top.counts {
		if prev == child {
			continue
		}
		k := pairKey{parent: parent, first: prev, second: child}
		st.pairCount[k] += c
		if st.pairFiles[k] == nil {
			st.pairFiles[k] = map[string]bool{}
		}
		st.pairFiles[k][v.fileID] = true
	}
	top.counts[child]++
}
