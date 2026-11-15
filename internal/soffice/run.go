// Package soffice runs LibreOffice (and the other helper binaries the
// tools call) so that nothing outlives the call: the command runs in its
// own process group, and the whole group is killed on timeout and after
// it exits (soffice starts a soffice.bin child that can stay behind
// otherwise). pptx render and xlsx --recalc share it.
package soffice

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Run executes name with args in dir ("" for the current directory),
// bounded by timeout, and returns its combined output. On timeout the
// error wraps context.DeadlineExceeded. Either way the process group is
// killed before Run returns.
func Run(timeout time.Duration, dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: the binaries are configured by the caller, never taken from a document
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	setProcessGroup(cmd)
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	// soffice may leave its child behind even on success; reap the group.
	killGroup(cmd)
	if ctx.Err() == context.DeadlineExceeded {
		return buf.Bytes(), fmt.Errorf("timed out after %s: %w", timeout, context.DeadlineExceeded)
	}
	return buf.Bytes(), err
}
