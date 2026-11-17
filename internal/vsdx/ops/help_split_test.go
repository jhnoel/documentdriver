package ops

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// TestHelpRequiredMatchesDecoder checks that the required/optional
// split help prints (cli.FieldDocs over the spec's tags) is the one the
// decoder enforces: decoding an op with no fields reports "missing
// field" for exactly the fields help calls required (review B3: help
// marked optional fields required).
func TestHelpRequiredMatchesDecoder(t *testing.T) {
	for i := range Ops {
		def := &Ops[i]
		var help []string
		for _, f := range cli.FieldDocs(reflect.TypeOf(def.New()).Elem(), nil) {
			if f.Required {
				help = append(help, f.Name)
			}
		}
		_, errs := ParsePlan([]byte(`{"ops":[{"op":"` + def.Name + `"}]}`))
		var missing []string
		for _, e := range errs {
			if e.Code == cli.CodeInvalidPlan && strings.HasPrefix(e.Message, "missing field") {
				missing = append(missing, strings.TrimPrefix(e.Path, "ops[0]."))
			}
		}
		slices.Sort(help)
		slices.Sort(missing)
		if !slices.Equal(help, missing) {
			t.Errorf("%s: help requires %v, the decoder requires %v", def.Name, help, missing)
		}
	}
}

// TestHelpOptionalInCreatePlans checks that every field the create
// plans c2 to c6 leave out of an op is optional in help (none of them
// may omit a required field and still decode).
func TestHelpOptionalInCreatePlans(t *testing.T) {
	for _, name := range []string{"c2-shapes", "c3-pages", "c4-refused-master", "c5-refused-glue", "c6-refused-base"} {
		p, errs := ParsePlan(createPlan(t, name+".json"))
		if len(errs) != 0 {
			t.Fatalf("%s: %v", name, errs)
		}
		for i, s := range p.Ops {
			def, ok := index().Of(s)
			if !ok {
				t.Fatalf("%s ops[%d]: no table entry", name, i)
			}
			for _, f := range cli.FieldDocs(reflect.TypeOf(def.New()).Elem(), nil) {
				if f.Required && reflect.ValueOf(s).Elem().FieldByNameFunc(func(n string) bool {
					sf, _ := reflect.TypeOf(s).Elem().FieldByName(n)
					tag, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
					return tag == f.Name
				}).IsZero() {
					t.Errorf("%s ops[%d] %s: required field %s is empty", name, i, def.Name, f.Name)
				}
			}
		}
	}
}
