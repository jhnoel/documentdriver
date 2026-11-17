package cli

import (
	"strconv"
	"time"

	"github.com/jhnoel/documentdriver/internal/ids"
)

// defaultClock is the fixed clock of deterministic mode when a hidden
// --seed or --clock is given without the other.
var defaultClock = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// SeedClock returns the deterministic id source the hidden --seed and
// --clock flags select, or nil when neither was given. A seed without a
// clock takes defaultClock, a clock without a seed takes seed 0; a bad
// --seed is usage at --seed and a bad --clock usage at --clock.
func SeedClock(p *Parsed) (ids.Source, error) {
	if !p.Has("seed") && !p.Has("clock") {
		return nil, nil
	}
	var seed int64
	clock := defaultClock
	if p.Has("seed") {
		n, err := strconv.ParseInt(p.Get("seed"), 10, 64)
		if err != nil {
			return nil, Errorf(CodeUsage, "--seed", "--seed is a whole number")
		}
		seed = n
	}
	if p.Has("clock") {
		c, err := time.Parse(time.RFC3339, p.Get("clock"))
		if err != nil {
			return nil, Errorf(CodeUsage, "--clock", "--clock is an RFC 3339 time")
		}
		clock = c
	}
	return ids.Fixed(seed, clock), nil
}
