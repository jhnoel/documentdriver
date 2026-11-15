package cli

import (
	"fmt"
	"slices"
	"strings"
)

// Flag is one named argument of a command.
type Flag struct {
	// Name is the long name without "--".
	Name string
	// Value names the flag's value in help ("slideN"); "" for a boolean
	// flag, which takes none.
	Value string
	// Enum lists the allowed values, when they are enumerated.
	Enum []string
	// Help is one line for the help text. A hidden flag is not listed.
	Help   string
	Hidden bool
}

// Command is one command's grammar and help.
type Command struct {
	Name string
	// Args names the positional arguments; all are required.
	Args []string
	// Optional names positional arguments after Args that may be left
	// out (xlsx create's plan).
	Optional []string
	// Flags are the command's own flags. Every command also takes
	// --help; commands that print JSON take --pretty (JSON).
	Flags []Flag
	// JSON marks a command whose result is JSON.
	JSON bool
	// Summary is one line; Help is the text after the usage line.
	Summary string
	Help    string
	// Hidden commands run but are not listed.
	Hidden bool
}

// Parsed is what a command line held.
type Parsed struct {
	// Args are the positional arguments.
	Args   []string
	values map[string]string
	// Help is set when --help was given; nothing else was checked.
	Help bool
}

// Has reports whether the flag was given.
func (p *Parsed) Has(name string) bool {
	_, ok := p.values[name]
	return ok
}

// Get returns the flag's value, "" when it was not given.
func (p *Parsed) Get(name string) string { return p.values[name] }

var (
	outFlag    = Flag{Name: "out", Value: "file", Help: "write to this file (-o)"}
	prettyFlag = Flag{Name: "pretty", Help: "indent the JSON"}
)

// OutFlag is the shared -o/--out flag, for commands that write a file.
func OutFlag() Flag { return outFlag }

// flag returns the named flag of the command, including pretty for JSON commands.
func (c *Command) flag(name string) (Flag, bool) {
	if c.JSON && name == "pretty" {
		return prettyFlag, true
	}
	for _, f := range c.Flags {
		if f.Name == name {
			return f, true
		}
	}
	return Flag{}, false
}

// usageErr builds a usage error for the command, hinting at its help.
func (c *Command) usageErr(tool, path, format string, args ...any) *Error {
	return Errorf(CodeUsage, path, format, args...).WithHint("run `" + tool + " help " + c.Name + "`")
}

// Parse parses the arguments after the command name: --name value or
// --name=value anywhere, -o as --out, -h as --help, "--" ending flags,
// and "-" as a positional. Anything else is a usage error.
func (c *Command) Parse(tool string, args []string) (*Parsed, *Error) {
	p := &Parsed{values: map[string]string{}}
	onlyArgs := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case onlyArgs || a == "-" || !strings.HasPrefix(a, "-"):
			p.Args = append(p.Args, a)
			continue
		case a == "--":
			onlyArgs = true
			continue
		case a == "-h" || a == "--help":
			p.Help = true
			return p, nil
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		switch {
		case a == "-o":
			name, hasVal = "out", false
		case !strings.HasPrefix(a, "--"):
			return nil, c.usageErr(tool, a, "unknown flag %s", a)
		}
		f, ok := c.flag(name)
		if !ok {
			return nil, c.usageErr(tool, "--"+name, "unknown flag --%s", name)
		}
		if p.Has(f.Name) {
			return nil, c.usageErr(tool, "--"+name, "--%s is given twice", name)
		}
		if f.Value == "" {
			if hasVal {
				return nil, c.usageErr(tool, "--"+name, "--%s takes no value", name)
			}
			p.values[f.Name] = "true"
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, c.usageErr(tool, "--"+name, "--%s needs a value (%s)", name, f.Value)
			}
			i++
			val = args[i] //nolint:gosec // G602: i+1 < len(args) is checked just above
		}
		if f.Enum != nil && !slices.Contains(f.Enum, val) {
			return nil, c.usageErr(tool, "--"+name, "--%s is %s, not %q", name, strings.Join(f.Enum, " or "), val)
		}
		p.values[f.Name] = val
	}
	switch {
	case len(p.Args) < len(c.Args):
		return nil, c.usageErr(tool, "", "missing argument <%s>", c.Args[len(p.Args)])
	case len(p.Args) > len(c.Args)+len(c.Optional):
		return nil, c.usageErr(tool, "", "unexpected argument %q", p.Args[len(c.Args)+len(c.Optional)])
	}
	return p, nil
}

// Usage is the command's usage line.
func (c *Command) Usage(tool string) string {
	var b strings.Builder
	b.WriteString(tool + " " + c.Name)
	for _, a := range c.Args {
		b.WriteString(" <" + a + ">")
	}
	for _, a := range c.Optional {
		b.WriteString(" [<" + a + ">]")
	}
	flags := slices.Clone(c.Flags)
	if c.JSON {
		flags = append(flags, prettyFlag)
	}
	for _, f := range flags {
		if f.Hidden {
			continue
		}
		b.WriteString(" [" + flagSpec(f) + "]")
	}
	return b.String()
}

// flagSpec formats one flag for the usage line.
func flagSpec(f Flag) string {
	name := "--" + f.Name
	if f.Name == "out" {
		name = "-o"
	}
	switch {
	case f.Value == "":
		return name
	case f.Enum != nil:
		return name + " " + strings.Join(f.Enum, "|")
	}
	return name + " <" + f.Value + ">"
}

// HelpText is the command's help: usage, the help body and the flags.
func (c *Command) HelpText(tool string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "usage: %s\n", c.Usage(tool))
	if c.Help != "" {
		b.WriteString("\n" + strings.TrimSpace(c.Help) + "\n")
	}
	flags := slices.Clone(c.Flags)
	if c.JSON {
		flags = append(flags, prettyFlag)
	}
	var lines []string
	for _, f := range flags {
		if !f.Hidden && f.Help != "" {
			lines = append(lines, fmt.Sprintf("  %-28s %s", flagSpec(f), f.Help))
		}
	}
	if len(lines) > 0 {
		b.WriteString("\nflags:\n" + strings.Join(lines, "\n") + "\n")
	}
	return b.String()
}
