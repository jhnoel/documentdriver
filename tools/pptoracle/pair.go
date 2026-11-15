package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Naming convention for smoke inputs:
//
//	<name>--source.pptx            the unedited deck
//	<name>--<op>[-<more>].pptx     an output made from it by operation <op>
//
// Any other .pptx is treated as a standalone source with no outputs.
const sourceLabel = "source"

// group is one source deck and the outputs that must be judged against it.
type group struct {
	Name    string
	Source  string   // "" when the outputs have no source on disk
	Outputs []string // in the order they are opened
}

// collect expands files and directories (non-recursive) into .pptx paths,
// sorted and de-duplicated.
func collect(args []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, a := range args {
		st, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			add(a)
			continue
		}
		ents, err := os.ReadDir(a)
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || strings.HasPrefix(n, "~$") || !strings.EqualFold(filepath.Ext(n), ".pptx") {
				continue
			}
			add(filepath.Join(a, n))
		}
	}
	sort.Strings(out)
	return out, nil
}

// splitName splits "deck--move-ab12.pptx" into ("deck", "move-ab12").
// label is "" for a file without "--".
func splitName(path string) (name, label string) {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name, label, _ = strings.Cut(stem, "--")
	return name, label
}

// opOf returns the operation part of a label: "move" for "move-ab12".
func opOf(label string) string {
	op, _, _ := strings.Cut(label, "-")
	return op
}

// pair groups files by the naming convention. Unless all is set, only the
// first output per source and operation is kept. With explicitSource set,
// every file is an output of that one source.
func pair(files []string, all bool, explicitSource string) ([]group, error) {
	if explicitSource != "" {
		g := group{Name: splitNameOnly(explicitSource), Source: explicitSource}
		for _, f := range files {
			if f != explicitSource {
				g.Outputs = append(g.Outputs, f)
			}
		}
		if len(g.Outputs) == 0 {
			return nil, fmt.Errorf("--source given but no outputs to check")
		}
		return []group{g}, nil
	}
	byName := map[string]*group{}
	var order []string
	get := func(name string) *group {
		g, ok := byName[name]
		if !ok {
			g = &group{Name: name}
			byName[name] = g
			order = append(order, name)
		}
		return g
	}
	seenOp := map[string]bool{}
	for _, f := range files {
		name, label := splitName(f)
		g := get(name)
		switch label {
		case "", sourceLabel:
			if g.Source != "" {
				return nil, fmt.Errorf("two sources for %q: %s and %s", name, g.Source, f)
			}
			g.Source = f
		default:
			key := name + "\x00" + opOf(label)
			if !all && seenOp[key] {
				continue
			}
			seenOp[key] = true
			g.Outputs = append(g.Outputs, f)
		}
	}
	out := make([]group, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}
	return out, nil
}

// splitNameOnly returns the deck name in a file path.
func splitNameOnly(path string) string {
	n, _ := splitName(path)
	return n
}
