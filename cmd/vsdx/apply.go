package main

import (
	"io"

	"github.com/jhnoel/documentdriver/internal/cli"
)

var applyCommand = &cli.Command{
	Name:    "apply",
	Args:    []string{"drawing.vsdx", "plan.json|-"},
	JSON:    true,
	Summary: "apply a plan of operations, all or nothing",
	Flags: []cli.Flag{
		{Name: "dry-run", Help: "do everything except write"},
		cli.OutFlag(),
		{Name: "changed", Value: "ids|full", Enum: []string{"ids", "full"}, Help: "show changed units as addresses only, or as read shows them (default)"},
		{Name: "seed", Value: "n", Hidden: true},
		{Name: "clock", Value: "rfc3339", Hidden: true},
	},
	Help: "Applies a plan to the drawing and prints the edit result (docs/vsdx-contract-draft.md, section 4).",
}

// runApply is the apply handler (chunk A2): the plan through
// ops.ParsePlan, apply.Run, the result as pptx apply prints it.
func runApply(p *cli.Parsed, stdout io.Writer) error {
	_, _ = p, stdout
	return cli.Errorf(cli.CodeOperationFailed, "", "vsdx apply is not implemented yet")
}
