package main

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/render/fonts"
)

// fontsHelp is `vsdx help fonts`: the fonts the binary embeds for
// render --renderer svg, their licences, and the licence texts
// themselves (from the embedded OFL.txt and LICENSE-DejaVu.txt), so
// the notices travel with the binary when it is copied on its own.
func fontsHelp() string {
	var b strings.Builder
	b.WriteString(`Fonts embedded in vsdx (render --renderer svg draws text with them).

  Carlito Regular, Bold, Italic, Bold Italic          SIL Open Font License 1.1
    Copyright (c) 2010-2013 by tyPoland Lukasz Dziedzic with Reserved
    Font Name "Carlito".
  Liberation Sans and Liberation Serif, each Regular,  SIL Open Font License 1.1
  Bold, Italic, Bold Italic (version 2.1.5)
    Digitized data copyright (c) 2010 Google Corporation.
    Copyright (c) 2012 Red Hat, Inc.
  DejaVu Sans (version 2.37)                           Bitstream Vera and Arev
    Fonts are (c) Bitstream; DejaVu changes are in the public domain;
    glyphs imported from Arev fonts are (c) Tavmjong Bah.

The fonts are unmodified copies of those LibreOffice 26.2 bundles; they
are not sold by themselves. The licence texts follow.

`)
	for _, name := range []string{"OFL.txt", "LICENSE-DejaVu.txt"} {
		text, err := fonts.Bytes(name)
		if err != nil {
			b.WriteString("(" + name + " is missing from this build: " + err.Error() + ")\n")
			continue
		}
		b.WriteString("==== " + name + " ====\n\n")
		b.Write(text)
		if len(text) > 0 && text[len(text)-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return b.String()
}
