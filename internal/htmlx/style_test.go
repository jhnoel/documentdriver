package htmlx

import (
	"reflect"
	"strings"
	"testing"
)

// TestStyleDecls records the StyleDecls rows: sorted by name, a later
// duplicate replaces an earlier one, url( is refused and names lower-case.
func TestStyleDecls(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []StyleDecl
	}{
		{"sorted", "font-size:12pt; color:#FF0000", []StyleDecl{{Name: "color", Value: "#FF0000"}, {Name: "font-size", Value: "12pt"}}},
		{"duplicate", "color:#00FF00; color:#FF0000", []StyleDecl{{Name: "color", Value: "#FF0000"}}},
		{"lowercased", "FONT-SIZE:12pt; CoLoR:#FF0000", []StyleDecl{{Name: "color", Value: "#FF0000"}, {Name: "font-size", Value: "12pt"}}},
		{"unset", "color:unset; font-size:12pt", []StyleDecl{{Name: "font-size", Value: "12pt"}}},
		{"empty", "", []StyleDecl{}},
		{"blank parts", " ; ; ", []StyleDecl{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StyleDecls(tc.in)
			if err != nil {
				t.Fatalf("StyleDecls(%q): %v", tc.in, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("StyleDecls(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// TestStyleDeclsErrors records the StyleDecls refusals.
func TestStyleDeclsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"url", "background:url(http://example.com/x.png)", "unsupported CSS value"},
		{"url uppercase", "background:URL(x)", "unsupported CSS value"},
		{"bang", "color:red!important", "unsupported CSS value"},
		{"angle", "color:<red>", "unsupported CSS value"},
		{"empty value", "color:", "unsupported CSS value"},
		{"no colon", "color", "CSS declarations require property: value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := StyleDecls(tc.in)
			if err == nil {
				t.Fatalf("StyleDecls(%q): no error, want %q", tc.in, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("StyleDecls(%q) error = %q, want containing %q", tc.in, err, tc.want)
			}
		})
	}
}
