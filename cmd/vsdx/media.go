package main

import (
	"io"
	"os"
	"path/filepath"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
)

var mediaCommand = &cli.Command{
	Name:    "media",
	Args:    []string{"drawing.vsdx", "address"},
	JSON:    true,
	Summary: "write the bytes of one image to a file",
	Flags:   []cli.Flag{cli.RequiredOutFlag()},
	Help: `Writes the bytes of one image to the file named by -o (required) and
prints {"base", "saved", "image", "type", "bytes", "sha256", "px"}.
address is a picture (page0/s5) or a media part as read shows it in
image (/visio/media/image1.png); anything else is not_found. It is the
only way vsdx gives out image bytes: read never inlines them. It
writes only -o (docs/contract.md, sections 1 and 5).`,
}

// runMedia is the media handler: the part is streamed into a
// temporary file next to -o, renamed into place once complete, so a
// failed extraction leaves no partial file.
func runMedia(p *cli.Parsed, stdout io.Writer) error {
	out := p.Get("out")
	if out == "" {
		return cli.Errorf(cli.CodeUsage, "-o", "media needs -o, the file to write")
	}
	input, err := readInput(p.Args[0])
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".vsdx-media-*")
	if err != nil {
		return cli.Errorf(cli.CodeIO, "-o", "write the image: %v", err)
	}
	defer os.Remove(tmp.Name())
	res, err := read.Media(input, cli.Hash(input), p.Args[1], tmp)
	if cerr := tmp.Close(); err == nil && cerr != nil {
		err = cli.Errorf(cli.CodeIO, "-o", "write the image: %v", cerr)
	}
	if err != nil {
		return cli.Wrap(cli.CodeInvalidDocument, "", err)
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return cli.Errorf(cli.CodeIO, "-o", "write the image: %v", err)
	}
	res.Saved = out
	j, err := cli.Marshal(res)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, j, p.Has("pretty"))
}
