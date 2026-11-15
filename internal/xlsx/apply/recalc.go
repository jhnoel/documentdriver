package apply

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/integrity"
	"github.com/jhnoel/documentdriver/internal/xlsx/recalc"
)

// recalcCommit is the --recalc step (chunk G18), run after the gate
// passed on sealed (the plan's output, not written: the first
// transaction ran as a dry run). It:
//
//  1. computes the formula values with recalc.Compute(sealed);
//  2. opens sealed as a second plan transaction (written to a private
//     temporary file, opened with plan.Begin and edit.New) and writes each
//     computed value into its formula cell with edit.Sheet.SetCachedValue
//     (only formula cells; a value for a cell without a formula, or on a
//     sheet sealed does not have, is skipped), then Flush (not Finish: the
//     recalculation policy already ran) and seals;
//  3. holds the second output to integrity.GateScoped(input, second,
//     scope) with the scope widened by the parts the splice changed (a
//     computed value can land on a sheet no op touched, a formula that
//     reads a changed cell) (a finding the input lacks is
//     invalid_output, path ops, as the first gate);
//  4. unless opt.DryRun, checks that the file at path still hashes to
//     cli.Hash(input) when target is path (in place: otherwise io,
//     "the workbook changed on disk while the plan ran"), and writes the
//     second output to target atomically (CommitSealed to target).
//
// It returns the bytes as written (or as they would be) and the parts
// changed from input to them, sorted: scope's and the splice's.
//
// Errors: recalc.ErrUnavailable is operation_failed at path --recalc
// with the hint "install LibreOffice or set DD_SOFFICE"; LibreOffice
// failing or timing out is operation_failed at --recalc with its
// message; nothing is written in any failure.
func recalcCommit(input, sealed []byte, path, target string, scope integrity.Scope, opt Options) ([]byte, []string, error) {
	ropts := recalc.Options{}
	if opt.Recalc != nil {
		ropts = recalc.Options{Soffice: opt.Recalc.Soffice, Timeout: opt.Recalc.Timeout}
	}
	values, err := recalc.Compute(sealed, ropts)
	if err != nil {
		if errors.Is(err, recalc.ErrUnavailable) {
			return nil, nil, cli.Errorf(cli.CodeOperationFailed, "--recalc", "%v", err).WithHint("install LibreOffice or set DD_SOFFICE")
		}
		return nil, nil, cli.Errorf(cli.CodeOperationFailed, "--recalc", "%v", err)
	}
	if target == "" {
		target = path
	}
	f, err := os.CreateTemp("", "xlsx-recalc-*.xlsx")
	if err != nil {
		return nil, nil, cli.Errorf(cli.CodeIO, "", "create temporary workbook: %v", err)
	}
	defer os.Remove(f.Name()) //nolint:errcheck // best-effort cleanup of a temporary file
	if _, err := f.Write(sealed); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return nil, nil, cli.Errorf(cli.CodeIO, "", "write temporary workbook: %v; close: %v", err, closeErr)
		}
		return nil, nil, cli.Errorf(cli.CodeIO, "", "write temporary workbook: %v", err)
	}
	if err := f.Close(); err != nil {
		return nil, nil, cli.Errorf(cli.CodeIO, "", "close temporary workbook: %v", err)
	}
	tx, ws, err := begin(f.Name(), opt)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	for _, v := range values {
		s, err := ws.Sheet(v.Sheet)
		if err != nil {
			var u *edit.Unreadable
			if errors.Is(err, edit.ErrNotFound) || errors.As(err, &u) {
				continue // a sheet sealed does not have (as a worksheet) is skipped
			}
			return nil, nil, fmt.Errorf("apply: recalc: %w", err)
		}
		c, err := s.Cell(v.Row, v.Col)
		if err != nil || !c.Exists || !c.Raw.F.Present {
			continue // only formula cells are spliced
		}
		t := v.T
		if t == "n" {
			t = "" // LibreOffice writes t="n" for numbers; the spliced form omits it
		}
		if err := s.SetCachedValue(v.Row, v.Col, t, v.V); err != nil {
			return nil, nil, fmt.Errorf("apply: recalc: %w", err)
		}
	}
	if err := ws.Flush(); err != nil {
		return nil, nil, finishRefusal(err)
	}
	more, err := ws.Package().ChangedParts()
	if err != nil {
		return nil, nil, fmt.Errorf("apply: recalc: %w", err)
	}
	parts := slices.Clone(scope.Parts)
	for _, p := range more {
		if !slices.ContainsFunc(parts, func(q string) bool { return plan.PartKey(q) == plan.PartKey(p) }) {
			parts = append(parts, p)
		}
	}
	slices.Sort(parts)
	scope = integrity.Scope{Parts: parts}
	var second []byte
	_, err = tx.FinishSealed(plan.Options{DryRun: opt.DryRun, Out: target}, func(out []byte) (*plan.Result, error) {
		second = out
		if _, gerr := integrity.GateScoped(input, out, scope); gerr != nil {
			var g *integrity.GateFailure
			if errors.As(gerr, &g) {
				return nil, invalidOutput(g, nil)
			}
			return nil, gerr
		}
		if !opt.DryRun && target == path {
			cur, rerr := os.ReadFile(path) //nolint:gosec // G304: the caller's own argument
			if rerr != nil {
				return nil, cli.Errorf(cli.CodeIO, "", "read the workbook: %v", rerr)
			}
			if cli.Hash(cur) != cli.Hash(input) {
				return nil, cli.Errorf(cli.CodeIO, "", "the workbook changed on disk while the plan ran")
			}
		}
		r := plan.NewResult()
		return &r, nil
	})
	if err != nil {
		return nil, nil, commitError(err)
	}
	return second, parts, nil
}
