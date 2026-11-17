package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseRegion parses a --region value as four comma-separated
// numbers, x,y,w,h, for the caller to wrap as a usage error on the
// flag.
func ParseRegion(s string) ([4]float64, error) {
	fields := strings.Split(s, ",")
	if len(fields) != 4 {
		return [4]float64{}, fmt.Errorf("--region is x,y,w,h")
	}
	var box [4]float64
	for i, f := range fields {
		v, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil {
			return [4]float64{}, fmt.Errorf("--region is four numbers: %q is not one", f)
		}
		box[i] = v
	}
	return box, nil
}
