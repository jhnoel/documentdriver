package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// maxEntry bounds what verifyWheel decompresses per entry; a wheel's
// largest entry is one binary of a few tens of MiB.
const maxEntry = 512 << 20

// dataDescriptorFlag is general-purpose bit 3 of a zip entry: its CRC-32
// and sizes follow the data in a descriptor instead of the local header.
// PyPI refuses wheels with one (https://docs.pypi.org/archives).
const dataDescriptorFlag = 0x8

// verifyWheel checks a wheel against what makeWheel promises: RECORD lists
// every other entry exactly once with its size and hash, and nothing
// else; the WHEEL Tag lines are exactly tags, in order; METADATA names
// the licence expression and every licence file, which the wheel
// carries (the fonts' with the tools'); the binaries
// are executable; and _digests.py records their SHA-256. It returns
// the binaries by name.
func verifyWheel(b []byte, pyVersion string, tags []string) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("open the wheel: %w", err)
	}
	if zr.Comment != "" {
		return nil, errors.New("the wheel has an archive comment, which PyPI rejects")
	}
	info := distInfo(pyVersion)
	entries := map[string][]byte{}
	var order []string
	for _, f := range zr.File {
		if _, dup := entries[f.Name]; dup {
			return nil, fmt.Errorf("entry %s appears twice", f.Name)
		}
		if f.Flags&dataDescriptorFlag != 0 {
			return nil, fmt.Errorf("entry %s has a data descriptor, which PyPI rejects", f.Name)
		}
		if f.Comment != "" {
			return nil, fmt.Errorf("entry %s has a comment, which PyPI rejects", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("entry %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxEntry+1))
		cerr := rc.Close()
		if err != nil {
			return nil, fmt.Errorf("entry %s: %w", f.Name, err)
		}
		if cerr != nil {
			return nil, fmt.Errorf("entry %s: %w", f.Name, cerr)
		}
		if len(data) > maxEntry {
			return nil, fmt.Errorf("entry %s is over %d bytes", f.Name, maxEntry)
		}
		if strings.HasPrefix(f.Name, distName+"/bin/") && f.Mode()&0o111 == 0 {
			return nil, fmt.Errorf("binary %s is not executable in the wheel", f.Name)
		}
		entries[f.Name] = data
		order = append(order, f.Name)
	}
	recordName := info + "/RECORD"
	record, ok := entries[recordName]
	if !ok {
		return nil, fmt.Errorf("no %s", recordName)
	}
	if order[len(order)-1] != recordName {
		return nil, fmt.Errorf("%s is not the last entry", recordName)
	}
	rows, err := csv.NewReader(bytes.NewReader(record)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse RECORD: %w", err)
	}
	listed := map[string]bool{}
	for _, row := range rows {
		if len(row) != 3 {
			return nil, fmt.Errorf("RECORD row %q: want 3 fields", row)
		}
		name := row[0]
		if listed[name] {
			return nil, fmt.Errorf("RECORD lists %s twice", name)
		}
		listed[name] = true
		data, ok := entries[name]
		if !ok {
			return nil, fmt.Errorf("RECORD lists %s, which the wheel lacks", name)
		}
		if name == recordName {
			if row[1] != "" || row[2] != "" {
				return nil, fmt.Errorf("RECORD's own row must have no hash or size")
			}
			continue
		}
		if got := recordHash(data); row[1] != got {
			return nil, fmt.Errorf("%s: RECORD hash %s, content %s", name, row[1], got)
		}
		if size, err := strconv.Atoi(row[2]); err != nil || size != len(data) {
			return nil, fmt.Errorf("%s: RECORD size %q, content %d bytes", name, row[2], len(data))
		}
	}
	for name := range entries {
		if !listed[name] {
			return nil, fmt.Errorf("%s is not in RECORD", name)
		}
	}
	if got, want := string(entries[info+"/WHEEL"]), string(wheelMeta(tags)); got != want {
		return nil, fmt.Errorf("WHEEL is %q, want %q", got, want)
	}
	meta := string(entries[info+"/METADATA"])
	if !strings.HasPrefix(meta, "Metadata-Version: 2.4\n") || !strings.Contains(meta, "\nVersion: "+pyVersion+"\n") ||
		!strings.Contains(meta, "\nLicense-Expression: "+licenseExpression+"\n") {
		return nil, fmt.Errorf("METADATA is not version 2.4 with Version %s and License-Expression %s", pyVersion, licenseExpression)
	}
	for _, l := range licenseFiles {
		if !strings.Contains(meta, "\nLicense-File: "+l.Name+"\n") {
			return nil, fmt.Errorf("METADATA names no License-File %s", l.Name)
		}
		if len(entries[info+"/licenses/"+l.Name]) == 0 {
			return nil, fmt.Errorf("the wheel has no %s/licenses/%s", info, l.Name)
		}
	}
	bins := map[string][]byte{}
	for name, data := range entries {
		if bin, ok := strings.CutPrefix(name, distName+"/bin/"); ok {
			bins[bin] = data
		}
	}
	if len(bins) == 0 {
		return nil, errors.New("the wheel has no binaries")
	}
	if got, want := string(entries[distName+"/_digests.py"]), string(digestsModule(pyVersion, bins)); got != want {
		return nil, fmt.Errorf("_digests.py does not match the binaries:\n%s", got)
	}
	return bins, nil
}

// versionInfo is what `<tool> --version` prints.
type versionInfo struct {
	Tool     string `json:"tool"`
	Version  string `json:"version"`
	Contract int    `json:"contract"`
}

// runVersion runs `<bin> --version` and checks it names tool and version.
func runVersion(bin, tool, version string) error {
	out, err := exec.Command(bin, "--version").Output() //nolint:gosec // G204: a binary this release just built
	if err != nil {
		return fmt.Errorf("%s --version: %w", bin, err)
	}
	var v versionInfo
	if err := json.Unmarshal(out, &v); err != nil {
		return fmt.Errorf("%s --version printed %q: %w", bin, out, err)
	}
	if v.Tool != tool || v.Version != version {
		return fmt.Errorf("%s --version = %s, want tool %s version %s", bin, strings.TrimSpace(string(out)), tool, version)
	}
	return nil
}

// runWheelBinaries writes the binaries of a verified wheel into a fresh
// directory under dir and runs each one's --version.
func runWheelBinaries(bins map[string][]byte, dir, version string) (err error) {
	d, err := os.MkdirTemp(dir, "wheel-bin-")
	if err != nil {
		return fmt.Errorf("unpack the wheel: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(d)) }()
	for _, name := range sortedKeys(bins) {
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, bins[name], 0o755); err != nil { //nolint:gosec // G306: an executable, written to a private temp directory
			return fmt.Errorf("unpack %s: %w", name, err)
		}
		if err := runVersion(p, name, version); err != nil {
			return err
		}
	}
	return nil
}
