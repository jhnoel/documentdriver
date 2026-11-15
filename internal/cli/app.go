package cli

import (
	"fmt"
	"io"
	"strings"
)

// Handler runs a parsed command. It writes the result to stdout and
// returns an error (an *Error, Errors, or one WriteErrors classifies)
// when it fails, having written nothing to stdout.
type Handler func(p *Parsed, stdout io.Writer) error

// App is one binary: its commands, help topics and version.
type App struct {
	// Tool is the binary's name; Version its release.
	Tool, Version string
	// Summary is one line about the tool.
	Summary string
	// Commands are the commands, in help order, each with its handler.
	Commands []*Command
	Handlers map[string]Handler
	// Topics are the extra `help <topic>` texts (ops, html, hazards), by
	// name. Topic, when set, answers names Topics does not have (one
	// operation's reference).
	Topics map[string]func() string
	Topic  func(name string) (string, bool)
}

// command returns the named command, or nil when the app has none.
func (a *App) command(name string) *Command {
	for _, c := range a.Commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Usage is the top-level help.
func (a *App) Usage() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nusage: %s <command> [arguments]\n\ncommands:\n", a.Summary, a.Tool)
	for _, c := range a.Commands {
		if !c.Hidden {
			fmt.Fprintf(&b, "  %-8s %s\n", c.Name, c.Summary)
		}
	}
	topics := make([]string, 0, len(a.Topics))
	for name := range a.Topics {
		topics = append(topics, name)
	}
	sortStrings(topics)
	fmt.Fprintf(&b, "  %-8s help [<command>%s]\n", "help", prefixedBar(topics))
	fmt.Fprintf(&b, "\n%s --version prints the tool, release and contract version.\nThe contract is docs/contract.md: one JSON result on stdout; on failure\none errors object on stderr and exit 1 (2 for a wrong command line).\n", a.Tool)
	return b.String()
}

// prefixedBar formats names as a bar-separated help suffix, or empty when none.
func prefixedBar(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "|" + strings.Join(names, "|") + "|<op>"
}

// sortStrings sorts s in place in ascending order.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// help answers `help [topic]`.
func (a *App) help(topic string) (string, *Error) {
	if topic == "" {
		return a.Usage(), nil
	}
	if c := a.command(topic); c != nil {
		return c.HelpText(a.Tool), nil
	}
	if f, ok := a.Topics[topic]; ok {
		return f(), nil
	}
	if a.Topic != nil {
		if text, ok := a.Topic(topic); ok {
			return text, nil
		}
	}
	return "", Errorf(CodeUsage, "", "no help for %q", topic).WithHint("run `" + a.Tool + " help`")
}

// Run is the binary: it parses the command line, runs the command and
// returns the exit code. On failure stdout is empty and stderr holds one
// errors object.
func (a *App) Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return WriteErrors(stderr, Errorf(CodeUsage, "", "no command").WithHint("run `"+a.Tool+" help`"))
	}
	text := func(s string, err *Error) int {
		if err != nil {
			return WriteErrors(stderr, err)
		}
		if _, werr := io.WriteString(stdout, s); werr != nil {
			return WriteErrors(stderr, Errorf(CodeIO, "", "write the help: %v", werr))
		}
		return ExitOK
	}
	switch args[0] {
	case "-h", "--help":
		return text(a.help(""))
	case "help":
		switch len(args) {
		case 1:
			return text(a.help(""))
		case 2:
			return text(a.help(args[1]))
		}
		return WriteErrors(stderr, Errorf(CodeUsage, "", "help takes one topic"))
	case "--version":
		b, err := marshal(struct {
			Tool     string `json:"tool"`
			Version  string `json:"version"`
			Contract int    `json:"contract"`
		}{a.Tool, a.Version, Contract})
		if err != nil {
			return WriteErrors(stderr, err)
		}
		return text(string(b)+"\n", nil)
	}
	c := a.command(args[0])
	if c == nil {
		return WriteErrors(stderr, Errorf(CodeUsage, "", "unknown command %q", args[0]).WithHint("run `"+a.Tool+" help`"))
	}
	p, perr := c.Parse(a.Tool, args[1:])
	if perr != nil {
		return WriteErrors(stderr, perr)
	}
	if p.Help {
		return text(c.HelpText(a.Tool), nil)
	}
	// The result is buffered so that a failure leaves stdout empty.
	var out strings.Builder
	if err := a.Handlers[c.Name](p, &out); err != nil {
		return WriteErrors(stderr, err)
	}
	if _, err := io.WriteString(stdout, out.String()); err != nil {
		return WriteErrors(stderr, Errorf(CodeIO, "", "write the result: %v", err))
	}
	return ExitOK
}
