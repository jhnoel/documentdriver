package officeapp

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// DefaultWorkdir is the folder the app has been granted: the one
// PowerPoint uses, <repo>/corpus/pptoracle, or $PPTORACLE_WORKDIR.
func DefaultWorkdir() string {
	if d := os.Getenv("PPTORACLE_WORKDIR"); d != "" {
		return d
	}
	c, err := repo.Corpus()
	if err != nil {
		return ""
	}
	return filepath.Join(c, "pptoracle")
}

// CopyFile copies src to a new dst.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("cannot write in the work folder: %w", err)
		}
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
