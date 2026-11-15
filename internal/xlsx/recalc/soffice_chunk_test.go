//go:build !wasip1 && !windows

package recalc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeChunkFailures(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		timeout      time.Duration
		want         error
	}{
		{"exit", "exit 3", time.Second, nil},
		{"no output", "exit 0", time.Second, nil},
		{"timeout", "exec sleep 10", 50 * time.Millisecond, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "soffice")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			_, err := Compute(nil, Options{Soffice: bin, Timeout: tc.timeout})
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("Compute error = %v, want %v", err, tc.want)
			}
		})
	}
}
