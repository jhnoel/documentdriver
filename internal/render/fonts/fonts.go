// Package fonts embeds the outline fonts the Go renderer draws text
// with, unmodified, under their licences (OFL.txt, LICENSE-DejaVu.txt;
// provenance in PROVENANCE.md): Carlito (metric-compatible with
// Calibri), Liberation Sans (Arial) and Liberation Serif (Times New
// Roman), each regular, bold, italic and bold italic, and DejaVu Sans,
// the fallback for characters those lack.
package fonts

import "embed"

//go:embed *.ttf OFL.txt LICENSE-DejaVu.txt
var files embed.FS

// Bytes returns the bytes of the embedded font file name
// (LiberationSans-Bold.ttf).
func Bytes(name string) ([]byte, error) {
	return files.ReadFile(name)
}
