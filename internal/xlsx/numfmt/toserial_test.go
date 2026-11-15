package numfmt

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K3: ToSerial, Serial's inverse (designer rows; frozen).

func TestToSerial(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct {
			text string
			d    bool
			want float64
			ok   bool
		}{
			{"2024-03-01", false, 45352, true},
			{"2024-03-01T13:30:00", false, 45352.5625, true},
			{"13:30:00", false, 0.5625, true},
			{"1900-03-01", false, 61, true},
			{"1904-01-01", true, 0, true},
			{"1900-02-28", false, 0, false},
			{"2024-02-30", false, 0, false},
			{"2024-3-1", false, 0, false},
			{"24:00:00", false, 0, false},
			{"10000-01-01", false, 0, false},
		} {
			got, ok := ToSerial(tc.text, tc.d)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Errorf("ToSerial(%q, %v) = %v %v, want %v %v", tc.text, tc.d, got, ok, tc.want, tc.ok)
			}
			if !ok {
				continue
			}
			k := Date
			switch len(tc.text) {
			case len("13:30:00"):
				k = Time
			case len("2024-03-01T13:30:00"):
				k = DateTime
			}
			if v, ok := Serial(got, tc.d, k); !ok || v.Text != tc.text {
				t.Errorf("Serial(ToSerial(%q)) = %q", tc.text, v.Text)
			}
		}
	})
}
