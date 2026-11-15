// Package apply runs a docx plan: open the package as a plan transaction,
// repair paragraph identity, run the operations in their change mode,
// check the result against the integrity gate and save it, as docxdriver
// 0.5.2's plan command does (api.rs execute_plan and fold_typed). Run
// returns a Result value (outcome, diagnostic, report of the operations
// that applied, changed parts and the markup of the changed paragraphs
// from the HTML projection of the saved snapshot); the CLI renders it.
//
// Intended difference from docxdriver: a plan that changes nothing writes
// nothing (docxdriver re-serialises and writes the package, persisting the
// paragraph-id repair, which reads the same).
package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/integrity"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// PreviewTime is the stamp a review without a given time writes into its
// discarded dry run (api.rs PREVIEW_TIME).
const PreviewTime = "2000-01-01T00:00:00Z"

// Options control a run.
type Options struct {
	// Commit is the preview key: set, the plan commits (when it matches);
	// empty, the plan is reviewed and nothing is written.
	Commit string
	// Out is the file a commit writes; "" writes the input in place.
	Out string
	// Now is the revision time a caller gave (DOCXDRIVER_NOW), "" for none:
	// a review then stamps PreviewTime and a commit the clock's time.
	Now string
	// PreviewClock: a dry run (no Commit) stamps the clock's
	// time instead of PreviewTime; a given Now still wins.
	PreviewClock bool
	// IDs supplies the clock; nil is ids.Default().
	IDs ids.Source
	// View renders the report's markup (the equation converter).
	View view.Options
	// Input, when set, is called with the read model of the input before
	// the operations run, for a caller that reads it too (the CLI's
	// changed units), so the run need not keep it.
	Input func(*model.Document)
	// Checkpoint, when set, is called where the run holds the most (the
	// input's read model; the working copy after the operations; each
	// side of the gate's check; the output's read model), for the memory
	// budget (laws.WithinMemory).
	Checkpoint func(string)
	// gate replaces the commit gate (nil: integrity.GateEditWith),
	// for the tests' law that the lazy gate is the eager one.
	gate gateFunc
}

// gateFunc is the commit gate's signature (integrity.GateEditWith).
type gateFunc func(before, after []byte, ws integrity.Workspace, log *integrity.OpLog, o integrity.GateOptions) (*integrity.Findings, error)

// mark calls the checkpoint, if any.
func (o Options) mark(name string) {
	if o.Checkpoint != nil {
		o.Checkpoint(name)
	}
}

// Outcome is how a plan run ended.
type Outcome string

// The outcomes.
const (
	Previewed Outcome = "previewed" // reviewed; nothing written
	Committed Outcome = "committed" // the preview key matched; saved
	Rejected  Outcome = "rejected"  // refused; nothing written
)

// Diagnostic is why a plan was rejected: a code, a message, and the
// offending field's path ("ops[2].format.width") and plan-text span when
// known.
type Diagnostic struct {
	Code, Message, Path string
	Span                *ops.Span
}

// OpReport is one operation's outcome.
type OpReport struct {
	Index   int
	Op      string
	Applied bool
}

// Report is what the operations did.
type Report struct {
	// Completed counts the operations that applied; StoppedAt is the one
	// that was refused, or -1.
	Completed, StoppedAt int
	Ops                  []OpReport
}

// Result is a plan run's outcome, independent of how a CLI renders it.
type Result struct {
	Outcome Outcome
	// Source is "sha256:<hex>" of the input.
	Source string
	// Plan is the parsed plan (nil when it did not parse).
	Plan *ops.Plan
	// PreviewKey is the key that commits this plan against this input.
	PreviewKey string
	// Diagnostic is set when the outcome is Rejected.
	Diagnostic *Diagnostic
	// Report is set once the operations ran (also for some rejections).
	Report *Report
	// Parts lists the parts the plan changed (none: a no-op plan writes
	// nothing).
	Parts []string
	// Aliases maps each `as` name to the address it got.
	Aliases map[string]string
	// Changed are the addresses (w14:paraId) of the main document's new
	// and changed paragraphs, then of its headers', footers' and notes'
	// (view.StoryParagraphs); Deleted those of its paragraphs that no
	// longer exist, then the equations and placeholders the operations
	// deleted through their delete fields (ops.Env.Deleted), in operation
	// order. The contract's --changed full renders Changed through the
	// read side.
	Changed, Deleted []string
	// Hazards are the integrity findings on the parts the plan changed:
	// pre-existing, since a plan that adds one is refused.
	Hazards []Hazard
	// Snapshot is the package as the plan leaves it (nil when the plan
	// changed nothing): what a commit writes.
	Snapshot []byte
	// Output is the read model of Snapshot (nil when the plan changed
	// nothing). The CLI reads the changed units from it (and from the
	// input's, given to Options.Input) without opening either again.
	Output *model.Document
	// Markup is the markup of each paragraph in Changed, from the HTML
	// projection of Output.
	Markup map[string]string
}

