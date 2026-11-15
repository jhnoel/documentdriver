package plan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// ErrTxDone is returned when a finished transaction is used again.
var ErrTxDone = errors.New("plan: transaction already committed or rolled back")

// ErrTxSealed is returned by Run once the working copy is sealed
// (FinishSealed): the bytes being checked and saved are fixed.
var ErrTxSealed = errors.New("plan: transaction already sealed; no operation may run")

// ErrTxFailed is returned by Commit after an operation failed: the plan
// is all or nothing, so a failed operation poisons the transaction.
var ErrTxFailed = errors.New("plan: an operation failed; nothing written")

// Workspace is a format's editing view of a transaction's working copy:
// its edit layer, which resolves addresses, hands out what operations
// change, and records the write set they declare.
type Workspace interface {
	// Verify checks the working copy against the declared write set
	// (usually Verify with the format's Units).
	Verify() error
}

// Tx is a plan's working copy of one package. Operations run in memory;
// Commit checks and saves the result, and anything else discards it. The
// file on disk changes only in Commit, and only if every check passes.
// The Tx never hands out the package: the format's workspace, built by
// Begin, is the only other holder.
type Tx struct {
	pkg    *opc.Package
	ws     Workspace
	done   bool
	failed error
	// ids supplies fresh GUIDs and the clock; operations use nothing else.
	ids ids.Source
	// verified is set by Finish between its snapshot, which verified the
	// working copy, and its commit: nothing runs in between, so the
	// commit does not verify again.
	verified bool
	// sealed is set by FinishSealed once the working copy's bytes are
	// fixed: no operation may run after it.
	sealed bool
}

// Option configures a transaction.
type Option func(*Tx)

// WithIDs sets the source of fresh ids and the clock (default
// ids.Default). A fixed source makes the output reproducible.
func WithIDs(s ids.Source) Option { return func(t *Tx) { t.ids = s } }

// Begin opens the package at path as a working copy and builds the
// format's workspace over it with open.
func Begin(path string, open func(*opc.Package) Workspace, opts ...Option) (*Tx, error) {
	pkg, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	t := &Tx{pkg: pkg, ws: open(pkg), ids: ids.Default()}
	for _, o := range opts {
		o(t)
	}
	return t, nil
}

// IDs returns the transaction's source of fresh ids and the clock.
func (t *Tx) IDs() ids.Source { return t.ids }

// Run runs one operation, named name, as apply. If it fails, the
// transaction can only be rolled back: Commit then fails with
// ErrTxFailed.
func (t *Tx) Run(name string, apply func() error) error {
	if t.done {
		return ErrTxDone
	}
	if t.sealed {
		return ErrTxSealed
	}
	if t.failed != nil {
		return fmt.Errorf("%w: %w", ErrTxFailed, t.failed)
	}
	if err := apply(); err != nil {
		t.failed = fmt.Errorf("%s: %w", name, err)
		return t.failed
	}
	return nil
}

// Snapshot writes the working copy as it stands to dst, a new file, after
// the same checks Commit makes (the write set, well-formedness), without
// finishing the transaction: it can still be committed or rolled back.
// It lets a caller read the result (the edit result of a plan, or a dry
// run) through a read model before anything is saved.
func (t *Tx) Snapshot(dst string) error {
	if t.done {
		return ErrTxDone
	}
	if t.failed != nil {
		return fmt.Errorf("%w: %w", ErrTxFailed, t.failed)
	}
	if err := t.ws.Verify(); err != nil {
		return fmt.Errorf("plan: snapshot: %w", err)
	}
	if err := t.pkg.SaveAs(dst); err != nil {
		return fmt.Errorf("plan: snapshot: %w", err)
	}
	return nil
}

// ChangedParts lists the parts whose bytes the plan changed so far.
func (t *Tx) ChangedParts() ([]string, error) {
	if t.done {
		return nil, ErrTxDone
	}
	return t.pkg.ChangedParts()
}

// Commit verifies the working copy against the plan's write set,
// re-tokenises every modified XML part as a well-formedness check and, if
// all pass, saves the package atomically. A working copy with no
// effective changes writes nothing. A nil error means the changes are on
// disk; any error means the file is untouched. The transaction is
// finished either way.
func (t *Tx) Commit() error {
	if err := t.finish(); err != nil {
		return err
	}
	// SaveAndClose re-tokenises every modified XML part before it creates
	// the temporary file, renames only after a complete, synced write, and
	// runs nothing that can fail after the rename.
	if err := t.pkg.SaveAndClose(); err != nil {
		return fmt.Errorf("plan: commit: %w", err)
	}
	return nil
}

// CommitTo is Commit writing to dst instead of the file the working copy
// was opened from, which is left untouched. With no effective changes dst
// receives a byte copy of the original.
func (t *Tx) CommitTo(dst string) error {
	if same, err := sameFile(t.pkg.Path(), dst); err != nil {
		t.Rollback()
		return err
	} else if same {
		return t.Commit()
	}
	if err := t.finish(); err != nil {
		return err
	}
	defer t.pkg.Close()
	if err := t.pkg.SaveAs(dst); err != nil {
		return fmt.Errorf("plan: commit: %w", err)
	}
	return nil
}

