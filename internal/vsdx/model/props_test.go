package model

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
)

// TestPropsV2 checks the M3 V2 rows: s1's Cost 0.0 currency comes from
// the master, s2's Cost 12.5 from its own Value over the master's row.
func TestPropsV2(t *testing.T) {
	d, r := openFixture(t, "v2-master")
	p := buildPageOf(t, d, r, 0)
	s1 := []Prop{{ID: "page0/s1/prop:Cost", Label: "Cost", Type: "currency", Value: 0.0, Origin: inherit.Master}}
	if got := byID(t, p, "page0/s1").Props; !reflect.DeepEqual(got, s1) {
		t.Errorf("s1 Props = %#v, want %#v", got, s1)
	}
	if got := byID(t, p, "page0/s1").Omitted; len(got) != 0 {
		t.Errorf("s1 Omitted = %v, want none", got)
	}
	s2 := []Prop{{ID: "page0/s2/prop:Cost", Label: "Cost", Type: "currency", Value: 12.5, Origin: inherit.Local}}
	if got := byID(t, p, "page0/s2").Props; !reflect.DeepEqual(got, s2) {
		t.Errorf("s2 Props = %#v, want %#v", got, s2)
	}
	if got := byID(t, p, "page0/s2").Omitted; len(got) != 0 {
		t.Errorf("s2 Omitted = %v, want none", got)
	}
}

// TestPropsV6 checks the M3 V6 row: page0/s1's nine rows in order,
// with Gone absent (deleted by the instance), Done a boolean, Due a
// date through the master, Size a number with unit MM through the
// master and the hidden and linked flags through the master.
func TestPropsV6(t *testing.T) {
	d, r := openFixture(t, "v6-data")
	p := buildPageOf(t, d, r, 0)
	want := []Prop{
		{ID: "page0/s1/prop:Cost", Label: "Cost", Type: "currency", Value: 12.5, Origin: inherit.Local},
		{ID: "page0/s1/prop:Owner", Label: "Owner", Type: "string", Value: "Ada", Origin: inherit.Local},
		{ID: "page0/s1/prop:Done", Label: "Done", Type: "boolean", Value: true, Origin: inherit.Local},
		{ID: "page0/s1/prop:Due", Label: "Due", Type: "date", Value: map[string]string{"date": "2024-03-01T00:00:00"}, Origin: inherit.Master},
		{ID: "page0/s1/prop:Size", Label: "Size", Type: "number", Value: 0.5, Unit: "MM", Origin: inherit.Master},
		{ID: "page0/s1/prop:Hidden", Label: "Hidden", Type: "string", Value: "h", Hidden: true, Origin: inherit.Master},
		{ID: "page0/s1/prop:Linked", Label: "Linked", Type: "string", Value: "L", Linked: true, Origin: inherit.Master},
		{ID: "page0/s1/prop:MasterOnly", Label: "MasterOnly", Type: "string", Value: "m", Origin: inherit.Master},
		{ID: "page0/s1/prop:NewRow", Label: "NewRow", Type: "number", Value: 42.0, Origin: inherit.Local},
	}
	o := byID(t, p, "page0/s1")
	if !reflect.DeepEqual(o.Props, want) {
		t.Errorf("s1 Props = %#v, want %#v", o.Props, want)
	}
	if len(o.Omitted) != 0 {
		t.Errorf("s1 Omitted = %v, want none", o.Omitted)
	}
}

// propsShape returns an inherit.Shape over one synthetic shape whose
// Property rows are the section wrapped by sectionXML.
func propsShape(t *testing.T, sectionXML string) (*inherit.Shape, *inherit.Resolver) {
	t.Helper()
	parsed, err := tree.ParseString(`<Shape ID='1'><Section N='Property'>` + sectionXML + `</Section></Shape>`)
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	sh, _ := sheet.Index(parsed.Root())
	return &inherit.Shape{Sheet: sh}, inherit.New(nil)
}

