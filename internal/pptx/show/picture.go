package show

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// Percent shows an ST_Percentage value (1000ths of a percent) as a
// percentage, exactly: 33333 is 33.333, 8125 is 8.125, -1 is -0.001.
func Percent(v int64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	s := sign + strconv.FormatInt(v/1000, 10)
	if f := v % 1000; f != 0 {
		s += strings.TrimRight(fmt.Sprintf(".%03d", f), "0")
	}
	return s
}

// Rect shows a CT_RelativeRect (a picture's crop or fill rectangle) as
// the view's [l, t, r, b] in percent.
func Rect(r [4]int64) json.RawMessage {
	parts := make([]string, 4)
	for i, v := range r {
		parts[i] = Percent(v)
	}
	return json.RawMessage("[" + strings.Join(parts, ",") + "]")
}

// Locks shows the picture locks that are set as the view's "locks"
// object, {"no_change_aspect": true}; nil when none is set.
func Locks(names []string) json.RawMessage {
	if len(names) == 0 {
		return nil
	}
	j := &obj{}
	for _, n := range names {
		j.raw(cli.Snake(n), []byte("true"))
	}
	return j.done()
}

// Media shows a media part's facts as the view's "media" object: its
// content type, SHA-256 and pixel size when derived.
func Media(contentType string, m derive.Media) json.RawMessage {
	j := &obj{}
	j.str("type", contentType)
	j.str("sha256", m.SHA256)
	if m.HasPx {
		j.raw("px", []byte("["+strconv.Itoa(m.W)+","+strconv.Itoa(m.H)+"]"))
	}
	return j.done()
}
