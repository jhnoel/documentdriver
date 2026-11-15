// Package apply runs an xlsx plan (docs/xlsx-contract-draft.md, §3 and
// §4): it opens the workbook as a plan transaction (plan.Begin) with the
// xlsx edit layer as its workspace, runs the operations in order, ends
// the workspace (each sheet's overlay flushed into a splice patch; the
// recalculation policy), seals the working copy once (FinishSealed),
// holds the sealed bytes to the scoped integrity gate, and, unless a dry
// run, writes those same bytes. It is internal/docx/apply/apply.go's
// shape without the docxdriver oracle fields (no preview key, no
// outcome, no review run): a refusal is a *cli.Error, and a plan that
// changes nothing writes nothing.
//
// The result's changed units are read back from the sealed bytes
// through internal/xlsx/read (ranges as read --range shows them, names
// as read --kind names lists them), never from the edit layer.
//
// Budget: apply of one cell.set on a sheet of N bytes with shared
// strings of T bytes holds at most 1.3 N + T of live heap
// (TestMemoryBudget): the sheet's original string, its index and the
// patch's edits (no copy of the part), the shared strings never opened
// (an edit never touches them), the sealed package (compressed) and the
// gate's streaming scan of the changed sheet.
package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/integrity"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

// Options control a run.
type Options struct {
	// DryRun does everything except write.
	DryRun bool
	// Out is the file to write; "" writes the input in place.
	Out string
	// IDs is the transaction's id source (nil: ids.Default()). xlsx
	// writes no fresh ids or times today; it is kept for the contract's
	// --seed and --clock.
	IDs ids.Source
	// Recalc, when set, runs the --recalc step after the gate: the
	// formula values LibreOffice computes are spliced into the output
	// (recalcCommit). Without it no value is ever computed.
	Recalc *RecalcOptions
	// Checkpoint, when set, is called where the run holds the most (the
	// operations done; the sealed bytes beside the working copy; the
	// gate), for the memory budget (laws.WithinMemory).
	Checkpoint func(string)
	// gate replaces the commit gate (nil: integrity.GateScoped), for the
	// tests that run the wiring before the scoped gate exists and for
	// TestGateMatchesFullCheck.
	gate func(input, output []byte, s integrity.Scope) (*integrity.Findings, error)
}

// RecalcOptions are the --recalc step's (internal/xlsx/recalc.Options).
type RecalcOptions struct {
	// Soffice is the LibreOffice binary ("" : $DD_SOFFICE, then PATH).
	Soffice string
	// Timeout bounds LibreOffice's run (0: two minutes).
	Timeout time.Duration
}

// mark calls the checkpoint callback with name, if one is configured.
func (o Options) mark(name string) {
	if o.Checkpoint != nil {
		o.Checkpoint(name)
	}
}

// Result is a run's edit result, the contract's §4 fields.
type Result struct {
	// Base is cli.Hash of the input; NewBase of the file written ("" on
	// a dry run; Base for a plan that changed nothing).
	Base, NewBase string
	DryRun        bool
	// Saved is the file written ("" on a dry run or a no-op).
	Saved string
	// Aliases maps each `as` name to its address ("sheet:New").
	Aliases map[string]string
	// Units are the changed units in the order the ops reported them,
	// deduplicated by id (last report wins); Changed is each one's JSON
	// (--changed full: as read shows it), in the same order.
	Units   []ops.Unit
	Changed []json.RawMessage
	// ChangedIDs are the changed units' ids, aligned with Changed
	// (the ids finishUnits reported, without re-decoding Changed's
	// JSON; --changed ids prints these).
	ChangedIDs []string
	// Deleted are the addresses that no longer exist, in op order.
	Deleted []string
	// Hazards: stale-values (the save asks Excel to recalculate; new
	// when the input did not already ask), ref-error (new: a delete made
	// a #REF!), and the gate's findings on the changed parts
	// (pre-existing, new false).
	Hazards []plan.Hazard
	// Parts are the changed parts, sorted.
	Parts []string
	// Output is the sealed package as written (nil for a no-op).
	Output []byte
}

