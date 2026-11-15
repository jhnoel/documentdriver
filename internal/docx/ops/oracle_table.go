package ops

// The docxdriver 0.5.2 side of the oracle mapping. docxdriver is pinned,
// so this table is a record, not a second op table: its 34 operations by
// name, each with its fields (and whether serde requires them), its
// validation rules with the example plans the parity baseline is keyed
// by (<name>/rule/<index>), and its change-mode class. The operation
// table (Ops) points into it through OpDef.Oracle; FromOracle translates
// a docxdriver plan into the contract's grammar, and tools/oracle
// generates and classifies its cases from here.

// OracleOp is one docxdriver 0.5.2 operation.
type OracleOp struct {
	// Name is docxdriver's op tag ("replace_text").
	Name string
	// Track and Direct are its change-mode classes.
	Track  Track
	Direct Direct
	// TrackRefused lists the edit_table action kinds track mode refuses.
	TrackRefused []string
	// Fields are its plan fields in declaration order.
	Fields []OracleFieldDef
	// Rules are its validation rules in docxdriver's order.
	Rules []OracleRule
}

// OracleFieldDef is one field of a docxdriver operation.
type OracleFieldDef struct {
	Name string
	// Required reports whether serde refuses a plan without it.
	Required bool
}

// OracleRule is one validation rule of a docxdriver operation.
type OracleRule struct {
	// Msg is docxdriver's message and Path the field at fault.
	Msg, Path string
	// Example is an operation, in docxdriver's grammar, that breaks the
	// rule and no earlier one; "" when decoding makes it unreachable.
	Example string
}

// OracleOps returns docxdriver 0.5.2's operations in its EditOp order.
func OracleOps() []OracleOp { return oracleOps }

// OracleLookup returns the docxdriver operation named name.
func OracleLookup(name string) (*OracleOp, bool) {
	for i := range oracleOps {
		if oracleOps[i].Name == name {
			return &oracleOps[i], true
		}
	}
	return nil, false
}

// ContractOps returns the contract operations that run as the docxdriver
// operation named name, in table order.
func ContractOps(oracle string) []*OpDef {
	var out []*OpDef
	for i := range Ops {
		if Ops[i].Oracle == oracle {
			out = append(out, &Ops[i])
		}
	}
	return out
}

