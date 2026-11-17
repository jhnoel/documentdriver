// Package vsd is the Visio vocabulary of the vsdx CLI: the namespaces a
// part may use, the element and cell names the reader looks for, unit
// conversion from the stored inches and radians, the style category of
// a cell, and the agent addresses (docs/vsdx-contract-draft.md, section
// 6). No other vsdx package spells a Visio namespace or an address
// prefix (archtest's vsdx vocabulary rule, chunk W1).
package vsd

import "errors"

// ErrNotImplemented is returned by a function of the skeleton whose
// chunk has not landed yet (.worktrees/vsdx-work/chunks.md).
var ErrNotImplemented = errors.New("vsdx: not implemented yet")
