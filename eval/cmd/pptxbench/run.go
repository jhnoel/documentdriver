package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jhnoel/documentdriver/eval/grade"
)

const (
	runnerName   = "pptxbench-claude-headless/1"
	piRunnerName = "pptxbench-pi/1"
)

// condition is one cell of the evaluation: the fixed read view and
// whether pptx render is available.
type condition struct {
	View   string // json or svg
	Render bool
}

// Name returns the condition name, including render when enabled.
func (c condition) Name() string {
	if c.Render {
		return c.View + "+render"
	}
	return c.View
}

// trialSpec is one planned trial.
type trialSpec struct {
	Task   *grade.Task
	Cond   condition
	Repeat int
}

// dir returns the trial's output directory under out.
func (s trialSpec) dir(out string) string {
	return filepath.Join(out, s.Task.ID, s.Cond.Name(), fmt.Sprint(s.Repeat))
}

// Tokens are the token counts of one trial. Input excludes cache reads
// and cache writes, which are counted apart.
type Tokens struct {
	Input       int `json:"input"`
	CachedInput int `json:"cached_input"`
	CacheWrite  int `json:"cache_write"`
	Output      int `json:"output"`
	Reasoning   int `json:"reasoning"`
}

// ToolCalls counts the agent's tool calls.
type ToolCalls struct {
	Count  int            `json:"count"`
	ByTool map[string]int `json:"by_tool"`
	Errors int            `json:"errors"`
}

// TrialJSON is trial.json, per eval/README.md "Runner contract".
type TrialJSON struct {
	Task         string    `json:"task"`
	BundleSHA256 string    `json:"bundle_sha256"`
	Arm          string    `json:"arm"`
	Condition    string    `json:"condition"`
	Runner       string    `json:"runner"`
	Model        string    `json:"model"`
	Repeat       int       `json:"repeat"`
	Status       string    `json:"status"`
	Error        string    `json:"error,omitempty"`
	Tokens       Tokens    `json:"tokens"`
	ToolCalls    ToolCalls `json:"tool_calls"`
	WallSeconds  float64   `json:"wall_seconds"`
	FinalAnswer  string    `json:"final_answer"`
	StartedAt    string    `json:"started_at"`

	CostUSD            float64  `json:"cost_usd"`
	Turns              int      `json:"turns"`
	TerminalReason     string   `json:"terminal_reason,omitempty"`
	SessionID          string   `json:"session_id,omitempty"`
	ClaudeVersion      string   `json:"claude_version,omitempty"`
	Command            []string `json:"command"`
	InstructionsSHA256 string   `json:"instructions_sha256"`
	Commands           []string `json:"bash_commands,omitempty"`
	Isolation          []string `json:"isolation_problems,omitempty"`
	AccessNotes        []string `json:"access_notes,omitempty"`
	Graded             bool     `json:"graded"`

	// Pi runner only.
	PiVersion      string   `json:"pi_version,omitempty"`
	ModelRequested string   `json:"model_requested,omitempty"`
	ModelObserved  string   `json:"model_observed,omitempty"`
	ModelMatch     *bool    `json:"model_match,omitempty"`
	Thinking       string   `json:"thinking,omitempty"`
	StopReason     string   `json:"stop_reason,omitempty"`
	ExitCode       *int     `json:"exit_code,omitempty"`
	Leak           bool     `json:"leak,omitempty"`
	ToolsActive    []string `json:"tools_active,omitempty"`
}

// Status values; anything but statusOK is a harness error.
const (
	statusOK            = "ok"
	statusTimeout       = "timeout"
	statusRunnerError   = "runner_error"
	statusProviderError = "provider_error"
)

// runConfig is run.json.
type runConfig struct {
	Condition     string   `json:"condition"`
	Model         string   `json:"model"`
	Repeats       int      `json:"repeats"`
	Jobs          int      `json:"jobs"`
	Seed          int64    `json:"seed"`
	TimeoutSec    int      `json:"timeout_seconds"`
	Tasks         []string `json:"tasks"`
	Order         []string `json:"order"`
	Runner        string   `json:"runner"`
	Thinking      string   `json:"thinking,omitempty"`
	ClaudeVersion string   `json:"claude_version,omitempty"`
	PiVersion     string   `json:"pi_version,omitempty"`
	GitCommit     string   `json:"git_commit"`
	GitDirty      bool     `json:"git_dirty"`
	PptxSHA256    string   `json:"pptx_sha256"`
	StartedAt     string   `json:"started_at"`
}

