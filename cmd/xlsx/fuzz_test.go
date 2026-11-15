package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// FuzzCommandLine runs arbitrary command lines (NUL-separated arguments)
// and holds the contract's stream rules: exit 0 with one JSON document or
// help text on stdout and nothing on stderr, or exit 1 or 2 with nothing
// on stdout and one errors object on stderr. A read or find command's
// file argument is replaced with the fixture workbook (written once,
// outside the fuzz target), so it never reads a real path (such as
// /dev/zero) or a path the fuzzer names.
func FuzzCommandLine(f *testing.F) {
	for _, s := range []string{"--version", "help", "help\x00read", "read\x00book.xlsx", "read\x00book.xlsx\x00--sheet\x00sheet:Budget", "find\x00book.xlsx\x00Item", "apply\x00a\x00b\x00--changed=ids", "create\x00-o", "read\x00--\x00--pretty"} {
		f.Add(s)
	}
	book := bookFile(f)
	f.Fuzz(func(t *testing.T, line string) {
		var args []string
		if line != "" {
			args = strings.Split(line, "\x00")
		}
		if len(args) > 1 && (args[0] == "read" || args[0] == "find") {
			args = replaceFirstPositional(args, book)
		}
		var out, errb bytes.Buffer
		code := run(args, &out, &errb)
		switch code {
		case cli.ExitOK:
			if errb.Len() != 0 {
				t.Fatalf("%q: exit 0 with stderr %q", args, errb.String())
			}
			if args[0] == "--version" && !json.Valid(out.Bytes()) {
				t.Fatalf("%q: --version is not JSON: %q", args, out.String())
			}
		case cli.ExitFailed, cli.ExitUsage:
			if out.Len() != 0 {
				t.Fatalf("%q: exit %d with stdout %q", args, code, out.String())
			}
			var v struct {
				Errors []cli.Error `json:"errors"`
			}
			dec := json.NewDecoder(&errb)
			if err := dec.Decode(&v); err != nil || len(v.Errors) == 0 || dec.More() {
				t.Fatalf("%q: stderr is not one errors object (%v): %q", args, err, errb.String())
			}
		default:
			t.Fatalf("%q: exit %d", args, code)
		}
	})
}

// replaceFirstPositional replaces the first positional argument of a
// read or find command line (args[0] is the command) with book. Flags
// are skipped as cli parses them: a flag with a value (sheet, range,
// view, limit, kind, detail, out) consumes the next argument unless it
// carries one with =, and -- ends flag parsing. An unknown flag ends
// the scan: the cli grammar refuses it before any file is read.
func replaceFirstPositional(args []string, book string) []string {
	onlyArgs := false
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case onlyArgs || a == "-" || !strings.HasPrefix(a, "-"):
			args[i] = book
			return args
		case a == "--":
			onlyArgs = true
		case a == "-o":
			i++ // -o takes a value
		case a == "-h" || a == "--help":
			// Boolean: the cli parser reports help and reads no file.
			return args
		case !strings.HasPrefix(a, "--"):
			return args
		default:
			name, _, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
			switch name {
			case "sheet", "range", "view", "limit", "kind", "detail", "out":
				if !hasVal {
					i++
				}
			case "pretty", "group", "ignore-case":
				// Boolean flags take no value.
			default:
				return args
			}
		}
	}
	return args
}
