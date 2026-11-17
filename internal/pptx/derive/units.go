package derive

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// EMU is a length as stored in DrawingML: whole English Metric Units.
// Stored coordinates and extents are EMU; values derived through group
// scaling are fractional and stay float64 until they are written.
type EMU int64

// EMUPerPoint is the number of EMU in one point.
const EMUPerPoint = 12700

// Points is a length in points, the unit the views show by default.
type Points float64

// Points converts a stored length to points.
func (e EMU) Points() Points { return Points(float64(e) / EMUPerPoint) }

// ToEMU converts points to (fractional) EMU.
func (p Points) ToEMU() float64 { return float64(p) * EMUPerPoint }

// RoundEMU rounds a derived length to the nearest whole EMU, halves away
// from zero, so that rounding d and -d gives opposite results.
func RoundEMU(v float64) EMU { return EMU(math.Round(v)) }

// Angle is an angle as stored in DrawingML: 60000ths of a degree.
type Angle int64

// AngleUnit is the number of Angle units in one degree.
const AngleUnit = 60000

// Degrees converts a stored angle to degrees.
func (a Angle) Degrees() float64 { return float64(a) / AngleUnit }

// RoundAngle rounds degrees to the nearest stored angle.
func RoundAngle(deg float64) Angle { return Angle(math.Round(deg * AngleUnit)) }

// FormatNum formats a number with at most two decimals, trimming zeros
// (geom.FormatNum, the one formatting rule of the views and of the
// property codecs, shared with vsdx).
func FormatNum(f float64) string { return geom.FormatNum(f) }

// ErrAbsent is wrapped by readers when an attribute or element is not
// written.
var ErrAbsent = errors.New("not written")

// AttrError is an attribute that is absent or cannot be read as its
// schema type.
type AttrError struct {
	Elem, Attr string // qualified names as written
	Value      string // the value as written; "" if absent
	Err        error  // ErrAbsent or the parse failure
}

// Error describes the element, attribute, and failure.
func (e *AttrError) Error() string {
	if errors.Is(e.Err, ErrAbsent) {
		return fmt.Sprintf("%s@%s is not written", e.Elem, e.Attr)
	}
	return fmt.Sprintf("%s@%s=%q: %v", e.Elem, e.Attr, e.Value, e.Err)
}

// Unwrap returns the underlying error.
func (e *AttrError) Unwrap() error { return e.Err }

// elemName returns the element name, or a missing marker.
func elemName(n *tree.Node) string {
	if n == nil {
		return "(missing element)"
	}
	return n.Name()
}

// AttrInt reads an integer attribute (xsd:int or xsd:long). It fails with
// an *AttrError wrapping ErrAbsent when the attribute is not written, and
// with one wrapping the parse error when it is not an integer.
func AttrInt(n *tree.Node, name string) (int64, error) {
	if n == nil {
		return 0, &AttrError{Elem: elemName(n), Attr: name, Err: ErrAbsent}
	}
	s, ok := n.Attr(name)
	if !ok {
		return 0, &AttrError{Elem: n.Name(), Attr: name, Err: ErrAbsent}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, &AttrError{Elem: n.Name(), Attr: name, Value: s, Err: errors.New("not an integer")}
	}
	return v, nil
}

// AttrEMU reads a coordinate attribute (ST_Coordinate,
// ST_PositiveCoordinate) written as whole EMU. Universal measures such as
// "2.5in" are reported as unreadable rather than converted.
func AttrEMU(n *tree.Node, name string) (EMU, error) {
	v, err := AttrInt(n, name)
	return EMU(v), err
}

// AttrAngle reads an ST_Angle attribute.
func AttrAngle(n *tree.Node, name string) (Angle, error) {
	v, err := AttrInt(n, name)
	return Angle(v), err
}

// AttrBool reads an xsd:boolean attribute: "true", "1", "false" or "0".
// Anything else is an error, never false.
func AttrBool(n *tree.Node, name string) (bool, error) {
	if n == nil {
		return false, &AttrError{Elem: elemName(n), Attr: name, Err: ErrAbsent}
	}
	s, ok := n.Attr(name)
	if !ok {
		return false, &AttrError{Elem: n.Name(), Attr: name, Err: ErrAbsent}
	}
	switch strings.TrimSpace(s) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	}
	return false, &AttrError{Elem: n.Name(), Attr: name, Value: s, Err: errors.New("not an xsd:boolean")}
}

// Absent reports whether err says only that the value is not written.
func Absent(err error) bool { return errors.Is(err, ErrAbsent) }

// OrDefault returns v when err is nil, the schema default def when err
// says only that the attribute is absent, and err otherwise. Use it only
// with the default the schema states for that attribute; dflt reports
// whether the default applied.
func OrDefault[T any](v T, err error, def T) (val T, dflt bool, _ error) {
	switch {
	case err == nil:
		return v, false, nil
	case Absent(err):
		return def, true, nil
	}
	var zero T
	return zero, false, err
}