// Hazard is an integrity finding on a part the plan changed.
type Hazard struct {
	// Kind is the finding's class ("schema", "integrity", "xml",
	// "package"), ID the part, Detail the finding.
	Kind, ID, Detail string
	// New is always false: a plan that adds a finding is refused.
	New bool
}

// Run runs plan p (decoded and validated by the plan contract) against
// the package at path.
func Run(path string, p *ops.Plan, opt Options) (*Result, error) {
	input, err := os.ReadFile(path) //nolint:gosec // G304: the caller's own argument
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(input)
	res := &Result{Source: "sha256:" + hex.EncodeToString(sum[:])}
	reject := func(d *Diagnostic, r *Report) (*Result, error) {
		res.Outcome, res.Diagnostic, res.Report = Rejected, d, r
		return res, nil
	}
	res.Plan = p
	if string(p.Base) != res.Source {
		return reject(&Diagnostic{Code: "source_mismatch", Message: "plan base does not match input bytes", Path: "base"}, nil)
	}
	res.PreviewKey = p.PreviewKey()
	if opt.Commit != "" && opt.Commit != res.PreviewKey {
		return reject(&Diagnostic{Code: "preview_key_mismatch", Message: "preview key does not match source and canonical plan", Path: "preview_key"}, nil)
	}
	now, bad := revisionTime(opt)
	if bad != nil {
		return reject(bad, nil)
	}
	f, err := fold(path, input, sum, p, now, opt)
	if err != nil {
		return nil, err
	}
	if f.diag != nil {
		return reject(f.diag, f.report)
	}
	res.Report, res.Parts, res.Changed, res.Deleted, res.Hazards = f.report, f.changed, f.paras, f.deleted, f.hazards
	res.Snapshot, res.Output, res.Markup = f.snapshot, f.output, f.markup
	res.Aliases = f.aliases
	if res.Aliases == nil {
		res.Aliases = map[string]string{}
	}
	res.Outcome = Previewed
	if opt.Commit != "" {
		res.Outcome = Committed
	}
	return res, nil
}

// revisionTime is api.rs revision_time: a review without a given time
// stamps PreviewTime; a given time must be a real UTC second; otherwise
// the clock's.
func revisionTime(opt Options) (time.Time, *Diagnostic) {
	if opt.Now == "" {
		if opt.Commit == "" && !opt.PreviewClock {
			t, _ := time.Parse(revise.DateLayout, PreviewTime) //nolint:errcheck // a constant that parses
			return t, nil
		}
		src := opt.IDs
		if src == nil {
			src = ids.Default()
		}
		return src.Now().UTC().Truncate(time.Second), nil
	}
	t, err := time.Parse(revise.DateLayout, opt.Now)
	if err != nil || len(opt.Now) != 20 || t.Format(revise.DateLayout) != opt.Now {
		return time.Time{}, &Diagnostic{Code: "invalid_now", Message: "now must be a UTC time YYYY-MM-DDTHH:MM:SSZ, got " + strconv.Quote(opt.Now), Path: "now"}
	}
	return t, nil
}

// folded is what running the operations gave.
type folded struct {
	report         *Report
	diag           *Diagnostic
	changed        []string
	paras, deleted []string
	hazards        []Hazard
	snapshot       []byte
	aliases        map[string]string
	output         *model.Document
	markup         map[string]string
}

// diag returns a Diagnostic with the given code and message.
func diag(code, msg string) *Diagnostic { return &Diagnostic{Code: code, Message: msg} }

// emptyReport returns a Report whose StoppedAt is -1.
func emptyReport() *Report { return &Report{StoppedAt: -1} }

