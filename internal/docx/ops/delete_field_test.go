package ops

import (
	"errors"
	"testing"
)

// TestAccountFor: html accounts for every equation and placeholder of the
// content it replaces, by sending it back or listing it in delete.
func TestAccountFor(t *testing.T) {
	have := []string{"0000000A/equation:1", "0000000A/opaque:1", "0000000A/opaque:2"}
	for _, c := range []struct {
		name        string
		sent, del   []string
		code, field string
	}{
		{"all sent", have, nil, "", ""},
		{"one deleted", have[:2], []string{"0000000a/opaque:2"}, "", ""},
		{"one left out", have[1:], nil, "operation_failed", "html"},
		{"delete names nothing", have, []string{"0000000A/opaque:3"}, "not_found", "delete[0]"},
		{"sent and deleted", have, []string{"0000000A/opaque:1"}, "invalid_operation", "delete[0]"},
	} {
		err := accountFor(have, c.sent, c.del, "html")
		var r *Rejection
		switch {
		case c.code == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.code != "" && (!errors.As(err, &r) || r.Code != c.code || r.Field != c.field):
			t.Errorf("%s: %v, want %s at %s", c.name, err, c.code, c.field)
		}
	}
	err := accountFor(have, nil, nil, "html")
	var r *Rejection
	if !errors.As(err, &r) || len(r.Matches) != 3 {
		t.Errorf("left out: %v", err)
	}
}

// TestDeleteRule: delete's entries are content addresses of the op's
// form, each once.
func TestDeleteRule(t *testing.T) {
	def, _ := Lookup("paragraph.replace")
	for _, c := range []struct {
		del []string
		bad bool
	}{
		{[]string{"0000000A/equation:1", "0000000A/opaque:2"}, false},
		{[]string{"opaque:1"}, true},
		{[]string{"0000000A/opaque:1", "0000000a/opaque:1"}, true},
		{[]string{"0000000A"}, true},
	} {
		_, _, bad := def.check(&ReplaceParagraph{At: "0000000A", HTML: "x", Delete: c.del})
		if bad != c.bad {
			t.Errorf("%v: bad %v", c.del, bad)
		}
	}
	h, _ := Lookup("header.set")
	if _, _, bad := h.check(&SetHeader{HTML: "x", Delete: []string{"opaque:1", "equation:2"}}); bad {
		t.Error("header delete refused")
	}
	if _, _, bad := h.check(&SetHeader{HTML: "x", Delete: []string{"0000000A/opaque:1"}}); !bad {
		t.Error("header delete with a paragraph address accepted")
	}
}
