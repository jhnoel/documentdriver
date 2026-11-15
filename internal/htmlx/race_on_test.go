//go:build race

package htmlx

import "time"

// timeBudget is the per-input budget of TestLargeInputsAreLinear; the race
// detector slows parsing about tenfold.
const timeBudget = 30 * time.Second
