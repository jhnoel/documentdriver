package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/integrity"
)

// Parity is judged on semantics only (owner decision 2026-11-13): what
// the output document means (projections read by the oracle, after
// accept-all and reject-all too, and the integrity findings it
// introduces), and whether a plan is accepted or refused with which error
// code at which field path. Message texts, CLI wording, report layout,
// exit codes and stderr are not expected values: they are kept as triage
// material only. Each engine's CLI is reached through an applier, the
// adapter that turns its output into that semantic outcome, so the Go
// CLI's envelope can change without touching the record.

// applied is an applier's semantic outcome.
type applied struct {
	accepted bool
	code     string // the refusal's error code
	path     string // the refusal's field path ("" for none)
	message  string // triage only: tags refusal sites at record time
	// triage are the raw outputs of each CLI step, by step name.
	triage []triageOut
	// unsupported: the engine cannot run the plan yet (the Go CLI has no
	// apply command, or the operation is not implemented).
	unsupported bool
}

type triageOut struct {
	name string
	text string
}

// An applier applies a plan file to in.docx in a work directory, writing
// out.docx when the plan is accepted.
type applier interface {
	// planFile renders the plan for the engine and names the file.
	planFile(p *WritePlan, src []byte, sourceHash, mode string) (name, text string, err error)
	apply(work, planFile string, p *WritePlan) (applied, error)
}

// oracleApplier is docxdriver 0.5.2's `plan PATH PLAN [--commit KEY]
// [-o OUT]`: the contract plan translated down to a TOML plan, the time
// from DOCXDRIVER_NOW, a preview whose preview_key the commit repeats.
// Its refusals are translated up to contract codes and paths.
type oracleApplier struct{ bin string }

// planFile translates the oracle plan down to a TOML plan file for docxdriver.
func (o oracleApplier) planFile(p *WritePlan, src []byte, sourceHash, mode string) (string, string, error) {
	contract, err := gridColumns(p.oracleText(sourceHash, mode), src)
	if err != nil {
		return "", "", fmt.Errorf("translate: %w", err)
	}
	down, err := downTranslate(contract)
	if err != nil {
		return "", "", fmt.Errorf("translate: %w", err)
	}
	v, err := parseJV([]byte(down))
	if err != nil {
		return "", "", fmt.Errorf("plan JSON: %w", err)
	}
	text, err := planTOML(v)
	return "plan.toml", text, err
}

// apply previews then commits the plan through docxdriver, translating refusal codes up.
func (o oracleApplier) apply(work, planFile string, p *WritePlan) (applied, error) {
	a, err := twoStep(work, p, func(key string) (result, error) {
		args := []string{"--json", "plan", "in.docx", planFile}
		if key != "" {
			args = append(args, "--commit", key, "-o", "out.docx")
		}
		return runIn(work, o.bin, args...)
	})
	if err != nil || a.accepted {
		return a, err
	}
	a.code = upCode(a.code, a.message)
	a.path = upPath(a.path, contractOpNames(p))
	return a, nil
}

// contractOpNames lists the plan's contract op names by index.
func contractOpNames(p *WritePlan) []string {
	ops := p.Ops
	if len(p.Equivalent) > 0 {
		ops = p.Equivalent
	}
	v, err := parseJV(ops)
	if err != nil || v.kind != 'a' {
		return nil
	}
	out := make([]string, len(v.items))
	for i, op := range v.items {
		if n := op.get("op"); n != nil {
			out[i] = n.s
		}
	}
	return out
}

// goApplier is the Go docx CLI, as the agent contract (docs/contract.md
// §1, §2) specifies it: `docx apply FILE PLAN.json -o OUT`, exit 0 with
// the result on stdout, exit 1 or 2 with {"errors":[{code,path,...}]} on
// stderr; the time from the hidden --clock flag (as pptx apply takes it).
// Only this adapter knows the Go CLI's surface.
type goApplier struct{ bin string }

// planFile returns the contract plan JSON file for the Go CLI.
func (g goApplier) planFile(p *WritePlan, _ []byte, sourceHash, mode string) (string, string, error) {
	return "plan.json", p.planText(sourceHash, mode), nil
}