// Run runs plan p (decoded and checked by ops.ParsePlan) against the
// workbook at path.
//
// Errors: a *cli.Error for every contract refusal: io (the file),
// stale_base, invalid_document (the package or its workbook cannot be
// opened, or a safety limit trips), the op's own refusal at ops[n] or
// ops[n].<field> (not_found, ambiguous, unreadable_target,
// operation_failed with detail), invalid_output (the gate: path the op
// that first changed the part, detail the finding). Any other error is
// a bug (internal).
func Run(path string, p *ops.Plan, opt Options) (*Result, error) {
	input, err := os.ReadFile(path) //nolint:gosec // G304: the caller's own argument
	if err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "read the workbook: %v", err)
	}
	if e := cli.CheckBase(p.Base, input); e != nil {
		return nil, e
	}
	res := &Result{Base: cli.Hash(input), DryRun: opt.DryRun, Aliases: map[string]string{}, Deleted: []string{}, Hazards: []plan.Hazard{}, Parts: []string{}}

	tx, ws, err := begin(path, opt)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// firstOp names, by part key, the op that first changed the part, for
	// invalid_output's path.
	firstOp := map[string]int{}
	var refErrs []string
	for i, op := range p.Ops {
		env := &ops.Env{WS: ws, Index: i}
		if err := ops.ResolveAliases(op.Spec, res.Aliases); err != nil {
			return nil, opRefusal(i, err)
		}
		alias := ops.AliasOf(op.Spec)
		if alias != "" {
			if _, dup := res.Aliases[alias]; dup {
				return nil, opRefusal(i, ops.Refuse(cli.CodeOperationFailed, "as", "the alias $%s is already defined by an earlier operation", alias))
			}
		}
		if err := tx.Run(op.Name, func() error { return op.Spec.Run(env) }); err != nil {
			return nil, opRefusal(i, unwrapRun(err))
		}
		// Units and aliases earlier ops reported move with this op's
		// structural edits and renames: ids are final addresses.
		rebase(res, env.Shifted, env.Renamed)
		if alias != "" && env.Created != "" {
			res.Aliases[alias] = env.Created
		}
		for _, k := range ws.Touched() {
			if _, ok := firstOp[k]; !ok {
				firstOp[k] = i
			}
		}
		units := map[string]int{}
		for j, u := range res.Units {
			units[u.ID] = j
		}
		for _, u := range env.Changed {
			if j, ok := units[u.ID]; ok {
				res.Units[j] = u
				continue
			}
			units[u.ID] = len(res.Units)
			res.Units = append(res.Units, u)
		}
		res.Deleted = append(res.Deleted, env.Deleted...)
		refErrs = append(refErrs, env.RefErrors...)
	}
	opt.mark("operations done")
	if err := ws.Finish(); err != nil {
		return nil, finishRefusal(err)
	}
	changed, err := ws.Package().ChangedParts()
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		// A plan that changes nothing writes nothing.
		tx.Rollback()
		if !opt.DryRun {
			res.NewBase = res.Base
		}
		res.Units, res.Changed, res.ChangedIDs = nil, []json.RawMessage{}, []string{}
		return res, nil
	}
	slices.Sort(changed)
	res.Parts = changed
	stale := ws.RecalcRequested()
	inputStale := ws.InputFullCalc()

	var gateErr error
	recalc := opt.Recalc != nil
	_, err = tx.FinishSealed(plan.Options{DryRun: opt.DryRun || recalc, Out: opt.Out}, func(sealed []byte) (*plan.Result, error) {
		opt.mark("sealed")
		ws.Drop() // the input sheet's original is not held beside the gate's scan
		gate := opt.gate
		if gate == nil {
			gate = integrity.GateScoped
		}
		found, err := gate(input, sealed, integrity.Scope{Parts: changed})
		opt.mark("gate")
		if err != nil {
			gateErr = err
			return nil, err
		}
		res.Hazards = append(res.Hazards, findingHazards(found, changed)...)
		res.Output = sealed
		r := plan.NewResult()
		return &r, nil
	})
	if gateErr != nil {
		var g *integrity.GateFailure
		if errors.As(gateErr, &g) {
			return nil, invalidOutput(g, firstOp)
		}
		return nil, gateErr
	}
	if err != nil {
		return nil, commitError(err)
	}
	tx.Rollback()
	ws = nil
	if recalc {
		target := opt.Out
		if target == "" {
			target = path
		}
		out, parts, err := recalcCommit(input, res.Output, path, target, integrity.Scope{Parts: changed}, opt)
		if err != nil {
			return nil, err
		}
		res.Output, res.Parts = out, parts
	}
	if !opt.DryRun {
		res.Saved = opt.Out
		if res.Saved == "" {
			res.Saved = path
		}
		res.NewBase = cli.Hash(res.Output)
	}
	if stale {
		res.Hazards = append([]plan.Hazard{{Kind: "stale-values", ID: "workbook", New: !inputStale,
			Detail: "the workbook asks Excel to recalculate on open: stored values of formula cells may be out of date until then"}}, res.Hazards...)
	}
	for _, r := range refErrs {
		res.Hazards = append(res.Hazards, plan.Hazard{Kind: "ref-error", ID: r, New: true, Detail: "a delete turned a reference into #REF!"})
	}
	return res, finishUnits(res, res.Output)
}

// failing is the workspace of a package the edit layer refused.
type failing struct{ err error }

