package officeapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Running uses pgrep -x on the exact process name, which never matches
// pgrep itself.
func (a *MacApp) Running() (bool, error) {
	err := exec.Command("pgrep", "-x", a.spec.ProcName).Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("pgrep: %w", err)
}

// Open launches the app on path without restoring windows.
func (a *MacApp) Open(path string) error {
	a.mu.Lock()
	a.launched = true
	a.mu.Unlock()
	out, err := exec.Command("open", "-F", "-a", a.spec.AppName, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("open: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// Run runs AppleScript lines with a timeout in seconds.
func (a *MacApp) Run(lines []string, seconds int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds+10)*time.Second)
	defer cancel()
	var args []string
	for _, l := range WithTimeout(seconds, lines...) {
		args = append(args, "-e", l)
	}
	cmd := exec.CommandContext(ctx, "osascript", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", &OSAError{Num: ErrAETimeout, Msg: "osascript did not return"}
	}
	if err != nil {
		return "", ParseOSAError(stderr.String())
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// ForceQuit sends SIGKILL to the exact process name, waits up to 15
// seconds for it to go, then the spec's quit settle more.
func (a *MacApp) ForceQuit() error {
	if err := exec.Command("pkill", "-9", "-x", a.spec.ProcName).Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 { // 1: nothing to kill
			return fmt.Errorf("pkill: %w", err)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		up, err := a.Running()
		if err != nil {
			return err
		}
		if !up {
			// The process is gone; give LaunchServices a moment to
			// register it before the app is launched again.
			time.Sleep(a.spec.QuitSettle)
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New(lowerFirst(a.spec.AppName) + " is still running 15s after SIGKILL")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// WasLaunched reports whether the app was launched by Open.
func (a *MacApp) WasLaunched() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.launched
}

// lowerFirst returns s with its first letter lowercased.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
