package main

import (
	"reflect"

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
		out = append(out, cli.OpDoc{Name: d.Name, Summary: d.Summary,
			Fields: cli.FieldDocs(reflect.TypeOf(d.New()).Elem(), nil)})
	}
	return out
}

// opsHelp is `vsdx help ops`.
func opsHelp() string { return cli.OpsHelp("vsdx", opsIntro, opDocs()) }

// opHelp is `vsdx help <op>`.
func opHelp(name string) (string, bool) { return cli.FindOpHelp(opDocs(), name) }
