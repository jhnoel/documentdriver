package ops

// NameDelete is name.delete.
type NameDelete struct {
	At NameAddress `json:"at" doc:"The name: name:Tax_Rate or name:Budget!Local."`
}

// opNameDelete is name.delete's entry in the operation table.
var opNameDelete = OpDef{
	Name: "name.delete", New: func() Spec { return &NameDelete{} },
	Summary: "Deletes a defined name. Formulas that use it then show #NAME?.",
	Rules:   nil,
}

// Run (chunk G15): not_found for the name; it removes the definedName
// (and an emptied definedNames, R7) and marks values. Deleted: the
// name.
func (o *NameDelete) Run(env *Env) error {
	if err := env.WS.DeleteDefinedName(o.At.Name, o.At.Sheet); err != nil {
		return FromEdit(err, "at")
	}
	env.WS.MarkValues()
	env.Deleted = append(env.Deleted, o.At.Raw)
	return nil
}
