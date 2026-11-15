package ops

// Ops is the operation table: the 42 operations of the agent contract
// (docs/contract.md, section 3), in its order. Each entry is defined in
// its own file, run_<name>.go, with the operation's Spec and Run, so that
// porting an operation touches that file only. An entry names the
// docxdriver 0.5.2 operation it runs as and what the contract fixes or
// moves (the oracle mapping tools/oracle translates plans by).
var Ops = []OpDef{
	opTextReplace,
	opTextRevise,
	opTextInsert,
	opTextFormat,
	opParagraphInsert,
	opParagraphReplace,
	opParagraphDelete,
	opParagraphFormat,
	opListSet,
	opEquationReplace,
	opEquationDelete,
	opTableInsert,
	opTableDelete,
	opTableFormat,
	opRowInsert,
	opRowDelete,
	opRowFormat,
	opColumnInsert,
	opColumnDelete,
	opCellMerge,
	opCellSplit,
	opCellFormat,
	opSectionSetMargins,
	opSectionSetLayout,
	opSectionSetBoundary,
	opSectionSetFirstPage,
	opDocumentSetEvenOddHeaders,
	opHeaderSet,
	opHeaderDelete,
	opFooterSet,
	opFooterDelete,
	opCommentAdd,
	opCommentReply,
	opCommentSetStatus,
	opCommentDelete,
	opBookmarkAdd,
	opBookmarkRename,
	opBookmarkMove,
	opBookmarkDelete,
	opRevisionAccept,
	opRevisionReject,
	opRevisionConvertToFormatting,
}

// Description is the operation's description.
func (d *OpDef) Description() string { return opDocs[d.Name] }
