package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

// runDir is one run's set of files in the work folder. PowerPoint's
// folder grant may cover only the folder the user selected, so the run
// never creates subfolders there: every file PowerPoint opens or writes is
// a flat <run-id>--<name> file directly in the work folder. Close removes
// them, and the ~$ lock files PowerPoint leaves beside them.
type runDir struct {
	dir string
	id  string
}

// newRunDir starts a run in workdir, which must already exist (it is the
// folder granted to PowerPoint; creating it would not grant it).
func newRunDir(workdir string) (*runDir, error) {
	if workdir == "" {
		return nil, fmt.Errorf("work folder: not inside the documentdriver repository, so <repo>/corpus/pptoracle cannot be located (run from the repo, or set PPTORACLE_WORKDIR or pass --workdir)")
	}
	st, err := os.Stat(workdir)
	if err != nil {
		return nil, fmt.Errorf("work folder: %v (create it and grant PowerPoint access to it once, or pass --workdir)", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("work folder %s is not a directory", workdir)
	}
	id := time.Now().Format("20060102-150405") + "-" + strconv.Itoa(os.Getpid())
	r := &runDir{dir: workdir, id: id}
	probe := r.file("probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, fmt.Errorf("cannot write in %s: %v (pass --workdir a folder this process can write and PowerPoint has been granted)", workdir, err)
		}
		return nil, err
	}
	os.Remove(probe)
	return r, nil
}

// file returns the flat path <workdir>/<run-id>--<name> for this run.
func (r *runDir) file(name string) string {
	return filepath.Join(r.dir, r.id+"--"+name)
}

// Close removes this run's files and PowerPoint's lock files for them.
func (r *runDir) Close() error {
	if r == nil {
		return nil
	}
	var first error
	for _, pat := range []string{r.id + "--*", "~$" + r.id + "--*"} {
		m, _ := filepath.Glob(filepath.Join(r.dir, pat))
		for _, f := range m {
			if err := os.Remove(f); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// moveFile renames src to dst, copying across file systems.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	_ = os.Remove(dst)
	if err := officeapp.CopyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}
