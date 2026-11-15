//go:build !race

package htmlx

import "time"

// timeBudget is the per-input budget of TestLargeInputsAreLinear.
const timeBudget = 3 * time.Second
