package cli

import (
	"strings"
	"testing"
)

func TestFindOpHelp(t *testing.T) {
	docs := []OpDoc{
		{Name: "text.replace", Summary: "replaces text", Fields: []FieldDoc{{Name: "at", Type: "address", Required: true}}},
		{Name: "text.format", Summary: "formats text"},
		{Name: "duplicate", Summary: "first"},
		{Name: "duplicate", Summary: "second"},
	}
	for _, tc := range []struct {
		name string
		want string
	}{
		{"text.replace", "replaces text"},
		{"text.format", "formats text"},
		{"duplicate", "first"},
	} {
		got, ok := FindOpHelp(docs, tc.name)
		if !ok {
			t.Errorf("FindOpHelp(%q) not found", tc.name)
			continue
		}
		if !strings.Contains(got, tc.want) || !strings.HasPrefix(got, tc.name+"\n") {
			t.Errorf("FindOpHelp(%q) = %q, want entry %q", tc.name, got, tc.want)
		}
	}
	if got, ok := FindOpHelp(docs, "text.replace"); !ok || got != OpHelp(docs[0]) {
		t.Errorf("FindOpHelp(text.replace) = %q, %v; want OpHelp(docs[0])", got, ok)
	}
	for _, docs := range [][]OpDoc{nil, docs} {
		if got, ok := FindOpHelp(docs, "not.an.op"); ok || got != "" {
			t.Errorf("FindOpHelp(not.an.op) = %q, %v; want \"\", false", got, ok)
		}
	}
	if got, ok := FindOpHelp(nil, ""); ok || got != "" {
		t.Errorf("FindOpHelp(nil, \"\") = %q, %v; want \"\", false", got, ok)
	}
}