type runOpts struct {
	cond       condition
	tasks      string
	repeats    int
	out        string
	jobs       int
	model      string
	dryRun     bool
	timeout    time.Duration
	seed       int64
	repo       string
	tasksDir   string
	bundles    string
	corpus     string
	claude     string
	keepWork   bool
	allowEdit  bool
	maxBudget  float64
	instrDir   string
	realPptx   string
	binDir     string
	claudeVers string
	runner     string // claude or pi
	thinking   string
	pi         string
	piVers     string
}

// cmdRun runs configured trials in parallel and records their results.
func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var o runOpts
	view := fs.String("condition", "", "read view: json or svg")
	render := fs.Bool("render", false, "make pptx render (and the Read tool for the PNG) available")
	fs.StringVar(&o.tasks, "tasks", "id-*", "task ids: comma-separated ids or globs (id-*, id-ow-gateway,id-ow-python)")
	fs.IntVar(&o.repeats, "repeats", 1, "trials per task")
	fs.StringVar(&o.out, "out", "", "result directory, e.g. results/<name>/<timestamp>")
	fs.IntVar(&o.jobs, "jobs", 1, "trials in parallel")
	fs.StringVar(&o.runner, "runner", "claude", "agent runner: claude (claude -p, default) or pi (Pi CLI)")
	fs.StringVar(&o.model, "model", "", "model id (default claude-haiku-5-5 for claude; required for pi, as provider/id)")
	fs.StringVar(&o.thinking, "thinking", "", "pi only: off|minimal|low|medium|high|xhigh|max")
	fs.StringVar(&o.pi, "pi", "pi", "pi executable")
	fs.BoolVar(&o.dryRun, "dry-run", false, "print the plan and write work dirs under <out>/dry-run; do not call claude")
	fs.DurationVar(&o.timeout, "timeout", 15*time.Minute, "per-trial timeout (the process group is killed)")
	fs.Int64Var(&o.seed, "seed", 0, "shuffle seed (default: clock)")
	fs.StringVar(&o.repo, "repo", ".", "repository root (builds ./cmd/pptx; reads eval/arms)")
	fs.StringVar(&o.tasksDir, "tasks-dir", "eval/tasks", "task specs")
	fs.StringVar(&o.bundles, "bundles", "eval/bundles", "bundle directory (materialised when missing)")
	fs.StringVar(&o.corpus, "corpus", "", "corpus root (default $PPTX_CORPUS or "+defaultCorpusDoc+")")
	fs.StringVar(&o.claude, "claude", "claude", "claude executable")
	fs.BoolVar(&o.keepWork, "keep-work", false, "keep each trial's work directory (default: remove after copying outputs)")
	fs.BoolVar(&o.allowEdit, "allow-edit", false, "allow non-Identify tasks (agents edit with pptx apply)")
	fs.Float64Var(&o.maxBudget, "max-budget-usd", 0, "per-trial spend cap passed to claude (0: none)")
	fs.Parse(args)
	if *view != "json" && *view != "svg" {
		return fmt.Errorf("run: --condition must be json or svg")
	}
	if err := o.checkRunner(); err != nil {
		return err
	}
	if o.out == "" || o.repeats < 1 || o.jobs < 1 {
		fs.Usage()
		return exitErr(2)
	}
	o.cond = condition{View: *view, Render: *render}

	ts, err := selectTasks(o.tasksDir, o.tasks)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if t.Type != grade.Identify && !o.allowEdit {
			return fmt.Errorf("run: task %s is %s; edit tasks need pptx apply (M3). Select Identify tasks or pass --allow-edit", t.ID, t.Type)
		}
	}
	var err2 error
	if o.out, err2 = filepath.Abs(o.out); err2 != nil {
		return err2
	}
	if o.repo, err2 = filepath.Abs(o.repo); err2 != nil {
		return err2
	}
	arm := "pptxdriver"
	if o.runner == "pi" {
		arm = "pptxdriver-pi" // describes the pptx tools, not a shell
	}
	o.instrDir = filepath.Join(o.repo, "eval", "arms", arm)
	instr, err := os.ReadFile(filepath.Join(o.instrDir, o.cond.Name()+".md"))
	if err != nil {
		return err
	}

	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	// Bundles: input.pptx comes from the bundle; materialise any that are missing.
	for _, t := range ts {
		bd := filepath.Join(o.bundles, t.ID)
		if !grade.FileExists(filepath.Join(bd, "input.pptx")) {
			if err := materialise(t, o.tasksDir, corpusDir(o.corpus), bd); err != nil {
				return err
			}
		}
	}
	// The real binary is built once, outside any work dir.
	o.binDir = filepath.Join(o.out, "bin")
	if err := os.MkdirAll(o.binDir, 0o755); err != nil {
		return err
	}
	o.realPptx = filepath.Join(o.binDir, "pptx-real")
	build := exec.Command("go", "build", "-trimpath", "-o", o.realPptx, "./cmd/pptx")
	build.Dir = o.repo
	if b, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("building pptx: %v\n%s", err, b)
	}
	if o.runner == "pi" {
		o.piVers = piVersion(o.pi)
	} else {
		o.claudeVers = claudeVersion(o.claude)
	}

	var specs []trialSpec
	for _, t := range ts {
		for n := 1; n <= o.repeats; n++ {
			specs = append(specs, trialSpec{t, o.cond, n})
		}
	}
	if o.seed == 0 {
		o.seed = time.Now().UnixNano()
	}
	rand.New(rand.NewSource(o.seed)).Shuffle(len(specs), func(i, j int) { specs[i], specs[j] = specs[j], specs[i] }) //nolint:gosec // G404: the shuffle seed is recorded in the run config; reproducibility is the point

	cfg := runConfig{Condition: o.cond.Name(), Model: o.model, Repeats: o.repeats, Jobs: o.jobs, Seed: o.seed,
		TimeoutSec: int(o.timeout / time.Second), Runner: o.runnerName(), Thinking: o.thinking,
		ClaudeVersion: o.claudeVers, PiVersion: o.piVers, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, t := range ts {
		cfg.Tasks = append(cfg.Tasks, t.ID)
	}
	for _, s := range specs {
		cfg.Order = append(cfg.Order, fmt.Sprintf("%s/%s/%d", s.Task.ID, s.Cond.Name(), s.Repeat))
	}
	cfg.GitCommit, cfg.GitDirty = gitState(o.repo)
	if b, err := os.ReadFile(o.realPptx); err == nil {
		sum := sha256.Sum256(b)
		cfg.PptxSHA256 = hex.EncodeToString(sum[:])
	}

	fmt.Printf("condition %s, model %s, %d trials (%d tasks x %d repeats), jobs %d, seed %d, runner %s\n",
		o.cond.Name(), o.model, len(specs), len(ts), o.repeats, o.jobs, o.seed, o.runnerName()+" "+o.claudeVers+o.piVers)
	if o.dryRun {
		for i, s := range specs {
			work, bin, err := prepareTrial(&o, s, filepath.Join(o.out, "dry-run", s.Task.ID, s.Cond.Name(), fmt.Sprint(s.Repeat)))
			if err != nil {
				return err
			}
			if o.runner == "pi" {
				argv := piArgv(&o, s.Task.Type, "instructions.md", piPrompt(s.Task.ToolMarkdown()))
				fmt.Printf("%3d. %s [%s] #%d\n     work %s\n     bin  %s\n     cwd=work %s  %s\n",
					i+1, s.Task.ID, s.Cond.Name(), s.Repeat, work, bin, piEnvNote(&o, s.Task.Type, filepath.Dir(work), work), shellJoin(argv))
				continue
			}
			argv := claudeArgv(&o, work, "@instructions.md")
			fmt.Printf("%3d. %s [%s] #%d\n     work %s\n     bin  %s\n     cwd=work PATH=%s:$PATH <task.md  %s\n",
				i+1, s.Task.ID, s.Cond.Name(), s.Repeat, work, bin, bin, shellJoin(argv))
		}
		fmt.Printf("dry run: nothing sent to the agent; work dirs under %s\n", filepath.Join(o.out, "dry-run"))
		return nil
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(o.out, "run.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}

	queue := make(chan trialSpec)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	for w := 0; w < o.jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range queue {
				tj := runTrial(&o, s, string(instr))
				mu.Lock()
				done++
				g := "-"
				if tj.Graded {
					g = "graded"
				}
				fmt.Printf("[%d/%d] %s [%s] #%d: %s %s  %.0fs $%.4f %d calls\n", done, len(specs), s.Task.ID, s.Cond.Name(), s.Repeat,
					tj.Status, g, tj.WallSeconds, tj.CostUSD, tj.ToolCalls.Count)
				mu.Unlock()
			}
		}()
	}
	for _, s := range specs {
		if grade.FileExists(filepath.Join(s.dir(o.out), "trial.json")) {
			mu.Lock()
			done++
			fmt.Printf("[%d/%d] %s [%s] #%d: already done, skipped\n", done, len(specs), s.Task.ID, s.Cond.Name(), s.Repeat)
			mu.Unlock()
			continue
		}
		queue <- s
	}
	close(queue)
	wg.Wait()
	fmt.Printf("done: %s (pptxbench report %s)\n", o.out, o.out)
	return nil
}

