// Package memgate is the vsdx process-memory gate (chunk RSS1,
// docs/vsdx-design.md section 11, decision 9): it builds cmd/vsdx and
// runs read --page page0, read --page page0 --detail full and find 3
// times on Generated(20000) and 3 times on Generated(5000),
// interleaved, reading each child's peak RSS from
// ProcessState.SysUsage. The package is test-only: it may run
// processes, as tools/ may, and it may import the test helpers
// (vsdxtest, laws), as tools/ tests may.
package memgate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// bigShapes and smallShapes are the shape counts of the gate's two
// generated drawings; the large one carries the ceilings and the pair
// carries the 20k/5k ratio (as the RM1 gate in internal/vsdx/read
// measures peak live heap on the same pair).
const (
	bigShapes   = 20000
	smallShapes = 5000
)

// maxRatio is the largest allowed ratio of a command's peak RSS on
// the big drawing to its peak on the small one (the RM1 ratio).
const maxRatio = 5.5

// runsPerSize is how many times each command runs on each drawing:
// 3 on Generated(20000) and 3 on Generated(5000), interleaved, every
// run checked against the ceiling (no best-of, no retry; the ratio
// uses the largest of each size).
const runsPerSize = 3

// gateCase is one command of the gate: its name for logs, its
// arguments for a drawing file, and its peak-RSS ceiling as a
// function of the run's output size in bytes.
type gateCase struct {
	name    string
	args    func(file string) []string
	ceiling func(outBytes int) uint64
}

// gateCases returns the three commands of the RSS1 rows, with the
// ceilings of RM1 (internal/vsdx/read) doubled: read.Page default and
// --detail full at most twice 2 KB per object plus 3 times the
// output's bytes (at Generated(20000) the default ceiling is about
// 184 MB), and Find at most twice min(2 KB per object, 45 MB),
// which is 90 MB at Generated(20000). The ceiling is the command's
// ceiling at Generated(20000), applied to every run at both sizes,
// as the RM1/RM2 absolute ceilings are.
func gateCases() []gateCase {
	return []gateCase{
		{
			name: "read page0",
			args: func(file string) []string { return []string{"read", file, "--page", "page0"} },
			ceiling: func(outBytes int) uint64 {
				return 2 * memoryCeiling(objects(bigShapes), outBytes)
			},
		},
		{
			name: "read page0 full",
			args: func(file string) []string { return []string{"read", file, "--page", "page0", "--detail", "full"} },
			ceiling: func(outBytes int) uint64 {
				return 2 * memoryCeiling(objects(bigShapes), outBytes)
			},
		},
		{
			name: "find Box",
			args: func(file string) []string { return []string{"find", file, "Box", "--limit", "0"} },
			ceiling: func(int) uint64 {
				return 2 * findCeiling(objects(bigShapes))
			},
		},
	}
}

// objects returns the object count of a Generated(n) page: n shapes
// plus one connector for every even shape that has a next shape.
func objects(n int) int {
	return n + n/2
}

// memoryCeiling is the RM1 peak-live ceiling for a page read with
// objects objects and outBytes bytes of output: 2 KB per object plus
// 3 times the output (internal/vsdx/read memoryCeiling).
func memoryCeiling(objects, outBytes int) uint64 {
	return uint64(objects)*2048 + 3*uint64(outBytes)
}

// findCeiling is the RM1 peak-live ceiling for Find with objects
// objects: 2 KB per object capped at 45 MB (internal/vsdx/read).
func findCeiling(objects int) uint64 {
	return min(uint64(objects)*2048, laws.MB(45))
}

// rssRatio returns the ratio of the big run's peak RSS to the small
// run's, with the small peak floored at 1 byte as the RM1 peak ratio
// floors it.
func rssRatio(small, big uint64) float64 {
	return float64(big) / float64(max(small, 1))
}

// generated writes a Generated(n) drawing with the vsdxtest helper
// and returns its path.
func generated(t *testing.T, n int) string {
	t.Helper()
	return vsdxtest.Generated(t, n)
}

// buildVsdx builds cmd/vsdx into a fresh binary in a temporary
// directory and returns its path.
func buildVsdx(t *testing.T) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("memgate: repo root: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "vsdx")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/vsdx") //nolint:gosec // G204: go with the fixed argument list above
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("memgate: build cmd/vsdx: %v: %s", err, out)
	}
	return bin
}

// peakRSS returns the peak resident set size of a finished child in
// bytes, from ProcessState.SysUsage: getrusage maxrss is in bytes on
// darwin and in KiB on linux. It reports false when the platform has
// no rusage or the state carries none.
func peakRSS(state *os.ProcessState) (uint64, bool) {
	if state == nil {
		return 0, false
	}
	ru, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || ru == nil || ru.Maxrss < 0 {
		return 0, false
	}
	rss := uint64(ru.Maxrss) //nolint:gosec // G115: maxrss is non-negative here
	if runtime.GOOS == "linux" {
		rss *= 1024
	}
	return rss, true
}

// runRSS runs bin with args, returning the command's stdout size in
// bytes and its peak RSS in bytes.
func runRSS(t *testing.T, bin string, args ...string) (outBytes int, rss uint64) {
	t.Helper()
	cmd := exec.Command(bin, args...) //nolint:gosec // G204: the binary and arguments are built above, never from a document
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("memgate: run %v: %v: %s", append([]string{filepath.Base(bin)}, args...), err, stderr.String())
	}
	rss, ok := peakRSS(cmd.ProcessState)
	if !ok {
		t.Fatalf("memgate: run %v: no peak RSS on %s", append([]string{filepath.Base(bin)}, args...), runtime.GOOS)
	}
	return stdout.Len(), rss
}

