package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectTasks(t *testing.T) {
	ts, err := selectTasks("../../tasks", "id-ow-*,id-sp7-twin-links,id-ow-python")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, x := range ts {
		ids = append(ids, x.ID)
	}
	if got := strings.Join(ids, ","); got != "id-ow-gateway,id-ow-python,id-sp7-twin-links" {
		t.Errorf("selected %s", got)
	}
	if _, err := selectTasks("../../tasks", "nope-*"); err == nil {
		t.Error("expected an error for a glob with no match")
	}
}

func TestWrapper(t *testing.T) {
	dir := t.TempDir()
	realBin := filepath.Join(dir, "realBin")
	// The stand-in real binary echoes its arguments.
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\necho \"real: $@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(c condition, args ...string) (string, error) {
		p := filepath.Join(dir, "pptx-"+c.Name())
		if err := os.WriteFile(p, []byte(wrapperScript(realBin, c)), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(p, args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	j, js := condition{"json", false}, condition{"json", true}
	if out, err := run(j, "read", "d.pptx", "--slide", "slide1"); err != nil || out != "real: read d.pptx --slide slide1 --format json" {
		t.Errorf("json read: %q %v", out, err)
	}
	if out, err := run(j, "read", "d.pptx"); err != nil || out != "real: read d.pptx" {
		t.Errorf("json list: %q %v", out, err)
	}
	if out, err := run(condition{"svg", false}, "read", "d.pptx", "--slide=slide1"); err != nil || out != "real: read d.pptx --slide=slide1 --format svg" {
		t.Errorf("svg read: %q %v", out, err)
	}
	if out, err := run(j, "read", "d.pptx", "--slide", "slide1", "--format", "svg"); err == nil || !strings.Contains(out, `{"errors":[{"code":"usage","message":"--format is not available`) {
		t.Errorf("json read --format svg: %q %v", out, err)
	}
	if out, err := run(j, "help", "ops"); err != nil || out != "real: help ops" {
		t.Errorf("help ops: %q %v", out, err)
	}
	if out, err := run(j, "render", "d.pptx"); err == nil || !strings.Contains(out, "render is not available") {
		t.Errorf("render without access: %q %v", out, err)
	}
	if out, err := run(js, "render", "d.pptx", "--slide", "slide1"); err != nil || out != "real: render d.pptx --slide slide1" {
		t.Errorf("render with access: %q %v", out, err)
	}
	if out, _ := run(j, "--help"); strings.Contains(out, "render") || strings.Contains(out, "svg") {
		t.Errorf("help leaks the other options: %q", out)
	}
}

func TestParseTranscript(t *testing.T) {
	const out = `{"type":"system","subtype":"init","cwd":"/w","tools":["Bash"],"mcp_servers":[],"slash_commands":[],"skills":[],"plugins":[{"source":"x@builtin"}],"session_id":"s1"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"pptx read input.pptx"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"pptx read input.pptx"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"cat /Users/x/.claude/CLAUDE.md"}}]}}
{"type":"result","subtype":"success","is_error":false,"result":"s216","total_cost_usd":0.01,"num_turns":3,"terminal_reason":"completed","usage":{"input_tokens":5,"cache_creation_input_tokens":100,"cache_read_input_tokens":200,"output_tokens":40,"output_tokens_details":{"thinking_tokens":7}},"modelUsage":{"claude-haiku-5-5":{}}}
`
	tj := &TrialJSON{ToolCalls: ToolCalls{ByTool: map[string]int{}}}
	parseTranscript([]byte(out), tj, "/w", condition{"json", false})
	if tj.Status != statusOK || tj.FinalAnswer != "s216" || tj.Model != "claude-haiku-5-5" {
		t.Errorf("status/answer/model: %+v", tj)
	}
	if tj.ToolCalls.Count != 2 || tj.ToolCalls.ByTool["Bash"] != 2 || tj.ToolCalls.Errors != 1 {
		t.Errorf("tool calls: %+v", tj.ToolCalls)
	}
	if tj.Tokens != (Tokens{Input: 5, CachedInput: 200, CacheWrite: 100, Output: 40, Reasoning: 7}) || tj.CostUSD != 0.01 || tj.Turns != 3 {
		t.Errorf("tokens/cost: %+v", tj)
	}
	if len(tj.Isolation) != 0 || len(tj.AccessNotes) != 1 {
		t.Errorf("isolation %v notes %v", tj.Isolation, tj.AccessNotes)
	}
	// An init that exposes a Read tool outside render conditions, or an MCP server, is an isolation problem.
	bad := `{"type":"system","subtype":"init","cwd":"/w","tools":["Bash","Read","WebFetch"],"mcp_servers":[{}],"plugins":[{"source":"p@market"}]}` + "\n"
	tj = &TrialJSON{ToolCalls: ToolCalls{ByTool: map[string]int{}}}
	parseTranscript([]byte(bad), tj, "/w", condition{"json", false})
	if len(tj.Isolation) != 4 || tj.Status != statusRunnerError {
		t.Errorf("bad init: %v %s", tj.Isolation, tj.Status)
	}
	// A provider error is a harness error.
	pe := `{"type":"result","subtype":"error_during_execution","is_error":true,"api_error_status":529,"result":"overloaded"}` + "\n"
	tj = &TrialJSON{ToolCalls: ToolCalls{ByTool: map[string]int{}}}
	parseTranscript([]byte(pe), tj, "/w", condition{"json", false})
	if tj.Status != statusProviderError {
		t.Errorf("provider error: %s", tj.Status)
	}
}

func TestClaudeArgvIsolation(t *testing.T) {
	o := &runOpts{claude: "claude", model: "claude-haiku-5-5", cond: condition{"svg", true}}
	got := " " + strings.Join(claudeArgv(o, "/w", "I"), " ") + " "
	for _, want := range []string{" --safe-mode ", " --setting-sources  ", " --strict-mcp-config ", " --disable-slash-commands ",
		" --no-session-persistence ", " --tools Bash,Read ", " --permission-mode dontAsk ", "Read(//w/**)"} {
		if !strings.Contains(got, want) {
			t.Errorf("argv lacks %q: %s", want, got)
		}
	}
	o.cond.Render = false
	if got := strings.Join(claudeArgv(o, "/w", "I"), " "); strings.Contains(got, "Read") {
		t.Errorf("Read offered without render: %s", got)
	}
}
