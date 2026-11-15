package edit

import (
	"errors"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// ErrNoDocument is returned when the package has no main document part.
var ErrNoDocument = errors.New("edit: package has no main document part")

// Workspace is a plan's editing view of a docx package: the edit core's
// workspace over the WordprocessingML Format, with the main document
// part found. It never saves: plan transactions do.
type Workspace struct {
	pkg  *opc.Package
	core *core.Workspace
	doc  string // main document part name
	opt  Options
	// ids is the identity of every paragraph of the main document part,
	// resolved when the workspace opened and bound to its node; repaired
	// marks those whose id is not (yet) in the document.
	ids      map[*tree.Node]model.ParaID
	repaired map[*tree.Node]bool
	alloc    allocState
	reg      *Registry
	joins    []Join
}

// Options are the inputs of a workspace's deterministic ids.
type Options struct {
	// Source is the SHA-256 of the input package's bytes, docxdriver's
	// source hash: the seed of repaired and inserted paragraph ids.
	// Content-derived ids come from the source document and the
	// operation's position in the plan only, never from the plan's bytes,
	// so the parity comparator relabels them by document order rather
	// than matching docxdriver's (which also hash the serde-canonical
	// plan).
	Source [32]byte
}

// New returns a workspace over pkg. The main document part is the target
// of the package's officeDocument relationship. Every paragraph of that
// part gets its identity now (see ParaID): valid w14:paraIds are kept,
// and missing, malformed or duplicate ones are repaired from opt.Source,
// bound to their paragraphs, and written at Finish.
func New(pkg *opc.Package, opt Options) (*Workspace, error) {
	doc, err := mainPart(pkg)
	if err != nil {
		return nil, err
	}
	w := &Workspace{pkg: pkg, core: core.New(pkg, Format), doc: doc, opt: opt}
	if err := w.resolveParaIDs(); err != nil {
		return nil, err
	}
	if w.reg, err = newRegistry(w); err != nil {
		return nil, err
	}
	return w, nil
}

// mainPart returns the main document part name reached from the package relationships.
func mainPart(pkg *opc.Package) (string, error) {
	rels, err := pkg.Rels("/")
	if err != nil {
		return "", err
	}
	for _, r := range rels.All() {
		if ctypes.Is(r.Type, ctypes.RelOfficeDocument) && !r.External() {
			name, err := rels.TargetPart(r)
			if err != nil {
				return "", err
			}
			if !pkg.Has(name) {
				return "", fmt.Errorf("%w: %s", ErrNoDocument, name)
			}
			return name, nil
		}
	}
	return "", ErrNoDocument
}

// Core returns the edit core's workspace: unit placement (SlotEnd,
// Insert, Move, Delete), part and container declarations, and the
// created-node record.
func (w *Workspace) Core() *core.Workspace { return w.core }

// Package returns the package being edited.
func (w *Workspace) Package() *opc.Package { return w.pkg }

// DocumentPart returns the main document part's name.
func (w *Workspace) DocumentPart() string { return w.doc }

// Tree returns the working tree of a part.
func (w *Workspace) Tree(part string) (*tree.Document, error) { return w.core.Tree(part) }

// Target returns a handle on unit e of part, declaring it a target of the
// plan: its own content may change. label names it in errors.
func (w *Workspace) Target(part, label string, e *tree.Node) *core.Handle {
	return w.core.Target(part, label, []*tree.Node{e})
}

// Verify checks the working copy against the plan's write set (see
// plan.Verify) over the docx units.
func (w *Workspace) Verify() error { return w.core.Verify() }
