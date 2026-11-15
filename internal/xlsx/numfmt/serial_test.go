package numfmt

import (
	"math"
	"testing"
)

func TestSerial(t *testing.T) {
	const (
		rollover = 61 + 86399.6/86400 // 1900-03-01 23:59:59.6 -> next day
		halfSec  = 61 + 0.5/86400     // 00:00:00.5 rounds to 00:00:01
	)
	tests := []struct {
		name     string
		v        float64
		date1904 bool
		kind     Kind
		want     Value
		wantOK   bool
	}{
		// Kinds that hold no date or time are refused whatever v is.
		{"general", 0, false, General, Value{}, false},
		{"number", 0, false, Number, Value{}, false},
		{"text", 0, false, Text, Value{}, false},
		{"duration", 0, false, Duration, Value{}, false},

		// Time: any date system, the fractional part of the serial.
		{"time zero", 0, false, Time, Value{Key: "time", Text: "00:00:00"}, true},
		{"time quarter", 0.25, false, Time, Value{Key: "time", Text: "06:00:00"}, true},
		{"time half", 0.5, true, Time, Value{Key: "time", Text: "12:00:00"}, true},
		{"time near end wraps", 0.9999953703703704, false, Time, Value{Key: "time", Text: "00:00:00"}, true},
		{"time whole day", 1, false, Time, Value{Key: "time", Text: "00:00:00"}, true},
		{"time one and a half", 1.5, false, Time, Value{Key: "time", Text: "12:00:00"}, true},
		{"time two and a quarter", 2.25, true, Time, Value{Key: "time", Text: "06:00:00"}, true},
		{"time half second rounds away", 0.5 / 86400, false, Time, Value{Key: "time", Text: "00:00:01"}, true},
		{"time negative", -0.5, false, Time, Value{}, false},
		{"time nan", math.NaN(), false, Time, Value{}, false},
		{"time inf", math.Inf(1), false, Time, Value{}, false},

		// 1900 system: epoch 1899-12-30, serial 61 is 1900-03-01.
		{"1900 date 61", 61, false, Date, Value{Key: "date", Text: "1900-03-01"}, true},
		{"1900 datetime 61", 61, false, DateTime, Value{Key: "date", Text: "1900-03-01T00:00:00"}, true},
		{"1900 date noon", 61.5, false, Date, Value{Key: "date", Text: "1900-03-01"}, true},
		{"1900 datetime noon", 61.5, false, DateTime, Value{Key: "date", Text: "1900-03-01T12:00:00"}, true},
		{"1900 datetime half second", halfSec, false, DateTime, Value{Key: "date", Text: "1900-03-01T00:00:01"}, true},
		{"1900 date rollover", rollover, false, Date, Value{Key: "date", Text: "1900-03-02"}, true},
		{"1900 datetime rollover", rollover, false, DateTime, Value{Key: "date", Text: "1900-03-02T00:00:00"}, true},
		{"1900 last date", 2958465, false, Date, Value{Key: "date", Text: "9999-12-31"}, true},
		{"1900 last datetime", 2958465.99999, false, DateTime, Value{Key: "date", Text: "9999-12-31T23:59:59"}, true},
		// Serial 60 is Excel's fake 1900-02-29; below 61 is refused.
		{"1900 serial 60", 60, false, Date, Value{}, false},
		{"1900 serial 1", 1, false, Date, Value{}, false},
		{"1900 serial 0", 0, false, Date, Value{}, false},
		{"1900 past end", 2958466, false, Date, Value{}, false},
		{"1900 past end datetime", 2958465.99999537, false, DateTime, Value{}, false},

		// 1904 system: epoch 1904-01-01, serial 0 is 1904-01-01.
		{"1904 date 0", 0, true, Date, Value{Key: "date", Text: "1904-01-01"}, true},
		{"1904 datetime 0", 0, true, DateTime, Value{Key: "date", Text: "1904-01-01T00:00:00"}, true},
		{"1904 datetime quarter", 0.25, true, DateTime, Value{Key: "date", Text: "1904-01-01T06:00:00"}, true},
		{"1904 last date", 2957003, true, Date, Value{Key: "date", Text: "9999-12-31"}, true},
		{"1904 past end", 2957004, true, Date, Value{}, false},
		{"1904 negative", -0.5, true, Date, Value{}, false},

		// Date and DateTime refuse the same invalid floats as Time.
		{"date nan", math.NaN(), false, Date, Value{}, false},
		{"datetime inf", math.Inf(1), false, DateTime, Value{}, false},
		{"date negative", -1, false, Date, Value{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Serial(tc.v, tc.date1904, tc.kind)
			if ok != tc.wantOK {
				t.Fatalf("Serial(%v, %v, %v) ok = %v; want %v", tc.v, tc.date1904, tc.kind, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("Serial(%v, %v, %v) = %+v; want %+v", tc.v, tc.date1904, tc.kind, got, tc.want)
			}
		})
	}
}

func TestHidden(t *testing.T) {
	for _, tc := range []struct {
		v    float64
		k    Kind
		want string
	}{
		{45000, Date, ""},
		{45000.5, Date, "time"},
		{45000.0000001, Date, ""}, // rounds to 00:00:00
		{45000.5, DateTime, ""},
		{0.5, Time, ""},
		{1.5, Time, "days"},
		{0.9999999, Time, ""}, // no stored whole day, though it rounds to midnight
		{-1, Date, ""},
		{45000.5, Number, ""},
	} {
		if got := Hidden(tc.v, tc.k); got != tc.want {
			t.Errorf("Hidden(%v, %v) = %q, want %q", tc.v, tc.k, got, tc.want)
		}
	}
}
