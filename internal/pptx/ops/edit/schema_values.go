package edit

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// ErrSchema is returned for a value outside the schema type of the
// attribute it would be written to (schema.ErrSchema).
var ErrSchema = schema.ErrSchema

// AttrType returns the simple type of attribute attr of complex type
// ctype ("a:CT_LineProperties", "w"), from the generated tables
// (schema.AttrType).
func AttrType(ctype, attr string) (string, bool) { return schema.AttrType(ctype, attr) }

// ChildType returns the complex type of child (a qualified name such as
// "a:lumMod") in the tabulated complex type ctype (schema.ChildType).
func ChildType(ctype, child string) (string, bool) { return schema.ChildType(ctype, child) }

// Enum returns the enumeration of simple type typ (nil when it has none).
func Enum(typ string) []string { return schema.Enum(typ) }

// CheckAttr checks v against the schema type of attribute attr of complex
// type ctype (schema.CheckAttr). An attribute the tables do not hold
// fails too, so that a codec never writes an attribute unchecked.
func CheckAttr(ctype, attr, v string) error { return schema.CheckAttr(ctype, attr, v) }

// CheckValue checks v against simple type typ or an xsd: built-in
// (schema.CheckValue).
func CheckValue(typ, v string) error { return schema.CheckValue(typ, v) }

// ColorType returns the complex type of a colour kind's element (scheme
// is a:CT_SchemeColor), from the colour choice of CT_SolidColorFillProperties.
func ColorType(kind string) (string, bool) {
	el := derive.ColorElement(kind)
	if el == "" {
		return "", false
	}
	return ChildType("a:CT_SolidColorFillProperties", "a:"+el)
}

// CheckColor checks a colour against the schema of its element: the
// value (scheme, system or preset name, sRGB hex), a system colour's
// lastClr, the hsl and scRGB components, and each modifier (a child the
// colour type allows, with a val exactly when its type has one, in its
// type's range). Errors wrap ErrSchema and name the part at fault.
func CheckColor(c derive.Color) error {
	ct, ok := ColorType(c.Kind)
	if !ok {
		return fmt.Errorf("%w: colour kind %q", ErrSchema, c.Kind)
	}
	if names, ok := derive.ColorComponents(c.Kind); ok {
		for i, a := range names {
			if err := CheckAttr(ct, a, strconv.FormatInt(c.Comp[i], 10)); err != nil {
				return fmt.Errorf("%s %s: %w", c.Kind, a, err)
			}
		}
	} else {
		if err := CheckAttr(ct, "val", c.Val); err != nil {
			return fmt.Errorf("%s colour: %w", c.Kind, err)
		}
		if c.LastClr != "" {
			if err := CheckAttr(ct, "lastClr", c.LastClr); err != nil {
				return fmt.Errorf("%s lastClr: %w", c.Kind, err)
			}
		}
	}
	for _, m := range c.Mods {
		mt, ok := ChildType(ct, "a:"+m.Name)
		if !ok {
			var names []string
			slots, _ := schema.Slots(ct)
			for _, s := range slots {
				for _, n := range s.Names {
					if n.Local != "extLst" {
						names = append(names, n.Local)
					}
				}
			}
			return fmt.Errorf("%w: %q is not a colour modifier (want one of %s)", ErrSchema, m.Name, schema.ListSome(names))
		}
		_, hasVal := AttrType(mt, "val")
		switch {
		case hasVal && m.Val == "":
			return fmt.Errorf("%w: modifier %s needs a value", ErrSchema, m.Name)
		case !hasVal && m.Val != "":
			return fmt.Errorf("%w: modifier %s takes no value", ErrSchema, m.Name)
		case hasVal:
			if err := CheckAttr(mt, "val", m.Val); err != nil {
				return fmt.Errorf("modifier %s: %w", m.Name, err)
			}
		}
	}
	return nil
}

// ErrPlaceholderColor is returned for the scheme colour phClr outside a
// theme's style matrix.
var ErrPlaceholderColor = errors.New("phClr is the placeholder colour of theme style matrices, not a slide colour; give a scheme colour such as accent1")

// CheckSlideColor checks a colour written on a slide: CheckColor, and
// not phClr. phClr is in a:ST_SchemeColorVal, but means "the colour the
// style reference supplies" and is valid only inside a theme's
// fillStyleLst, lnStyleLst, effectStyleLst and bgFillStyleLst.
func CheckSlideColor(c derive.Color) error {
	if c.Kind == "scheme" && c.Val == "phClr" {
		return ErrPlaceholderColor
	}
	return CheckColor(c)
}
