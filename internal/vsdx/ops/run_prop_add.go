package ops

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run adds a shape data row to the shape at At: it encodes Value by
// Type, asks the edit layer for the row's effective state (a row
// already there, by name, is refused), and writes a full Property
// row. Changed: the new row's address.
func (s *PropAddSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	shapeAddr, err := resolveShape(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	if err := targetRefusal(ws, shapeAddr, "at", false); err != nil {
		return "", nil, nil, "", err
	}
	code, ok := edit.PropTypeCode(s.Type)
	if !ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "type", "unknown type %q", s.Type)
	}
	v, u, err := encodeAddValue(s.Type, s.Value)
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "value", "%s", err)
	}
	state, err := ws.PropState(shapeAddr, s.Name)
	if err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		var u *edit.Unreadable
		if errors.As(err, &u) {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	if state.Exists {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "name", "a row named %q already exists on %s", s.Name, shapeAddr)
	}
	label := s.Label
	if label == "" {
		label = s.Name
	}
	add := edit.PropAdd{Name: s.Name, Label: label, Format: s.Format, TypeCode: code, V: v, U: u}
	if err := ws.AddProp(shapeAddr, add); err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		var un *edit.Unreadable
		if errors.As(err, &un) {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	return "", []string{vsd.PropID(shapeAddr, s.Name)}, nil, "", nil
}

// resolveShape resolves At to a canonical page shape address, through
// a $name alias when At is one.
func resolveShape(at string, aliases map[string]string) (string, error) {
	rc := at
	if strings.HasPrefix(rc, "$") {
		hit, ok := aliases[rc[1:]]
		if !ok {
			return "", cli.Errorf(cli.CodeNotFound, "at", "no alias $%s", rc[1:])
		}
		rc = hit
	}
	a, err := vsd.Parse(rc)
	if err != nil {
		return "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
	}
	if a.Kind != vsd.KindShape || a.InMaster {
		return "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a page shape", at)
	}
	return canonicalShape(a), nil
}

// resolveProp resolves At to the shape address and row name of a shape
// data row, through a $name alias (or $name with a /prop suffix) when
// At is one.
func resolveProp(at string, aliases map[string]string) (string, string, error) {
	if strings.HasPrefix(at, "$") {
		head, suffix, hasSuffix := strings.Cut(at[1:], "/")
		if !hasSuffix {
			hit, ok := aliases[head]
			if !ok {
				return "", "", cli.Errorf(cli.CodeNotFound, "at", "no alias $%s", head)
			}
			a, err := vsd.Parse(hit)
			if err != nil {
				return "", "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
			}
			if a.Kind != vsd.KindProp || a.InMaster {
				return "", "", cli.Errorf(cli.CodeOperationFailed, "at", "at must name a shape data row or $name")
			}
			return canonicalShape(a), a.Name, nil
		}
		hit, ok := aliases[head]
		if !ok {
			return "", "", cli.Errorf(cli.CodeNotFound, "at", "no alias $%s", head)
		}
		shape, err := resolveShape(hit, nil)
		if err != nil {
			return "", "", err
		}
		joined := shape + "/" + suffix
		a, err := vsd.Parse(joined)
		if err != nil || a.Kind != vsd.KindProp || a.InMaster || a.Name == "" {
			return "", "", cli.Errorf(cli.CodeOperationFailed, "at", "at must name a shape data row or $name")
		}
		return shape, a.Name, nil
	}
	a, err := vsd.Parse(at)
	if err != nil {
		return "", "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
	}
	if a.Kind != vsd.KindProp || a.InMaster {
		return "", "", cli.Errorf(cli.CodeOperationFailed, "at", "at must name a shape data row or $name")
	}
	return canonicalShape(a), a.Name, nil
}

// canonicalShape returns the canonical address of the shape a names.
func canonicalShape(a vsd.Address) string {
	return vsd.ShapeID(vsd.PageID(a.Container, a.PageDup), a.Shape, a.Dup)
}

