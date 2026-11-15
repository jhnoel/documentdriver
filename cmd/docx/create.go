package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/create"
)

var createCommand = &cli.Command{
	Name:    "create",
	Args:    []string{"doc.html|-"},
	JSON:    true,
	Summary: "make a new document from HTML",
	Flags: []cli.Flag{
		cli.OutFlag(),
		{Name: "dry-run", Help: "check the HTML and write nothing"},
	},
	Help: `Makes a new Word document whose body is the HTML (a file, or - for
stdin) and writes it to the file named by -o, which is required unless
--dry-run. The HTML is the dialect docx read shows (docx help html). The
result names the file and its hash: {"dry_run", "saved", "new_base"};
use new_base as the base of a plan on the new document.

` + create.Scope,
}

// runCreate builds a new document from HTML and writes the JSON result.
func runCreate(p *cli.Parsed, stdout io.Writer) error {
	out, dry := p.Get("out"), p.Has("dry-run")
	if out == "" && !dry {
		return cli.Errorf(cli.CodeUsage, "--out", "create needs -o, the document to write")
	}
	var html []byte
	var err error
	if p.Args[0] == "-" {
		html, err = io.ReadAll(os.Stdin)
	} else {
		html, err = os.ReadFile(p.Args[0]) //nolint:gosec // G304: the HTML is the file the caller named
	}
	if err != nil {
		return cli.Errorf(cli.CodeIO, "", "read the HTML: %v", err)
	}
	doc, err := create.Build(string(html))
	if err != nil {
		var refused *create.Error
		if errors.As(err, &refused) {
			return &cli.Error{Code: refused.Code, Path: refused.Path, Message: refused.Msg, Cause: err}
		}
		return err
	}
	result := struct {
		DryRun  bool   `json:"dry_run"`
		Saved   string `json:"saved,omitempty"`
		NewBase string `json:"new_base,omitempty"`
	}{DryRun: dry}
	if !dry {
		if err := writeAtomic(out, doc); err != nil {
			return cli.Errorf(cli.CodeIO, "--out", "write the document: %v", err)
		}
		result.Saved, result.NewBase = out, cli.Hash(doc)
	}
	b, err := cli.Marshal(result)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, b, p.Has("pretty"))
}

// writeAtomic writes data to path through a temporary file beside it, so
// that the file is either the whole document or untouched.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".docx-create-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		return errors.Join(err, os.Remove(name))
	}
	if err := os.Chmod(name, 0o644); err != nil { //nolint:gosec // G302: a document for the caller, as any tool would write it
		return errors.Join(err, os.Remove(name))
	}
	if err := os.Rename(name, path); err != nil {
		return errors.Join(err, os.Remove(name))
	}
	return nil
}
