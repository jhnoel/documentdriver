// Package ops is the docx plan grammar of the agent contract
// (docs/contract.md, section 3): its 42 operations, how a plan is parsed,
// validated and normalised, and its canonical JSON.
//
// The operation table (Ops) has one entry per operation, defined in the
// operation's own file, run_<name>.go, beside its fields (a Spec struct
// whose tags give the contract's field name, default, plan enum, the
// WordprocessingML simple type a stored value must satisfy, and doc
// text) and its Run. An entry holds the validation rules, the
// change-mode class, how direct mode is made, and the docxdriver 0.5.2
// operation it runs as with what the contract fixes or moves.
// tools/schemagen generates the plan JSON Schema and the operation
// reference (docs/docx-plan.md) from the table.
//
// docxdriver's side of the mapping is a record (OracleOps): its 34
// operations with their fields and rule examples, which the parity
// baseline is keyed by. FromOracle translates a docxdriver plan into the
// contract's grammar; the differential test (oracle_test.go) checks that
// each recorded docxdriver plan, translated, is accepted or refused as
// docxdriver's recorded verdict says, with the same code, apart from the
// cases listed in Differences.
//
// NewPlan builds a plan from Go values. Plans are JSON only: the docx
// CLI does not read TOML.
//
// revision.accept, revision.reject, paragraph.format and paragraph.insert
// run; every other Spec's Run returns ErrNotImplemented until its
// operation is ported.
package ops