// selectTasks resolves a comma-separated list of ids and globs.
func selectTasks(dir, spec string) ([]*grade.Task, error) {
	all, err := grade.LoadTasks(dir)
	if err != nil {
		return nil, err
	}
	var out []*grade.Task
	seen := map[string]bool{}
	for _, pat := range strings.Split(spec, ",") {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		hit := false
		for _, t := range all {
			if ok, _ := path.Match(pat, t.ID); ok {
				hit = true
				if !seen[t.ID] {
					seen[t.ID] = true
					out = append(out, t)
				}
			}
		}
		if !hit {
			return nil, fmt.Errorf("--tasks: %q matches no task", pat)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--tasks: no tasks selected")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// claudeVersion returns the claude executable's version, or "unknown" on failure.
func claudeVersion(claude string) string {
	b, err := exec.Command(claude, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

// gitState returns the repository head commit and whether it is dirty.
func gitState(repo string) (string, bool) {
	c := exec.Command("git", "rev-parse", "HEAD")
	c.Dir = repo
	b, err := c.Output()
	if err != nil {
		return "", false
	}
	d := exec.Command("git", "status", "--porcelain")
	d.Dir = repo
	s, _ := d.Output()
	return strings.TrimSpace(string(b)), len(bytes.TrimSpace(s)) > 0
}

// wrapperScript is the pptx on the agent's PATH. It fixes the read view
// and rejects the other one and, outside render conditions, pptx render.
func wrapperScript(realBin string, c condition) string {
	other := "svg"
	if c.View == "svg" {
		other = "json"
	}
	render := "0"
	if c.Render {
		render = "1"
	}
	return fmt.Sprintf(`#!/bin/sh
# pptxbench wrapper: read view fixed to --format %[2]s; render %[4]s.
REAL=%[1]s
VIEW=%[2]s
OTHER=%[3]s
RENDER=%[5]s
usage() {
  echo "usage: pptx <command> [args]"
  echo
  echo "commands:"
  echo "  read    deck.pptx [--slide slideN] [--detail sites|full] [--ids slide1/s12,slide1/s13] [--region x,y,w,h]"
  echo "          (a slide's objects are always shown in the %[2]s view)"
  echo "  apply   deck.pptx plan.json [--dry-run] [-o out.pptx]   (pptx help ops lists the operations)"
  echo "  xml     deck.pptx slideN/sID"
  if [ "$RENDER" = 1 ]; then
    echo "  render  deck.pptx --slide slideN [--overlay ids,sites,glue] [--ids slide1/s12] -o out.png"
  fi
  echo "  help    [ops|<op>]"
}
refuse() {
  printf '{"errors":[{"code":"usage","message":"%%s"}]}\n' "$1" >&2; exit 2
}
case "$1" in
  ""|-h|--help) usage; exit 0 ;;
  help)
    shift
    case "$1" in
      "") usage; exit 0 ;;
      render) if [ "$RENDER" != 1 ]; then refuse "render is not available here"; fi; exec "$REAL" help "$@" ;;
      read) usage; exit 0 ;;
      *) exec "$REAL" help "$@" ;;
    esac ;;
  read)
    shift
    slide=0
    for a in "$@"; do
      case "$a" in
        --format|--format=*) refuse "--format is not available here; a slide is always shown in the $VIEW view" ;;
        --slide|--slide=*) slide=1 ;;
        -h|--help) usage; exit 0 ;;
      esac
    done
    if [ "$slide" = 1 ]; then exec "$REAL" read "$@" --format $VIEW; fi
    exec "$REAL" read "$@" ;;
  xml) shift; exec "$REAL" xml "$@" ;;
  apply) shift; exec "$REAL" apply "$@" ;;
  render)
    if [ "$RENDER" != 1 ]; then refuse "render is not available here"; fi
    shift; exec "$REAL" render "$@" ;;
  *) usage >&2; refuse "unknown command $1" ;;
esac
`, realBin, c.View, other, map[bool]string{true: "available", false: "not available"}[c.Render], render)
}

// prepareTrial creates a work directory holding only input.pptx and
// task.md, and a bin directory (outside it) with the pptx wrapper.
// root, when set, is where both go (dry runs); otherwise temp dirs.
func prepareTrial(o *runOpts, s trialSpec, root string) (work, bin string, err error) {
	if root != "" {
		work, bin = filepath.Join(root, "work"), filepath.Join(root, "bin")
		if err = os.RemoveAll(root); err != nil {
			return
		}
		if err = os.MkdirAll(work, 0o755); err != nil {
			return
		}
		if err = os.MkdirAll(bin, 0o755); err != nil {
			return
		}
	} else {
		// Outside the repository, so no project CLAUDE.md or AGENTS.md
		// can be discovered above the work dir.
		var base string
		if base, err = os.MkdirTemp("", "pptxbench-"); err != nil {
			return
		}
		if base, err = filepath.EvalSymlinks(base); err != nil {
			return
		}
		work, bin = filepath.Join(base, "work"), filepath.Join(base, "bin")
		if err = os.MkdirAll(work, 0o755); err != nil {
			return
		}
		if err = os.MkdirAll(bin, 0o755); err != nil {
			return
		}
	}
	if work, err = filepath.EvalSymlinks(work); err != nil {
		return
	}
	in, err := os.ReadFile(filepath.Join(o.bundles, s.Task.ID, "input.pptx"))
	if err != nil {
		return
	}
	if err = os.WriteFile(filepath.Join(work, "input.pptx"), in, 0o444); err != nil {
		return
	}
	if err = os.WriteFile(filepath.Join(work, "task.md"), []byte(s.Task.Markdown()), 0o644); err != nil {
		return
	}
	// The wrapper execs a copy of the binary in the trial directory, so
	// neither names a path in the repository or the results directory.
	lib := filepath.Join(filepath.Dir(work), "lib")
	if err = os.MkdirAll(lib, 0o755); err != nil {
		return
	}
	cp := filepath.Join(lib, "pptx")
	if err = copyFile(o.realPptx, cp, 0o755); err != nil {
		return
	}
	err = os.WriteFile(filepath.Join(bin, "pptx"), []byte(wrapperScript(cp, s.Cond)), 0o755)
	return
}

// allowedBash are the Bash command prefixes the agent may run.
var allowedBash = []string{"pptx", "ls", "cat", "head", "tail", "grep", "wc", "echo", "cp", "mkdir", "jq", "sort"}

// claudeArgv is the claude command line. instr is the instruction text
// (or a placeholder when recording the command).
func claudeArgv(o *runOpts, work, instr string) []string {
	var allow []string
	for _, c := range allowedBash {
		allow = append(allow, fmt.Sprintf("Bash(%s *)", c))
	}
	allow = append(allow, "Bash(ls)")
	// Shell redirection (echo 17 > answer.txt) is a file write that
	// needs an edit rule; the Edit tool itself is not available.
	allow = append(allow, "Edit(/"+work+"/**)")
	tools := "Bash"
	if o.cond.Render {
		tools = "Bash,Read"
		allow = append(allow, "Read(/"+work+"/**)")
	}
	argv := []string{o.claude, "-p",
		"--model", o.model,
		"--output-format", "stream-json", "--verbose",
		// Isolation from the user's own setup.
		"--safe-mode",
		"--setting-sources", "",
		// Drop the commit and PR attribution text from the context.
		"--settings", `{"attribution":{"commit":"","pr":""},"includeCoAuthoredBy":false}`,
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--disable-slash-commands",
		"--no-session-persistence",
		// Tools: Bash limited to pptx and basic file commands; Read only with render.
		"--tools", tools,
		"--permission-mode", "dontAsk",
		"--allowedTools", strings.Join(allow, ","),
		"--append-system-prompt", instr,
	}
	if o.maxBudget > 0 {
		argv = append(argv, "--max-budget-usd", fmt.Sprint(o.maxBudget))
	}
	return argv
}

// shellJoin joins command arguments into a shell-quoted string.
func shellJoin(a []string) string {
	var p []string
	for _, s := range a {
		if s == "" || strings.ContainsAny(s, " \t\n\"'$&|;<>(){}*") {
			s = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
		p = append(p, s)
	}
	return strings.Join(p, " ")
}

// write saves a trial artifact. A failed write is a harness error: it is
// recorded in tj.Error unless an earlier error is already there.
func (tj *TrialJSON) write(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil && tj.Error == "" {
		tj.Error = "write artifact: " + err.Error()
	}
}

// runTrial runs, collects and grades one trial. It never returns an
// error: failures are recorded in trial.json as harness errors.
func runTrial(o *runOpts, s trialSpec, instr string) *TrialJSON {
	tdir := s.dir(o.out)
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		return &TrialJSON{Task: s.Task.ID, Arm: "pptxdriver", Condition: s.Cond.Name(), Runner: o.runnerName(),
			Model: o.model, Repeat: s.Repeat, Status: statusRunnerError, Error: "prepare: " + err.Error()}
	}
	isum := sha256.Sum256([]byte(instr))
	bsum := sha256.Sum256(nil)
	if b, err := os.ReadFile(filepath.Join(o.bundles, s.Task.ID, "input.pptx")); err == nil {
		bsum = sha256.Sum256(b)
	}
	tj := &TrialJSON{
		Task: s.Task.ID, BundleSHA256: hex.EncodeToString(bsum[:]), Arm: "pptxdriver", Condition: s.Cond.Name(),
		Runner: o.runnerName(), Model: o.model, Repeat: s.Repeat, Status: statusRunnerError,
		StartedAt: time.Now().UTC().Format(time.RFC3339), ClaudeVersion: o.claudeVers,
		InstructionsSHA256: hex.EncodeToString(isum[:]), ToolCalls: ToolCalls{ByTool: map[string]int{}},
	}
	finish := func() *TrialJSON {
		b, _ := json.MarshalIndent(tj, "", "  ")
		tj.write(filepath.Join(tdir, "trial.json"), append(b, '\n'))
		return tj
	}
	work, bin, err := prepareTrial(o, s, "")
	if err != nil {
		tj.Error = "prepare: " + err.Error()
		return finish()
	}
	if !o.keepWork {
		defer os.RemoveAll(filepath.Dir(work))
	}
	tj.write(filepath.Join(tdir, "instructions.md"), []byte(instr))
	taskMD := s.Task.Markdown()
	if o.runner == "pi" {
		taskMD = s.Task.ToolMarkdown()
	}
	tj.write(filepath.Join(tdir, "task.md"), []byte(taskMD))
	if o.runner == "pi" {
		tj.ClaudeVersion = ""
		tj.PiVersion = o.piVers
		tj.ModelRequested = o.model
		tj.Thinking = o.thinking
		if !runPi(o, s, tj, work, bin, tdir, instr) {
			return finish()
		}
	} else if !runClaude(o, s, tj, work, bin, tdir, instr) {
		return finish()
	}

	// Collect outputs.
	if b, err := os.ReadFile(filepath.Join(work, "answer.txt")); err == nil {
		tj.write(filepath.Join(tdir, "answer.txt"), b)
	}
	if b, err := os.ReadFile(filepath.Join(work, "output.pptx")); err == nil {
		tj.write(filepath.Join(tdir, "output.pptx"), b)
	} else if s.Task.Type == grade.Identify {
		// An Identify trial leaves the deck unchanged; grade it as such.
		if b, err := os.ReadFile(filepath.Join(work, "input.pptx")); err == nil {
			tj.write(filepath.Join(tdir, "output.pptx"), b)
		}
	}
	// Grade; a harness error is not graded.
	if tj.Status == statusOK {
		if err := gradeTrial(o, s, tdir); err != nil {
			tj.Status = statusRunnerError
			tj.Error = "grade: " + err.Error()
		} else {
			tj.Graded = true
		}
	}
	return finish()
}

// runClaude runs claude -p for one trial and fills tj. It reports false
// when the trial cannot continue to collection and grading.
func runClaude(o *runOpts, s trialSpec, tj *TrialJSON, work, bin, tdir, instr string) bool {
	argv := claudeArgv(o, work, instr)
	tj.Command = claudeArgv(o, work, "@instructions.md")
	var err error
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
	)
	cmd.Stdin = strings.NewReader(s.Task.Markdown())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	start := time.Now()
	timedOut := false
	if err := cmd.Start(); err != nil {
		tj.Error = "start claude: " + err.Error()
		return false
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err = <-waitErr:
	case <-time.After(o.timeout):
		timedOut = true
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		err = <-waitErr
	}
	// Reap anything the agent left behind (soffice children).
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	tj.WallSeconds = time.Since(start).Seconds()

	tj.write(filepath.Join(tdir, "transcript.jsonl"), stdout.Bytes())
	tj.write(filepath.Join(tdir, "stderr.txt"), stderr.Bytes())
	parseTranscript(stdout.Bytes(), tj, work, s.Cond)
	switch {
	case timedOut:
		tj.Status = statusTimeout
		tj.Error = fmt.Sprintf("killed after %s", o.timeout)
	case tj.Status == statusProviderError:
	case err != nil && tj.Status != statusOK:
		tj.Status = statusRunnerError
		tj.Error = fmt.Sprintf("claude: %v: %s", err, tail(stderr.String(), 400))
	}
	if tj.Status == statusOK && len(tj.Isolation) > 0 {
		tj.Status = statusRunnerError
		tj.Error = "isolation check failed: " + strings.Join(tj.Isolation, "; ")
	}

	return true
}

// tail returns the last n characters of a string, trimmed.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}

// gradeTrial grades a completed trial and writes its grade.json.
func gradeTrial(o *runOpts, s trialSpec, tdir string) error {
	opt := grade.Options{}
	if b, err := os.ReadFile(filepath.Join(tdir, "answer.txt")); err == nil {
		a := string(b)
		opt.Answer = &a
	} else if s.Task.Type == grade.Identify {
		a := ""
		opt.Answer = &a
	}
	output := filepath.Join(tdir, "output.pptx")
	if !grade.FileExists(output) {
		return fmt.Errorf("no output.pptx")
	}
	res, err := grade.Grade(s.Task, filepath.Join(o.bundles, s.Task.ID, "input.pptx"), output, opt)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	return os.WriteFile(filepath.Join(tdir, "grade.json"), append(b, '\n'), 0o644)
}

// parseTranscript fills tj from claude's stream-json output.
func parseTranscript(out []byte, tj *TrialJSON, work string, cond condition) {
	type block struct {
		Type      string         `json:"type"`
		Name      string         `json:"name"`
		ID        string         `json:"id"`
		Input     map[string]any `json:"input"`
		ToolUseID string         `json:"tool_use_id"`
		IsError   bool           `json:"is_error"`
	}
	type line struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		// init
		Tools         []string                  `json:"tools"`
		MCPServers    []json.RawMessage         `json:"mcp_servers"`
		SlashCommands []string                  `json:"slash_commands"`
		Skills        []string                  `json:"skills"`
		Plugins       []struct{ Source string } `json:"plugins"`
		SessionID     string                    `json:"session_id"`
		Cwd           string                    `json:"cwd"`
		Model         string                    `json:"model"`
		// result
		IsError        bool    `json:"is_error"`
		Result         string  `json:"result"`
		CostUSD        float64 `json:"total_cost_usd"`
		NumTurns       int     `json:"num_turns"`
		TerminalReason string  `json:"terminal_reason"`
		APIErrorStatus *int    `json:"api_error_status"`
		Usage          struct {
			Input       int `json:"input_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
			Output      int `json:"output_tokens"`
			Details     struct {
				Thinking int `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	seen := map[string]bool{}
	gotResult := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	for sc.Scan() {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch l.Type {
		case "system":
			if l.Subtype != "init" {
				continue
			}
			tj.SessionID = l.SessionID
			var bad []string
			for _, t := range l.Tools {
				if t != "Bash" && !(t == "Read" && cond.Render) {
					bad = append(bad, "tool "+t)
				}
			}
			if len(l.MCPServers) > 0 {
				bad = append(bad, "mcp servers present")
			}
			if len(l.SlashCommands) > 0 || len(l.Skills) > 0 {
				bad = append(bad, "skills or slash commands present")
			}
			for _, p := range l.Plugins {
				if !strings.HasSuffix(p.Source, "@builtin") {
					bad = append(bad, "plugin "+p.Source)
				}
			}
			if l.Cwd != "" && l.Cwd != work {
				bad = append(bad, "cwd "+l.Cwd)
			}
			tj.Isolation = append(tj.Isolation, bad...)
		case "assistant", "user":
			var bs []block
			if json.Unmarshal(l.Message.Content, &bs) != nil {
				continue
			}
			for _, b := range bs {
				switch b.Type {
				case "tool_use":
					if seen[b.ID] {
						continue
					}
					seen[b.ID] = true
					tj.ToolCalls.Count++
					tj.ToolCalls.ByTool[b.Name]++
					tj.checkToolUse(b.Name, b.Input, work)
				case "tool_result":
					if b.IsError {
						tj.ToolCalls.Errors++
					}
				}
			}
		case "result":
			gotResult = true
			tj.FinalAnswer = l.Result
			tj.CostUSD = l.CostUSD
			tj.Turns = l.NumTurns
			tj.TerminalReason = l.TerminalReason
			tj.Tokens = Tokens{Input: l.Usage.Input, CachedInput: l.Usage.CacheRead, CacheWrite: l.Usage.CacheCreate,
				Output: l.Usage.Output, Reasoning: l.Usage.Details.Thinking}
			for m := range l.ModelUsage {
				tj.Model = m
			}
			switch {
			case l.IsError && (l.APIErrorStatus != nil || l.Subtype == "error_during_execution"):
				tj.Status = statusProviderError
				tj.Error = l.Subtype + ": " + tail(l.Result, 300)
			case l.IsError && !strings.HasPrefix(l.Subtype, "error_max"):
				tj.Status = statusProviderError
				tj.Error = l.Subtype + ": " + tail(l.Result, 300)
			default:
				tj.Status = statusOK
			}
		}
	}
	if !gotResult && tj.Status != statusProviderError {
		tj.Status = statusRunnerError
		tj.Error = "no result message in claude output"
	}
}

// checkToolUse records Bash commands and flags anything that reaches
// outside the work directory or the allowed commands.
func (tj *TrialJSON) checkToolUse(name string, in map[string]any, work string) {
	switch name {
	case "Bash":
		cmd, _ := in["command"].(string)
		tj.Commands = append(tj.Commands, cmd)
		for _, bad := range []string{"/Users/", "~/", "$HOME", ".claude", "task.json", "rubric"} {
			if strings.Contains(cmd, bad) {
				tj.AccessNotes = append(tj.AccessNotes, fmt.Sprintf("bash command mentions %q: %s", bad, tail(cmd, 120)))
				break
			}
		}
	case "Read":
		fp, _ := in["file_path"].(string)
		if fp != "" && !strings.HasPrefix(fp, work+"/") {
			tj.AccessNotes = append(tj.AccessNotes, "Read outside the work dir: "+fp)
		}
	}
}
