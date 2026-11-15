// Package edit is the docx edit layer over the shared edit core
// (internal/ooxml/edit): the WordprocessingML format the core needs
// (namespace families, the units the commit-time verifier compares, the
// schema type of an element, the units a path may not step into), and
// the docx workspace operations and the revision core build on.
//
// Units divide a part with stories for plan.Verify: the body, every
// paragraph, table, row and cell, every section (w:sectPr, except the
// previous properties inside w:sectPrChange), block-, row- and cell-level
// content controls with their content, and the part roots of the main
// document, headers and footers. A target unit may change its own content
// (its runs, its properties), a container unit its list of child units;
// everything else in a changed part stays byte for byte.
//
// Like the pptx layer, this package never mutates a tree itself: every
// change goes through the core's Handle, its structural verbs and unit
// placement (enforced by internal/archtest).
package edit
