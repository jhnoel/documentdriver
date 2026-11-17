package laws

import "testing"

// TestVerdict checks the scaling verdict, the absolute floor included:
// near-zero noise passes, linear work passes, and quadratic work fails
// whether its small run is large or tiny, as long as its large run is
// over the floor.
func TestVerdict(t *testing.T) {
	for _, c := range []struct {
		name         string
		small, large Cost
		fails        int
	}{
		// The -race flake: no allocation against one, a ratio of 16.
		{"noise", Cost{Bytes: 0, Mallocs: 0}, Cost{Bytes: 16, Mallocs: 1}, 0},
		{"zero", Cost{}, Cost{}, 0},
		{"at floor", Cost{Bytes: 1, Mallocs: 1}, Cost{Bytes: FloorBytes, Mallocs: FloorMallocs}, 0},
		{"linear", Cost{Bytes: 1 << 20, Mallocs: 10_000}, Cost{Bytes: 4 << 20, Mallocs: 40_000}, 0},
		{"quadratic", Cost{Bytes: 1 << 20, Mallocs: 10_000}, Cost{Bytes: 16 << 20, Mallocs: 160_000}, 2},
		// Quadratic from a tiny small run: both large measures pass
		// their floor, so the ratio of 16 still fails them.
		{"quadratic tiny", Cost{Bytes: 8 << 10, Mallocs: 10}, Cost{Bytes: 128 << 10, Mallocs: 160}, 2},
		// Just over the floor from nothing: judged by the ratio.
		{"over floor from zero", Cost{}, Cost{Bytes: FloorBytes + 1, Mallocs: FloorMallocs + 1}, 2},
		{"quadratic bytes only", Cost{Bytes: 1 << 20, Mallocs: 10}, Cost{Bytes: 16 << 20, Mallocs: 40}, 1},
	} {
		if got := verdict(c.name, c.small, c.large); len(got) != c.fails {
			t.Errorf("%s: %d failures %q, want %d", c.name, len(got), got, c.fails)
		}
	}
}