// oracleOps is the record, taken from the port of docxdriver's op table
// as it stood on 2026-11-13.
var oracleOps = []OracleOp{
	{Name: "replace_equation", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"latex", true}, {"equation", false}, {"display", false}},
		Rules: []OracleRule{
			{Msg: "LaTeX errors from math.CanonicalLaTeX (internal/docx/math), in display math unless display is false", Path: "", Example: `{"op":"replace_equation","at":"0000000A","latex":"\\frac{a}"}`},
			{Msg: "equation must be positive (1-based)", Path: "equation", Example: `{"op":"replace_equation","at":"0000000A","latex":"x^2","equation":0}`},
		}},
	{Name: "delete_equation", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"equation", false}},
		Rules: []OracleRule{
			{Msg: "equation must be positive (1-based)", Path: "equation", Example: `{"op":"delete_equation","at":"0000000A","equation":0}`},
		}},
	{Name: "replace_text", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"select", true}, {"with", true}, {"occurrence", false}},
		Rules: []OracleRule{
			{Msg: "select must be non-empty", Path: "select", Example: `{"op":"replace_text","at":"0000000A","select":"","with":"y"}`},
			{Msg: "occurrence must be positive", Path: "occurrence", Example: `{"op":"replace_text","at":"0000000A","select":"x","with":"y","occurrence":0}`},
		}},
	{Name: "revise_text", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", false}, {"within", false}, {"occurrence", false}, {"edits", true}},
		Rules: []OracleRule{
			{Msg: "within must be non-empty when given", Path: "within", Example: `{"op":"revise_text","within":"","edits":[{"select":"a","with":"b"}]}`},
			{Msg: "occurrence must be positive", Path: "occurrence", Example: `{"op":"revise_text","within":"w","occurrence":0,"edits":[{"select":"a","with":"b"}]}`},
			{Msg: "occurrence picks a within match; it requires within", Path: "occurrence", Example: `{"op":"revise_text","occurrence":1,"edits":[{"select":"a","with":"b"}]}`},
			{Msg: "edits must list at least one replacement", Path: "edits", Example: `{"op":"revise_text","edits":[]}`},
			{Msg: "edits[N].select must be non-empty | edits[N].occurrence must be positive | edits[N] sets both all and occurrence; choose one", Path: "", Example: `{"op":"revise_text","edits":[{"select":"a","with":"b"},{"select":"c","with":"d","all":true,"occurrence":2}]}`},
		}},
	{Name: "replace_paragraph", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"with", true}},
	},
	{Name: "edit_table", Track: Exact, Direct: AcceptAuthored,
		TrackRefused: []string{"insert_columns", "delete_columns", "merge_cells", "split_cell"},
		Fields:       []OracleFieldDef{{"at", true}, {"action", true}},
		Rules: []OracleRule{
			{Msg: "edit_table requires a table handle", Path: "at", Example: `{"op":"edit_table","at":"1","action":{"kind":"delete"}}`},
			{Msg: "invalid inserted table group alias", Path: "action.as", Example: `{"op":"edit_table","at":"table:1","action":{"kind":"insert_rows","row":1,"position":"after","count":1,"as":"new rows"}}`},
			{Msg: "table edit coordinates/counts must be positive; inserted count <=100", Path: "", Example: `{"op":"edit_table","at":"table:1","action":{"kind":"delete_rows","row":1,"count":101}}`},
		}},
	{Name: "insert_table", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"position", true}, {"rows", true}, {"columns", true}, {"cells", false}, {"as", false}},
		Rules: []OracleRule{
			{Msg: "insert_table dimensions must be 1..100", Path: "", Example: `{"op":"insert_table","at":"0000000A","position":"after","rows":0,"columns":2}`},
			{Msg: "invalid alias", Path: "as", Example: `{"op":"insert_table","at":"0000000A","position":"after","rows":1,"columns":1,"as":"t-1"}`},
			{Msg: "cells has N rows but the table has R: remove cells[R..] (row R+1[ to row N])", Path: "", Example: `{"op":"insert_table","at":"0000000A","position":"after","rows":1,"columns":1,"cells":[["a"],["b"],["c"]]}`},
			{Msg: "cells[I] (row I+1) has N cells but the table has C columns: remove cells[I][C..] (column C+1[ to column N])", Path: "", Example: `{"op":"insert_table","at":"0000000A","position":"after","rows":2,"columns":1,"cells":[["a"],["b","c"]]}`},
		}},
	{Name: "format_table", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"format", true}},
		Rules: []OracleRule{
			{Msg: "expected <form> address, got \"…\" | invalid table address component \"…\"", Path: "", Example: `{"op":"format_table","at":"table:1/row:1","format":{"alignment":"center"}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.padding.top", Example: `{"op":"format_table","at":"table:1","format":{"padding":{"top":3e9}}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.padding.right", Example: `{"op":"format_table","at":"table:1","format":{"padding":{"right":3e9}}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.padding.bottom", Example: `{"op":"format_table","at":"table:1","format":{"padding":{"bottom":3e9}}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.padding.left", Example: `{"op":"format_table","at":"table:1","format":{"padding":{"left":3e9}}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.cell_spacing", Example: `{"op":"format_table","at":"table:1","format":{"cell_spacing":2e9}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "", Example: `{"op":"format_table","at":"table:1","format":{"column_widths":[72,2e9]}}`},
			{Msg: "column_widths cannot be cleared: the grid must retain its dimensions", Path: "", Example: `{"op":"format_table","at":"table:1","format":{"clear":["column_widths"]}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.width.value", Example: `{"op":"format_table","at":"table:1","format":{"width":{"unit":"points","value":2e9}}}`},
			{Msg: "table percentage width must be > 0 and <= 100", Path: "format.width.value", Example: `{"op":"format_table","at":"table:1","format":{"width":{"unit":"percent","value":150}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.top.style", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"top":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.top.color", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"top":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.top.width", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"top":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.left.style", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"left":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.left.color", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"left":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.left.width", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"left":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.bottom.style", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"bottom":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.bottom.color", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"bottom":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.bottom.width", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"bottom":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.right.style", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"right":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.right.color", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"right":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.right.width", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"right":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.inside_horizontal.style", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"inside_horizontal":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.inside_horizontal.color", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"inside_horizontal":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.inside_horizontal.width", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"inside_horizontal":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.inside_vertical.style", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"inside_vertical":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.inside_vertical.color", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"inside_vertical":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.inside_vertical.width", Example: `{"op":"format_table","at":"table:1","format":{"borders":{"inside_vertical":{"style":"single","width":0.3}}}}`},
			{Msg: "unknown clear property \"…\"", Path: "", Example: `{"op":"format_table","at":"table:1","format":{"clear":["shading"]}}`},
			{Msg: "layout must be autofit or fixed", Path: "format.layout", Example: `{"op":"format_table","at":"table:1","format":{"layout":"auto"}}`},
			{Msg: "table alignment must be left, center or right", Path: "format.alignment", Example: `{"op":"format_table","at":"table:1","format":{"alignment":"justify"}}`},
			{Msg: "column_widths must contain positive point widths", Path: "format.column_widths", Example: `{"op":"format_table","at":"table:1","format":{"column_widths":[72,0]}}`},
			{Msg: "format_table requires a property", Path: "format", Example: `{"op":"format_table","at":"table:1","format":{}}`},
		}},
	{Name: "format_row", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"format", true}},
		Rules: []OracleRule{
			{Msg: "expected <form> address, got \"…\" | invalid table address component \"…\"", Path: "", Example: `{"op":"format_row","at":"table:1/cell:1","format":{"allow_split":true}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.height", Example: `{"op":"format_row","at":"table:1/row:1","format":{"height":2e9}}`},
			{Msg: "unknown clear property \"…\"", Path: "", Example: `{"op":"format_row","at":"table:1/row:1","format":{"clear":["width"]}}`},
			{Msg: "height_rule must be auto, at_least or exact", Path: "format.height_rule", Example: `{"op":"format_row","at":"table:1/row:1","format":{"height_rule":"atLeast"}}`},
			{Msg: "format_row requires a property", Path: "format", Example: `{"op":"format_row","at":"table:1/row:1","format":{}}`},
		}},
	{Name: "format_cell", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"format", true}},
		Rules: []OracleRule{
			{Msg: "expected <form> address, got \"…\" | invalid table address component \"…\"", Path: "", Example: `{"op":"format_cell","at":"table:1/row:1/cell:0","format":{"fill":"#FFFFFF"}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.padding.top", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"padding":{"top":3e9}}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.padding.right", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"padding":{"right":3e9}}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.padding.bottom", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"padding":{"bottom":3e9}}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.padding.left", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"padding":{"left":3e9}}}`},
			{Msg: "table dimensions must be nonnegative and fit signed 32-bit twips", Path: "format.width.value", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"width":{"unit":"points","value":2e9}}}`},
			{Msg: "table percentage width must be > 0 and <= 100", Path: "format.width.value", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"width":{"unit":"percent","value":150}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.top.style", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"top":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.top.color", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"top":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.top.width", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"top":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.left.style", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"left":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.left.color", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"left":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.left.width", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"left":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.bottom.style", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"bottom":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.bottom.color", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"bottom":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.bottom.width", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"bottom":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.right.style", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"right":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.right.color", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"right":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.right.width", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"right":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.inside_horizontal.style", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"inside_horizontal":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.inside_horizontal.color", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"inside_horizontal":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.inside_horizontal.width", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"inside_horizontal":{"style":"single","width":0.3}}}}`},
			{Msg: "unsupported table border style", Path: "format.borders.inside_vertical.style", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"inside_vertical":{"style":"groove"}}}}`},
			{Msg: "border color must be #RRGGBB", Path: "format.borders.inside_vertical.color", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"inside_vertical":{"style":"single","color":"red"}}}}`},
			{Msg: "border width must be 0.25..12 points in quarter-point increments", Path: "format.borders.inside_vertical.width", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"borders":{"inside_vertical":{"style":"single","width":0.3}}}}`},
			{Msg: "unknown clear property \"…\"", Path: "", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"clear":["shading"]}}`},
			{Msg: "cell fill must be #RRGGBB", Path: "format.fill", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"fill":"#fff"}}`},
			{Msg: "vertical_alignment must be top, center or bottom", Path: "format.vertical_alignment", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{"vertical_alignment":"middle"}}`},
			{Msg: "format_cell requires a property", Path: "format", Example: `{"op":"format_cell","at":"table:1/row:1/cell:1","format":{}}`},
		}},
	{Name: "format_text", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"select", true}, {"occurrence", false}, {"bold", false}, {"italic", false}, {"underline", false}, {"strike", false}, {"superscript", false}, {"subscript", false}, {"color", false}, {"highlight", false}, {"font_size", false}, {"clear", false}},
		Rules: []OracleRule{
			{Msg: "select must be non-empty", Path: "select", Example: `{"op":"format_text","at":"0000000A","select":"","bold":true}`},
			{Msg: "occurrence must be positive", Path: "occurrence", Example: `{"op":"format_text","at":"0000000A","select":"x","occurrence":0,"bold":true}`},
			{Msg: "superscript and subscript cannot both be enabled", Path: "superscript", Example: `{"op":"format_text","at":"0000000A","select":"x","superscript":true,"subscript":true}`},
			{Msg: "color must be #RRGGBB", Path: "color", Example: `{"op":"format_text","at":"0000000A","select":"x","color":"#abcdef"}`},
			{Msg: "format_text requires a property", Path: "", Example: `{"op":"format_text","at":"0000000A","select":"x"}`},
			{Msg: "format_text clear contains an unknown property", Path: "", Example: `{"op":"format_text","at":"0000000A","select":"x","clear":["bold","shadow"]}`},
		}},
	{Name: "format_paragraph", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"style", false}, {"alignment", false}, {"indent_left", false}, {"indent_right", false}, {"first_line_indent", false}, {"space_before", false}, {"space_after", false}, {"line_spacing", false}, {"clear", false}},
		Rules: []OracleRule{
			{Msg: "paragraph indentation must be between -100000 and 100000 points", Path: "", Example: ``},
			{Msg: "style must be non-empty", Path: "style", Example: `{"op":"format_paragraph","at":"0000000A","style":""}`},
			{Msg: "alignment must be justify, not both", Path: "alignment", Example: `{"op":"format_paragraph","at":"0000000A","alignment":"Both"}`},
			{Msg: "invalid alignment", Path: "alignment", Example: `{"op":"format_paragraph","at":"0000000A","alignment":"start"}`},
			{Msg: "format_paragraph requires a property", Path: "", Example: `{"op":"format_paragraph","at":"0000000A"}`},
		}},
	{Name: "set_list", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"levels", false}, {"at", true}, {"num_id", false}, {"format", false}, {"level", false}, {"start", false}, {"remove", false}},
		Rules: []OracleRule{
			{Msg: "levels requires a new list with 1..9 definitions covering the selected level", Path: "levels", Example: `{"op":"set_list","at":["0000000A"],"level":1,"levels":[{"format":"decimal","label":"%1."}]}`},
			{Msg: "invalid list level format, label, start or restart ancestor | list indentation is out of range | list labels may only reference this level or ancestor %1..%9 counters | invalid list marker font or six-digit RGB color", Path: "", Example: `{"op":"set_list","at":["0000000A"],"levels":[{"format":"decimal","label":"%1.","marker_color":"red"},{"format":"roman","label":"%2"}]}`},
			{Msg: "set_list requires paragraphs, level 0..8 and IDs/starts in 1..2147483647", Path: "", Example: `{"op":"set_list","at":["0000000A"],"num_id":0}`},
			{Msg: "set_list remove cannot be combined with list settings", Path: "remove", Example: `{"op":"set_list","at":["0000000A"],"remove":true,"level":1}`},
			{Msg: "unsupported set_list format", Path: "format", Example: `{"op":"set_list","at":["0000000A"],"format":"roman"}`},
			{Msg: "format cannot restyle an existing list; create a new list", Path: "format", Example: `{"op":"set_list","at":["0000000A"],"format":"bullet","num_id":3}`},
		}},
	{Name: "insert_paragraph", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}, {"position", true}, {"with", false}, {"style", false}, {"as", false}},
		Rules: []OracleRule{
			{Msg: "invalid alias", Path: "as", Example: `{"op":"insert_paragraph","at":"0000000A","position":"after","as":"1st"}`},
			{Msg: "a with list must not be empty", Path: "with", Example: `{"op":"insert_paragraph","at":"0000000A","position":"after","with":[]}`},
		}},
	{Name: "delete_paragraphs", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"at", true}},
		Rules: []OracleRule{
			{Msg: "at must not be empty", Path: "at", Example: `{"op":"delete_paragraphs","at":[]}`},
		}},
	{Name: "set_page_margins", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"section", false}, {"top", false}, {"right", false}, {"bottom", false}, {"left", false}, {"header", false}, {"footer", false}, {"gutter", false}},
		Rules: []OracleRule{
			{Msg: "section must be positive", Path: "section", Example: `{"op":"set_page_margins","section":0,"top":72}`},
			{Msg: "set_page_margins requires a margin", Path: "", Example: `{"op":"set_page_margins","section":1}`},
		}},
	{Name: "set_section_boundary", Track: DirectOnly, Direct: OnlyDirect,
		Fields: []OracleFieldDef{{"at", true}, {"remove", false}, {"break_type", false}},
		Rules: []OracleRule{
			{Msg: "removing a boundary cannot set break_type", Path: "break_type", Example: `{"op":"set_section_boundary","at":"0000000A","remove":true,"break_type":"nextPage"}`},
			{Msg: "unsupported section break_type", Path: "break_type", Example: `{"op":"set_section_boundary","at":"0000000A","break_type":"nextpage"}`},
		}},
	{Name: "set_section_layout", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"section_handle", false}, {"section", false}, {"page_size", false}, {"width", false}, {"height", false}, {"orientation", false}, {"columns", false}, {"column_spacing", false}, {"column_widths", false}, {"column_separator", false}, {"break_type", false}, {"page_number_format", false}, {"page_number_start", false}, {"page_number_continue", false}},
		Rules: []OracleRule{
			{Msg: "section and section_handle are mutually exclusive", Path: "section_handle", Example: `{"op":"set_section_layout","section":1,"section_handle":"section:1","orientation":"landscape"}`},
			{Msg: "section_handle must use an inventory handle", Path: "section_handle", Example: `{"op":"set_section_layout","section_handle":"1","orientation":"landscape"}`},
			{Msg: "section must be positive", Path: "section", Example: `{"op":"set_section_layout","section":0,"orientation":"landscape"}`},
			{Msg: "page_size must be letter/legal/a4/a3/a5", Path: "page_size", Example: `{"op":"set_section_layout","page_size":"A4"}`},
			{Msg: "page_size conflicts with width/height", Path: "page_size", Example: `{"op":"set_section_layout","page_size":"a4","width":595,"height":842}`},
			{Msg: "width and height must be supplied together", Path: "", Example: `{"op":"set_section_layout","width":612}`},
			{Msg: "page dimensions must be >0 and <=1584 points", Path: "", Example: `{"op":"set_section_layout","width":612,"height":2000}`},
			{Msg: "orientation must be portrait/landscape", Path: "orientation", Example: `{"op":"set_section_layout","orientation":"wide"}`},
			{Msg: "columns must be between 1 and 45", Path: "columns", Example: `{"op":"set_section_layout","columns":46}`},
			{Msg: "column_spacing must be between 0 and 1584 points", Path: "column_spacing", Example: `{"op":"set_section_layout","column_spacing":1600}`},
			{Msg: "column_widths requires 1..45 positive widths of at most 1584 points", Path: "", Example: `{"op":"set_section_layout","column_widths":[200,0]}`},
			{Msg: "columns must match column_widths length", Path: "columns", Example: `{"op":"set_section_layout","columns":3,"column_widths":[200,200]}`},
			{Msg: "unsupported section break_type", Path: "break_type", Example: `{"op":"set_section_layout","break_type":"page"}`},
			{Msg: "unsupported page_number_format", Path: "page_number_format", Example: `{"op":"set_section_layout","page_number_format":"roman"}`},
			{Msg: "page_number_continue only accepts true; use page_number_start to restart", Path: "page_number_continue", Example: `{"op":"set_section_layout","page_number_continue":false}`},
			{Msg: "page_number_start must be between 1 and 2147483647", Path: "page_number_start", Example: `{"op":"set_section_layout","page_number_start":0}`},
			{Msg: "page_number_start conflicts with page_number_continue", Path: "page_number_start", Example: `{"op":"set_section_layout","page_number_start":3,"page_number_continue":true}`},
			{Msg: "set_section_layout requires a property", Path: "", Example: `{"op":"set_section_layout","section":2}`},
		}},
	{Name: "set_first_page_mode", Track: Exact, Direct: AcceptAuthored,
		Fields: []OracleFieldDef{{"section", false}, {"enabled", true}},
		Rules: []OracleRule{
			{Msg: "section must be positive", Path: "section", Example: `{"op":"set_first_page_mode","section":0,"enabled":true}`},
		}},
	{Name: "set_even_and_odd_headers", Track: DirectOnly, Direct: OnlyDirect,
		Fields: []OracleFieldDef{{"even_and_odd", true}},
	},
	{Name: "set_header", Track: KeepsAdditions, Direct: OwnPath,
		Fields: []OracleFieldDef{{"section", false}, {"kind", false}, {"with", true}},
		Rules: []OracleRule{
			{Msg: "section must be positive", Path: "section", Example: `{"op":"set_header","section":0,"with":"<p>x</p>"}`},
		}},
	{Name: "set_footer", Track: ComplexField, Direct: OwnPath,
		Fields: []OracleFieldDef{{"section", false}, {"kind", false}, {"with", true}},
		Rules: []OracleRule{
			{Msg: "section must be positive", Path: "section", Example: `{"op":"set_footer","section":0,"with":"<p>x</p>"}`},
		}},
	{Name: "clear_header", Track: EmptiesStory, Direct: OwnPath,
		Fields: []OracleFieldDef{{"section", false}, {"kind", false}},
		Rules: []OracleRule{
			{Msg: "section must be positive", Path: "section", Example: `{"op":"clear_header","section":0}`},
		}},
	{Name: "clear_footer", Track: EmptiesStory, Direct: OwnPath,
		Fields: []OracleFieldDef{{"section", false}, {"kind", false}},
		Rules: []OracleRule{
			{Msg: "section must be positive", Path: "section", Example: `{"op":"clear_footer","section":0}`},
		}},
	{Name: "comment_add", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"at", true}, {"select", false}, {"occurrence", false}, {"text", true}},
		Rules: []OracleRule{
			{Msg: "comment text must be non-empty", Path: "text", Example: `{"op":"comment_add","at":"0000000A","text":""}`},
			{Msg: "occurrence must be positive", Path: "occurrence", Example: `{"op":"comment_add","at":"0000000A","select":"x","occurrence":0,"text":"t"}`},
			{Msg: "occurrence requires select", Path: "occurrence", Example: `{"op":"comment_add","at":"0000000A","occurrence":1,"text":"t"}`},
		}},
	{Name: "comment_reply", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"comment_id", true}, {"text", true}},
		Rules: []OracleRule{
			{Msg: "comment_id must be decimal", Path: "comment_id", Example: `{"op":"comment_reply","comment_id":"c1","text":"t"}`},
			{Msg: "comment text must be non-empty", Path: "text", Example: `{"op":"comment_reply","comment_id":"1","text":""}`},
		}},
	{Name: "comment_set_status", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"comment_id", true}, {"status", true}},
		Rules: []OracleRule{
			{Msg: "comment_id must be decimal", Path: "comment_id", Example: `{"op":"comment_set_status","comment_id":"x1","status":"open"}`},
		}},
	{Name: "comment_delete", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"comment_id", true}},
		Rules: []OracleRule{
			{Msg: "comment_id must be decimal", Path: "comment_id", Example: `{"op":"comment_delete","comment_id":""}`},
		}},
	{Name: "bookmark_add", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"at", true}, {"name", true}, {"select", false}, {"occurrence", false}},
		Rules: []OracleRule{
			{Msg: "name \"…\" starts with \"_\" … | name \"…\" is not a valid bookmark name: … | name \"…\" has N characters; …", Path: "", Example: `{"op":"bookmark_add","at":"0000000A","name":"Clause 4"}`},
			{Msg: "select must be non-empty; omit it to bookmark the whole paragraph", Path: "select", Example: `{"op":"bookmark_add","at":"0000000A","name":"Mark","select":""}`},
			{Msg: "occurrence must be positive", Path: "occurrence", Example: `{"op":"bookmark_add","at":"0000000A","name":"Mark","select":"x","occurrence":0}`},
			{Msg: "occurrence requires select", Path: "occurrence", Example: `{"op":"bookmark_add","at":"0000000A","name":"Mark","occurrence":1}`},
		}},
	{Name: "bookmark_rename", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"name", true}, {"new_name", true}},
		Rules: []OracleRule{
			{Msg: "name must name an existing bookmark; read kind=\"bookmarks\" lists them", Path: "name", Example: `{"op":"bookmark_rename","name":"","new_name":"Mark"}`},
			{Msg: "new_name \"…\" starts with \"_\" … | new_name \"…\" is not a valid bookmark name: … | new_name \"…\" has N characters; …", Path: "", Example: `{"op":"bookmark_rename","name":"Old","new_name":"_Hidden"}`},
		}},
	{Name: "bookmark_move", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"name", true}, {"at", true}, {"select", false}, {"occurrence", false}},
		Rules: []OracleRule{
			{Msg: "name must name an existing bookmark; read kind=\"bookmarks\" lists them", Path: "name", Example: `{"op":"bookmark_move","name":"","at":"0000000A"}`},
			{Msg: "select must be non-empty; omit it to bookmark the whole paragraph", Path: "select", Example: `{"op":"bookmark_move","name":"Mark","at":"0000000A","select":""}`},
			{Msg: "occurrence must be positive", Path: "occurrence", Example: `{"op":"bookmark_move","name":"Mark","at":"0000000A","select":"x","occurrence":0}`},
			{Msg: "occurrence requires select", Path: "occurrence", Example: `{"op":"bookmark_move","name":"Mark","at":"0000000A","occurrence":1}`},
		}},
	{Name: "bookmark_delete", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"name", true}},
		Rules: []OracleRule{
			{Msg: "name must name an existing bookmark; read kind=\"bookmarks\" lists them", Path: "name", Example: `{"op":"bookmark_delete","name":""}`},
		}},
	{Name: "revision_settle", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"target", true}, {"action", true}},
		Rules: []OracleRule{
			{Msg: "revision target must be decimal", Path: "target", Example: ``},
		}},
	{Name: "convert_revisions_to_formatting", Track: Untracked, Direct: Same,
		Fields: []OracleFieldDef{{"target", true}, {"at", false}},
		Rules: []OracleRule{
			{Msg: "at must name at least one paragraph when given", Path: "at", Example: `{"op":"convert_revisions_to_formatting","target":"all","at":[]}`},
		}},
}
