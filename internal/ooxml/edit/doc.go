// Package edit is the format-neutral edit core of the shared engine:
// the only code besides internal/ooxml/tree and internal/opc that
// changes a document's XML. A format's edit layer (internal/pptx/ops/edit
// today) builds on it: it resolves the format's addresses to elements and
// hands out the core's Handles, and the core does every mutation.
//
// What the core holds, each parameterised by the caller's vocabulary
// rather than by one format's namespaces:
//
//   - qualified names and paths (NS, Name, Path, Vocab): a namespace
//     family (transitional and strict URIs), the prefix new elements take,
//     and the schema table it is checked against;
//   - the Workspace, which keys a plan's write set (plan.WriteSet) by part,
//     hands out Handles that declare their elements as targets, declares
//     containers and parts, and verifies the working copy at commit with
//     the format's plan.Units;
//   - Handle, a write handle on one addressed thing, possibly several
//     branch elements (every mc:AlternateContent branch of one object):
//     attribute edits, Ensure and the placed inserts, each applied to
//     every branch or to none;
//   - the schema-placed inserts, driven by the generated child order of
//     internal/ooxml/schema: Ensure (single children, choices replaced),
//     AppendEach (the end of a repeated slot), ReplaceChildren (a list's
//     content), ReplaceRun (the run of one repeated child, such as the
//     paragraphs of a text body), and unit placement (Insert, Move and
//     Delete at a Position from SlotEnd, Before, After or JustAfter);
//   - value checks before writing (SetChecked, through schema.CheckAttr)
//     and the property codec machinery (Codec, Set) that keeps the
//     round-trip rule.
//
// Inserting elements happens only in place.go and unit.go: every insert
// there reads the schema's child order or is placed next to an existing
// sibling (enforced by internal/archtest).
package edit
