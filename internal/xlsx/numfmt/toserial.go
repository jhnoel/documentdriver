package numfmt

import "time"

// ToSerial converts the ISO text of a plan's date or time value to a
// cell serial (chunk K3): "2024-03-01" and "2024-03-01T13:30:00" to days
// since the date system's epoch (date1904 selects the 1904 system; the
// 1900 system counts Excel's fictitious 1900-02-29, so 1900-03-01 is
// 61), "13:30:00" to the fraction of a day. It is Serial's inverse:
// Serial(ToSerial(s), date1904, k).Text == s for the kind k of s. It
// reports false for text that is not exactly one of the three forms,
// a date that is not a calendar date, a time past 23:59:59, and a date
// before the system's first unambiguous day (1900-03-01; 1904-01-01) or
// after 9999-12-31.
func ToSerial(text string, date1904 bool) (float64, bool) {
	layout := ""
	switch len(text) {
	case len("2006-01-02"):
		layout = "2006-01-02"
	case len("2006-01-02T15:04:05"):
		layout = "2006-01-02T15:04:05"
	case len("15:04:05"):
		layout = "15:04:05"
	default:
		return 0, false
	}
	value, err := time.Parse(layout, text)
	if err != nil || value.Format(layout) != text {
		return 0, false
	}
	seconds := value.Hour()*3600 + value.Minute()*60 + value.Second()
	fraction := float64(seconds) / 86400
	if layout == "15:04:05" {
		return fraction, true
	}
	epoch := epoch1900
	first := epoch1900.AddDate(0, 0, date1900Low)
	if date1904 {
		epoch = epoch1904
		first = epoch
	}
	if value.Before(first) || value.Year() > 9999 {
		return 0, false
	}
	days := (value.Unix() - epoch.Unix()) / 86400
	return float64(days) + fraction, true
}
