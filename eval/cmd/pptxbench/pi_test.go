package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/eval/grade"
)

func TestPiToolsAndArgv(t *testing.T) {
	o := &runOpts{pi: "pi", repo: "/r", model: "p/m", thinking: "low", cond: condition{"json", false}}
	got := strings.Join(piToolNames(o.cond, grade.Identify), ",")
	if got != "pptx_list,pptx_read,pptx_xml,submit_answer" {
		t.Errorf("identify tools %s", got)
	}
	o.cond.Render = true
	got = strings.Join(piToolNames(o.cond, grade.Connect), ",")
	if got != "pptx_list,pptx_read,pptx_xml,pptx_apply,pptx_read_output,pptx_render" {
		t.Errorf("edit+render tools %s", got)
	}
	argv := strings.Join(piArgv(o, grade.Connect, "i.md", "prompt"), " ")
	for _, want := range []string{"--no-builtin-tools", "-e /r/eval/pi/pptx-extension.mjs", "--tools " + got} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv lacks %q: %s", want, argv)
		}
	}
	for _, bad := range []string{"bash", "edit,", ",read", "write"} {
		if strings.Contains(argv, bad) {
			t.Errorf("argv mentions %q: %s", bad, argv)
		}
	}
	env := strings.Join(piExtEnv(o, grade.Connect, "/b", "/b/work"), " ")
	if !strings.Contains(env, "PPTXBENCH_RENDER=1") || !strings.Contains(env, "PPTXBENCH_TASK=edit") || !strings.Contains(env, "PPTXBENCH_VIEW=json") {
		t.Errorf("env %s", env)
	}
}

func TestCheckPiToolArgs(t *testing.T) {
	tj := &TrialJSON{ToolCalls: ToolCalls{ByTool: map[string]int{}}}
	tj.checkPiTool("pptx_read", map[string]any{"slide": "slide1"}, "/w", "/repo")
	if tj.Leak || len(tj.AccessNotes) != 0 {
		t.Errorf("clean call noted: %+v", tj)
	}
	tj.checkPiTool("pptx_xml", map[string]any{"address": "/repo/eval/tasks/x/task.json"}, "/w", "/repo")
	if !tj.Leak || len(tj.AccessNotes) != 1 {
		t.Errorf("leak not detected: %+v", tj)
	}
}

// TestPiExtension runs the extension against the real pptx binary with a
// fake pi (no model). Rendering is covered when soffice is available.
func TestPiExtension(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	if testing.Short() {
		t.Skip("builds pptx")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "pptx")
	build := exec.Command("go", "build", "-o", bin, "./cmd/pptx")
	build.Dir = "../../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	deck, _ := filepath.Abs("../../../testdata/soffice/diagram.pptx")
	if _, err := os.Stat(deck); err != nil {
		t.Skip("no test deck")
	}
	cmd := exec.Command(node, "../../pi/extension_test.mjs", bin, deck)
	if _, err := exec.LookPath("soffice"); err == nil {
		cmd.Env = append(os.Environ(), "PPTXBENCH_TEST_RENDER=1")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestParsePiEventsRetried checks that a request error pi recovered from by
// retrying (auto_retry_start/end, then a normal final message) is not a
// provider error. The fixture is a real trial's event stream.
func TestParsePiEventsRetried(t *testing.T) {
	b, err := os.ReadFile("testdata/pi-retried.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var tj TrialJSON
	tj.ToolCalls.ByTool = map[string]int{}
	parsePiEvents(b, &tj, "/work", "/repo")
	if tj.Status != statusOK {
		t.Fatalf("status %q (error %q), want ok", tj.Status, tj.Error)
	}
	if tj.StopReason != "stop" {
		t.Fatalf("stop reason %q, want stop", tj.StopReason)
	}

	// The same stream cut after the failed request is a provider error.
	cut := b[:bytes.Index(b, []byte(`"auto_retry_start"`))]
	cut = cut[:bytes.LastIndexByte(cut, '\n')+1]
	var tj2 TrialJSON
	tj2.ToolCalls.ByTool = map[string]int{}
	parsePiEvents(cut, &tj2, "/work", "/repo")
	if tj2.Status != statusProviderError {
		t.Fatalf("cut stream: status %q, want %q", tj2.Status, statusProviderError)
	}
}
