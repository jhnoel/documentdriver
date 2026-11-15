// Package numfmt holds the SpreadsheetML number-format vocabulary: the
// built-in formats of ECMA-376 Part 1 §18.8.30, a classification of a
// format code into the kind of data it renders, and the conversion of
// the serial values Excel stores for date and time cells into their
// "YYYY-MM-DD", "YYYY-MM-DDTHH:MM:SS" and "HH:MM:SS" text forms.
//
// It is pure: it imports only the standard library and knows nothing of
// packages, parts or the engine (docs/design.md, xlsx).
package numfmt
