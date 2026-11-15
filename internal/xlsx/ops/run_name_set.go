package ops

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// NameSet is name.set.
type NameSet struct {
	At       NameAddress `json:"at" doc:"The name: name:Tax_Rate or name:Budget!Local."`
	RefersTo string      `json:"refers_to" doc:"What it refers to, as a formula."`
}

// opNameSet is name.set's entry in the operation table.
var opNameSet = OpDef{
	Name: "name.set", New: func() Spec { return &NameSet{} },
	Summary: "Changes what a defined name refers to.",
	Rules:   nameSetRules,
}

// Run (chunk G14): not_found for the name; it replaces the
// definedName's text with the stored formula, keeping its attributes,
// and marks values. Changed: the name.
func (o *NameSet) Run(env *Env) error {
	body, ok := strings.CutPrefix(o.RefersTo, "=")
	if !ok || body == "" {
		return fmt.Errorf("invalid refers_to formula")
	}
	stored, err := formula.Store(body)
	if err != nil {
		return fmt.Errorf("store refers_to formula: %w", err)
	}
	if err := env.WS.SetDefinedName(o.At.Name, o.At.Sheet, stored); err != nil {
		return FromEdit(err, "at")
	}
	env.WS.MarkValues()
	env.ChangedName(o.At.Raw)
	return nil
}
