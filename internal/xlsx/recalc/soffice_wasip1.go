//go:build wasip1

package recalc

// Compute is unavailable on wasip1: there are no processes.
func Compute([]byte, Options) ([]Value, error) { return nil, ErrUnavailable }
