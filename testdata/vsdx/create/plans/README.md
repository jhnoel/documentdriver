# create plans (designer-authored)

Inputs of `vsdx create <plan> -o out.vsdx`; the expected results are
the rows of chunks CR1 and CL1 (.worktrees/vsdx-work/chunks.md) and of
docs/vsdx-design.md (Create). Lengths are points, y down, page 0 is
8.5 x 11 in.

- c2-shapes: a rect and an ellipse with text, a straight connector glued
  point to point (right middle of a, left middle of b), a shape data row.
- c3-pages: orientation flip of page 0, a new 17 x 11 in page, a rename,
  a shape on the new page through its alias.
- c4..c6: refusals (no masters in a new drawing; whole-shape glue on a
  master-less connector; base on create).
