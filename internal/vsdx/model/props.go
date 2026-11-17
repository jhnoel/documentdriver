package model

import (
	"fmt"
	"time"

	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// propTypes is the Property Type code table: the Type cell holds one of
// these codes and the Value cell renders by it. Provenance: shape data
// type codes from the format researcher's notes section 8 (UNCONFIRMED,
// VE9); the corpus evidence of 2026-11-17 agrees, with type 5 rows
// storing U DATE and ISO text, type 2 numbers where a length U such as
// MM rides beside the value in inches, types 0, 1 and 4 storing STR,
// type 7 currency and types 3 and 6 never occurring in the corpus.
var propTypes = []string{"string", "fixed_list", "number", "boolean", "variable_list", "date", "duration", "currency"}

// readProps returns the shape's effective shape data rows (Property
// section) in order: label, type from the Type cell, value from the
// Value cell by type, format, hidden (Invisible) and linked
// (DataLinked). A row whose value does not parse as its type keeps the
// stored text as a string and gets an omission. Chunk M3.
func readProps(shape string, r *inherit.Resolver, s *inherit.Shape) ([]Prop, []Omission) {
	rows := r.Rows(s, vsd.SectionProperty)
	if len(rows) == 0 {
		return nil, nil
	}
	props := make([]Prop, 0, len(rows))
	var omitted []Omission
	for _, row := range rows {
		id := vsd.PropID(shape, row.Key.Row)
		p := Prop{ID: id, Origin: row.Origin, Label: propRowLabel(r, s, row)}
		typeName, code, rawType, badType := propRowType(r, s, row.Key)
		p.Type = typeName
		if badType {
			omitted = append(omitted, propOmission(id, fmt.Sprintf("type %q is not a known property type", rawType)))
		}
		value, unit, valueOrigin, valueOmission := propRowValue(r, s, row.Key, code, id)
		p.Value, p.Unit = value, unit
		if valueOrigin != "" {
			p.Origin = valueOrigin
		}
		if valueOmission.Reason != "" {
			omitted = append(omitted, valueOmission)
		}
		p.Format = propRowText(r, s, row.Key, "Format")
		hidden, rawHidden, badHidden := propRowBool(r, s, row.Key, "Invisible")
		p.Hidden = hidden
		if badHidden {
			omitted = append(omitted, propOmission(id, fmt.Sprintf("Invisible value %q is not a boolean", rawHidden)))
		}
		linked, rawLinked, badLinked := propRowBool(r, s, row.Key, "DataLinked")
		p.Linked = linked
		if badLinked {
			omitted = append(omitted, propOmission(id, fmt.Sprintf("DataLinked value %q is not a boolean", rawLinked)))
		}
		props = append(props, p)
	}
	return props, omitted
}

// propRowLabel returns the row's Label cell, or the row name when no
// sheet on the chain stores a Label. Chunk M3.
func propRowLabel(r *inherit.Resolver, s *inherit.Shape, row inherit.Row) string {
	c, _, ok := r.Cell(s, row.Key.WithCell("Label"))
	if !ok {
		return row.Key.Row
	}
	return c.V
}

// propRowType returns the row's type name and code from its Type cell.
// An absent Type is the first code (string); a Type that does not parse
// as a code in the table comes back as string with bad set and raw
// holding the stored text for an omission. Chunk M3.
func propRowType(r *inherit.Resolver, s *inherit.Shape, key sheet.Key) (name string, code int, raw string, bad bool) {
	c, _, ok := r.Cell(s, key.WithCell("Type"))
	if !ok {
		return propTypes[0], 0, "", false
	}
	f, ok := vsd.Number(c.V)
	if ok {
		n := int(f)
		if f == float64(n) && n >= 0 && n < len(propTypes) {
			return propTypes[n], n, c.V, false
		}
	}
	return propTypes[0], 0, c.V, true
}

// propRowValue returns the row's Value by its type code: a number for
// number and currency, a boolean for boolean, map{"date": ...} for
// date and the stored text for everything else. The unit is the Value
// cell's U only for a number with a length unit; the value stays in
// stored inches. A value that does not parse as its type comes back as
// the stored text with an omission, except a date, which comes back
// with no value (nil) and an omission (chunk CC8). Chunk M3.
func propRowValue(r *inherit.Resolver, s *inherit.Shape, key sheet.Key, code int, id string) (value any, unit string, origin inherit.Origin, omitted Omission) {
	c, foundOrigin, ok := r.Cell(s, key.WithCell("Value"))
	text, cellUnit := "", ""
	if ok {
		text, cellUnit = c.V, c.U
		origin = foundOrigin
	}
	switch code {
	case 2, 7: // number, currency
		if f, ok := vsd.Number(text); ok {
			if code == 2 {
				unit = cellUnit
			}
			return f, unit, origin, Omission{}
		}
		if code == 2 {
			unit = cellUnit
		}
		return text, unit, origin, propOmission(id, fmt.Sprintf("value %q is not a number", text))
	case 3: // boolean
		if b, ok := vsd.Bool(text); ok {
			return b, "", origin, Omission{}
		}
		return text, "", origin, propOmission(id, fmt.Sprintf("value %q is not a boolean", text))
	case 5: // date
		if validPropDate(text) {
			return map[string]string{"date": text}, "", origin, Omission{}
		}
		return nil, "", origin, propOmission(id, fmt.Sprintf("date value %q is not a date", text))
	default: // string, fixed_list, variable_list, duration: the stored text.
		return text, "", origin, Omission{}
	}
}

// propRowText returns the effective V of a Property cell (Format), or
// "" when no sheet on the chain stores it. Chunk M3.
func propRowText(r *inherit.Resolver, s *inherit.Shape, key sheet.Key, cell string) string {
	c, _, ok := r.Cell(s, key.WithCell(cell))
	if !ok {
		return ""
	}
	return c.V
}

// propRowBool returns the effective boolean of a Property cell
// (Invisible, DataLinked): absent is false; a stored value that does
// not parse comes back false with bad set and raw holding the stored
// text for an omission. Chunk M3.
func propRowBool(r *inherit.Resolver, s *inherit.Shape, key sheet.Key, cell string) (value bool, raw string, bad bool) {
	c, _, ok := r.Cell(s, key.WithCell(cell))
	if !ok {
		return false, "", false
	}
	b, ok := vsd.Bool(c.V)
	if ok {
		return b, c.V, false
	}
	return false, c.V, true
}

// validPropDate reports whether s is a stored date in read's form,
// YYYY-MM-DDThh:mm:ss with a valid calendar value and nothing else
// (no zone, no fraction). Chunk CC8.
func validPropDate(s string) bool {
	if len(s) != 19 {
		return false
	}
	if s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' || s[16] != ':' {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 4, 7, 10, 13, 16:
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	_, err := time.Parse("2006-01-02T15:04:05", s)
	return err == nil
}

// propOmission returns one value that could not be derived faithfully,
// naming the prop by its ID. Chunk M3.
func propOmission(id string, reason string) Omission {
	return Omission{Prop: id, Reason: reason}
}
