// Package ops is the xlsx plan: the operation table, the plan decoder,
// the operations' rules and their Run (docs/xlsx-contract-draft.md, §3;
// docs/design.md, xlsx, P2 and P3).
//
// # Layout
//
//   - opdef.go: OpDef (one operation: name, fields, rules, help), Rule,
//     Lookup and OpOf.
//   - table.go: Ops, the table, in the contract's order. `xlsx help ops`
//     and `xlsx help <op>` are generated from it.
//   - plan.go: Plan, ParsePlan (through internal/cli: every
//     document-independent fault of the whole plan reported together)
//     and NewPlan (tests build plans from Go values).
//   - addresses.go: the address field types and their decoding.
//   - values.go: cell values (value, values) and their decoding.
//   - env.go: Env (what Run gets), Rejection and the mapping of edit
//     errors onto contract codes.
//   - alias.go: `as` and `$name`.
//   - run_<name>.go: one file per operation (text.replace is
//     run_text_replace.go): its Spec struct, its table entry and its Run.
//     An operation is filled in by editing its own file only; shared
//     helpers go in files of their own (write_value.go, ...).
//
// # Errors
//
// Decoding and rules: invalid_plan for the plan's shape (unknown or
// missing field, wrong JSON type, null), invalid_operation for a value
// that breaks a rule (a bad address, a malformed formula, a grid that
// does not fit its range), each with the field's path, all reported
// together. Run: a *Rejection (not_found, ambiguous, unreadable_target,
// operation_failed) with the field at fault, the first one stopping the
// plan. Any other error from Run is a bug (internal).
package ops
