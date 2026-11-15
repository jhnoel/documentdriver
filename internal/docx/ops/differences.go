package ops

// DifferenceClass is one kind of intended difference from docxdriver
// 0.5.2's plan parsing, with the reason for it.
type DifferenceClass struct {
	// Name identifies the class; Why gives the reason.
	Name, Why string
}

// DifferenceClasses are the kinds of intended difference between
// docxdriver's verdict on a plan and this package's on the same plan in
// the contract's grammar (accepted or refused, and the code). The cases
// in Differences show each.
var DifferenceClasses = []DifferenceClass{
	{"positional-form", "serde accepts a struct (the plan, a format, an edit) or an internally or adjacently tagged enum (an operation, a table edit, a line spacing) written as a JSON array of its fields in declaration order. The plan schema allows only objects, and a positional plan is unreadable and breaks silently when a field is added, so the Go parser refuses arrays where objects are expected."},
	{"enum-map-form", "serde accepts a unit enum value (change_mode, position, highlight, …) written as a one-key map {\"track\": null}, and reports an empty or larger map as \"expected map with a single key\". The schema allows only the string; the Go parser refuses maps with \"invalid type: map, expected a string\"."},
	{"unit-variant-members", "serde ignores members next to the tag of a unit variant ({\"unit\": \"auto\", \"value\": 3}, {\"kind\": \"delete\", \"x\": 1}) despite deny_unknown_fields, so a mistyped edit is silently dropped. The Go parser refuses them as unknown fields."},
	{"latex-equation", "The docx CLI takes equations as LaTeX only (planner decision): equation.replace has a required `latex` field where docxdriver's replace_equation has `mathml`, and canonical JSON holds the canonical LaTeX (math.CanonicalLaTeX), and malformed LaTeX is an invalid_operation at `latex`. A docxdriver plan with `mathml` gets the unknown-field error; docxdriver refuses `latex` the same way. The schema's replace_equation variant differs accordingly."},
	{"variant-index", "serde accepts an integer tag as a variant index ({\"kind\": 3} is delete_columns). The schema allows only names; the Go parser refuses integers as it refuses other non-string tags."},
	{"contract-addresses", "The contract names a section by one address (`at`: section:N, section:final or a section alias) where docxdriver has two fields, a number (`section`) and a handle (`section_handle`), and refuses a plan that gives both. FromOracle cannot say \"both\" in the contract's grammar, so it keeps the two docxdriver fields and the plan is refused as having unknown fields (invalid_plan) where docxdriver refuses it by rule (invalid_operation)."},
	{"null-refused", "serde reads null as an absent optional field. The contract's decoder (internal/cli, shared with pptx) refuses null everywhere: a plan leaves a field out instead, so a null that was meant as a value is never silently dropped."},
	{"duplicate-keys", "serde_json keeps the last of duplicate object keys, so {\"select\":\"x\",\"select\":\"z\"} means z. The contract's parser refuses duplicate keys (invalid_json): which one was meant cannot be known."},
	{"nesting-limit", "serde_json stops at 127 levels of nesting (its recursion limit is 128 including the outermost value's frame); the shared parser takes 128 levels and then refuses the value as a plan (invalid_plan). Either way the text is refused."},
	{"revision-selection", "docxdriver's convert_revisions_to_formatting has a required `target` (all, one id or one author); the contract says the same with three optional fields (`all`, `at`, `author`), so a plan that says neither `at` nor `all` breaks a rule (invalid_operation) where docxdriver's is missing a field (invalid_plan)."},
}

// Difference is a differential case that shows an intended difference:
// the oracle's outcome and this package's for the same plan translated
// into the contract's grammar, each "ok" or the diagnostic's code. The
// test fails if either side changes.
type Difference struct {
	// Class is the DifferenceClasses entry; Case the differential case.
	Class, Case string
	// Rust and Go are the two outcomes.
	Rust, Go string
}

// Differences are the cases that show the case-checked classes.
var Differences = []Difference{
	{Class: "latex-equation", Case: "rule/replace_equation/0", Rust: "invalid_plan", Go: "invalid_operation"},
	{Class: "latex-equation", Case: "rule/replace_equation/1", Rust: "invalid_plan", Go: "invalid_operation"},
	{Class: "latex-equation", Case: "valid/0", Rust: "invalid_plan", Go: "ok"},
	{Class: "latex-equation", Case: "valid/0/direct", Rust: "invalid_plan", Go: "ok"},
	{Class: "latex-equation", Case: "valid/1", Rust: "ok", Go: "invalid_plan"},
	{Class: "latex-equation", Case: "src/crates/docxdriver-core/tests/change_mode.rs:358", Rust: "ok", Go: "invalid_plan"},
	{Class: "enum-map-form", Case: "serde/15", Rust: "ok", Go: "invalid_plan"},
	{Class: "unit-variant-members", Case: "serde/25", Rust: "ok", Go: "invalid_plan"},
	{Class: "positional-form", Case: "serde/30", Rust: "invalid_operation", Go: "invalid_plan"},
	{Class: "unit-variant-members", Case: "serde/33", Rust: "ok", Go: "invalid_plan"},
	{Class: "positional-form", Case: "serde/95", Rust: "ok", Go: "invalid_plan"},
	{Class: "positional-form", Case: "serde/100", Rust: "ok", Go: "invalid_plan"},
	{Class: "positional-form", Case: "serde/126", Rust: "ok", Go: "invalid_plan"},
	{Class: "enum-map-form", Case: "serde/140", Rust: "ok", Go: "invalid_plan"},
	{Class: "enum-map-form", Case: "serde/178", Rust: "ok", Go: "invalid_plan"},
	{Class: "positional-form", Case: "serde/195", Rust: "ok", Go: "invalid_plan"},
	{Class: "contract-addresses", Case: "rule/set_section_layout/0", Rust: "invalid_operation", Go: "invalid_plan"},
	{Class: "revision-selection", Case: "serde/158", Rust: "invalid_plan", Go: "invalid_operation"},
	{Class: "duplicate-keys", Case: "serde/71", Rust: "ok", Go: "invalid_json"},
	{Class: "duplicate-keys", Case: "serde/72", Rust: "invalid_plan", Go: "invalid_json"},
	{Class: "duplicate-keys", Case: "serde/73", Rust: "invalid_plan", Go: "invalid_json"},
	{Class: "null-refused", Case: "serde/80", Rust: "ok", Go: "invalid_plan"},
	{Class: "null-refused", Case: "serde/102", Rust: "ok", Go: "invalid_plan"},
	{Class: "null-refused", Case: "serde/125", Rust: "ok", Go: "invalid_plan"},
	{Class: "null-refused", Case: "serde/144", Rust: "ok", Go: "invalid_plan"},
	{Class: "null-refused", Case: "valid/39", Rust: "ok", Go: "invalid_plan"},
	{Class: "null-refused", Case: "valid/39/direct", Rust: "ok", Go: "invalid_plan"},
	{Class: "nesting-limit", Case: "serde/depth128", Rust: "invalid_json", Go: "invalid_plan"},
}
