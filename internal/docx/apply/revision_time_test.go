package apply

import (
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/ids"
)

// TestRevisionTimePreviewClock: without a Commit, PreviewClock false
// stamps PreviewTime and true stamps the IDs clock's second; a given
// Now wins in both.
func TestRevisionTimePreviewClock(t *testing.T) {
	clock := time.Date(2026, 3, 4, 5, 6, 7, 890000000, time.UTC)
	wantClock := clock.Truncate(time.Second)

	preview, _ := time.Parse(revise.DateLayout, PreviewTime)
	given, _ := time.Parse(revise.DateLayout, "2030-01-02T03:04:05Z")

	tests := []struct {
		name string
		opt  Options
		want time.Time
	}{
		{"preview time", Options{}, preview},
		{"dry run clock", Options{PreviewClock: true, IDs: ids.Fixed(0, clock)}, wantClock},
		{"now wins without preview clock", Options{Now: "2030-01-02T03:04:05Z", IDs: ids.Fixed(0, clock)}, given},
		{"now wins with preview clock", Options{Now: "2030-01-02T03:04:05Z", PreviewClock: true, IDs: ids.Fixed(0, clock)}, given},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, bad := revisionTime(tc.opt)
			if bad != nil {
				t.Fatalf("revisionTime: %v", bad)
			}
			if !got.Equal(tc.want) {
				t.Errorf("revisionTime = %s, want %s", got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}
