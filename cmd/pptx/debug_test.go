package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

func TestDebugRoundTrip(t *testing.T) {
	decks := testdeck.All(t)
	var out, errb bytes.Buffer
	args := []string{"debug", "roundtrip", decks["minimal"], decks["quirky"], decks["soffice"]}
	if code := run(args, &out, &errb); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out.String(), errb.String())
	}
	if strings.Contains(out.String(), "DIFFERS") || strings.Count(out.String(), " 0 differ") != 3 {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestDebugSetAttr(t *testing.T) {
	src := testdeck.Minimal(t)
	before, _ := os.ReadFile(src)
	dst := filepath.Join(t.TempDir(), "out.pptx")
	var out, errb bytes.Buffer
	code := run([]string{"debug", "set-attr", src, "/ppt/slides/slide1.xml", "//p:cNvPr[@id='3']", "name", "Store", "-o", dst}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if after, _ := os.ReadFile(src); !bytes.Equal(before, after) {
		t.Fatal("input modified")
	}
	p, err := opc.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	pt, _ := p.Part("/ppt/slides/slide1.xml")
	b, _ := pt.Bytes()
	if !bytes.Contains(b, []byte(`<p:cNvPr id="3" name="Store"/>`)) {
		t.Fatal("attribute not set")
	}

	// A selector that matches nothing fails and writes nothing.
	errb.Reset()
	if code := run([]string{"debug", "set-attr", src, "/ppt/slides/slide1.xml", "//p:nope", "x", "1"}, &out, &errb); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if after, _ := os.ReadFile(src); !bytes.Equal(before, after) {
		t.Fatal("failed edit modified the input")
	}
}
