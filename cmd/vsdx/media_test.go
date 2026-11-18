package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestMediaCommand checks the media rows: -o is required (usage), the
// picture's bytes land in -o with the result's sha256, and a
// not_found address writes no file.
func TestMediaCommand(t *testing.T) {
	src := vsdxtest.Fixture(t, "v18-jpeg")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"media", src, "page0/s5"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"path":"-o"`) {
		t.Fatalf("no -o: exit %d %s, want usage at -o", code, stderr.String())
	}
	out := filepath.Join(t.TempDir(), "x.jpeg")
	code, res, _ := runCLI(t, "media", src, "page0/s5", "-o", out)
	if code != 0 {
		t.Fatalf("media: exit %d", code)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if !strings.Contains(res, `"sha256":"`+hex.EncodeToString(sum[:])+`"`) || !strings.Contains(res, `"image":"/visio/media/image3.jpeg"`) {
		t.Errorf("result %s does not match the written bytes", res)
	}
	missing := filepath.Join(t.TempDir(), "y.bin")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"media", src, "page0/s9", "-o", missing}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "not_found") {
		t.Errorf("page0/s9: exit %d %s, want not_found", code, stderr.String())
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("a not_found media wrote its -o")
	}
}
