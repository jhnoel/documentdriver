package main

import (
	"bytes"
	"fmt"
	"io"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/ops"
)

const debugUsage = `usage: pptx debug <command> [args]

Hidden commands for exercising the engine by hand.

  roundtrip deck.pptx...
      Parse every XML part and report any whose serialisation differs from
      the stored bytes. Exits 1 if any part differs. Parts that are not well
      formed are listed but are not differences: they are never edited.

  set-attr deck.pptx <part> <selector> <attr> <value> [-o out.pptx]
      Set an attribute on every element the selector matches, in one
      transaction. Without -o the deck is edited in place.
      Selector: /p:sld/p:cSld/p:spTree/p:sp[2]/p:spPr/a:xfrm/a:off,
      //p:cNvPr[@id='4'], //a:off[1] (prefixes as declared on the root).
`

// runDebug dispatches a hidden debug command and returns its exit code.
func runDebug(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, debugUsage)
		return 2
	}
	switch args[0] {
	case "roundtrip":
		return debugRoundTrip(args[1:], stdout, stderr)
	case "set-attr":
		return debugSetAttr(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, debugUsage)
		return 0
	}
	fmt.Fprintf(stderr, "pptx debug: unknown command %q\n%s", args[0], debugUsage)
	return 2
}

// debugRoundTrip reports XML parts whose reserialisation differs from their stored bytes.
func debugRoundTrip(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, debugUsage)
		return 2
	}
	status := 0
	for _, path := range args {
		p, err := opc.Open(path)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			status = 1
			continue
		}
		var parts, same, malformed, differ int
		for _, name := range p.Names() {
			pt, err := p.Part(name)
			if err != nil || !p.IsXML(name) {
				continue
			}
			parts++
			orig, err := pt.Original()
			if err != nil {
				fmt.Fprintf(stdout, "%s: %s: unreadable: %v\n", path, name, err)
				malformed++
				continue
			}
			doc, err := pt.Tree()
			if err != nil {
				fmt.Fprintf(stdout, "%s: %s: not well formed (would be copied untouched): %v\n", path, name, err)
				malformed++
				continue
			}
			got := doc.Bytes()
			if bytes.Equal(got, orig) {
				same++
				continue
			}
			differ++
			fmt.Fprintf(stdout, "%s: %s: DIFFERS at byte %d\n", path, name, firstDiff(got, orig))
		}
		p.Close()
		fmt.Fprintf(stdout, "%s: %d XML parts, %d identical, %d differ, %d not parsed\n", path, parts, same, differ, malformed)
		if differ > 0 {
			status = 1
		}
	}
	return status
}

// firstDiff returns the index of the first byte where two slices differ.
func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// debugSetAttr sets one attribute on every element a selector matches in a deck.
func debugSetAttr(args []string, stdout, stderr io.Writer) int {
	var out string
	var pos []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			out = args[i+1]
			i++
			continue
		}
		pos = append(pos, args[i])
	}
	if len(pos) != 5 {
		fmt.Fprint(stderr, debugUsage)
		return 2
	}
	deck := pos[0]
	op := &ops.DebugSetAttr{Part: pos[1], Selector: pos[2], Attr: pos[3], Value: pos[4]}
	var err error
	if out == "" {
		err = ops.Apply(deck, op)
	} else {
		err = ops.ApplyTo(deck, out, op)
	}
	if err != nil {
		fmt.Fprintf(stderr, "pptx debug set-attr: %v (nothing written)\n", err)
		return 1
	}
	dst := deck
	if out != "" {
		dst = out
	}
	fmt.Fprintf(stdout, "set %s on %d element(s) in %s; wrote %s\n", op.Attr, op.Matched, op.Part, dst)
	return 0
}