// finish marks the transaction done and reports whether it may save:
// no operation failed and the write set holds.
func (t *Tx) finish() error {
	if t.done {
		return ErrTxDone
	}
	if t.failed != nil {
		t.Rollback()
		return fmt.Errorf("%w: %w", ErrTxFailed, t.failed)
	}
	if !t.verified {
		if err := t.ws.Verify(); err != nil {
			t.Rollback()
			return fmt.Errorf("plan: commit: %w", err)
		}
	}
	t.done = true
	return nil
}

// sameFile reports whether paths a and b name the same file, false if b is absent.
func sameFile(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

// Rollback discards the working copy. It is safe to call after Commit.
func (t *Tx) Rollback() {
	if t.done {
		return
	}
	t.done = true
	t.pkg.Close()
	t.release()
}

// Options control how a plan's transaction finishes.
type Options struct {
	// DryRun builds the result without saving anything.
	DryRun bool
	// IDs supplies fresh GUIDs and the clock; nil means ids.Default().
	IDs ids.Source
	// Out is the file to write; "" writes the package in place.
	Out string
	// Unchecked (FinishSealed only) leaves out the well-formedness check
	// of the modified XML parts: the caller's build parses every changed
	// part of the sealed bytes and fails on one that is not well formed
	// (opc.Package.SealUnchecked).
	Unchecked bool
}

// Finish ends a transaction whose operations have all run. It writes the
// working copy, after Commit's checks, to a snapshot in a temporary
// directory; build makes the edit result from the snapshot (reading it
// through the format's read model). Then, unless opt.DryRun, it commits
// to opt.Out, or in place when that is "", and records the file in the
// result's Saved. A plan that changes nothing (no part's saved bytes
// differ) writes nothing and records no Saved. Any failure leaves every
// file untouched.
func (t *Tx) Finish(opt Options, build func(snapshot string) (*Result, error)) (*Result, error) {
	changed, err := t.ChangedParts()
	if err != nil {
		return nil, fmt.Errorf("plan: snapshot: %w", err)
	}
	sort.Strings(changed)
	dir, err := os.MkdirTemp("", "plan-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir) //nolint:errcheck // best-effort removal of a scratch directory after the result is built
	snap := filepath.Join(dir, "snapshot"+filepath.Ext(t.pkg.Path()))
	if err := t.Snapshot(snap); err != nil {
		return nil, err
	}
	res, err := build(snap)
	if err != nil {
		return nil, err
	}
	res.Parts = changed
	if res.Parts == nil {
		res.Parts = []string{}
	}
	res.DryRun = opt.DryRun
	if opt.DryRun {
		return res, nil
	}
	// The snapshot verified the working copy, and build only read it.
	t.verified = true
	if len(changed) == 0 {
		// A plan that changes nothing writes nothing: no output file, and
		// no saved file in the result (the format sets new_base to base).
		// CommitTo would copy the input to opt.Out, which the contract
		// forbids, so the working copy is discarded without a save.
		t.Rollback()
		return res, nil
	}
	if opt.Out != "" {
		err = t.CommitTo(opt.Out)
		res.Saved = opt.Out
	} else {
		path := t.pkg.Path()
		err = t.Commit()
		res.Saved = path
	}
	if err != nil {
		return nil, err
	}
	t.release()
	return res, nil
}

// FinishSealed ends a transaction whose operations have all run, as
// Finish does, without a snapshot file: it verifies the working copy and
// seals it into memory once (opc.Package.Seal: each modified part
// serialised, checked and compressed once); build makes the edit result
// from those bytes; then, unless opt.DryRun, the same bytes are written
// to opt.Out, or in place when that is "" (refused with
// opc.ErrChangedOnDisk when the file changed since it was opened), and
// the file is recorded in the result's Saved. The bytes build reads are
// the bytes written. Once sealed, Run refuses. Any failure leaves every
// file untouched. The sealed bytes are held in memory, so a format whose
// packages carry large media (pptx) keeps Finish.
func (t *Tx) FinishSealed(opt Options, build func(sealed []byte) (*Result, error)) (*Result, error) {
	if t.done {
		return nil, ErrTxDone
	}
	if t.failed != nil {
		return nil, fmt.Errorf("%w: %w", ErrTxFailed, t.failed)
	}
	if err := t.ws.Verify(); err != nil {
		return nil, fmt.Errorf("plan: commit: %w", err)
	}
	seal := t.pkg.Seal
	if opt.Unchecked {
		seal = t.pkg.SealUnchecked
	}
	sealed, err := seal()
	if err != nil {
		return nil, fmt.Errorf("plan: commit: %w", err)
	}
	t.sealed = true
	res, err := build(sealed.Bytes())
	if err != nil {
		return nil, err
	}
	res.DryRun = opt.DryRun
	if opt.DryRun {
		return res, nil
	}
	t.done = true
	path := t.pkg.Path()
	err = t.pkg.CommitSealed(sealed, opt.Out)
	t.release()
	if err != nil {
		return nil, fmt.Errorf("plan: commit: %w", err)
	}
	res.Saved = path
	if opt.Out != "" {
		res.Saved = opt.Out
	}
	return res, nil
}

// release drops the finished transaction's working copy, so that its
// trees can be collected while the caller still holds the Tx.
func (t *Tx) release() {
	if t.done {
		t.pkg, t.ws = nil, nil
	}
}