// TestPropsBadValues checks the M3 parse-failure row: a Value that does
// not parse as its type stays the stored text as a string with one
// omission, and an unknown Type code comes back as a string with one.
// It also covers the remaining type names (fixed_list, variable_list,
// duration), a false boolean, an absent Value and Label, an absent
// Type and an unparseable flag, all read from one in-test sheet.
func TestPropsBadValues(t *testing.T) {
	section := `<Row N='Num'><Cell N='Value' V='abc'/><Cell N='Type' V='2'/></Row>` +
		`<Row N='Cur'><Cell N='Value' V='x'/><Cell N='Type' V='7'/></Row>` +
		`<Row N='Bool'><Cell N='Value' V='yes'/><Cell N='Type' V='3'/></Row>` +
		`<Row N='Off'><Cell N='Value' V='0'/><Cell N='Type' V='3'/></Row>` +
		`<Row N='NoDate'><Cell N='Value' V='' U='DATE'/><Cell N='Type' V='5'/></Row>` +
		`<Row N='NoVal'><Cell N='Type' V='2'/></Row>` +
		`<Row N='Odd'><Cell N='Value' V='z'/><Cell N='Type' V='9'/></Row>` +
		`<Row N='Fixed'><Cell N='Value' V='pick'/><Cell N='Type' V='1'/></Row>` +
		`<Row N='Var'><Cell N='Value' V='free'/><Cell N='Type' V='4'/></Row>` +
		`<Row N='Dur'><Cell N='Value' V='PT1H'/><Cell N='Type' V='6'/></Row>` +
		`<Row N='Str'><Cell N='Value' V='' U='STR'/><Cell N='Type' V='0'/><Cell N='Invisible' V='x'/><Cell N='DataLinked' V='0'/><Cell N='Format' V='#,##0'/></Row>` +
		`<Row N='Bare'><Cell N='Value' V='plain'/></Row>`
	s, r := propsShape(t, section)
	props, omitted := readProps("page0/s9", r, s)
	want := []Prop{
		{ID: "page0/s9/prop:Num", Label: "Num", Type: "number", Value: "abc", Origin: inherit.Local},
		{ID: "page0/s9/prop:Cur", Label: "Cur", Type: "currency", Value: "x", Origin: inherit.Local},
		{ID: "page0/s9/prop:Bool", Label: "Bool", Type: "boolean", Value: "yes", Origin: inherit.Local},
		{ID: "page0/s9/prop:Off", Label: "Off", Type: "boolean", Value: false, Origin: inherit.Local},
		{ID: "page0/s9/prop:NoDate", Label: "NoDate", Type: "date", Origin: inherit.Local},
		{ID: "page0/s9/prop:NoVal", Label: "NoVal", Type: "number", Value: "", Origin: inherit.Local},
		{ID: "page0/s9/prop:Odd", Label: "Odd", Type: "string", Value: "z", Origin: inherit.Local},
		{ID: "page0/s9/prop:Fixed", Label: "Fixed", Type: "fixed_list", Value: "pick", Origin: inherit.Local},
		{ID: "page0/s9/prop:Var", Label: "Var", Type: "variable_list", Value: "free", Origin: inherit.Local},
		{ID: "page0/s9/prop:Dur", Label: "Dur", Type: "duration", Value: "PT1H", Origin: inherit.Local},
		{ID: "page0/s9/prop:Str", Label: "Str", Type: "string", Value: "", Format: "#,##0", Origin: inherit.Local},
		{ID: "page0/s9/prop:Bare", Label: "Bare", Type: "string", Value: "plain", Origin: inherit.Local},
	}
	if !reflect.DeepEqual(props, want) {
		t.Errorf("Props = %#v, want %#v", props, want)
	}
	wantOmitted := []Omission{
		{Prop: "page0/s9/prop:Num", Reason: `value "abc" is not a number`},
		{Prop: "page0/s9/prop:Cur", Reason: `value "x" is not a number`},
		{Prop: "page0/s9/prop:Bool", Reason: `value "yes" is not a boolean`},
		{Prop: "page0/s9/prop:NoDate", Reason: `date value "" is not a date`},
		{Prop: "page0/s9/prop:NoVal", Reason: `value "" is not a number`},
		{Prop: "page0/s9/prop:Odd", Reason: `type "9" is not a known property type`},
		{Prop: "page0/s9/prop:Str", Reason: `Invisible value "x" is not a boolean`},
	}
	if !reflect.DeepEqual(omitted, wantOmitted) {
		t.Errorf("Omitted = %#v, want %#v", omitted, wantOmitted)
	}
}

