package ops

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// The rename guard kernel (chunk K15): pure.

// RenameParts are the texts RenameGuard reads (the glue fills them with
// Workspace.PartText): every chart part, every pivot cache definition and
// every external link part of the workbook.
type RenameParts struct {
	Charts, PivotCaches, ExternalLinks []string
}

// RenameGuard refuses renaming sheet old when a construct on
// renameRefuses names it: a chart's <c:f> whose formula references old
// (formula.SplitQualified on each reference, ignoring case); a pivot
// cache whose worksheetSource@sheet is old; an external link whose
// sheetNames list old. Each refusal is a Rejection, operation_failed,
// detail naming the construct and its part. Nothing naming old is never
// a refusal.
func RenameGuard(p RenameParts, old string) error {
	for _, group := range []struct {
		parts []string
		kind  string
	}{{p.Charts, "chart"}, {p.PivotCaches, "pivot cache"}, {p.ExternalLinks, "external link"}} {
		for i, src := range group.parts {
			v := &renameGuardVisitor{kind: group.kind, old: old}
			if err := tree.ScanString(src, v); err != nil {
				return fmt.Errorf("scan %s part %d: %w", group.kind, i+1, err)
			}
			if v.found {
				detail := fmt.Sprintf("%s part %d names sheet %q", group.kind, i+1, old)
				return &Rejection{Code: cli.CodeOperationFailed, Msg: "sheet rename refused", Detail: detail}
			}
		}
	}
	return nil
}

type renameGuardVisitor struct {
	kind, old                       string
	depth, formulaDepth, namesDepth int
	text                            strings.Builder
	found                           bool
}

// Start tracks nesting and notes references to the old sheet name.
func (v *renameGuardVisitor) Start(n *tree.Node) error {
	v.depth++
	switch v.kind {
	case "chart":
		if n.Local() == "f" && v.formulaDepth == 0 {
			v.formulaDepth = v.depth
			v.text.Reset()
		}
	case "pivot cache":
		if n.Local() == "worksheetSource" {
			value, ok := n.Attr("sheet")
			v.found = v.found || ok && strings.EqualFold(value, v.old)
		}
	case "external link":
		if n.Local() == "sheetNames" {
			v.namesDepth = v.depth
		} else if v.namesDepth > 0 && n.Local() == "sheetName" {
			value, ok := n.Attr("val")
			v.found = v.found || ok && strings.EqualFold(value, v.old)
		}
	}
	return nil
}

// End checks completed chart formulas for the old sheet name and unwinds nesting.
func (v *renameGuardVisitor) End(_ *tree.Node) error {
	if v.formulaDepth == v.depth {
		for _, token := range formula.Lex(v.text.String()) {
			if token.Kind != formula.Ref && token.Kind != formula.Name && token.Kind != formula.Func {
				continue
			}
			prefix, _, ok := formula.SplitQualified(token.Text)
			if ok && !prefix.HasBook && (strings.EqualFold(prefix.First, v.old) || prefix.Last != "" && strings.EqualFold(prefix.Last, v.old)) {
				v.found = true
			}
		}
		v.formulaDepth = 0
	}
	if v.namesDepth == v.depth {
		v.namesDepth = 0
	}
	v.depth--
	return nil
}

// Leaf appends text inside a chart formula to the visitor's buffer.
func (v *renameGuardVisitor) Leaf(n *tree.Node) error {
	if v.formulaDepth > 0 {
		v.text.WriteString(n.Data())
	}
	return nil
}
