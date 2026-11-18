package main

import (
	"reflect"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
)

// opsIntro is the text above the generated operation list.
const opsIntro = `Operations of a vsdx plan. Required fields first, optional in brackets.
at is an address as vsdx read shows it: page0/s5, master2/s5, $name for
what an earlier operation's as created. Lengths are points unless the
plan sets units.`

// opDocs returns documentation for every implemented vsdx plan operation.
func opDocs() []cli.OpDoc {
	var out []cli.OpDoc
	for i := range ops.Ops {
		d := &ops.Ops[i]
		if !d.Implemented() {
			continue
		}
		doc := cli.OpDoc{Name: d.Name, Summary: d.Summary,
			Fields: cli.FieldDocs(reflect.TypeOf(d.New()).Elem(), nil)}
		if info, ok := ops.HelpFor(d.Name); ok {
			doc.Rules = append(doc.Rules, info.Rules...)
		}
		out = append(out, doc)
	}
	return out
}

// opsHelp is `vsdx help ops`.
func opsHelp() string { return cli.OpsHelp("vsdx", opsIntro, opDocs()) }

// opHelp is `vsdx help <op>`: the summary, fields with their notes,
// rules and refuses, as xlsx prints them. The refuses (the
// document-dependent refusals of the op, one per line) are appended
// after cli.OpHelp, which prints summary, fields and rules.
func opHelp(name string) (string, bool) {
	text, ok := cli.FindOpHelp(opDocs(), name)
	if !ok {
		return "", false
	}
	if info, ok := ops.HelpFor(name); ok && len(info.Refuses) > 0 {
		var b strings.Builder
		b.WriteString(text)
		b.WriteString("\nrefuses:\n")
		for _, r := range info.Refuses {
			b.WriteString("  " + r + "\n")
		}
		text = b.String()
	}
	return text, true
}
