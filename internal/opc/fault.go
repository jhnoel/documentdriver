package opc

import (
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// Save stages, in order. A fault hook sees each one; StageWrite is seen
// once per write to the temporary file. Failures injected before
// StageDirSync leave the original file untouched; StageDirSync runs after
// the rename and cannot fail a save.
const (
	StageCreate  = "create"  // before the temporary file is created
	StageWrite   = "write"   // before each write to the temporary file
	StageChmod   = "chmod"   // before the temporary file's mode is set
	StageSync    = "sync"    // before the temporary file is synced
	StageClose   = "close"   // before the temporary file is closed
	StageRename  = "rename"  // before the temporary file replaces the target
	StageDirSync = "dirsync" // after the rename, before the directory sync
)

// faultHook, when set, is called at every save stage with the stage and
// the temporary file's name (the target's for StageDirSync, "" before
// creation). A non-nil error fails the stage. faultFor holds the hooks
// SetFaultHookFor scoped to one target. Both are guarded by faultMu, and
// faultHooks counts what is installed so that saves skip the lock when
// nothing is.
var (
	faultMu    sync.RWMutex
	faultHook  func(stage, file string) error
	faultFor   = map[string]func(stage, file string) error{}
	faultHooks atomic.Int32
)

// SetFaultHook installs fn as the save fault hook and returns a function
// that removes it. It exists for tests of atomicity, in this package and
// in internal/pptx/ops; production code never calls it (the architecture test
// enforces that).
func SetFaultHook(fn func(stage, file string) error) (restore func()) {
	faultMu.Lock()
	defer faultMu.Unlock()
	prev := faultHook
	faultHook = fn
	faultHooks.Add(1)
	return func() {
		faultMu.Lock()
		defer faultMu.Unlock()
		faultHook = prev
		faultHooks.Add(-1)
	}
}

// SetFaultHookFor is SetFaultHook for the saves of one document only:
// saves whose target is dst, and saves of a package opened from dst to
// another file (a plan's snapshots), which a process-wide hook sees as
// well. Tests saving other documents at the same time (parallel corpus
// runs) neither see the fault nor feed the hook. A scoped hook runs
// before the global one.
func SetFaultHookFor(dst string, fn func(stage, file string) error) (restore func()) {
	key := filepath.Clean(dst)
	faultMu.Lock()
	defer faultMu.Unlock()
	prev, had := faultFor[key]
	faultFor[key] = fn
	faultHooks.Add(1)
	return func() {
		faultMu.Lock()
		defer faultMu.Unlock()
		if had {
			faultFor[key] = prev
		} else {
			delete(faultFor, key)
		}
		faultHooks.Add(-1)
	}
}

// faultScope is what a save is of: its target and the path of the
// package it writes ("" for one opened from memory).
type faultScope struct{ dst, src string }

// fault runs the hooks for a save stage.
func fault(at faultScope, stage, file string) error {
	if faultHooks.Load() == 0 {
		return nil
	}
	faultMu.RLock()
	scoped, global := faultFor[filepath.Clean(at.dst)], faultHook
	if scoped == nil && at.src != "" {
		scoped = faultFor[filepath.Clean(at.src)]
	}
	faultMu.RUnlock()
	if scoped != nil {
		if err := scoped(stage, file); err != nil {
			return err
		}
	}
	if global != nil {
		return global(stage, file)
	}
	return nil
}

// faultWriter passes each write of a save through the fault hook.
type faultWriter struct {
	w  io.Writer
	at faultScope
}

// Write runs the write fault hook, then writes b to the underlying writer.
func (f faultWriter) Write(b []byte) (int, error) {
	if err := fault(f.at, StageWrite, ""); err != nil {
		return 0, err
	}
	return f.w.Write(b)
}
