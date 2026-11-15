package ops

import (
	"errors"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// SetEvenAndOddHeaders is document.set_even_odd_headers (docxdriver's set_even_and_odd_headers).
type SetEvenAndOddHeaders struct {
	EvenAndOdd bool `json:"even_and_odd" doc:"Whether even and odd pages have their own headers and footers (w:evenAndOddHeaders in the document settings)."`
}

// opDocumentSetEvenOddHeaders is document.set_even_odd_headers's entry in the operation table.
var opDocumentSetEvenOddHeaders = OpDef{
	Name: "document.set_even_odd_headers", Oracle: "set_even_and_odd_headers", New: func() Spec { return &SetEvenAndOddHeaders{} },
	Track: DirectOnly, Direct: OnlyDirect, TrackGuards: refusedInTrack,
}

// Run turns the document's distinct even-page headers on or off
// (headers.rs set_even_and_odd_headers_typed): a w:evenAndOddHeaders in
// the settings part where the schema puts it (and the settings part
// itself, related and registered, when the document has none), or none.
// Word records no revision for a document setting, so track mode refuses
// it. A document already so writes nothing; a settings part that cannot
// be read is its unreadable target.
func (o *SetEvenAndOddHeaders) Run(env *Env) error {
	if env.Mode != ModeDirect {
		return reject("refused_in_mode", "", "document.set_even_odd_headers cannot be tracked: it changes a document setting (w:evenAndOddHeaders in the settings part), which Word records no revision for; run it in a change_mode=direct plan")
	}
	name, err := env.WS.SettingsPart()
	if err != nil {
		return err
	}
	var current bool
	if name != "" {
		doc, err := env.WS.Tree(name)
		if err != nil || doc.Root() == nil || !model.IsW(doc.Root(), "settings") {
			return &Rejection{Code: "unreadable_target", Msg: "the settings part " + strings.TrimPrefix(name, "/") + " cannot be read, so w:evenAndOddHeaders cannot be set: " + errText(err)}
		}
		if n := model.ChildW(doc.Root(), "evenAndOddHeaders"); n != nil {
			v, _ := model.Attr(n, "val")
			current = v != "0" && v != "false" && v != "off"
		}
	}
	if current == o.EvenAndOdd {
		return nil
	}
	if name == "" {
		if name, err = env.WS.EnsureSettingsPart(); err != nil {
			if errors.Is(err, opc.ErrContentTypes) {
				return &Rejection{Code: "unreadable_target", Msg: "a settings part must be added, but " + err.Error()}
			}
			return err
		}
	}
	h, err := env.WS.Core().Part(name)
	if err != nil {
		return err
	}
	root := h.Elem(0)
	existing := model.ChildW(root, "evenAndOddHeaders")
	if !o.EvenAndOdd {
		return h.RemoveNode(existing)
	}
	if existing != nil {
		return removeW(h, existing, "val")
	}
	_, err = h.EnsureIn(root, docxedit.W("evenAndOddHeaders"))
	return err
}
