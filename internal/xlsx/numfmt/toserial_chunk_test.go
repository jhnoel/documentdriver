package numfmt

import "testing"

func TestToSerialChunkBoundaries(t *testing.T) {
	for _, tc := range []struct {
		text  string
		d1904 bool
		kind  Kind
		ok    bool
	}{
		{"1900-03-01T00:00:00", false, DateTime, true},
		{"1904-01-01T00:00:00", true, DateTime, true},
		{"1903-12-31T23:59:59", true, DateTime, false},
		{"9999-12-31T23:59:59", false, DateTime, true},
		{"9999-12-31T23:59:59", true, DateTime, true},
		{"23:59:59", false, Time, true},
		{"2000-02-29", false, Date, true},
		{"1900-02-29", false, Date, false},
		{"2024-01-01T12:60:00", false, DateTime, false},
		{"2024-01-01t12:00:00", false, DateTime, false},
		{"2024-01-01T12:00:00Z", false, DateTime, false},
		{"12:00:60", false, Time, false},
		{"", false, Date, false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			serial, ok := ToSerial(tc.text, tc.d1904)
			if ok != tc.ok {
				t.Fatalf("ToSerial(%q) ok = %v", tc.text, ok)
			}
			if ok {
				value, valid := Serial(serial, tc.d1904, tc.kind)
				if !valid || value.Text != tc.text {
					t.Fatalf("round trip = %+v, %v", value, valid)
				}
			}
		})
	}
}
