package ops

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// GroupCreateSpec is group.create: wrap shapes of one container in a
// new group, as pptx's group.create does and as Visio's Group command
// writes it.
type GroupCreateSpec struct {
	At   []string `json:"at" doc:"The shapes to group: shape addresses as read shows them (page0/s1) or $name, two or more, all in one container (the page or one group)."`
	As   string   `json:"as,omitempty" plan:"optional" doc:"A name for the new group; later operations write $name for it."`
	Name *string  `json:"name,omitempty" plan:"optional" doc:"The group's name, as read shows name."`
}

// GroupUngroupSpec is group.ungroup: move a group's children into its
// container and remove the group.
type GroupUngroupSpec struct {
	At string `json:"at" doc:"A group address as read shows it (page0/s3), or $name."`
}

// DecodePlan implements cli.PlanDecoder for group.create.
func (s *GroupCreateSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "as", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.As) }},
		{Name: "name", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			var str string
			dd.Value(mv, fp, &str)
			s.Name = &str
		}},
	})
}

// DecodePlan implements cli.PlanDecoder for group.ungroup.
func (s *GroupUngroupSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
	})
}

// check reports the first rule group.create breaks (groupCreateRules).
func (s *GroupCreateSpec) check() (string, string, bool) {
	if len(s.At) < 2 {
		return groupCreateRules[0], "at", true
	}
	seen := map[string]bool{}
	for i, a := range s.At {
		field := "at[" + strconv.Itoa(i) + "]"
		if msg, _, bad := needAddress(a, field, "a shape address", vsd.KindShape); bad {
			return msg, field, true
		}
		if seen[a] {
			return groupCreateRules[1], field, true
		}
		seen[a] = true
	}
	if s.As != "" && !cli.ValidAlias(s.As) {
		return groupCreateRules[2], "as", true
	}
	return "", "", false
}

// check reports the first rule group.ungroup breaks.
func (s *GroupUngroupSpec) check() (string, string, bool) {
	return needAddress(s.At, "at", "a shape address", vsd.KindShape)
}

// groupCreateRules are group.create's document-independent checks, in
// the order its check returns them ("at[i] must be a shape address or
// $name" is checked per entry between the first two).
var groupCreateRules = []string{
	"at must list two or more shapes",
	"at must not name a shape twice",
	"as must match [A-Za-z_][A-Za-z0-9_]*",
}

// groupCreateRefuses are group.create's document-dependent refusals.
var groupCreateRefuses = []string{
	"shapes of different containers: group shapes of one container (the page or one group)",
	"masters and stencils: a master shape or a shape in a stencil cannot be edited",
	"containers/lists/callouts and their members (User.msvStructureType, Relationships)",
	"a sub-shape a master instance inherits",
	"scaled-page geometry: a page with a drawing scale",
	"a shape whose transform is not derived (its box cannot be found)",
}

// groupUngroupRules are group.ungroup's document-independent checks.
var groupUngroupRules = []string{
	"at must be a shape address or $name",
}

// groupUngroupRefuses are group.ungroup's document-dependent refusals.
var groupUngroupRefuses = []string{
	"a shape that is not a group, or a master instance (its sub-shapes come from the master)",
	"a group that holds text or shape data, which ungrouping would delete",
	"a group a Connect row names (glued to or from): the glue would point at nothing",
	"masters and stencils, containers/lists/callouts, a scaled page, as group.create",
}

// init registers the help data of group.create and group.ungroup.
func init() {
	registerHelp("group.create", groupCreateRules, groupCreateRefuses)
	registerHelp("group.ungroup", groupUngroupRules, groupUngroupRefuses)
}
