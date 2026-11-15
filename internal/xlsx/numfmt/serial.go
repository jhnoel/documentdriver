package numfmt

import (
	"fmt"
	"math"
	"time"
)

// Value is a date or time read from a serial.
type Value struct {
	// Key is "date" for the Date and DateTime kinds (a date serial) and
	// "time" for the Time kind (a fraction of a day).
	Key string
	// Text is the value in its text form.
	Text string
}

// The serial bounds of the two date systems, in days.
const (
	// The 1900 system has no 1900-02-29: serials below 61 are ambiguous
	// (Excel's fake 1900-02-29 shifts the days before 1900-03-01), so
	// they are refused. Serial 61 is 1900-03-01 and 2958465 is 9999-12-31.
	date1900Low = 61
	date1900Hi  = 2958466
	// The 1904 system starts at serial 0 (1904-01-01) and ends at 2957003
	// (9999-12-31).
	date1904Low = 0
	date1904Hi  = 2957004
)

// The two date systems' epochs: serial 0 of the 1900 system is
// 1899-12-30 (so 1900-03-01, past Excel's fictitious 1900-02-29, is
// date1900Low) and of the 1904 system 1904-01-01.
var (
	epoch1900 = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	epoch1904 = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
)

// Serial converts the serial v of a cell whose number format classifies
// as k to its text form. date1904 selects the 1904 date system; the Time
// kind is the same in either. It reports false when the kind holds no date
// or time, or when v is not a valid serial for the kind and date system.
//
// v is rounded to the nearest whole second, halves away from zero, before
// its date and time are split, so a value at 23:59:59.6 rolls into the
// next day.
func Serial(v float64, date1904 bool, k Kind) (Value, bool) {
	switch k {
	case Time:
		if !finiteNonNeg(v) {
			return Value{}, false
		}
		return Value{Key: "time", Text: timeOfDay(v)}, true
	case Date, DateTime:
	default:
		return Value{}, false
	}

	if !finiteNonNeg(v) {
		return Value{}, false
	}
	lo, hi := float64(date1900Low), float64(date1900Hi)
	epoch := epoch1900
	if date1904 {
		lo, hi = date1904Low, date1904Hi
		epoch = epoch1904
	}
	if v < lo || v >= hi {
		return Value{}, false
	}
	days, secs := splitSerial(v)
	if days >= int64(hi) {
		return Value{}, false
	}
	text := epoch.AddDate(0, 0, int(days)).Format("2006-01-02")
	if k == Date {
		return Value{Key: "date", Text: text}, true
	}
	return Value{Key: "date", Text: text + "T" + hms(secs)}, true
}

// Hidden reports which part of serial v a date or time form of kind k
// leaves out: "time" when k is Date and v has a time of day (after
// rounding to the second, as Serial rounds), "days" when k is Time and v
// stores one or more whole days (before rounding), and "" otherwise (including DateTime, which
// shows both, and values Serial refuses).
func Hidden(v float64, k Kind) string {
	if !finiteNonNeg(v) {
		return ""
	}
	_, secs := splitSerial(v)
	switch {
	case k == Date && secs != 0:
		return "time"
	case k == Time && math.Floor(v) >= 1:
		return "days"
	}
	return ""
}

// finiteNonNeg reports whether v is finite and not negative.
func finiteNonNeg(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

// splitSerial rounds v to the nearest whole second, halves away from zero,
// and returns it as whole days and seconds into the day, carrying a
// rounding that reaches a full day into the next one.
func splitSerial(v float64) (days, secs int64) {
	whole := math.Floor(v)
	s := int64(math.Round((v - whole) * 86400))
	if s == 86400 {
		whole++
		s = 0
	}
	return int64(whole), s
}

// timeOfDay is the fractional part of v as "HH:MM:SS", rounded to the
// nearest whole second (halves away from zero). A rounding that reaches
// midnight wraps to 00:00:00: a Time has no date to carry into.
func timeOfDay(v float64) string {
	frac := v - math.Floor(v)
	s := int64(math.Round(frac * 86400))
	if s == 86400 {
		s = 0
	}
	return hms(s)
}

// hms formats seconds into a day as "HH:MM:SS".
func hms(secs int64) string {
	return fmt.Sprintf("%02d:%02d:%02d", secs/3600, secs%3600/60, secs%60)
}
