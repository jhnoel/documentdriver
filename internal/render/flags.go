package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// Result is the JSON result of one render, shared by the CLIs that
// rasterise through this package: the file written, its size in
// pixels, the page size in points as the read views format it
// (geom.FormatNum, as a json.Number), the scale, whether the page
// was hidden, and the key of numbered marks.
type Result struct {
	Saved  string         `json:"saved"`
	Px     [2]int         `json:"px"`
	Size   [2]json.Number `json:"size"`
	Scale  float64        `json:"scale"`
	Hidden bool           `json:"hidden"`
	Key    []KeyEntry     `json:"key"`
}

// MarshalJSON returns the JSON form of k as a render result's key
// shows it: n and id.
func (k KeyEntry) MarshalJSON() ([]byte, error) {
	return cli.Marshal(struct {
		N  int    `json:"n"`
		ID string `json:"id"`
	}{N: k.N, ID: k.ID})
}

// UnmarshalJSON reads the JSON form of a render result's key, n
// and id, into k.
func (k *KeyEntry) UnmarshalJSON(b []byte) error {
	var v struct {
		N  int    `json:"n"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*k = KeyEntry{N: v.N, ID: v.ID}
	return nil
}

// ParseOverlay parses a --overlay value: a comma-separated list of
// ids, sites or glue (an empty value selects nothing). It reports a
// word that is none of those, for the caller to wrap as a usage
// error on the flag.
func ParseOverlay(s string) (Overlay, error) {
	var overlay Overlay
	for word := range strings.SplitSeq(s, ",") {
		switch strings.TrimSpace(word) {
		case "":
		case "ids":
			overlay.IDs = true
		case "sites":
			overlay.Sites = true
		case "glue":
			overlay.Glue = true
		default:
			return overlay, fmt.Errorf("unknown overlay %q: ids, sites or glue", word)
		}
	}
	return overlay, nil
}

// ParseScale parses a --scale value as a positive number, for the
// caller to wrap as a usage error on the flag. The caller supplies
// DefaultScale when the flag is absent; an empty value is not a
// positive number. Infinity and NaN (which ParseFloat accepts) are
// not a positive number either.
func ParseScale(s string) (float64, error) {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, errors.New("--scale is a positive number")
	}
	return n, nil
}
