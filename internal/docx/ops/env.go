package ops

import (
	"errors"
	"fmt"
	"strings"
	"time"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Env is what an operation runs against: the plan's working copy, its
// author, change mode and clock, and the Reviser that authors the
// operation's revisions. Run reports what it deleted and created
// (Deleted, Created).
type Env struct {
	// Author is the plan's author.
	Author string
	// Mode is the plan's change mode.
	Mode ChangeMode
	// Index is the operation's position in the plan.
	Index int
	// Now is the time revisions are stamped with.
	Now time.Time
	// Source is the SHA-256 of the input package, PlanSeed the SHA-256 of
	// the plan's canonical JSON: with Index they seed new paragraph ids.
	Source, PlanSeed [32]byte
	// WS is the plan's working copy.
	WS *docxedit.Workspace
	// Rev authors the operation's revisions; the runner finishes it.
	Rev *revise.Reviser

	// Deleted are the addresses of the equations and placeholders the
	// operation deleted because its delete field listed them
	// (docs/contract.md, section 7), for the result's deleted.
	Deleted []string
	// Created are the addresses of what the operation created, in order:
	// the plan's `as` names them (one, or a group of several).
	Created []string
}

// Rejection is an operation refusing its input: a contract error code
// (not_found, ambiguous, refused_in_mode, unreadable_target,
// operation_failed), the field at fault ("" for the operation), and the
// message, the report's summary.
type Rejection struct {
	Code, Field, Msg string
	// Matches lists the candidates of an ambiguous address.
	Matches []string
}

// Error returns the message.
func (r *Rejection) Error() string { return r.Msg }

// reject returns a Rejection with a code, field and formatted message.
func reject(code, field, format string, args ...any) error {
	return &Rejection{Code: code, Field: field, Msg: fmt.Sprintf(format, args...)}
}

// IsRejection reports whether err is an operation's refusal rather than
// a failure of the engine.
func IsRejection(err error) bool {
	var r *Rejection
	return errors.As(err, &r)
}

// Story opens a region over the content of root, a story's container in
// part, declaring it and every unit inside it before anything changes.
func (env *Env) Story(part string, root *tree.Node) *core.Handle {
	return env.WS.Core().Story(part, part, root)
}

// Main returns the main document part's name and working tree.
func (env *Env) Main() (string, *model.XML, error) {
	part := env.WS.DocumentPart()
	doc, err := env.WS.Tree(part)
	if err != nil {
		return "", nil, err
	}
	return part, &model.XML{Doc: doc}, nil
}

// ReadModel returns the read model over the working copy as it stands,
// for the reads that need a package (styles, header and footer
// resolution, numbering): its views read the working trees and the parts'
// current bytes, without serialising the package into a zip and opening
// it again. Untouched parts are read only when a view asks for them, so a
// damaged member nothing reads fails nothing.
func (env *Env) ReadModel() *model.Package {
	return model.OpenPackage(env.WS.Package())
}

// wordParts returns the working trees of the package's WordprocessingML
// parts but the main document and the numbering part: word/*.xml parts
// that parse (lists.rs referenced_num_ids).
func (env *Env) wordParts(main, numbering string) []*tree.Node {
	var roots []*tree.Node
	for _, name := range env.WS.Package().Names() {
		n := strings.TrimPrefix(name, "/")
		if n == main || n == numbering || !strings.HasPrefix(n, "word/") || !strings.HasSuffix(n, ".xml") {
			continue
		}
		doc, err := env.WS.Tree(name)
		if err != nil || doc.Root() == nil {
			continue
		}
		roots = append(roots, doc.Root())
	}
	return roots
}
