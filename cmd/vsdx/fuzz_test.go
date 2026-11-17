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
// on stdout and one errors object on stderr. A read, find or xml
// command's file argument is replaced with the fixture drawing
// (written once, outside the fuzz target), so it never reads a real
// path (such as /dev/zero) or a path the fuzzer names. It never panics.
func FuzzCommandLine(f *testing.F) {
	for _, s := range []string{"--version", "help", "help\x00read", "help\x00html", "read\x00drawing.vsdx", "read\x00drawing.vsdx\x00--page\x00page0", "find\x00drawing.vsdx\x00Hello", "xml\x00drawing.vsdx\x00page0/s1", "xml\x00drawing.vsdx\x00page0", "read\x00--\x00--pretty"} {
		f.Add(s)
	}
	drawing := drawingFile(f)
	f.Fuzz(func(t *testing.T, line string) {
		var args []string
		if line != "" {
			args = strings.Split(line, "\x00")
		}
		if len(args) > 1 && (args[0] == "read" || args[0] == "find" || args[0] == "xml") {
			args = replaceFirstPositional(args, drawing)
		}
		var out, errb bytes.Buffer
		code := run(args, &out, &errb)
		switch code {
		case cli.ExitOK:
			if errb.Len() != 0 {
				t.Fatalf("%q: exit 0 with stderr %q", args, errb.String())
			}
			if len(args) > 0 && args[0] == "--version" && !json.Valid(out.Bytes()) {
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
// read, find or xml command line (args[0] is the command) with drawing.
// Flags are skipped as cli parses them: a flag with a value (page,
// master, detail, ids, region, units, limit) consumes the next argument
// unless it carries one with =, and -- ends flag parsing. An unknown
// flag ends the scan: the cli grammar refuses it before any file is read.
func replaceFirstPositional(args []string, drawing string) []string {
	onlyArgs := false
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case onlyArgs || a == "-" || !strings.HasPrefix(a, "-"):
			args[i] = drawing
			return args
		case a == "--":
			onlyArgs = true
		case a == "-h" || a == "--help":
			// Boolean: the cli parser reports help and reads no file.
			return args
		case !strings.HasPrefix(a, "--"):
			return args
		default:
			name, _, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
			switch name {
			case "page", "master", "detail", "ids", "region", "units", "limit":
				if !hasVal {
					i++
				}
			case "pretty", "ignore-case":
				// Boolean flags take no value.
			default:
				return args
			}
		}
	}
	return args
}