// propValueRefusal reports whether f is a formula the writes allow:
// absent, Inh, No Formula, THEMEVAL() or THEMEGUARD(...), or a
// constant number with an optional unit. Anything else is refused
// with the cell and formula in the detail.
func propValueRefusal(row, f string, hasF bool) error {
	if !hasF || f == "" || f == vsd.FormulaInherited || f == vsd.FormulaNone {
		return nil
	}
	if f == "THEMEVAL()" || strings.HasPrefix(f, "THEMEGUARD(") {
		return nil
	}
	if isNumberConstant(f) {
		return nil
	}
	return fmt.Errorf("Prop.%s.Value formula %q is not a constant", row, f)
}

// isNumberConstant reports whether f is a constant number with an
// optional unit ("20", "-1.5", "1 MM").
func isNumberConstant(f string) bool {
	t := strings.TrimSpace(f)
	if _, ok := vsd.Number(t); ok {
		return true
	}
	head, _, hasUnit := strings.Cut(t, " ")
	if !hasUnit {
		return false
	}
	_, ok := vsd.Number(strings.TrimSpace(head))
	if !ok {
		return false
	}
	unit := strings.TrimSpace(t[len(head):])
	return unit != ""
}

// encodeAddValue encodes a plan value for prop.add by its type: the
// stored V and U. An absent value (nil) gives the type's empty
// value.
func encodeAddValue(typeName string, value any) (string, string, error) {
	if value == nil {
		return defaultAddValue(typeName)
	}
	return encodePropValue(typeName, value)
}

// defaultAddValue returns the stored V and U of an absent prop.add
// value for its type.
func defaultAddValue(typeName string) (string, string, error) {
	switch typeName {
	case "string", "fixed_list", "variable_list":
		return "", "STR", nil
	case "duration":
		return "", "", nil
	case "number":
		return "0", "", nil
	case "currency":
		return "0", "CY", nil
	case "boolean":
		return "0", "BOOL", nil
	case "date":
		return "", "DATE", nil
	}
	return "", "", fmt.Errorf("unknown type %q", typeName)
}

// encodePropValue encodes a present plan value by its type: the
// stored V and U, with a refusal when the value is not the type's
// form.
func encodePropValue(typeName string, value any) (string, string, error) {
	switch typeName {
	case "string", "fixed_list", "variable_list":
		s, ok := value.(string)
		if !ok {
			return "", "", fmt.Errorf("value for type %s must be a string", typeName)
		}
		return s, "STR", nil
	case "duration":
		s, ok := value.(string)
		if !ok {
			return "", "", fmt.Errorf("value for type duration must be a string")
		}
		return s, "", nil
	case "number":
		f, ok := propNumber(value)
		if !ok {
			return "", "", fmt.Errorf("value for type number must be a number")
		}
		return vsd.FormatInches(f), "", nil
	case "currency":
		f, ok := propNumber(value)
		if !ok {
			return "", "", fmt.Errorf("value for type currency must be a number")
		}
		return vsd.FormatInches(f), "CY", nil
	case "boolean":
		b, ok := value.(bool)
		if !ok {
			return "", "", fmt.Errorf("value for type boolean must be a boolean")
		}
		if b {
			return "1", "BOOL", nil
		}
		return "0", "BOOL", nil
	case "date":
		date, err := propDate(value)
		if err != nil {
			return "", "", err
		}
		return date, "DATE", nil
	}
	return "", "", fmt.Errorf("unknown type %q", typeName)
}

// propNumber returns a plan value as a number.
func propNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	}
	return 0, false
}

// propDate returns the date text of a plan date value, which read
// shows as {"date": "2024-03-01T00:00:00"}.
func propDate(value any) (string, error) {
	switch v := value.(type) {
	case map[string]any:
		if len(v) != 1 {
			return "", fmt.Errorf("value for type date must be {\"date\": \"...\"}")
		}
		s, ok := v["date"].(string)
		if !ok {
			return "", fmt.Errorf("value for type date must be {\"date\": \"...\"}")
		}
		return s, nil
	case map[string]string:
		if len(v) != 1 {
			return "", fmt.Errorf("value for type date must be {\"date\": \"...\"}")
		}
		s, ok := v["date"]
		if !ok {
			return "", fmt.Errorf("value for type date must be {\"date\": \"...\"}")
		}
		return s, nil
	}
	return "", fmt.Errorf("value for type date must be {\"date\": \"...\"}")
}
