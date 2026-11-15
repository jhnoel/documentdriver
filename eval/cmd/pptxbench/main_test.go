package main

import (
	"os"
	"testing"
)

// TestSelftest runs the grader self-test over every task. It needs the
// corpus (PPTX_CORPUS).
func TestSelftest(t *testing.T) {
	c := os.Getenv("PPTX_CORPUS")
	if c == "" {
		t.Skip("PPTX_CORPUS not set")
	}
	if err := cmdSelftest([]string{"-corpus", c, "-tasks", "../../tasks"}); err != nil {
		t.Fatal(err)
	}
}
