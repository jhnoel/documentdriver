package show

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// Body is a text body's a:bodyPr properties as derived: the stored value
// of each attribute in derive.BodyAttrs ("" where not derived) and the
// autofit choice.
type Body struct {
	Attrs   map[string]string
	Autofit derive.Autofit
}

// bodyLen are the attributes shown as lengths in the view's units.
var bodyLen = map[string]bool{"spcCol": true}

// bodyBool are the boolean attributes.
var bodyBool = map[string]bool{
	"rtlCol": true, "anchorCtr": true, "upright": true, "compatLnSpc": true,
	"spcFirstLastPara": true, "fromWordArt": true, "forceAA": true,
}

// BodyValue shows one stored body value as the views show it under key
// (an attribute of derive.BodyAttrs, "fontScale" or "lnSpcReduction"),
// num formatting EMU lengths in the view's units; false when the stored
// value is not readable, so the key is left out.
func BodyValue(key, v string, num func(emu float64) string) (json.RawMessage, bool) {
	switch {
	case v == "":
		return nil, false
	case bodyBool[key]:
		switch v {
		case "1", "true":
			return json.RawMessage("true"), true
		case "0", "false":
			return json.RawMessage("false"), true
		}
		return nil, false
	case bodyLen[key]:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, false
		}
		return json.RawMessage(num(float64(n))), true
	case key == "numCol":
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return nil, false
		}
		return json.RawMessage(v), true
	case key == "rot":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, false
		}
		return json.RawMessage(derive.FormatNum(float64(n) / 60000)), true
	case key == "fontScale" || key == "lnSpcReduction":
		// 1000ths of a percent, shown as a percentage number; the
		// percentage-string form ("62.5%") as its number.
		if s, ok := strings.CutSuffix(v, "%"); ok {
			if _, err := strconv.ParseFloat(s, 64); err != nil {
				return nil, false
			}
			return json.RawMessage(strings.TrimPrefix(s, "+")), true
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, false
		}
		return json.RawMessage(derive.FormatNum(float64(n) / 1000)), true
	}
	return str(v), true
}

// BodyJSON shows a text body's properties: the "text" object of --detail
// full. Keys are derive.BodyAttrs in the contract's spelling (cli.Snake), with the four insets as one array
// shown only when all are derived, and autofit (with fontScale and
// lnSpcReduction when a:normAutofit stores them) after the insets.
func BodyJSON(b Body, num func(emu float64) string) json.RawMessage {
	j := &obj{}
	for _, k := range []string{"anchor", "wrap"} {
		if v := b.Attrs[k]; v != "" {
			j.str(k, v)
		}
	}
	var ins []string
	for _, k := range []string{"lIns", "tIns", "rIns", "bIns"} {
		n, err := strconv.ParseInt(b.Attrs[k], 10, 64)
		if err != nil {
			ins = nil
			break
		}
		ins = append(ins, num(float64(n)))
	}
	if len(ins) == 4 {
		j.raw("insets", []byte("["+strings.Join(ins, ",")+"]"))
	}
	if b.Autofit.Mode != "" {
		j.str("autofit", b.Autofit.Mode)
		for _, x := range []struct{ k, v string }{{"fontScale", b.Autofit.FontScale}, {"lnSpcReduction", b.Autofit.LnSpcReduction}} {
			if v, ok := BodyValue(x.k, x.v, num); ok {
				j.raw(cli.Snake(x.k), v)
			}
		}
	}
	for _, k := range derive.BodyAttrs[6:] {
		if v, ok := BodyValue(k, b.Attrs[k], num); ok {
			j.raw(cli.Snake(k), v)
		}
	}
	return j.done()
}