// fold runs the plan's operations on a transaction over the package.
func fold(path string, input []byte, source [32]byte, p *ops.Plan, now time.Time, opt Options) (*folded, error) {
	// The read model's open gives docxdriver's wording for a package it
	// refuses (not a zip, no document part, Strict, malformed XML). It
	// is read once, for everything the run needs of the input (the
	// paragraphs before the plan, the caller's own reading), and not kept
	// while the operations run.
	inDoc, err := model.OpenDocument(input)
	if err != nil {
		return &folded{report: emptyReport(), diag: diag("source_invalid", err.Error())}, nil
	}
	before := paragraphsByID(inDoc)
	if opt.Input != nil {
		opt.Input(inDoc)
	}
	opt.mark("input read")
	runtime.KeepAlive(inDoc)
	var ws *docxedit.Workspace
	var openErr error
	tx, err := plan.Begin(path, func(pkg *opc.Package) plan.Workspace {
		ws, openErr = docxedit.New(pkg, docxedit.Options{Source: source})
		if openErr != nil {
			return failing{openErr}
		}
		return ws
	})
	if err != nil {
		return &folded{report: emptyReport(), diag: diag("source_invalid", err.Error())}, nil
	}
	defer tx.Rollback()
	if openErr != nil {
		if errors.Is(openErr, model.ErrParaIDSpace) {
			return &folded{report: emptyReport(), diag: diag("paragraph_id_allocation", openErr.Error())}, nil
		}
		return &folded{report: emptyReport(), diag: diag("source_invalid", openErr.Error())}, nil
	}
	rep := &Report{StoppedAt: -1}
	seed := sha256.Sum256([]byte(p.CanonicalJSON()))
	mode := revise.Track
	if p.ChangeMode == "direct" {
		mode = revise.Direct
	}
	// removed are the addresses the operations deleted through their
	// delete fields (equations, placeholders), in operation order.
	var removed []string
	changedBy := map[string]int{}
	oplog := integrity.NewOpLog()
	for i, op := range p.Ops {
		env := &ops.Env{
			Author: p.Author, Mode: p.ChangeMode, Index: i, Now: now, Source: source, PlanSeed: seed,
			WS: ws, Rev: revise.New(p.Author, now, mode),
		}
		spec, err := resolveAliases(ws.Registry(), op.Spec)
		if err == nil {
			err = claimAlias(ws.Registry(), spec)
		}
		if err == nil {
			err = tx.Run(op.Name, func() error {
				if err := spec.Run(env); err != nil {
					return err
				}
				if _, err := env.Rev.Finish(); err != nil {
					return err
				}
				return defineAliases(ws.Registry(), spec, env.Created)
			})
		}
		entry := OpReport{Index: i, Op: op.Name, Applied: true}
		if err != nil {
			if errors.Is(err, ops.ErrNotImplemented) {
				// An operation not ported yet refuses at the operation,
				// before it changes anything: the plan writes nothing.
				err = &ops.Rejection{Code: "operation_failed", Msg: op.Name + " is not implemented yet; nothing was written"}
			}
			if errors.Is(err, opc.ErrContentTypes) {
				// The package cannot register the part the operation
				// needs: refused up front, at the operation.
				err = &ops.Rejection{Code: "unreadable_target", Msg: err.Error()}
			}
			if !ops.IsRejection(err) {
				return nil, fmt.Errorf("ops[%d] %s: %w", i, op.Name, err)
			}
			entry.Applied = false
			msg := rejectionMessage(err)
			rep.Ops = append(rep.Ops, entry)
			rep.Completed, rep.StoppedAt = i, i
			code, field := "operation_failed", ""
			var r *ops.Rejection
			if errors.As(err, &r) {
				code, field = r.Code, r.Field
			}
			path := "ops[" + strconv.Itoa(i) + "]"
			if field != "" {
				path += "." + field
			}
			return &folded{report: rep, diag: &Diagnostic{Code: code, Message: msg, Path: path}}, nil
		}
		if err := oplog.Record(i, ws.Core()); err != nil {
			return nil, err
		}
		rep.Ops = append(rep.Ops, entry)
		rep.Completed = i + 1
		removed = append(removed, env.Deleted...)
		// The first operation to change each part, to name the operation
		// that introduced a nonconformity in it. The parts an earlier
		// operation changed are not compared again.
		now, err := ws.Package().ChangedPartsExcept(func(name string) bool {
			_, seen := changedBy[plan.PartKey(name)]
			return seen
		})
		if err != nil {
			return nil, err
		}
		for _, name := range now {
			changedBy[plan.PartKey(name)] = i
		}
	}
	opt.mark("operations done")
	changed, err := ws.Package().ChangedParts()
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		// The plan changed nothing: the file stays as it is, without the
		// paragraph-id repair (reading it gives the same ids), and no
		// output file is written.
		tx.Rollback()
		return &folded{report: rep, aliases: aliasAddresses(ws.Registry())}, nil
	}
	// The repaired ids and the created paragraphs' ids are written now.
	if err := ws.Finish(); err != nil {
		return &folded{report: rep, diag: diag("package_write", err.Error())}, nil
	}
	if changed, err = ws.Package().ChangedParts(); err != nil {
		return nil, err
	}
	var gateErr error
	f := &folded{report: rep, changed: changed, aliases: aliasAddresses(ws.Registry())}
	// The working copy is sealed into memory once; the gate checks those
	// bytes and a commit writes them. The gate parses every changed part
	// and refuses one that is not well formed (an "xml:" finding the input
	// lacks), so the seal does not tokenise them first.
	_, err = tx.FinishSealed(plan.Options{DryRun: opt.Commit == "", Out: opt.Out, Unchecked: true}, func(out []byte) (*plan.Result, error) {
		// One check of the output: the gate's, whose findings on the
		// changed parts are the hazards.
		gate := opt.gate
		if gate == nil {
			gate = integrity.GateEditWith
		}
		found, err := gate(input, out, ws.Core(), oplog, integrity.GateOptions{Mark: opt.Checkpoint, Sealed: true})
		if err != nil {
			gateErr = err
			return nil, err
		}
		f.hazards = hazardsIn(found, changed)
		f.snapshot = out
		r := plan.NewResult()
		return &r, nil
	})
	if gateErr != nil {
		var g *integrity.GateFailure
		if errors.As(gateErr, &g) {
			return &folded{report: rep, diag: invalidOutput(g, changedBy)}, nil
		}
		return nil, gateErr
	}
	if err != nil {
		return nil, err
	}
	// The working copy is done with (a dry run's too): its trees, which
	// the op log also reaches, go before the output's read model is
	// built, so the two are never held at once.
	tx.Rollback()
	ws, oplog = nil, nil
	// One read model of the output: the report's markup, the changed
	// paragraphs and the CLI's changed units.
	if f.output, err = model.OpenDocument(f.snapshot); err != nil {
		return nil, err
	}
	f.paras, f.deleted = paragraphChanges(before, f.output)
	f.deleted = append(f.deleted, removed...)
	if f.markup, err = view.AffectedMarkup(f.output, f.paras, opt.View); err != nil {
		return nil, err
	}
	opt.mark("output read")
	return f, nil
}

