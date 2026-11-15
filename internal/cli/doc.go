// Package cli is the part of the agent contract (docs/contract.md) that
// both binaries share: the command-line grammar, the errors object and
// exit codes, JSON output, the plan envelope and its decoder, and the
// operation reference generated from a format's op table.
//
// It knows nothing about either format and imports neither.
package cli

// Contract is the contract version this code implements.
const Contract = 1
