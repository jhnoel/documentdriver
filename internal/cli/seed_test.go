package cli

import (
	"errors"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/ids"
)

// TestSeedClock checks the hidden deterministic flags: neither means nil,
// a seed or a clock selects ids.Fixed with the other defaulting, and a
// bad value is usage at its flag.
func TestSeedClock(t *testing.T) {
	parsed := func(values map[string]string) *Parsed { return &Parsed{values: values} }
	if got, err := SeedClock(parsed(nil)); got != nil || err != nil {
		t.Errorf("no flags: got %v, %v; want nil, nil", got, err)
	}
	clock, err := time.Parse(time.RFC3339, "2026-01-02T03:04:05Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		values map[string]string
		want   ids.Source
	}{
		{map[string]string{"seed": "5"}, ids.Fixed(5, defaultClock)},
		{map[string]string{"seed": "0"}, ids.Fixed(0, defaultClock)},
		{map[string]string{"seed": "-3"}, ids.Fixed(-3, defaultClock)},
		{map[string]string{"clock": "2026-01-02T03:04:05Z"}, ids.Fixed(0, clock)},
		{map[string]string{"seed": "7", "clock": "2026-01-02T03:04:05Z"}, ids.Fixed(7, clock)},
	} {
		got, err := SeedClock(parsed(c.values))
		if err != nil {
			t.Errorf("%v: error %v", c.values, err)
			continue
		}
		if got == nil || !got.Now().Equal(c.want.Now()) || got.GUID() != c.want.GUID() {
			t.Errorf("%v: got clock %v guid (want %v); want clock %v guid", c.values, got, c.want, c.want)
		}
	}
	for _, c := range []struct {
		values map[string]string
		path   string
	}{
		{map[string]string{"seed": "bogus"}, "--seed"},
		{map[string]string{"seed": "1.5"}, "--seed"},
		{map[string]string{"clock": "bogus"}, "--clock"},
		{map[string]string{"clock": "2026-13-02T03:04:05Z"}, "--clock"},
	} {
		got, err := SeedClock(parsed(c.values))
		var e *Error
		if got != nil || !errors.As(err, &e) || e.Code != CodeUsage || e.Path != c.path {
			t.Errorf("%v: got %v, %v; want usage at %s", c.values, got, err, c.path)
		}
	}
}
