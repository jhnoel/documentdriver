// Package model is the agent-visible xlsx vocabulary that read and find
// print and that apply (P2) will take: sheet-qualified addresses
// (docs/xlsx-contract-draft.md, §6), cell value forms (§7a), notes
// (contract §8) and the text forms find matches against.
//
// It is pure: it imports only the standard library and
// internal/xlsx/formula (for A1 parsing and sheet quoting, which it never
// re-implements) and knows nothing of packages or parts.
//
// Every type here is a value the agent sees or types. A change to a JSON
// form is a contract change: it starts in docs/xlsx-contract-draft.md.
package model
