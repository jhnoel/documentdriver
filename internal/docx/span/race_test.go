//go:build race

package span

// raceEnabled: the race detector is on (the corpus test is skipped: it
// runs single-threaded code twenty times slower and proves nothing more).
const raceEnabled = true
