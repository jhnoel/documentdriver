//go:build unix

package soffice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRunKillsChildOnTimeout: a command that starts a background child
// (as soffice starts soffice.bin) and then hangs is killed with its child
// when the timeout passes.
func TestRunKillsChildOnTimeout(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	_, err := Run(200*time.Millisecond, "", "/bin/sh", "-c", "sleep 30 & echo $! > "+pidFile+"; wait")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want a timeout", err)
	}
	b, err := os.ReadFile(pidFile) //nolint:gosec // G304: the test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d outlived the timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRunReapsChildOnSuccess: a child left behind by a command that
// exits is killed too.
func TestRunReapsChildOnSuccess(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	out, err := Run(5*time.Second, "", "/bin/sh", "-c", "sleep 30 >/dev/null 2>&1 & echo $! > "+pidFile+"; echo done")
	if err != nil || strings.TrimSpace(string(out)) != "done" {
		t.Fatalf("got %q, %v", out, err)
	}
	b, err := os.ReadFile(pidFile) //nolint:gosec // G304: the test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d outlived the command", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
