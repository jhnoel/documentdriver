// Package tree is a lossless XML tree for OOXML parts. A custom tokenizer
// keeps each node's original byte span, attribute order, namespace prefixes,
// quote style and whitespace. Untouched nodes are written back by copying
// their original bytes; a modified node re-emits only what changed.
//
// It does not use encoding/xml, which loses prefixes and spans.
package tree
