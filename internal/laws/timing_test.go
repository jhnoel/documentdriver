package laws

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestTimeVerdict checks the verdict logic directly: the bound
// MaxTimeRatio is inclusive, and a zero small time does not divide by zero.
func TestTimeVerdict(t *testing.T) {
	for _, tc := range []struct {
		best    [2]time.Duration
		fail    bool
		wantSub string
	}{
		{[2]time.Duration{time.Millisecond, 4 * time.Millisecond}, false, ""},
		{[2]time.Duration{time.Millisecond, 8 * time.Millisecond}, false, ""},
		{[2]time.Duration{time.Millisecond, 16 * time.Millisecond}, true, "16.00 times as long"},
		{[2]time.Duration{}, false, ""},
	} {
		got := timeVerdict("x", tc.best)
		if tc.fail && (!strings.Contains(got, tc.wantSub) || !strings.Contains(got, "super-linear")) {
			t.Errorf("best %v: want a failure naming %q and \"super-linear\", got %q", tc.best, tc.wantSub, got)
		}
		if !tc.fail && got != "" {
			t.Errorf("best %v: want no failure, got %q", tc.best, got)
		}
	}
}

// TestTimeScalesVerdictMode checks that a TimeScales case whose setup
// is quadratic in time passes without LAWS_TIME_VERDICT (logged) and
// fails with it, so the verdict lives only in make scaling.
func TestTimeScalesVerdictMode(t *testing.T) {
	if RaceEnabled {
		t.Skip("time ratios are not measured under -race (TimeScales)")
	}
	quad := func(n int) (func() error, error) {
		return func() error { sink += spin(n, true); return nil }, nil
	}
	TimeScales(t, "quadratic no verdict", quad)
	if got := os.Getenv(timeVerdictEnv); got != "" {
		t.Skipf("verdict mode already set (%s=%s); the failing half runs in the child", timeVerdictEnv, got)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestTimeScalesVerdictMode$", "-test.v") //nolint:gosec // G204: runs this test binary's own method, no caller input
	cmd.Env = append(os.Environ(), timeVerdictEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("quadratic TimeScales passed with %s=1, want a failure; output:\n%s", timeVerdictEnv, out)
	}
	for _, want := range []string{"quadratic no verdict", "super-linear", "at n=" + strconv.Itoa(Small), "at n=" + strconv.Itoa(Large) + " (setup's n)"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("child output lacks %q; child error %v; output:\n%s", want, err, out)
		}
	}
}

// spin does n*n (quadratic) or n (linear) steps of work that allocate
// nothing.
var sink int

// spin does n*n (quadratic) or n (linear) steps of work that allocate
// nothing.
func spin(n int, quadratic bool) int {
	s := 0
	m := n
	if quadratic {
		m = n * n
	}
	for i := range m {
		s += i ^ (s >> 3)
	}
	return s
}

// TestTimeRatio checks the measurement TimeScales relies on: interleaved
// best-of times give a ratio near 4 for linear work and well over the
// bound for quadratic work at the gate's sizes (scaled up so the runs
// last long enough to time).
func TestTimeRatio(t *testing.T) {
	if RaceEnabled {
		t.Skip("time ratios are not measured under -race (TimeScales)")
	}
	sink := 0
	mk := func(quadratic bool, scale int) [2]func() error {
		var r [2]func() error
		for i, n := range []int{Small, Large} {
			n := n * scale
			r[i] = func() error { sink += spin(n, quadratic); return nil }
		}
		return r
	}
	lin, _ := timeRatio(t, "linear", mk(false, 2000), TimeRounds)
	quad, _ := timeRatio(t, "quadratic", mk(true, 1), TimeRounds)
	t.Logf("linear %.2f, quadratic %.2f (sink %d)", lin, quad, sink)
	if lin > MaxTimeRatio {
		t.Errorf("linear work measured at ratio %.2f, over the bound %d", lin, MaxTimeRatio)
	}
	if quad < MaxTimeRatio {
		t.Errorf("quadratic work measured at ratio %.2f, under the bound %d: the check cannot fail", quad, MaxTimeRatio)
	}
}