// TestMemGate is the RSS1 gate: on darwin and linux (other platforms
// skip), each of the three commands runs 3 times on Generated(5000)
// and 3 times on Generated(20000), interleaved small, big, small,
// big, small, big. Every run must stay at or under the command's
// ceiling (twice the RM1 live-heap ceiling of the same command; no
// best-of, no retry), and the 20k/5k peak-RSS ratio of the largest
// run of each size is at most 5.5. Every run is logged with its peak
// RSS; a run within 15% of its ceiling is reported, not tuned. The
// measured peaks, ceilings and ratios are logged for the design
// record.
func TestMemGate(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("memgate: peak RSS needs getrusage (darwin, linux), not %s", runtime.GOOS)
	}
	bin := buildVsdx(t)
	files := map[int]string{smallShapes: generated(t, smallShapes), bigShapes: generated(t, bigShapes)}
	for _, tc := range gateCases() {
		var largestSmall, largestBig uint64
		for run := 1; run <= runsPerSize; run++ {
			out, rss := runRSS(t, bin, tc.args(files[smallShapes])...)
			ceiling := tc.ceiling(out)
			t.Logf("memgate: %s Generated(%d) run %d/%d: peak RSS %d bytes (%d MB), output %d bytes, ceiling %d bytes", tc.name, smallShapes, run, runsPerSize, rss, rss>>20, out, ceiling)
			if rss > ceiling {
				t.Errorf("%s: peak RSS Generated(%d) run %d/%d %d bytes, want at most %d (twice the RM1 ceiling)", tc.name, smallShapes, run, runsPerSize, rss, ceiling)
			}
			if rss > ceiling*85/100 {
				t.Logf("memgate: %s Generated(%d) run %d/%d peak RSS %d bytes within 15%% of ceiling %d bytes: reported, not tuned", tc.name, smallShapes, run, runsPerSize, rss, ceiling)
			}
			largestSmall = max(largestSmall, rss)
			out, rss = runRSS(t, bin, tc.args(files[bigShapes])...)
			ceiling = tc.ceiling(out)
			t.Logf("memgate: %s Generated(%d) run %d/%d: peak RSS %d bytes (%d MB), output %d bytes, ceiling %d bytes", tc.name, bigShapes, run, runsPerSize, rss, rss>>20, out, ceiling)
			if rss > ceiling {
				t.Errorf("%s: peak RSS Generated(%d) run %d/%d %d bytes, want at most %d (twice the RM1 ceiling)", tc.name, bigShapes, run, runsPerSize, rss, ceiling)
			}
			if rss > ceiling*85/100 {
				t.Logf("memgate: %s Generated(%d) run %d/%d peak RSS %d bytes within 15%% of ceiling %d bytes: reported, not tuned", tc.name, bigShapes, run, runsPerSize, rss, ceiling)
			}
			largestBig = max(largestBig, rss)
		}
		ratio := rssRatio(largestSmall, largestBig)
		t.Logf("%s: largest peak RSS Generated(%d) %d bytes, Generated(%d) %d bytes, ratio %.2f", tc.name, smallShapes, largestSmall, bigShapes, largestBig, ratio)
		if ratio > maxRatio {
			t.Errorf("%s: peak RSS ratio 20k/5k is %.2f, want at most %.1f", tc.name, ratio, maxRatio)
		}
	}
}

// TestGateMath checks the gate's ceiling and ratio arithmetic
// against the RM1 rows it doubles, on every platform.
func TestGateMath(t *testing.T) {
	if got, want := memoryCeiling(7500, 0), uint64(7500)*2048; got != want {
		t.Errorf("memoryCeiling(7500, 0) = %d, want %d", got, want)
	}
	if got, want := memoryCeiling(30000, 100), uint64(30000)*2048+300; got != want {
		t.Errorf("memoryCeiling(30000, 100) = %d, want %d", got, want)
	}
	if got, want := findCeiling(objects(bigShapes)), laws.MB(45); got != want {
		t.Errorf("findCeiling(objects(20000)) = %d, want %d (45 MB)", got, want)
	}
	if got, want := 2*findCeiling(objects(bigShapes)), 2*laws.MB(45); got != want {
		t.Errorf("twice the find ceiling = %d, want %d (90 MB)", got, want)
	}
	cases := gateCases()
	if len(cases) != 3 {
		t.Fatalf("gateCases has %d cases, want 3", len(cases))
	}
	// The default ceiling at Generated(20000) is 2 times the RM1
	// budget; with no output it is exactly twice 30000 objects of
	// 2 KB, and it grows by 6 bytes per output byte.
	def := cases[0].ceiling(0)
	if want := 2 * uint64(30000) * 2048; def != want {
		t.Errorf("default ceiling with no output = %d, want %d", def, want)
	}
	if got := cases[0].ceiling(100) - def; got != 600 {
		t.Errorf("default ceiling growth per 100 output bytes = %d, want 600", got)
	}
	if got := cases[2].ceiling(12345); got != 2*laws.MB(45) {
		t.Errorf("find ceiling = %d, want %d (90 MB)", got, 2*laws.MB(45))
	}
	if got, want := rssRatio(100, 550), 5.5; got != want {
		t.Errorf("rssRatio(100, 550) = %v, want %v", got, want)
	}
	if got := rssRatio(0, 100); got != 100 {
		t.Errorf("rssRatio(0, 100) = %v, want 100 (small peak floored at 1)", got)
	}
	if _, ok := peakRSS(nil); ok {
		t.Error("peakRSS(nil) reported a peak, want none")
	}
}
