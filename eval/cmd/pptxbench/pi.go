package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jhnoel/documentdriver/eval/grade"
)

// The Pi runner drives the Pi CLI (`pi --print --mode json`). The model
// gets no bash, read, edit or write tool: only the tools of the pptx
// extension (eval/pi/pptx-extension.mjs), which run the pptx binary on fixed
// files in the trial's work directory. The model never supplies a path, so
// it cannot reach the repository, the results or the home directory. The
// trial's configuration (view, render, work dir, binary) reaches the
// extension through the environment of the pi process, which the model has
// no tool to read or change. Tool arguments are checked afterwards
// (access_notes; a mention of task.json or a rubric is a "leak", a harness
// error) as a second line of defence.

var thinkingLevels = map[string]bool{"off": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// systemPath is the PATH the agent gets after the wrapper directory.
const systemPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// runnerName returns the runner identifier for the configured runner.
func (o *runOpts) runnerName() string {
	if o.runner == "pi" {
		return piRunnerName
	}
	return runnerName
}

// checkRunner validates --runner, --model and --thinking and applies the
// model default.
func (o *runOpts) checkRunner() error {
	switch o.runner {
	case "claude":
		if o.thinking != "" {
			return fmt.Errorf("run: --thinking applies to --runner pi only")
		}
		if o.model == "" {
			o.model = "claude-haiku-5-5"
		}
	case "pi":
		p, m, ok := strings.Cut(o.model, "/")
		if !ok || p == "" || m == "" {
			return fmt.Errorf("run: --runner pi needs --model provider/id (got %q)", o.model)
		}
		if o.thinking == "" {
			return fmt.Errorf("run: --runner pi needs --thinking off|minimal|low|medium|high|xhigh|max")
		}
		if !thinkingLevels[o.thinking] {
			return fmt.Errorf("run: --thinking %q: want off|minimal|low|medium|high|xhigh|max", o.thinking)
		}
		if o.maxBudget > 0 {
			return fmt.Errorf("run: --max-budget-usd applies to --runner claude only")
		}
	default:
		return fmt.Errorf("run: --runner must be pi or claude")
	}
	return nil
}

// piVersion returns the pi executable's version, or "unknown" on failure.
func piVersion(pi string) string {
	b, err := exec.Command(pi, "--version").CombinedOutput()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

// extensionPath is the Pi extension that provides the pptx tools.
func extensionPath(repo string) string {
	return filepath.Join(repo, "eval", "pi", "pptx-extension.mjs")
}

// piPrompt is the user message: the task text and a pointer to the
// appended instructions. The work directory is not named: tools use fixed
// files and the model never needs a path.
func piPrompt(task string) string {
	return strings.TrimRight(task, "\n") + "\nFollow the appended instructions.\n"
}

// piToolNames are the only tools the model gets, for a task and condition.
func piToolNames(c condition, t string) []string {
	n := []string{"pptx_list", "pptx_read", "pptx_xml"}
	if t == grade.Identify {
		n = append(n, "submit_answer")
	} else {
		n = append(n, "pptx_apply", "pptx_read_output")
	}
	if c.Render {
		n = append(n, "pptx_render")
	}
	return n
}

// piArgv is the pi command line; instrFile is the --append-system-prompt file.
func piArgv(o *runOpts, t string, instrFile, prompt string) []string {
	return []string{o.pi, "--print", "--mode", "json", "--no-session", "--offline",
		"--no-extensions", "--no-mcp", "--no-skills", "--no-context-files",
		"--no-prompt-templates", "--no-themes",
		"--no-builtin-tools", "-e", extensionPath(o.repo),
		"--tools", strings.Join(piToolNames(o.cond, t), ","),
		"--model", o.model, "--thinking", o.thinking,
		"--append-system-prompt", instrFile, prompt}
}

// piExtEnv is the extension's configuration (see the extension).
func piExtEnv(o *runOpts, t string, base, work string) []string {
	task := "edit"
	if t == grade.Identify {
		task = "identify"
	}
	render := "0"
	if o.cond.Render {
		render = "1"
	}
	return []string{"PPTXBENCH_PPTX=" + filepath.Join(base, "lib", "pptx"), "PPTXBENCH_WORK=" + work,
		"PPTXBENCH_VIEW=" + o.cond.View, "PPTXBENCH_RENDER=" + render, "PPTXBENCH_TASK=" + task}
}

// piEnvNote returns the trial environment description for display purposes.
func piEnvNote(o *runOpts, t string, base, work string) string {
	return fmt.Sprintf("PATH=%s:%s/node HOME=%s/home PI_CODING_AGENT_DIR=%s/pi-config PI_OFFLINE=1 %s",
		systemPath, base, base, base, strings.Join(piExtEnv(o, t, base, work), " "))
}

// piAuthFile is the user's credential store, which is only symlinked into
// the per-trial config dir.
func piAuthFile() string {
	dir := os.Getenv("PI_CODING_AGENT_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".pi", "agent")
	}
	return filepath.Join(dir, "auth.json")
}

// runPi runs pi for one trial and fills tj. It reports false when the
// trial cannot continue to collection and grading.
func runPi(o *runOpts, s trialSpec, tj *TrialJSON, work, _, tdir, instr string) bool {
	base := filepath.Dir(work)
	cfg, home, tmp := filepath.Join(base, "pi-config"), filepath.Join(base, "home"), filepath.Join(base, "tmp")
	for _, d := range []string{cfg, home, tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			tj.Error = "prepare: " + err.Error()
			return false
		}
	}
	if realBin, err := filepath.EvalSymlinks(piAuthFile()); err == nil {
		if err := os.Symlink(realBin, filepath.Join(cfg, "auth.json")); err != nil {
			tj.Error = "prepare: " + err.Error()
			return false
		}
	}
	instrFile := filepath.Join(base, "instructions.md")
	if err := os.WriteFile(instrFile, []byte(instr), 0o644); err != nil {
		tj.Error = "prepare: " + err.Error()
		return false
	}
	prompt := piPrompt(s.Task.ToolMarkdown())
	argv := piArgv(o, s.Task.Type, instrFile, prompt)
	tj.Command = piArgv(o, s.Task.Type, "instructions.md", prompt)

	// The managed pi launcher finds its Node under $XDG_DATA_HOME, which
	// defaults to $HOME/.local/share; keep pointing it at the real one.
	realHome, _ := os.UserHomeDir()
	xdg := os.Getenv("XDG_DATA_HOME")
	if xdg == "" {
		xdg = filepath.Join(realHome, ".local", "share")
	}
	// pi is a Node script; give it a PATH directory holding only node.
	nodeDir := filepath.Join(base, "node")
	if np, err := exec.LookPath("node"); err == nil {
		if realBin, err := filepath.EvalSymlinks(np); err == nil {
			if err := os.MkdirAll(nodeDir, 0o755); err != nil {
				tj.Error = "prepare: " + err.Error()
				return false
			}
			if err := os.Symlink(realBin, filepath.Join(nodeDir, "node")); err != nil {
				tj.Error = "prepare: " + err.Error()
				return false
			}
		}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = work
	cmd.Env = append(piExtEnv(o, s.Task.Type, base, work),
		"PATH="+systemPath+string(os.PathListSeparator)+nodeDir,
		"HOME="+home,
		"TMPDIR="+tmp,
		"XDG_DATA_HOME="+xdg,
		"PI_CODING_AGENT_DIR="+cfg,
		"PI_OFFLINE=1",
		"LANG=en_US.UTF-8")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	start := time.Now()
	timedOut := false
	if err := cmd.Start(); err != nil {
		tj.Error = "start pi: " + err.Error()
		return false
	}
	pgid := cmd.Process.Pid
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var err error
	select {
	case err = <-waitErr:
	case <-time.After(o.timeout):
		timedOut = true
		syscall.Kill(-pgid, syscall.SIGTERM)
		select {
		case err = <-waitErr:
		case <-time.After(5 * time.Second):
			syscall.Kill(-pgid, syscall.SIGKILL)
			err = <-waitErr
		}
	}
	// Reap anything the agent left behind (soffice children).
	syscall.Kill(-pgid, syscall.SIGKILL)
	tj.WallSeconds = time.Since(start).Seconds()
	exit := -1
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}
	tj.ExitCode = &exit

	tj.write(filepath.Join(tdir, "transcript.jsonl"), stdout.Bytes())
	tj.write(filepath.Join(tdir, "stderr.txt"), stderr.Bytes())
	events := parsePiEvents(stdout.Bytes(), tj, work, o.repo)
	tj.write(filepath.Join(tdir, "transcript.md"), []byte(piTranscriptMD(events)))

	switch {
	case timedOut:
		tj.Status = statusTimeout
		tj.Error = fmt.Sprintf("killed after %s", o.timeout)
	case tj.Status == statusProviderError:
	case providerFailure(stderr.String()) && (tj.Turns == 0 || exit != 0):
		tj.Status = statusProviderError
		tj.Error = "stderr: " + tail(stderr.String(), 400)
	case tj.Status == statusOK:
	default:
		tj.Status = statusRunnerError
		tj.Error = fmt.Sprintf("pi exit %d (%v): %s", exit, err, tail(stderr.String(), 400))
	}
	tj.ToolsActive = activeTools(stderr.String())
	if tj.Status == statusOK {
		want := strings.Join(piToolNames(o.cond, s.Task.Type), ",")
		got := append([]string(nil), tj.ToolsActive...)
		sort.Strings(got)
		w := strings.Split(want, ",")
		sort.Strings(w)
		if strings.Join(got, ",") != strings.Join(w, ",") {
			tj.Status = statusRunnerError
			tj.Error = fmt.Sprintf("tool set: want %s, pi reported %v", want, tj.ToolsActive)
		}
	}
	if tj.Status == statusOK && tj.Leak {
		tj.Status = statusRunnerError
		tj.Error = "leak: the agent mentioned task.json or a rubric"
	}
	if tj.Status == statusOK && tj.ModelMatch != nil && !*tj.ModelMatch {
		tj.Status = statusRunnerError
		tj.Error = fmt.Sprintf("model mismatch: requested %s, observed %s", tj.ModelRequested, tj.ModelObserved)
	}
	return true
}

var providerErrRE = regexp.MustCompile(`(?i)api key|unauthori[sz]ed|authenticat|\b40[13]\b|\b429\b|\b5\d\d\b|rate limit|credential|no models? available|overloaded|quota|billing`)

// providerFailure reports whether stderr text indicates a provider failure.
func providerFailure(stderr string) bool { return providerErrRE.MatchString(stderr) }

type piEvent map[string]any

// str returns the string value stored under a key, or empty.
func (e piEvent) str(k string) string { s, _ := e[k].(string); return s }

// m returns the map value stored under a key, or nil.
func (e piEvent) m(k string) map[string]any {
	m, _ := e[k].(map[string]any)
	return m
}

// num returns the numeric value stored under a key, or zero.
func num(m map[string]any, k string) float64 { f, _ := m[k].(float64); return f }

// parsePiEvents fills tj from pi's JSON event stream and returns the events.
func parsePiEvents(out []byte, tj *TrialJSON, work, repo string) []piEvent {
	var events []piEvent
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	models := map[string]bool{}
	var observed []string
	var lastText string
	agentErr := ""
	for sc.Scan() {
		var e piEvent
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		events = append(events, e)
		switch e.str("type") {
		case "message_end":
			msg := e.m("message")
			if msg == nil || msg["role"] != "assistant" {
				continue
			}
			tj.Turns++
			if m := msg["model"]; m != nil {
				id := fmt.Sprintf("%v/%v", msg["provider"], m)
				if !models[id] {
					models[id] = true
					observed = append(observed, id)
				}
			}
			if u, ok := msg["usage"].(map[string]any); ok {
				tj.Tokens.Input += int(num(u, "input"))
				tj.Tokens.Output += int(num(u, "output"))
				tj.Tokens.CachedInput += int(num(u, "cacheRead"))
				tj.Tokens.CacheWrite += int(num(u, "cacheWrite"))
				tj.Tokens.Reasoning += int(num(u, "reasoning"))
				if c, ok := u["cost"].(map[string]any); ok {
					tj.CostUSD += num(c, "total")
				}
			}
			tj.StopReason, _ = msg["stopReason"].(string)
			// Only the last assistant message decides: pi retries a failed
			// request (auto_retry_start/end), and a later message that ends
			// normally means the error was recovered.
			agentErr = ""
			if tj.StopReason == "error" || tj.StopReason == "aborted" {
				agentErr = fmt.Sprint(msg["errorMessage"])
				if agentErr == "<nil>" || agentErr == "" {
					agentErr = tj.StopReason
				}
			}
			lastText = ""
			if cs, ok := msg["content"].([]any); ok {
				for _, c := range cs {
					if cm, ok := c.(map[string]any); ok && cm["type"] == "text" {
						lastText += fmt.Sprint(cm["text"])
					}
				}
			}
		case "tool_execution_start":
			name := e.str("toolName")
			tj.ToolCalls.Count++
			tj.ToolCalls.ByTool[name]++
			tj.checkPiTool(name, e.m("args"), work, repo)
		case "tool_execution_end":
			isErr, _ := e["isError"].(bool)
			if r, _ := e.m("result")["isError"].(bool); r {
				isErr = true
			}
			if isErr {
				tj.ToolCalls.Errors++
			}
		}
	}
	tj.FinalAnswer = lastText
	tj.ModelObserved = strings.Join(observed, ",")
	if tj.ModelObserved != "" {
		tj.Model = tj.ModelObserved
		match := len(observed) == 1 && observed[0] == tj.ModelRequested
		tj.ModelMatch = &match
	}
	switch {
	case agentErr != "":
		tj.Status = statusProviderError
		tj.Error = tj.StopReason + ": " + tail(agentErr, 300)
	case tj.Turns == 0:
		tj.Status = statusRunnerError
		tj.Error = "no assistant message in pi output"
	default:
		tj.Status = statusOK
	}
	return events
}

// checkPiTool records every tool call's arguments and notes anything that
// mentions the repository, the user's home or the grader's files. With the
// pptx tools the model cannot name a path, so a note here means the model
// tried; task.json and rubric are leaks.
func (tj *TrialJSON) checkPiTool(name string, in map[string]any, _, repo string) {
	b, _ := json.Marshal(in)
	text := string(b)
	tj.Commands = append(tj.Commands, name+" "+text)
	for _, t := range []string{"task.json", "rubric"} {
		if strings.Contains(text, t) {
			tj.Leak = true
			tj.AccessNotes = append(tj.AccessNotes, fmt.Sprintf("leak: %s mentions %q: %s", name, t, tail(text, 120)))
			return
		}
	}
	terms := []string{"/Users/", "~/", "$HOME", ".claude", ".pi", "eval/bundles", "results", "../"}
	if repo != "" {
		terms = append([]string{repo}, terms...)
	}
	for _, t := range terms {
		if strings.Contains(text, t) {
			tj.AccessNotes = append(tj.AccessNotes, fmt.Sprintf("%s mentions %q: %s", name, t, tail(text, 120)))
			return
		}
	}
}

// piTranscriptMD renders tool calls, results and assistant text.
func piTranscriptMD(events []piEvent) string {
	lines := []string{"# Agent trace", ""}
	for _, e := range events {
		switch e.str("type") {
		case "tool_execution_start":
			b, _ := json.MarshalIndent(e["args"], "", "  ")
			lines = append(lines, "## "+e.str("toolName"), "", "```json", string(b), "```", "")
		case "tool_execution_end":
			var parts []string
			if cs, ok := e.m("result")["content"].([]any); ok {
				for _, c := range cs {
					cm, _ := c.(map[string]any)
					if t, ok := cm["text"].(string); ok {
						parts = append(parts, t)
					} else {
						parts = append(parts, "[image]")
					}
				}
			}
			lines = append(lines, "```text", strings.Join(parts, "\n"), "```", "")
		case "message_end":
			msg := e.m("message")
			if msg == nil || msg["role"] != "assistant" {
				continue
			}
			if cs, ok := msg["content"].([]any); ok {
				for _, c := range cs {
					if cm, ok := c.(map[string]any); ok && cm["type"] == "text" {
						lines = append(lines, fmt.Sprint(cm["text"]), "")
					}
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}
