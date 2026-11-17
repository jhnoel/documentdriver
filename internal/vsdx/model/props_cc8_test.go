package model

import (
	"reflect"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
)

// TestPropsDateBadCC8 checks the CC8 date rows: a test-built date row
// with stored Value "0" (and one empty, one a serial number) reads
// with no value (nil) and the omission date value "0" is not a date,
// while a valid date still reads as the map.
func TestPropsDateBadCC8(t *testing.T) {
	section := `<Row N='Zero'><Cell N='Value' V='0' U='DATE'/><Cell N='Type' V='5'/></Row>` +
		`<Row N='Empty'><Cell N='Value' V='' U='DATE'/><Cell N='Type' V='5'/></Row>` +
		`<Row N='Serial'><Cell N='Value' V='45292' U='DATE'/><Cell N='Type' V='5'/></Row>` +
		`<Row N='When'><Cell N='Value' V='2024-03-01T00:00:00' U='DATE'/><Cell N='Type' V='5'/></Row>`
	s, r := propsShape(t, section)
	props, omitted := readProps("page0/s1", r, s)
	want := []Prop{
		{ID: "page0/s1/prop:Zero", Label: "Zero", Type: "date", Origin: inherit.Local},
		{ID: "page0/s1/prop:Empty", Label: "Empty", Type: "date", Origin: inherit.Local},
		{ID: "page0/s1/prop:Serial", Label: "Serial", Type: "date", Origin: inherit.Local},
		{ID: "page0/s1/prop:When", Label: "When", Type: "date", Value: map[string]string{"date": "2024-03-01T00:00:00"}, Origin: inherit.Local},
	}
	if !reflect.DeepEqual(props, want) {
		t.Errorf("Props = %#v, want %#v", props, want)
	}
	for _, p := range props[:3] {
		if p.Value != nil {
			t.Errorf("%s Value = %#v, want nil (no value)", p.ID, p.Value)
		}
	}
	wantOmitted := []Omission{
		{Prop: "page0/s1/prop:Zero", Reason: `date value "0" is not a date`},
		{Prop: "page0/s1/prop:Empty", Reason: `date value "" is not a date`},
		{Prop: "page0/s1/prop:Serial", Reason: `date value "45292" is not a date`},
	}
	if !reflect.DeepEqual(omitted, wantOmitted) {
		t.Errorf("Omitted = %#v, want %#v", omitted, wantOmitted)
	}
}

// TestPropsNonASCIIValidCC8 checks the CC8 condition: rows a file
// already has with non-ASCII or space names stay readable, listed by
// their address.
func TestPropsNonASCIIValidCC8(t *testing.T) {
	section := `<Row N='Größe'><Cell N='Value' V='x' U='STR'/><Cell N='Type' V='0'/></Row>` +
		`<Row N='my name'><Cell N='Value' V='y' U='STR'/><Cell N='Type' V='0'/></Row>`
	s, r := propsShape(t, section)
	props, omitted := readProps("page0/s1", r, s)
	want := []Prop{
		{ID: "page0/s1/prop:Größe", Label: "Größe", Type: "string", Value: "x", Origin: inherit.Local},
		{ID: "page0/s1/prop:my name", Label: "my name", Type: "string", Value: "y", Origin: inherit.Local},
	}
	if !reflect.DeepEqual(props, want) {
		t.Errorf("Props = %#v, want %#v", props, want)
	}
	if len(omitted) != 0 {
		t.Errorf("Omitted = %v, want none", omitted)
	}
}
