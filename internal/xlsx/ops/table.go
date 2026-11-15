package ops

// Ops is the operation table: the xlsx v1 operations of the draft
// contract (docs/xlsx-contract-draft.md, §3), in its order. Each entry
// is defined in its own run_<name>.go.
var Ops = []OpDef{
	opCellSet,
	opRangeSet,
	opRangeClear,
	opTextReplace,
	opRowInsert,
	opRowDelete,
	opColumnInsert,
	opColumnDelete,
	opRowFormat,
	opColumnFormat,
	opRangeFormat,
	opCellMerge,
	opCellSplit,
	opSheetAdd,
	opSheetRename,
	opNameAdd,
	opNameSet,
	opNameDelete,
}
