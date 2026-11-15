package main

import (
	"io"
	"os"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/read"
)

var views = []string{"markup", "final", "original"}

var readCommand = &cli.Command{
	Name:    "read",
	Args:    []string{"file.docx"},
	JSON:    true,
	Summary: "show the document, or one kind of thing in it",
	Flags: []cli.Flag{
		{Name: "kind", Value: "kind", Enum: read.Kinds, Help: "what to list (default document)"},
		{Name: "view", Value: "view", Enum: views, Help: "the text with tracked changes shown (markup, the default), accepted (final) or rejected (original)"},
	},
	Help: `Prints the document as JSON: markup (HTML, with each paragraph's address
in its id), and the tables, sections, styles, equations and comments the
markup refers to. --kind lists one kind of thing instead: comments,
revisions, bookmarks, styles or assets. Every result starts with base,
the file's hash, and has notes: the parts that could not be read. Read
never writes.`,
}

var findCommand = &cli.Command{
	Name:    "find",
	Args:    []string{"file.docx", "query"},
	JSON:    true,
	Summary: "list the paragraphs whose text contains the query",
	Flags: []cli.Flag{
		{Name: "ignore-case", Help: "match letters of either case"},
		{Name: "view", Value: "view", Enum: views, Help: "the text to search (default markup)"},
	},
	Help: `Prints {"base", "matches", "notes"}: one entry per paragraph whose text
contains the query, with its address (id), its text in the view and its
selectable text (what select matches in a plan). The empty query matches
every paragraph.`,
}

// readFile reads the document at the path the caller gave.
func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the document is the file the caller named
	if err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "read the document: %v", err)
	}
	return b, nil
}

// writeResult encodes a read result as JSON and writes it to stdout.
func writeResult(stdout io.Writer, res read.Result, pretty bool) error {
	out, err := read.EncodeResult(res, pretty)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(stdout, out+"\n"); err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
	}
	return nil
}

// runRead reads the named document and writes its JSON result.
func runRead(p *cli.Parsed, stdout io.Writer, opt read.Options) error {
	input, err := readFile(p.Args[0])
	if err != nil {
		return err
	}
	kind := "document"
	if p.Has("kind") {
		kind = p.Get("kind")
	}
	if p.Has("view") && kind != "document" {
		return cli.Errorf(cli.CodeUsage, "--view", "--view goes with --kind document")
	}
	res, err := read.Read(input, kind, p.Get("view"), opt)
	if err != nil {
		return err
	}
	return writeResult(stdout, res, p.Has("pretty"))
}

// runFind searches the named document for paragraphs containing the query and writes JSON.
func runFind(p *cli.Parsed, stdout io.Writer, opt read.Options) error {
	input, err := readFile(p.Args[0])
	if err != nil {
		return err
	}
	view := "markup"
	if p.Has("view") {
		view = p.Get("view")
	}
	res, err := read.Find(input, p.Args[1], p.Has("ignore-case"), view, opt)
	if err != nil {
		return err
	}
	return writeResult(stdout, res, p.Has("pretty"))
}
