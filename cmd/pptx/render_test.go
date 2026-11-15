package main

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/pptx/render"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// TestRenderCommand renders the minimal deck with every overlay and checks
// that the API box's pixels (known position and fill) are where the marks
// say, and that the overlay outline lands on the box edge.
func TestRenderCommand(t *testing.T) {
	if _, err := render.New(); errors.Is(err, render.ErrNoSoffice) || errors.Is(err, render.ErrNoRasteriser) {
		t.Skip(err)
	}
	deck := testdeck.Minimal(t)
	dir := t.TempDir()
	plain, over := filepath.Join(dir, "plain.png"), filepath.Join(dir, "over.png")
	var out, errb bytes.Buffer
	if code := run([]string{"render", deck, "--slide", "slide1", "-o", plain}, &out, &errb); code != 0 {
		t.Fatalf("render: exit %d: %s", code, errb.String())
	}
	if code := run([]string{"render", deck, "--slide", "slide1", "--overlay", "ids,sites,glue", "--scale", "1", "-o", over}, &out, &errb); code != 0 {
		t.Fatalf("render overlay: exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), `"size":[960,540]`) {
		t.Errorf("stdout = %q", out.String())
	}
	decode := func(p string) (w, h int, at func(x, y float64) (r, g, b uint32)) {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		im, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		bd := im.Bounds()
		return bd.Dx(), bd.Dy(), func(x, y float64) (uint32, uint32, uint32) {
			r, g, b, _ := im.At(int(x*float64(bd.Dx())/960), int(y*float64(bd.Dy())/540)).RGBA()
			return r >> 8, g >> 8, b >> 8
		}
	}
	_, _, at := decode(plain)
	if r, g, b := at(72+90, 144+45); r > 245 && g > 245 && b > 245 {
		t.Error("API box interior is background in the plain render")
	}
	if r, g, b := at(72-20, 144+45); r < 250 || g < 250 || b < 250 {
		t.Error("pixels left of the API box are not background")
	}
	_, _, at = decode(over)
	if r, g, b := at(72+40, 144); r != 230 || g != 0 || b != 200 {
		t.Errorf("overlay outline at the API box's top edge = %d,%d,%d", r, g, b)
	}
}

func TestRenderUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"render", "x.pptx"}, &out, &errb); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if code := run([]string{"render", "x.pptx", "--slide", "slide1", "--overlay", "bogus", "-o", "x.png"}, &out, &errb); code != 2 {
		t.Errorf("exit %d for a bad overlay, want 2", code)
	}
}