// rejectionMessage is the summary of a refused operation.
func rejectionMessage(err error) string {
	var r *ops.Rejection
	if errors.As(err, &r) {
		return r.Msg
	}
	return err.Error()
}

// failing is the workspace of a package the docx edit layer refused.
type failing struct{ err error }

// Verify returns the stored error.
func (f failing) Verify() error { return f.err }

// invalidOutput is the refusal of a plan whose output has a nonconformity
// its input did not: the path names the operation that first changed the
// part it is in ("ops" when no single operation can be named), the
// message what was introduced.
func invalidOutput(g *integrity.GateFailure, changedBy map[string]int) *Diagnostic {
	if op := g.Op(); op >= 0 {
		return &Diagnostic{Code: "invalid_output", Message: g.Summary(), Path: "ops[" + strconv.Itoa(op) + "]"}
	}
	path := "ops"
	op := -1
	for _, f := range g.Introduced {
		i, ok := changedBy[plan.PartKey(f.Part)]
		if !ok || (op >= 0 && i != op) {
			op = -1
			break
		}
		op = i
	}
	if op >= 0 {
		path = "ops[" + strconv.Itoa(op) + "]"
	}
	return &Diagnostic{Code: "invalid_output", Message: g.Summary(), Path: path}
}

// paragraphs are a document's paragraphs by address, in order, each as
// the hash of its bytes (what a comparison needs, not a copy of the
// document's text).
type paragraphs struct {
	sums  map[string][sha256.Size]byte
	order []string
}

// paragraphsByID lists the main document's paragraphs by address, then
// those of its headers, footers and notes that have one
// (view.StoryParagraphs; a comment's paragraphs are its thread's, which
// the result reports as a comment unit).
func paragraphsByID(d *model.Document) paragraphs {
	ids := d.ParaIDs().IDs
	out := paragraphs{sums: map[string][sha256.Size]byte{}}
	for _, p := range d.XML.AllParagraphs() {
		if id, ok := ids[p]; ok {
			out.sums[id.ID] = sha256.Sum256(p.Bytes())
			out.order = append(out.order, id.ID)
		}
	}
	for _, p := range view.StoryParagraphs(d) {
		if !p.Comment {
			out.sums[p.ID] = sha256.Sum256(p.Node.Bytes())
			out.order = append(out.order, p.ID)
		}
	}
	return out
}

// paragraphChanges compares the main document's paragraphs by address:
// new or changed ones, and those that went.
func paragraphChanges(before paragraphs, output *model.Document) (changed, deleted []string) {
	after := paragraphsByID(output)
	for _, id := range after.order {
		if b, ok := before.sums[id]; !ok || b != after.sums[id] {
			changed = append(changed, id)
		}
	}
	for _, id := range before.order {
		if _, ok := after.sums[id]; !ok {
			deleted = append(deleted, id)
		}
	}
	return changed, deleted
}

// hazardsIn returns the integrity findings of the output on the changed
// parts, from the gate's findings (complete on every changed part).
func hazardsIn(found *integrity.Findings, changed []string) []Hazard {
	in := map[string]bool{}
	for _, c := range changed {
		in[plan.PartKey(c)] = true
	}
	var out []Hazard
	for _, k := range found.Kinds() {
		if !in[plan.PartKey(k.Part)] {
			continue
		}
		kind, _, _ := strings.Cut(k.Message, ": ")
		out = append(out, Hazard{Kind: kind, ID: k.Part, Detail: k.Message})
	}
	return out
}
