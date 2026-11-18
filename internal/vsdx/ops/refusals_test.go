package ops_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// refusalOut runs plan on path and returns its result, failing the
// test when apply fails.
func refusalOut(t *testing.T, path, plan string) (out string, changed []string) {
	t.Helper()
	out = filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, plan), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run(%s): %v", plan, err)
	}
	for _, c := range res.Changed {
		changed = append(changed, string(c))
	}
	return out, changed
}

// expectRefusal checks that plan on path fails with operation_failed
// at ops[0].field and a message containing want.
func expectRefusal(t *testing.T, path, plan, field, want string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err := apply.Run(path, parsePlan(t, plan), apply.Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("%s: error %v, want *cli.Error", plan, err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0]."+field {
		t.Errorf("%s: got %s at %q, want operation_failed at ops[0].%s", plan, ce.Code, ce.Path, field)
	}
	if !strings.Contains(ce.Message, want) {
		t.Errorf("%s: message %q, want it to contain %q", plan, ce.Message, want)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("%s: a refused op wrote its out file", plan)
	}
}

// v1Structure builds V1 with s1's User.msvStructureType set to
// structure and, when second holds, a second plain shape (s2) with
// the same text, returning the drawing's path.
func v1Structure(t *testing.T, structure string, second bool) string {
	t.Helper()
	raw, err := laws.Parts(vsdxtest.Fixture(t, "v1-minimal"))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string][]byte{}
	for n, b := range raw {
		parts[strings.TrimPrefix(n, "/")] = b
	}
	page := string(parts["visio/pages/page1.xml"])
	if second {
		start := strings.Index(page, "<Shape ID='1'")
		end := strings.Index(page, "</Shape>") + len("</Shape>")
		if start < 0 || end < len("</Shape>") {
			t.Fatalf("V1 page1.xml holds no s1 shape: %q", page)
		}
		dup := strings.Replace(page[start:end], "ID='1'", "ID='2'", 1)
		page = page[:end] + dup + page[end:]
	}
	section := `<Section N='User'><Row N='msvStructureType'><Cell N='Value' V='` + structure + `' U='STR'/><Cell N='Prompt' V=''/></Row></Section>`
	if page = strings.Replace(page, "</Text></Shape>", "</Text>"+section+"</Shape>", 1); !strings.Contains(page, section) {
		t.Fatalf("insert User section into V1 page: %q", page)
	}
	parts["visio/pages/page1.xml"] = []byte(page)
	names := make([]string, 0, len(parts))
	for n := range parts {
		if n != "[Content_Types].xml" {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	members := []opc.ZipMember{{Name: "[Content_Types].xml", Body: string(parts["[Content_Types].xml"])}}
	for _, n := range names {
		members = append(members, opc.ZipMember{Name: n, Body: string(parts[n])})
	}
	b, err := opc.BuildZip(members...)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "v1-structure.vsdx")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRefusalsContainerV1 checks the CC6 rows: a test-built container
// on V1 (User row msvStructureType "Container") refuses each listed
// op with the container message at at.
func TestRefusalsContainerV1(t *testing.T) {
	path := v1Structure(t, "Container", false)
	want := "page0/s1 is a container (User.msvStructureType): containers, lists and callouts are not edited in v1"
	for _, plan := range []string{
		`{"ops":[{"op":"object.set","at":"page0/s1","box":[72,90,144,72]}]}`,
		`{"ops":[{"op":"object.set","at":"page0/s1","fill":{"hex":"#00FF00"}}]}`,
		`{"ops":[{"op":"object.move","at":"page0/s1","dx":72}]}`,
		`{"ops":[{"op":"object.delete","at":"page0/s1"}]}`,
		`{"ops":[{"op":"object.reorder","at":"page0/s1","to":"front"}]}`,
		`{"ops":[{"op":"text.set","at":"page0/s1","html":"<p>Bye</p>"}]}`,
		`{"ops":[{"op":"text.replace","at":"page0/s1","select":"Hello","with":"Bye"}]}`,
		`{"ops":[{"op":"prop.add","at":"page0/s1","name":"Cost","type":"number","value":5}]}`,
		`{"ops":[{"op":"prop.set","at":"page0/s1/prop:Cost","value":5}]}`,
		`{"ops":[{"op":"prop.delete","at":"page0/s1/prop:Cost"}]}`,
		`{"ops":[{"op":"object.add","at":"page0/s1","geom":"rect","box":[0,0,72,36]}]}`,
	} {
		expectRefusal(t, path, plan, "at", want)
	}
}

// TestRefusalsContainerSkippedByPageReplace checks the CC6 row: a
// page-wide text.replace skips the container (not an error) and
// replaces the second shape's matching text.
func TestRefusalsContainerSkippedByPageReplace(t *testing.T) {
	path := v1Structure(t, "Container", true)
	_, changed := refusalOut(t, path, `{"ops":[{"op":"text.replace","at":"page0","select":"Hello","with":"Bye","all":true}]}`)
	if len(changed) != 1 || !strings.Contains(changed[0], "page0/s2") {
		t.Errorf("changed = %v, want only page0/s2 (the container skipped)", changed)
	}
}

// TestRefusalsListAndCallout checks the CC6 rows: List and Callout
// are structured like Container, so object.move on them is refused.
func TestRefusalsListAndCallout(t *testing.T) {
	want := "page0/s1 is a container (User.msvStructureType): containers, lists and callouts are not edited in v1"
	for _, structure := range []string{"List", "Callout"} {
		expectRefusal(t, v1Structure(t, structure, false), `{"ops":[{"op":"object.move","at":"page0/s1","dx":72}]}`, "at", want)
	}
}

// TestRefusalsHeadingNotStructured checks the CC6 row: Heading and 0
// are not structured, so object.move on such a shape is accepted.
func TestRefusalsHeadingNotStructured(t *testing.T) {
	for _, structure := range []string{"Heading", "0"} {
		path := v1Structure(t, structure, false)
		_, changed := refusalOut(t, path, `{"ops":[{"op":"object.move","at":"page0/s1","dx":72}]}`)
		if len(changed) != 1 {
			t.Errorf("%s: changed = %v, want page0/s1 moved", structure, changed)
		}
	}
}

// TestRefusalsV13Scaled checks the CC6 rows: V13 object.set box is
// refused at box with the scaled message, object.set fill is
// accepted, and object.move, object.add and page.set size are
// refused while text.set is accepted.
func TestRefusalsV13Scaled(t *testing.T) {
	path := vsdxtest.Fixture(t, "v13-scale")
	want := "page0 has a drawing scale (1 in = 120 ft): geometry is not edited on scaled pages in v1"
	expectRefusal(t, path, `{"ops":[{"op":"object.set","at":"page0/s1","box":[72,90,144,72]}]}`, "box", want)
	expectRefusal(t, path, `{"ops":[{"op":"object.move","at":"page0/s1","dx":72}]}`, "at", want)
	expectRefusal(t, path, `{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,72,36]}]}`, "at", want)
	expectRefusal(t, path, `{"ops":[{"op":"page.set","at":"page0","size":[792,612]}]}`, "size", want)
	if _, changed := refusalOut(t, path, `{"ops":[{"op":"object.set","at":"page0/s1","fill":{"hex":"#00FF00"}}]}`); len(changed) != 1 {
		t.Errorf("object.set fill on V13: changed = %v, want accepted", changed)
	}
	if _, changed := refusalOut(t, path, `{"ops":[{"op":"text.set","at":"page0/s1","html":"<p>Bye</p>"}]}`); len(changed) != 1 {
		t.Errorf("text.set on V13: changed = %v, want accepted", changed)
	}
}

// corpusDiagramPath returns the path of apache-poi test-data/diagram
// test.vsdx under VSDX_CORPUS, trying the worktree-root, corpus and
// repos-directory layouts. It skips when VSDX_CORPUS is unset and
// fails naming the path when it is set but the file is missing.
func corpusDiagramPath(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("VSDX_CORPUS")
	if dir == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	rel := filepath.Join("apache-poi", "test-data", "diagram", "test.vsdx")
	for _, cand := range []string{
		filepath.Join(dir, "repos", rel),
		filepath.Join(dir, "corpus", "vsdx", "repos", rel),
		filepath.Join(dir, rel),
	} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	t.Fatalf("corpus file %s not found under VSDX_CORPUS %s", filepath.Join(dir, "repos", rel), dir)
	return ""
}

// TestRefusalsCorpusMemberS8 checks the CC6 row: test.vsdx page0/s8
// refuses object.set box with the member message and accepts
// object.set fill.
func TestRefusalsCorpusMemberS8(t *testing.T) {
	path := corpusDiagramPath(t)
	expectRefusal(t, path, `{"ops":[{"op":"object.set","at":"page0/s8","box":[72,90,144,72]}]}`, "at",
		"page0/s8 is in a container or list (Relationships): its geometry and membership are not edited in v1")
	if _, changed := refusalOut(t, path, `{"ops":[{"op":"object.set","at":"page0/s8","fill":{"hex":"#00FF00"}}]}`); len(changed) != 1 {
		t.Errorf("object.set fill on s8: changed = %v, want accepted", changed)
	}
}

// TestRefusalsCorpusStructureCounts checks the CC6 row: the corpus
// files' Container and List shapes refuse object.move, with the
// counts logged.
func TestRefusalsCorpusStructureCounts(t *testing.T) {
	path := corpusDiagramPath(t)
	page := readPage(t, path)
	var containers, members int
	for _, obj := range page.Objects {
		out := filepath.Join(t.TempDir(), "out.vsdx")
		_, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"`+obj.ID+`","dx":72}]}`), apply.Options{Out: out})
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed {
			continue
		}
		switch {
		case strings.Contains(ce.Message, "is a container (User.msvStructureType)"):
			containers++
		case strings.Contains(ce.Message, "is in a container or list (Relationships)"):
			members++
		}
	}
	t.Logf("test.vsdx page0: %d container/list shapes and %d container members refused object.move", containers, members)
	if containers == 0 {
		t.Error("no Container or List shape refused object.move")
	}
	if members == 0 {
		t.Error("no container member refused object.move")
	}
}

// TestPageReplaceOnlyInSkippedShapeNotFound checks the row: a
// page-wide text.replace whose only match is in a container, list or
// callout (skipped) is not_found at select, not a silent no-op.
func TestPageReplaceOnlyInSkippedShapeNotFound(t *testing.T) {
	for _, structure := range []string{"Container", "List", "Callout"} {
		path := v1Structure(t, structure, false)
		expectRefusalCode(t, path, `{"ops":[{"op":"text.replace","at":"page0","select":"Hello","with":"Bye","all":true}]}`, "not_found", "select")
	}
}

// expectRefusalCode runs plan on path and wants code at ops[0].field,
// with no out file written.
func expectRefusalCode(t *testing.T, path, plan, code, field string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err := apply.Run(path, parsePlan(t, plan), apply.Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("%s: error %v, want *cli.Error %s", plan, err, code)
	}
	if string(ce.Code) != code || ce.Path != "ops[0]."+field {
		t.Errorf("%s: got %s at %q, want %s at ops[0].%s", plan, ce.Code, ce.Path, code, field)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("%s: a refused op wrote its out file", plan)
	}
}
