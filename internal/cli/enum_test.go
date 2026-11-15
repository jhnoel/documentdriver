package cli

import "testing"

// TestDecodeEnum checks that DecodeEnum accepts a variant, refuses a
// non-string and a non-variant with the contract's messages, and leaves
// the destination unchanged on refusal.
func TestDecodeEnum(t *testing.T) {
	variants := []string{"track", "direct"}
	newDec := func() *Decoder { return NewDecoder([]byte(`{"m":"track"}`)) }

	t.Run("accept", func(t *testing.T) {
		d := newDec()
		var mode string
		if !DecodeEnum(d, &Value{Kind: String, Str: "direct"}, "change_mode", variants, &mode) || mode != "direct" {
			t.Errorf("DecodeEnum accepted: got %q, err %v", mode, d.Err())
		}
		if err := d.Err(); err != nil {
			t.Errorf("DecodeEnum accepted: err %v", err)
		}
	})

	t.Run("refuse non-string", func(t *testing.T) {
		d := newDec()
		mode := "keep"
		if DecodeEnum(d, &Value{Kind: Number, Num: "1"}, "change_mode", variants, &mode) {
			t.Error("DecodeEnum accepted a number")
		}
		if mode != "keep" {
			t.Errorf("DecodeEnum refusal changed dst to %q", mode)
		}
		errs := d.Errs()
		if len(errs) != 1 || errs[0].Message != "expected a string, found a number" {
			t.Errorf("DecodeEnum non-string fault: %v", errs)
		}
	})

	t.Run("refuse non-variant", func(t *testing.T) {
		d := newDec()
		mode := "keep"
		if DecodeEnum(d, &Value{Kind: String, Str: "bogus"}, "change_mode", variants, &mode) {
			t.Error("DecodeEnum accepted a non-variant")
		}
		if mode != "keep" {
			t.Errorf("DecodeEnum refusal changed dst to %q", mode)
		}
		errs := d.Errs()
		if len(errs) != 1 || errs[0].Message != `"bogus" is not one of track, direct` {
			t.Errorf("DecodeEnum non-variant fault: %v", errs)
		}
	})

	// TestJoin checks that Join appends a name to a path, an empty path
	// becoming the name and an empty name the path.
	t.Run("join", func(t *testing.T) {
		cases := []struct{ path, name, want string }{
			{"ops[0]", "at", "ops[0].at"},
			{"", "author", "author"},
			{"ops[0]", "", "ops[0]"},
		}
		for _, c := range cases {
			if got := Join(c.path, c.name); got != c.want {
				t.Errorf("Join(%q, %q) = %q, want %q", c.path, c.name, got, c.want)
			}
		}
	})
}
