package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The kinds of write plans.
const (
	kindPositive = "positive" // a valid plan the oracle should apply
	kindNegative = "negative" // a valid plan aimed to fail at apply time
	kindRule     = "rule"     // one plan per validation rule of the op table
	kindSchema   = "schema"   // a plan that breaks the op's field shape
	kindDirty    = "dirty"    // a valid plan on a source with pre-existing findings
)

var planKinds = []string{kindPositive, kindNegative, kindRule, kindSchema, kindDirty}

// The change modes every plan runs in.
var writeModes = []string{"track", "direct"}

// writeClock is the time both engines stamp on revisions in a recorded
// run (docxdriver's DOCXDRIVER_NOW, the Go CLI's --clock).
const writeClock = "2000-01-01T00:00:00Z"

// writeAuthor is the plan author unless a plan names another.
const writeAuthor = "Oracle"

// A WritePlan is one recorded plan: the operations in the Go docx CLI's
// JSON form, run on one source document in both change modes.
type WritePlan struct {
	// ID is unique and stable: <op>/<kind>/<source tag>/<variant>.
	ID string `json:"id"`
	// Op is the operation under test (the plans file it lives in).
	Op string `json:"op"`
	// Kind is positive, negative, rule or schema.
	Kind string `json:"kind"`
	// Set and File name the source document (see sources.tsv).
	Set  string `json:"set"`
	File string `json:"file"`
	// Note says what the plan covers.
	Note string `json:"note"`
	// Author overrides writeAuthor when set.
	Author string `json:"author,omitempty"`
	// Ops are the operations, in the agent contract's form
	// (docs/contract.md; translate.go turns them into docxdriver's).
	Ops json.RawMessage `json:"ops"`
	// Equivalent, for a Go-only op (text.insert), are contract ops
	// docxdriver can run whose result the case must equal: the oracle
	// records those instead (see compareWrite for which projections).
	Equivalent json.RawMessage `json:"equivalent,omitempty"`
	// Plan, when set, is the whole plan text (base, author, change_mode
	// and ops are then not filled in), for plan-level negative cases; the
	// literal "$BASE" in it is replaced by the source's hash and "$MODE"
	// by the change mode.
	Plan string `json:"plan,omitempty"`
	// Base overrides the plan base: "stale" uses a hash of other bytes.
	Base string `json:"base,omitempty"`
	// CommitKey, when set, commits with this preview key instead of the
	// one the preview returned.
	CommitKey string `json:"commit_key,omitempty"`
	// Rule is the rule's index in the op's Rules, for kind rule.
	Rule *int `json:"rule,omitempty"`
}

// staleBase is a well-formed base no source has.
const staleBase = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// planText builds the contract-form plan text for a source hash and mode
// (translate.go turns it into docxdriver's).
func (p *WritePlan) planText(sourceHash, mode string) string {
	return p.planWith(p.Ops, sourceHash, mode)
}

// oracleText is the contract plan the oracle runs: the Equivalent ops for
// a Go-only case, else the plan itself.
func (p *WritePlan) oracleText(sourceHash, mode string) string {
	if len(p.Equivalent) > 0 {
		return p.planWith(p.Equivalent, sourceHash, mode)
	}
	return p.planText(sourceHash, mode)
}

// planWith builds the plan text for ops, source hash and mode, honouring overrides.
func (p *WritePlan) planWith(ops json.RawMessage, sourceHash, mode string) string {
	base := "sha256:" + sourceHash
	if p.Base == "stale" {
		base = staleBase
	}
	if p.Plan != "" {
		return strings.NewReplacer("$BASE", base, "$MODE", mode).Replace(p.Plan)
	}
	author := writeAuthor
	if p.Author != "" {
		author = p.Author
	}
	return `{"base":` + jsonString(base) + `,"author":` + jsonString(author) + `,"change_mode":` + jsonString(mode) + `,"ops":` + string(ops) + `}`
}

// writeDir is the write baseline's directory.
func (c *config) writeDir() string { return filepath.Join(c.baseline, "write") }

// plansPath is the plans file of one operation.
func (c *config) plansPath(op string) string {
	return filepath.Join(c.writeDir(), "plans", op+".jsonl")
}

// loadPlans reads every plans file, in op table order then file order.
func (c *config) loadPlans() ([]*WritePlan, error) {
	var out []*WritePlan
	for _, op := range opNames() {
		b, err := os.ReadFile(c.plansPath(op))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(nil, 1<<24)
		for n := 1; sc.Scan(); n++ {
			if len(bytes.TrimSpace(sc.Bytes())) == 0 {
				continue
			}
			var p WritePlan
			dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", c.plansPath(op), n, err)
			}
			out = append(out, &p)
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// savePlans writes the plans, one file per op, one compact line each.
func (c *config) savePlans(plans []*WritePlan) error {
	byOp := map[string][]*WritePlan{}
	for _, p := range plans {
		byOp[p.Op] = append(byOp[p.Op], p)
	}
	dir := filepath.Join(c.writeDir(), "plans")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, op := range opNames() {
		var b bytes.Buffer
		for _, p := range byOp[op] {
			line, err := marshalLine(p)
			if err != nil {
				return err
			}
			b.Write(line)
		}
		if b.Len() == 0 {
			continue
		}
		if err := os.WriteFile(c.plansPath(op), b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// marshalLine encodes v as one JSON line without HTML escaping.
func marshalLine(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// selectPlans filters plans by -ops, -kinds and -only (a substring of the
// plan id or source file).
func selectPlans(plans []*WritePlan, ops, kinds, only string) []*WritePlan {
	set := func(s string) map[string]bool {
		if s == "" {
			return nil
		}
		m := map[string]bool{}
		for v := range strings.SplitSeq(s, ",") {
			m[strings.TrimSpace(v)] = true
		}
		return m
	}
	opSet, kindSet := set(ops), set(kinds)
	var out []*WritePlan
	for _, p := range plans {
		if opSet != nil && !opSet[p.Op] {
			continue
		}
		if kindSet != nil && !kindSet[p.Kind] {
			continue
		}
		if only != "" && !strings.Contains(p.ID, only) && !strings.Contains(p.File, only) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// checkPlanIDs fails on duplicate ids or ids filed under the wrong op.
func checkPlanIDs(plans []*WritePlan) error {
	seen := map[string]bool{}
	for _, p := range plans {
		if seen[p.ID] {
			return fmt.Errorf("duplicate plan id %s", p.ID)
		}
		seen[p.ID] = true
		if !strings.HasPrefix(p.ID, p.Op+"/") {
			return fmt.Errorf("plan %s is filed under op %s", p.ID, p.Op)
		}
		if !slices.Contains(planKinds, p.Kind) {
			return fmt.Errorf("plan %s: unknown kind %q", p.ID, p.Kind)
		}
	}
	return nil
}
