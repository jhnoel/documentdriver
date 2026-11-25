package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestHelpFonts checks that `vsdx help fonts` carries the embedded
// fonts' notices and their full licence texts, so a copied binary
// carries them: Carlito's Reserved Font Name, the Liberation copyright,
// the OFL 1.1 conditions, and the Bitstream Vera and Arev licences.
func TestHelpFonts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help", "fonts"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help fonts: exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`Reserved Font Name "Carlito"`, "Copyright (c) 2012 Red Hat, Inc.",
		"SIL Open Font", "PERMISSION & CONDITIONS", "Bitstream Vera Fonts Copyright", "Arev Fonts Copyright",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help fonts lacks %q", want)
		}
	}
}
