// Package xmlscan reads XML that tree.Check or tree.Scan has already
// accepted, by byte position, without building nodes or allocating: it
// is the lexer the streaming kernels use to find the spans a splice
// replaces, and the one XML character codec of the module (decoding,
// escaping, and the name and character classes).
//
// Use tree.Scan to understand a document; use xmlscan to locate and
// rewrite bytes of one you have already scanned. Do not write another
// lexer, quote-tracking loop, entity decoder or escaper (archtest's
// TestOneXMLLexer).
//
// Every Token and Attr is a span of the source string: nothing is copied
// until a caller asks for a decoded value. Next checks what it walks (a
// tag's name, each attribute's name, '=', quotes and value, the close),
// which is why iterating a tag's attributes cannot fail. On bytes tree
// would reject, Next returns ErrSyntax where it cannot find a token
// boundary and the decoders keep a malformed reference as written;
// nothing here panics on any input.
package xmlscan
