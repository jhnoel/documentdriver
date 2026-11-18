package ops

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// fakeInfo is a regular-file FileInfo of a given size.
type fakeInfo struct{ size int64 }

func (f fakeInfo) Name() string       { return "image1.png" }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() fs.FileMode  { return 0o644 }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any           { return nil }

// TestPictureBothPartAndFile checks the refusal of an image value that
// names both a media part of the drawing and an existing file: the
// file system is faked so /visio/media/image1.png exists as a file,
// and the drawing has that part.
func TestPictureBothPartAndFile(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	statImage = func(string) (os.FileInfo, error) { return fakeInfo{int64(b.Len())}, nil }
	readImage = func(string) ([]byte, error) { return b.Bytes(), nil }
	t.Cleanup(func() { statImage, readImage = os.Stat, os.ReadFile })
	in, _, err := loadImage("/visio/media/image1.png")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := edit.Begin(vsdxtest.Blank(t))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Tx().Rollback()
	if err := ws.AddPart("/visio/media/image1.png", b.Bytes(), "image/png"); err != nil {
		t.Fatal(err)
	}
	_, err = resolveImage(ws, in)
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeInvalidOperation || ce.Path != "image" {
		t.Errorf("both part and file: err %v, want invalid_operation at image", err)
	}
}
