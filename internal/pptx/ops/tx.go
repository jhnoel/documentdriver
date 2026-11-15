package ops

import (
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// ErrTxDone is returned when a finished transaction is used again
// (plan.ErrTxDone).
var ErrTxDone = plan.ErrTxDone

// ErrTxFailed is returned by Commit after an operation failed: the plan
// is all or nothing, so a failed operation poisons the transaction
// (plan.ErrTxFailed).
var ErrTxFailed = plan.ErrTxFailed

// Op is one operation of a plan. Operations are defined in this package
// only; each runs against the transaction's working copy.
type Op interface {
	// Name is the operation's plan name, such as "object.set".
	Name() string
	apply(*Tx) error
}

// Tx is a plan's working copy of one deck: a plan.Tx whose workspace is
// the pptx edit layer. Operations run in memory; Commit checks the write
// set and saves the result, and anything else discards it. The working
// copy is never handed out: operations are the only way to change it.
type Tx struct {
	tx    *plan.Tx
	pkg   *opc.Package
	ws    *edit.Workspace
	media map[string]string // media part by SHA-256 of its bytes, once listed
	// addedDefaults are the content-type Default extensions this plan
	// added for new media, lower case.
	addedDefaults map[string]bool
}

// Option configures a transaction.
type Option = plan.Option

// WithIDs sets the source of fresh ids and the clock (default
// ids.Default). A fixed source makes the output reproducible.
func WithIDs(s ids.Source) Option { return plan.WithIDs(s) }

// Begin opens the deck at path as a working copy.
func Begin(path string, opts ...Option) (*Tx, error) {
	t := &Tx{}
	tx, err := plan.Begin(path, func(pkg *opc.Package) plan.Workspace {
		t.pkg, t.ws = pkg, edit.New(pkg)
		return t.ws
	}, opts...)
	if err != nil {
		return nil, err
	}
	t.tx = tx
	return t, nil
}

// ids returns the transaction's source of fresh GUIDs and the clock;
// operations use nothing else.
func (t *Tx) ids() ids.Source { return t.tx.IDs() }

// Run applies op to the working copy. If it fails, the transaction can
// only be rolled back: Commit then fails with ErrTxFailed.
func (t *Tx) Run(op Op) error {
	return t.tx.Run(op.Name(), func() error { return op.apply(t) })
}

// Snapshot writes the working copy as it stands to dst (plan.Tx.Snapshot).
func (t *Tx) Snapshot(dst string) error { return t.tx.Snapshot(dst) }

// ChangedParts lists the parts whose bytes the plan changed so far.
func (t *Tx) ChangedParts() ([]string, error) { return t.tx.ChangedParts() }

// Aliases returns the plan's aliases (without $) and the full addresses
// (slide3#1001) they are bound to.
func (t *Tx) Aliases() map[string]string {
	out := map[string]string{}
	for a, r := range t.ws.Aliases() {
		out[string(a)] = r.String()
	}
	return out
}

// Commit verifies the working copy against the plan's write set (see
// edit.Workspace.Verify) and saves the deck atomically (plan.Tx.Commit).
// A nil error means the changes are on disk; any error means the file is
// untouched.
func (t *Tx) Commit() error { return t.tx.Commit() }

// CommitTo is Commit writing to dst instead of the file the working copy
// was opened from (plan.Tx.CommitTo).
func (t *Tx) CommitTo(dst string) error { return t.tx.CommitTo(dst) }

// Finish snapshots the working copy, builds the edit result from the
// snapshot with build, and commits unless opt.DryRun (plan.Tx.Finish).
func (t *Tx) Finish(opt plan.Options, build func(snapshot string) (*plan.Result, error)) (*plan.Result, error) {
	return t.tx.Finish(opt, build)
}

// Rollback discards the working copy. It is safe to call after Commit.
func (t *Tx) Rollback() { t.tx.Rollback() }

// Apply runs ops in order against a working copy of the deck at path and
// commits the result. If any operation fails, or the commit's checks fail,
// nothing is written.
func Apply(path string, ops ...Op) error {
	tx, err := Begin(path)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, op := range ops {
		if err := tx.Run(op); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ApplyTo is Apply writing the result to dst; src is left untouched.
func ApplyTo(src, dst string, ops ...Op) error {
	tx, err := Begin(src)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, op := range ops {
		if err := tx.Run(op); err != nil {
			return err
		}
	}
	return tx.CommitTo(dst)
}
