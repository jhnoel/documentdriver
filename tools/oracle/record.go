package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// record runs the pinned docxdriver CLI over the inputs and writes the
// baseline: the manifest of each set, the full fixture outputs in the
// baseline and the full corpus outputs under -out.
func record(args []string) error {
	fs := flag.NewFlagSet("record", flag.ExitOnError)
	var c config
	c.flags(fs)
	oracle := fs.String("oracle", os.Getenv("DOCX_ORACLE"), "docxdriver CLI built at 1ffb0d7 (default $DOCX_ORACLE, else <root>/bin/oracle-target/release/docxdriver)")
	fs.Parse(args)
	if err := c.resolve(); err != nil {
		return err
	}
	if *oracle == "" {
		*oracle = filepath.Join(c.root, "bin", "oracle-target", "release", "docxdriver")
	}
	version, err := exec.Command(*oracle, "--version").Output()
	if err != nil {
		return fmt.Errorf("%s --version: %w", *oracle, err)
	}
	ks, err := c.selectCases(func(Case) bool { return true })
	if err != nil {
		return err
	}
	for _, set := range c.sets {
		ins, err := c.inputs(set)
		if err != nil {
			return err
		}
		m, err := loadManifest(c.manifestPath(set))
		if err != nil {
			return err
		}
		m.comments = []string{"# oracle: " + strings.TrimSpace(string(version)) + " (docxdriver 1ffb0d7)"}
		full := filepath.Join(c.out, "oracle", set)
		if set != setCorpus {
			full = filepath.Join(c.baseline, set)
		}
		var jobs []job
		for _, in := range ins {
			for _, k := range ks {
				jobs = append(jobs, job{in, k})
			}
		}
		var mu sync.Mutex
		err = runAll(jobs, c.jobs, func(j job) error {
			r, err := runCLI(*oracle, j.in, j.k)
			if err != nil {
				return err
			}
			if err := writeOutputs(full, j.in.path, j.k.Name, r); err != nil {
				return err
			}
			mu.Lock()
			m.entries[key{j.in.path, j.k.Name}] = entryOf(j.in.path, j.k.Name, r)
			mu.Unlock()
			return nil
		})
		if err != nil {
			return err
		}
		if err := m.save(c.manifestPath(set)); err != nil {
			return err
		}
		fmt.Printf("%s: %d inputs x %d cases recorded\n", set, len(ins), len(ks))
	}
	return nil
}