// apply runs the Go CLI apply and translates its result into an applied outcome.
func (g goApplier) apply(work, planFile string, _ *WritePlan) (applied, error) {
	var a applied
	r, err := runIn(work, g.bin, "apply", "in.docx", planFile, "-o", "out.docx", "--clock", writeClock)
	if err != nil {
		return a, err
	}
	a.triage = append(a.triage, triageOut{"apply", rawOutput(r)})
	raw := string(r.stdout) + string(r.stderr)
	if r.exit == 0 {
		if _, err := os.Stat(filepath.Join(work, "out.docx")); err == nil {
			a.accepted = true
			return a, nil
		}
		// A no-op plan writes nothing (contract.md §3): the output is
		// the input.
		src, err := os.ReadFile(filepath.Join(work, "in.docx"))
		if err != nil {
			return a, err
		}
		if err := os.WriteFile(filepath.Join(work, "out.docx"), src, 0o644); err != nil {
			return a, err
		}
		a.accepted = true
		return a, nil
	}
	v, err := parseJV(bytes.TrimSpace(r.stderr))
	if err == nil {
		if errs := v.get("errors"); errs != nil && errs.kind == 'a' && len(errs.items) > 0 {
			e := errs.items[0]
			if c := e.get("code"); c != nil {
				a.code = c.s
			}
			if p := e.get("path"); p != nil {
				a.path = p.s
			}
			if m := e.get("message"); m != nil {
				a.message = m.s
			}
		}
	}
	if a.code == "" {
		a.code = fmt.Sprintf("exit-%d", r.exit)
	}
	if strings.Contains(raw, "not implemented") || strings.Contains(raw, "unknown command") || strings.Contains(raw, "unrecognized subcommand") {
		a.unsupported = true
	}
	return a, nil
}

// twoStep previews, then commits with the preview key (or the plan's
// CommitKey), reading docxdriver's result JSON.
func twoStep(work string, p *WritePlan, run func(key string) (result, error)) (applied, error) {
	var a applied
	r, err := run("")
	if err != nil {
		return a, err
	}
	a.triage = append(a.triage, triageOut{"preview", rawOutput(r)})
	out := parseOutcome(r.stdout)
	if out.outcome != "previewed" {
		a.code, a.path, a.message = out.code, out.path, out.message
		if a.code == "" {
			a.code = fmt.Sprintf("exit-%d", r.exit) // no result JSON at all
		}
		return a, nil
	}
	key := out.key
	if p.CommitKey != "" {
		key = p.CommitKey
	}
	r, err = run(key)
	if err != nil {
		return a, err
	}
	a.triage = append(a.triage, triageOut{"commit", rawOutput(r)})
	out = parseOutcome(r.stdout)
	if out.outcome != "committed" {
		a.code, a.path, a.message = out.code, out.path, out.message
		if a.code == "" {
			a.code = fmt.Sprintf("exit-%d", r.exit)
		}
		return a, nil
	}
	if _, err := os.Stat(filepath.Join(work, "out.docx")); err != nil {
		a.code = "no-output"
		return a, nil //nolint:nilerr // a commit that wrote nothing is a recorded outcome
	}
	a.accepted = true
	return a, nil
}

// rawOutput is a step's triage text: the exit code, stdout without the
// output package bytes, and stderr.
func rawOutput(r result) string {
	s := fmt.Sprintf("exit %d\n%s", r.exit, dropKeys(r.stdout, "bytes"))
	if len(r.stderr) > 0 {
		s += "\nstderr:\n" + string(r.stderr)
	}
	return s
}

type outcomeJSON struct {
	outcome, key, code, path, message string
}

// parseOutcome extracts outcome, preview key and diagnostic from CLI JSON output.
func parseOutcome(stdout []byte) outcomeJSON {
	var o outcomeJSON
	v, err := parseJV(bytes.TrimSpace(stdout))
	if err != nil || v.kind != 'o' {
		return o
	}
	str := func(x *jv) string {
		if x != nil && x.kind == 's' {
			return x.s
		}
		return ""
	}
	o.outcome = str(v.get("outcome"))
	o.key = str(v.get("preview_key"))
	d := v.get("diagnostic")
	o.code, o.path, o.message = str(d.get("code")), str(d.get("path")), str(d.get("message"))
	return o
}

