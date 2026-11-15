package main

import (
	"reflect"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

// opsHelp is `xlsx help ops` and opHelp `xlsx help <op>` (chunk G19c):
// generated from ops.Ops with cli.OpsHelp and cli.OpHelp (Summary;
// fields from cli.FieldDocs with an xlsx typeName: the address types as
// "address", Grid as "rows of values", CellValue as "value", Position as
// "before|after", NameScope as "workbook|sheet:Name"; rules' Msg; and for
// structural ops and sheet.rename the Adjusts and Refuses lists as two
// more paragraphs). Never a hand-written list.
func opsHelp() string {
	docs := make([]cli.OpDoc, 0, len(ops.Ops))
	for i := range ops.Ops {
		docs = append(docs, operationDoc(&ops.Ops[i]))
	}
	return cli.OpsHelp("xlsx", "Operations of an xlsx plan. Required fields first, optional in brackets.", docs)
}

// operationDoc converts a table entry to the shared help representation.
func operationDoc(d *ops.OpDef) cli.OpDoc {
	doc := cli.OpDoc{Name: d.Name, Summary: d.Summary,
		Fields: cli.FieldDocs(reflect.TypeOf(d.New()).Elem(), xlsxTypeName)}
	for _, r := range d.Rules {
		doc.Rules = append(doc.Rules, r.Msg)
	}
	return doc
}

// xlsxTypeName names plan types as they appear in JSON.
func xlsxTypeName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t {
	case reflect.TypeFor[ops.CellAddress](), reflect.TypeFor[ops.RangeAddress](),
		reflect.TypeFor[ops.RowsAddress](), reflect.TypeFor[ops.ColsAddress](),
		reflect.TypeFor[ops.TextScope](), reflect.TypeFor[ops.SheetAddress](),
		reflect.TypeFor[ops.NameAddress]():
		return "address"
	case reflect.TypeFor[ops.Grid]():
		return "rows of values"
	case reflect.TypeFor[ops.CellValue]():
		return "value"
	case reflect.TypeFor[ops.Position]():
		return "before|after"
	case reflect.TypeFor[ops.NameScope]():
		return "workbook|sheet:Name"
	}
	if t.Kind() == reflect.Slice {
		return "list of " + xlsxTypeName(t.Elem())
	}
	return cli.TypeName(t)
}

// opHelp answers `xlsx help <op>`: false for a name that is no op.
func opHelp(name string) (string, bool) {
	d, ok := ops.Lookup(name)
	if !ok {
		return "", false
	}
	text := cli.OpHelp(operationDoc(d))
	if len(d.Adjusts) > 0 {
		text += "\nadjusts: " + strings.Join(d.Adjusts, ", ") + "\n"
	}
	if len(d.Refuses) > 0 {
		text += "\nrefuses: " + strings.Join(d.Refuses, ", ") + "\n"
	}
	return text, true
}