// Verify returns the error that made opening the package or workbook fail.
func (f failing) Verify() error { return f.err }

// begin opens path as a plan transaction with the xlsx edit layer as
// its workspace (plan.Begin with edit.New, the transaction id source
// from opt.IDs) and returns the transaction and its workspace. A
// failure to open the package or its workbook is openRefusal's error.
func begin(path string, opt Options) (*plan.Tx, *edit.Workspace, error) {
	var ws *edit.Workspace
	var openErr error
	popts := []plan.Option{}
	if opt.IDs != nil {
		popts = append(popts, plan.WithIDs(opt.IDs))
	}
	tx, err := plan.Begin(path, func(pkg *opc.Package) plan.Workspace {
		ws, openErr = edit.New(pkg)
		if openErr != nil {
			return failing{openErr}
		}
		return ws
	}, popts...)
	if err != nil {
		return nil, nil, openRefusal(err)
	}
	if openErr != nil {
		tx.Rollback()
		return nil, nil, openRefusal(openErr)
	}
	return tx, ws, nil
}

// openRefusal maps a failure to open the package or its workbook.
func openRefusal(err error) error {
	var ce *cli.Error
	if errors.As(err, &ce) {
		return ce
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return cli.Errorf(cli.CodeIO, "", "read the workbook: %v", err)
	}
	return cli.Errorf(cli.CodeInvalidDocument, "", "the file cannot be opened as a workbook: %v", err)
}

// unwrapRun takes the op's own error back out of plan.Tx.Run's wrapping.
func unwrapRun(err error) error {
	var r *ops.Rejection
	if errors.As(err, &r) {
		return r
	}
	return err
}

// opRefusal maps op i's error: a Rejection (or an edit error FromEdit
// maps) is the contract error at ops[i] or ops[i].<field>; a safety
// limit tripping on a part read lazily is invalid_document; anything else
// is a bug.
func opRefusal(i int, err error) error {
	if errors.Is(err, opc.ErrLimit) {
		return cli.Errorf(cli.CodeInvalidDocument, "", "the workbook exceeds a safety limit: %v", err)
	}
	err = ops.FromEdit(err, "")
	var r *ops.Rejection
	if !errors.As(err, &r) {
		return cli.Errorf(cli.CodeInternal, cli.Index("ops", i), "%v", err)
	}
	path := cli.Index("ops", i)
	if r.Field != "" {
		path = cli.Join(path, r.Field)
	}
	e := cli.Errorf(r.Code, path, "%s", r.Msg)
	e.Detail = r.Detail
	e.Matches = r.Matches
	return e
}

// finishRefusal maps a failure of Workspace.Finish (the flush, the
// recalculation policy).
func finishRefusal(err error) error {
	var u *edit.Unreadable
	if errors.As(err, &u) {
		e := cli.Errorf(cli.CodeUnreadableTarget, "ops", "%s cannot be edited safely", u.Target)
		e.Detail = u.Detail
		return e
	}
	return fmt.Errorf("apply: finish: %w", err)
}

// commitError maps a failure to write the sealed bytes.
func commitError(err error) error {
	if errors.Is(err, opc.ErrChangedOnDisk) {
		return cli.Errorf(cli.CodeIO, "", "the workbook changed on disk while the plan ran; nothing was written")
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return cli.Errorf(cli.CodeIO, "", "write the workbook: %v", err)
	}
	return err
}

// invalidOutput is the refusal of a plan whose output has a finding its
// input lacks: path the op that first changed the part (ops when the
// findings are in parts different ops changed), detail the finding.
func invalidOutput(g *integrity.GateFailure, firstOp map[string]int) error {
	op := -1
	for _, f := range g.Introduced {
		i, ok := firstOp[plan.PartKey(f.Part)]
		if !ok || (op >= 0 && i != op) {
			op = -1
			break
		}
		op = i
	}
	path := "ops"
	if op >= 0 {
		path = cli.Index("ops", op)
	}
	e := cli.Errorf(cli.CodeInvalidOutput, path, "the plan would damage the workbook")
	e.Detail = strings.TrimPrefix(g.Error(), "the edit would damage the workbook: ")
	return e
}

// findingHazards are the gate's findings of the output on the changed
// parts: pre-existing (an introduced one was refused).
func findingHazards(found *integrity.Findings, changed []string) []plan.Hazard {
	in := map[string]bool{}
	for _, c := range changed {
		in[plan.PartKey(c)] = true
	}
	var out []plan.Hazard
	for _, k := range found.Kinds() {
		if k.Part == "" || !in[plan.PartKey(k.Part)] {
			continue
		}
		out = append(out, plan.Hazard{Kind: string(k.Kind), ID: strings.TrimPrefix(k.Part, "/"), Detail: k.String()})
	}
	return out
}