// The projections read from an accepted output, all with the oracle.
// Each "accept-" or "reject-" projection reads the output after the
// oracle's revision-settle --target all accepts or rejects every
// revision (the output itself when there is nothing to settle). The
// id-bearing ones are relabelled (relabel.go).
var projections = []struct {
	name string
	doc  string // out, accept or reject
	args []string
}{
	{"doc", "out", []string{"read"}},
	{"doc-final", "out", []string{"read", "--view", "final"}},
	{"doc-original", "out", []string{"read", "--view", "original"}},
	{"comments", "out", []string{"read", "--kind", "comments"}},
	{"revisions", "out", []string{"read", "--kind", "revisions"}},
	{"bookmarks", "out", []string{"read", "--kind", "bookmarks"}},
	{"styles", "out", []string{"read", "--kind", "styles"}},
	{"accept-doc", "accept", []string{"read"}},
	{"accept-comments", "accept", []string{"read", "--kind", "comments"}},
	{"accept-bookmarks", "accept", []string{"read", "--kind", "bookmarks"}},
	{"reject-doc", "reject", []string{"read"}},
	{"reject-comments", "reject", []string{"read", "--kind", "comments"}},
	{"reject-bookmarks", "reject", []string{"read", "--kind", "bookmarks"}},
}

// A writeResult is one plan in one change mode. Outcome, Code, Path,
// Proj and Integrity are the compared values; Parts is checked one way
// (the Go output may not change a part the oracle left alone); Sites and
// Triage are not compared.
type writeResult struct {
	ID   string `json:"id"`
	Mode string `json:"mode"`
	// Class is the op class the comparison asserts (opclass.go).
	Class string `json:"class"`
	// Outcome is accepted or refused.
	Outcome string `json:"outcome"`
	// Code and Path are the refusal's error code and field path.
	Code string `json:"code,omitempty"`
	Path string `json:"path,omitempty"`
	// Proj maps each projection to "exit digest" of its normalised,
	// relabelled stdout.
	Proj map[string]string `json:"proj,omitempty"`
	// Parts are the package parts the output changed: "+name" added,
	// "-name" removed, "~name" changed (decompressed bytes differ).
	Parts []string `json:"parts,omitempty"`
	// Integrity are the WML integrity findings the output introduced
	// (internal/docx/integrity, docxdriver's checks), "part: message".
	Integrity []string `json:"integrity,omitempty"`
	// Dropped are the input's findings the output no longer has: a
	// pre-existing defect the edit repaired or removed instead of leaving
	// it alone.
	Dropped []string `json:"dropped,omitempty"`
	// Sites are the docxdriver refusal sites whose message template
	// matches the refusal (sites.go): a tag, not compared.
	Sites []string `json:"sites,omitempty"`
	// Triage maps each CLI step to the digest of its raw output (exit
	// code, stdout without bytes, stderr) in the blobs: triage material,
	// not compared.
	Triage map[string]string `json:"-"`

	message     string // the refusal message, for site tagging
	unsupported bool
}

// blobs collects normalised outputs by digest.
type blobs struct {
	m map[string]string
}

// put stores text under its digest in b and returns the digest.
func (b *blobs) put(text string) string {
	d := digest([]byte(text))
	if text != "" && b != nil {
		b.m[d] = text
	}
	return d
}

// runWrite applies one plan in one mode with the applier in a fresh work
// directory, and reads the output with the oracle. Every output text it
// digests goes into bl.
func runWrite(ap applier, oracle string, src []byte, p *WritePlan, mode, work string, bl *blobs) (*writeResult, error) {
	if err := os.RemoveAll(work); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	if err := os.WriteFile(filepath.Join(work, "in.docx"), src, 0o644); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(src)
	name, text, err := ap.planFile(p, src, hex.EncodeToString(sum[:]), mode)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.ID, err)
	}
	if err := os.WriteFile(filepath.Join(work, name), []byte(text), 0o644); err != nil {
		return nil, err
	}
	res := &writeResult{ID: p.ID, Mode: mode, Class: opClass(p, mode), Triage: map[string]string{}}
	a, err := ap.apply(work, name, p)
	if err != nil {
		return nil, err
	}
	for _, t := range a.triage {
		res.Triage[t.name] = bl.put(t.text)
	}
	res.unsupported, res.message = a.unsupported, a.message
	if !a.accepted {
		res.Outcome, res.Code, res.Path = "refused", a.code, a.path
		return res, nil
	}
	res.Outcome = "accepted"
	outBytes, err := os.ReadFile(filepath.Join(work, "out.docx"))
	if err != nil {
		return nil, err
	}
	res.Parts, err = changedParts(src, outBytes)
	if err != nil {
		res.Parts = []string{"!" + err.Error()}
	}
	before, after := integrity.Check(src), integrity.Check(outBytes)
	for _, f := range integrity.Introduced(before, after) {
		res.Integrity = append(res.Integrity, f.Part+": "+f.Message)
	}
	for _, f := range before.Kinds() {
		if !after.Has(f) {
			res.Dropped = append(res.Dropped, f.Part+": "+f.Message)
		}
	}
	docs := map[string]string{"out": "out.docx"}
	for _, action := range []string{"accept", "reject"} {
		file := action + ".docx"
		r, err := runIn(work, oracle, "--json", "revision-settle", "out.docx", "--target", "all", "--action", action, "-o", file)
		if err != nil {
			return nil, err
		}
		res.Triage["settle-"+action] = bl.put(rawOutput(r))
		if r.exit == 0 {
			docs[action] = file
		} else {
			docs[action] = "out.docx"
		}
	}
	known, err := sourceIDs(oracle, work)
	if err != nil {
		return nil, err
	}
	res.Proj = map[string]string{}
	for _, pr := range projections {
		file := docs[pr.doc]
		b, err := os.ReadFile(filepath.Join(work, file))
		if err != nil {
			return nil, err
		}
		h := sha256.Sum256(b)
		args := append([]string{"--json"}, pr.args[0], file)
		args = append(args, pr.args[1:]...)
		r, err := runIn(work, oracle, args...)
		if err != nil {
			return nil, err
		}
		text := strings.ReplaceAll(string(r.stdout), hex.EncodeToString(h[:]), "<output>")
		text = relabelNewIDs(relabel(pr.name, text), known)
		res.Proj[pr.name] = fmt.Sprintf("%d %s", r.exit, bl.put(text))
	}
	return res, nil
}

