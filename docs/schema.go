// Package docs holds the documents that ship inside the binaries: the
// docx plan JSON Schema, which tools/schemagen generates from the
// operation table (its staleness test keeps the committed file current)
// and `docx help schema` prints as committed.
package docs

import _ "embed" // for go:embed

// DocxPlanSchema is docx-plan.schema.json, the docx plan JSON Schema
// (draft-07).
//
//go:embed docx-plan.schema.json
var DocxPlanSchema string
