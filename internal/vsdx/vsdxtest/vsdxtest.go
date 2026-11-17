// Package vsdxtest builds the hand-written .vsdx fixtures of the tests
// (V1 to V17, docs/vsdx-design.md, Verification) from XML committed
// under testdata/vsdx/<name>/, with opc.BuildZip, and generates large
// drawings for the scaling laws. It is imported only by tests.
package vsdxtest

import "testing"

// Fixture builds the fixture testdata/vsdx/<name> into a fresh file in
// t.TempDir and returns its path. Every member of the directory is a
// zip member at the same relative path; PROVENANCE.md is skipped.
// Chunk F1.
func Fixture(t testing.TB, name string) string {
	t.Helper()
	t.Skipf("vsdxtest.Fixture(%q): chunk F1 not landed", name)
	return ""
}

// Generated builds a drawing of one page with n rectangles, each an
// instance of one master with text and two shape data rows, every
// second pair joined by a glued dynamic connector, and returns its
// path. Chunk F1.
func Generated(t testing.TB, n int) string {
	t.Helper()
	t.Skipf("vsdxtest.Generated(%d): chunk F1 not landed", n)
	return ""
}