// runIn runs bin with args in dir, with the oracle's clock set.
func runIn(dir, bin string, args ...string) (result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOCXDRIVER_NOW="+writeClock)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return result{exit: -1, stderr: []byte("timeout")}, nil
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return result{stdout: stdout.Bytes(), stderr: stderr.Bytes()}, nil
	case errors.As(err, &exitErr):
		return result{exit: exitErr.ExitCode(), stdout: stdout.Bytes(), stderr: stderr.Bytes()}, nil
	default:
		return result{}, fmt.Errorf("run %s: %w", bin, err)
	}
}

// dropKeys removes top-level members from a JSON object, keeping the
// rest byte for byte; anything that is not a JSON object is returned
// unchanged.
func dropKeys(data []byte, keys ...string) string {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if d, ok := tok.(json.Delim); err != nil || !ok || d != '{' {
		return string(data)
	}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return string(data)
		}
		key, _ := kt.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return string(data)
		}
		if slices.Contains(keys, key) {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(jsonString(key))
		b.WriteByte(':')
		b.Write(raw)
	}
	b.WriteString("}\n")
	return b.String()
}

// changedParts compares two packages member by member (decompressed).
func changedParts(before, after []byte) ([]string, error) {
	read := func(b []byte) (map[string][32]byte, error) {
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return nil, err
		}
		out := map[string][32]byte{}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			h := sha256.New()
			_, err = io.Copy(h, io.LimitReader(rc, 1<<28))
			rc.Close()
			if err != nil {
				return nil, err
			}
			var s [32]byte
			copy(s[:], h.Sum(nil))
			out[f.Name] = s
		}
		return out, nil
	}
	a, err := read(before)
	if err != nil {
		return nil, err
	}
	b, err := read(after)
	if err != nil {
		return nil, err
	}
	var out []string
	for name, hb := range b {
		ha, ok := a[name]
		switch {
		case !ok:
			out = append(out, "+"+name)
		case ha != hb:
			out = append(out, "~"+name)
		}
	}
	for name := range a {
		if _, ok := b[name]; !ok {
			out = append(out, "-"+name)
		}
	}
	slices.SortFunc(out, func(x, y string) int { return strings.Compare(x[1:], y[1:]) })
	return out, nil
}

// sourceIDs are the paragraph ids the oracle reads in the input (its
// repaired ids included), and the comment paragraph ids.
func sourceIDs(oracle, work string) (map[string]bool, error) {
	known := map[string]bool{}
	for _, args := range [][]string{{"--json", "find", "in.docx", ""}, {"--json", "read", "in.docx", "--kind", "comments"}, {"--json", "read", "in.docx", "--kind", "bookmarks"}} {
		r, err := runIn(work, oracle, args...)
		if err != nil {
			return nil, err
		}
		for _, m := range quotedID.FindAllStringSubmatch(string(r.stdout), -1) {
			known[m[2]] = true
		}
	}
	return known, nil
}