// TestPropsDateUnit checks the M3 unit and date rows on one sheet: a
// good date is the map with the stored text, and only a number's U
// rides on Unit (a currency U CY and an empty U stay out).
func TestPropsDateUnit(t *testing.T) {
	section := `<Row N='When'><Cell N='Value' V='2024-01-02T03:04:05' U='DATE'/><Cell N='Type' V='5'/></Row>` +
		`<Row N='Len'><Cell N='Value' V='3.5' U='MM'/><Cell N='Type' V='2'/></Row>` +
		`<Row N='Money'><Cell N='Value' V='4.25' U='CY'/><Cell N='Type' V='7'/></Row>` +
		`<Row N='Plain'><Cell N='Value' V='1'/><Cell N='Type' V='2'/></Row>`
	s, r := propsShape(t, section)
	props, omitted := readProps("page0/s9", r, s)
	want := []Prop{
		{ID: "page0/s9/prop:When", Label: "When", Type: "date", Value: map[string]string{"date": "2024-01-02T03:04:05"}, Origin: inherit.Local},
		{ID: "page0/s9/prop:Len", Label: "Len", Type: "number", Value: 3.5, Unit: "MM", Origin: inherit.Local},
		{ID: "page0/s9/prop:Money", Label: "Money", Type: "currency", Value: 4.25, Origin: inherit.Local},
		{ID: "page0/s9/prop:Plain", Label: "Plain", Type: "number", Value: 1.0, Origin: inherit.Local},
	}
	if !reflect.DeepEqual(props, want) {
		t.Errorf("Props = %#v, want %#v", props, want)
	}
	if len(omitted) != 0 {
		t.Errorf("Omitted = %v, want none", omitted)
	}
}

// TestPropsEdgeCases checks readProps on shapes with no Property rows:
// a nil shape, an empty shape and V1's s1 (no Property section), all
// giving no props and no omissions.
func TestPropsEdgeCases(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	p := buildPageOf(t, d, r, 0)
	o := byID(t, p, "page0/s1")
	if o.Props != nil {
		t.Errorf("V1 s1 Props = %#v, want nil", o.Props)
	}
	if o.Omitted != nil {
		t.Errorf("V1 s1 Omitted = %v, want nil", o.Omitted)
	}
	if props, omitted := readProps("page0/s1", r, nil); props != nil || omitted != nil {
		t.Errorf("readProps(nil shape) = (%v, %v), want (nil, nil)", props, omitted)
	}
	if props, omitted := readProps("page0/s1", nil, nil); props != nil || omitted != nil {
		t.Errorf("readProps(nil resolver, nil shape) = (%v, %v), want (nil, nil)", props, omitted)
	}
	if props, omitted := readProps("page0/s1", r, &inherit.Shape{}); props != nil || omitted != nil {
		t.Errorf("readProps(empty shape) = (%v, %v), want (nil, nil)", props, omitted)
	}
}

// TestScalingProps checks the scaling row: reading one shape data row
// for each of N and 4N shapes (N = 1000) allocates at most 5.5 times
// as much for 4 times the shapes (laws.TimeScales time ratio at most 8).
func TestScalingProps(t *testing.T) {
	setup := func(n int) (func() error, error) {
		s, r := propsShape(t, `<Row N='Cost'><Cell N='Value' V='12.5' U='CY'/><Cell N='Type' V='7'/><Cell N='Label' V='Cost'/></Row>`)
		many := make([]*inherit.Shape, n)
		for i := range many {
			many[i] = s
		}
		want := []Prop{{ID: "page0/s1/prop:Cost", Label: "Cost", Type: "currency", Value: 12.5, Origin: inherit.Local}}
		return func() error {
			for _, sh := range many {
				props, omitted := readProps("page0/s1", r, sh)
				if len(omitted) != 0 {
					return fmt.Errorf("readProps omissions = %v, want none", omitted)
				}
				if !reflect.DeepEqual(props, want) {
					return fmt.Errorf("readProps = %#v, want %#v", props, want)
				}
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx readProps", setup)
	laws.TimeScales(t, "vsdx readProps", setup)
}
