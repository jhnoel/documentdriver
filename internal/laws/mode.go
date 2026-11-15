package laws

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Change modes, as a format's plans name them.
const (
	Track  = "track"
	Direct = "direct"
)

// ModeClass is how an operation's direct mode relates to its track mode.
type ModeClass uint8

const (
	// AcceptAuthored means direct equals track with the revisions the plan
	// authored accepted.
	AcceptAuthored ModeClass = iota
	// Same means the operation writes no revision; both modes give one result.
	Same
	// OnlyDirect means track mode refuses the operation.
	OnlyDirect
	// OwnPath means direct mode is its own path; only refusal parity and the
	// rejection law are checked.
	OwnPath
)

// ModeSuite is the change-mode law of one operation (or plan) as a table
// entry. Apply, Refused, Project and Settle are required; AcceptAuthored
// is required for the AcceptAuthored class.
type ModeSuite struct {
	// Name names the suite in failures.
	Name  string
	Class ModeClass
	// Apply applies the suite's plan to src in mode, writing dst.
	Apply func(src, dst, mode string) error
	// Refused reports whether err is the format's refusal of the plan
	// (rather than a failure of the engine).
	Refused func(err error) bool
	// Guarded reports whether a refusal in one mode only is one the
	// format documents for that mode (OpDef guards); nil: none.
	Guarded func(mode string, err error) bool
	// Project returns what the document at path means, as the format's
	// views show it: two documents that project the same are equal.
	Project func(path string) (string, error)
	// Settle accepts or rejects every revision of src, writing dst.
	Settle func(src, dst string, accept bool) error
	// AcceptAuthored accepts the revisions the suite's plan authored in
	// src (and no other), writing dst.
	AcceptAuthored func(src, dst string) error
}

// Check runs the change-mode laws on the document src (a private copy is
// used):
//
//   - refusal parity per mode: track and direct both apply or are both
//     refused, except a refusal Guarded documents for its mode, and
//     OnlyDirect, where track is always refused;
//   - AcceptAuthored: direct projects as track with its authored
//     revisions accepted;
//   - Same: track projects as direct;
//   - rejection: rejecting every revision of the tracked output projects
//     as rejecting every revision of the input (not as the input: the
//     input may hold revisions of its own), for every class with a
//     tracked form that writes revisions (AcceptAuthored and OwnPath).
func (s *ModeSuite) Check(t *testing.T, src string) {
	t.Helper()
	dir := t.TempDir()
	in := Private(t, src)
	track, direct := filepath.Join(dir, "track.docx"), filepath.Join(dir, "direct.docx")
	terr := s.Apply(in, track, Track)
	derr := s.Apply(in, direct, Direct)
	for _, e := range []error{terr, derr} {
		if e != nil && !s.Refused(e) {
			t.Errorf("%s: %s: %v", s.Name, filepath.Base(src), e)
			return
		}
	}
	if s.Class == OnlyDirect {
		if terr == nil {
			t.Errorf("%s: %s: track mode applied an operation only direct mode may", s.Name, filepath.Base(src))
		}
		return
	}
	switch {
	case terr != nil && derr != nil:
		return
	case terr != nil:
		if s.Guarded == nil || !s.Guarded(Track, terr) {
			t.Errorf("%s: %s: refused in track mode only: %v", s.Name, filepath.Base(src), terr)
		}
		return
	case derr != nil:
		if s.Guarded == nil || !s.Guarded(Direct, derr) {
			t.Errorf("%s: %s: refused in direct mode only: %v", s.Name, filepath.Base(src), derr)
		}
		return
	}
	same := func(law, a, b string) {
		pa, err := s.Project(a)
		if err != nil {
			t.Errorf("%s: %s: %s: %v", s.Name, filepath.Base(src), law, err)
			return
		}
		pb, err := s.Project(b)
		if err != nil {
			t.Errorf("%s: %s: %s: %v", s.Name, filepath.Base(src), law, err)
			return
		}
		if pa != pb {
			t.Errorf("%s: %s: %s: %s", s.Name, filepath.Base(src), law, firstDiff(pa, pb))
		}
	}
	switch s.Class {
	case Same:
		same("track equals direct", track, direct)
		return
	case AcceptAuthored:
		accepted := filepath.Join(dir, "accepted.docx")
		if err := s.AcceptAuthored(track, accepted); err != nil {
			t.Errorf("%s: %s: accepting the authored revisions: %v", s.Name, filepath.Base(src), err)
			return
		}
		same("direct equals accept(authored) of track", accepted, direct)
	}
	rt, ri := filepath.Join(dir, "rejected-track.docx"), filepath.Join(dir, "rejected-input.docx")
	if err := s.Settle(track, rt, false); err != nil {
		t.Errorf("%s: %s: rejecting the tracked output: %v", s.Name, filepath.Base(src), err)
		return
	}
	if err := s.Settle(in, ri, false); err != nil {
		t.Errorf("%s: %s: rejecting the input: %v", s.Name, filepath.Base(src), err)
		return
	}
	same("reject-all(track) equals reject-all(input)", rt, ri)
}

// firstDiff describes where two projections first differ.
func firstDiff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := max(0, i-60)
	return fmt.Sprintf("differ at %d: %q vs %q", i, a[lo:min(len(a), i+60)], b[lo:min(len(b), i+60)])
}

// CopyFile copies src to dst, for an Apply or Settle that changes nothing
// and writes no output of its own.
func CopyFile(src, dst string) error {
	b, err := os.ReadFile(src) //nolint:gosec // G304: the harness's own private copies
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600) //nolint:gosec // G703: the harness's own private copies
}
