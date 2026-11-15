// Command pptxbench materialises evaluation bundles from the corpus,
// grades trial outputs, writes reference outputs and self-tests the
// graders. See eval/README.md.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jhnoel/documentdriver/eval/grade"
	"github.com/jhnoel/documentdriver/eval/oracle"
	"github.com/jhnoel/documentdriver/internal/repo"
)

const usage = `usage: pptxbench <command> [flags]

  materialise  copy corpus decks into bundles (eval/bundles/<task>/)
  grade        grade one trial output against its bundle; writes grade.json
  reference    write a reference (or noop, sloppy, reserialised) output
  references   write every editing task's reference, named for pptoracle smoke --all
  selftest     grade reference, no-op, sloppy and reserialised outputs of every task
  list         print the task table as Markdown
  run          run trials (--runner claude|pi, --condition json|svg [--render] ...)
  report       aggregate a run directory: success, wrong-object rate, tokens, cost, calls, time
  compare      two run directories paired by task

Run "pptxbench <command> -h" for flags.
`

const defaultCorpusDoc = "<repo>/corpus"

// main dispatches a pptxbench subcommand and exits with its code.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "materialise", "materialize":
		err = cmdMaterialise(os.Args[2:])
	case "grade":
		err = cmdGrade(os.Args[2:])
	case "reference":
		err = cmdReference(os.Args[2:])
	case "references":
		err = cmdReferences(os.Args[2:])
	case "selftest":
		err = cmdSelftest(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "report":
		err = cmdReport(os.Args[2:])
	case "compare":
		err = cmdCompare(os.Args[2:])
	case "list":
		err = cmdList(os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		var ex exitErr
		if errors.As(err, &ex) {
			os.Exit(int(ex))
		}
		fmt.Fprintln(os.Stderr, "pptxbench:", err)
		os.Exit(1)
	}
}

type exitErr int

// Error returns the exit code as an error message.
func (e exitErr) Error() string { return fmt.Sprintf("exit %d", int(e)) }

// corpusDir is the --corpus flag, else $PPTX_CORPUS, else <repo>/corpus
// found by walking up from the working directory.
func corpusDir(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("PPTX_CORPUS"); v != "" {
		return v
	}
	c, err := repo.Corpus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pptxbench: corpus root:", err, "(pass --corpus or set PPTX_CORPUS)")
		os.Exit(2)
	}
	return c
}

// materialise copies a task's deck into dir and writes task.json, task.md
// and any rubric.
func materialise(t *grade.Task, tasksDir, corpus, dir string) error {
	src := filepath.Join(corpus, filepath.FromSlash(t.Deck.Path))
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	sum := sha256.Sum256(b)
	if got := hex.EncodeToString(sum[:]); got != t.Deck.SHA256 {
		return fmt.Errorf("%s: %s has sha256 %s, task expects %s", t.ID, src, got, t.Deck.SHA256)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "input.pptx"), b, 0o644); err != nil {
		return err
	}
	tj, err := os.ReadFile(filepath.Join(tasksDir, t.ID, "task.json"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "task.json"), tj, 0o644); err != nil {
		return err
	}
	if t.Rubric != "" {
		rb, err := os.ReadFile(filepath.Join(tasksDir, t.ID, t.Rubric))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, t.Rubric), rb, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "task.md"), []byte(t.Markdown()), 0o644)
}

// loadTasks loads tasks from a directory, optionally keeping only named ids.
func loadTasks(dir, only string) ([]*grade.Task, error) {
	ts, err := grade.LoadTasks(dir)
	if err != nil {
		return nil, err
	}
	if only == "" {
		return ts, nil
	}
	want := map[string]bool{}
	for _, s := range strings.Split(only, ",") {
		want[strings.TrimSpace(s)] = true
	}
	var out []*grade.Task
	for _, t := range ts {
		if want[t.ID] {
			out = append(out, t)
		}
	}
	return out, nil
}

// cmdMaterialise copies selected tasks' decks into bundles and reports the count.
func cmdMaterialise(args []string) error {
	fs := flag.NewFlagSet("materialise", flag.ExitOnError)
	corpus := fs.String("corpus", "", "corpus root (default $PPTX_CORPUS or "+defaultCorpusDoc+")")
	tasks := fs.String("tasks", "eval/tasks", "task specs")
	out := fs.String("out", "eval/bundles", "bundle directory")
	only := fs.String("only", "", "comma-separated task ids")
	fs.Parse(args)
	ts, err := loadTasks(*tasks, *only)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if err := materialise(t, *tasks, corpusDir(*corpus), filepath.Join(*out, t.ID)); err != nil {
			return err
		}
	}
	fmt.Printf("%d bundles in %s\n", len(ts), *out)
	return nil
}

// cmdGrade grades one trial output against its bundle and writes grade.json.
func cmdGrade(args []string) error {
	fs := flag.NewFlagSet("grade", flag.ExitOnError)
	bundle := fs.String("bundle", "", "bundle directory (input.pptx, task.json)")
	trial := fs.String("trial", "", "trial directory: reads output.pptx and answer.txt, writes grade.json there")
	output := fs.String("output", "", "output deck to grade")
	answer := fs.String("answer", "", "Identify answer file")
	pp := fs.Bool("powerpoint", false, "also open the output in PowerPoint via `go run ./tools/pptoracle smoke` (needs the user's machine)")
	repo := fs.String("repo", ".", "repository root for --powerpoint")
	jsonOut := fs.String("o", "", "grade.json path (default: beside the output)")
	fs.Parse(args)
	if *bundle == "" || (*output == "" && *trial == "") {
		fs.Usage()
		return exitErr(2)
	}
	if *trial != "" {
		if *output == "" {
			*output = filepath.Join(*trial, "output.pptx")
		}
		if *answer == "" && grade.FileExists(filepath.Join(*trial, "answer.txt")) {
			*answer = filepath.Join(*trial, "answer.txt")
		}
	}
	if *jsonOut == "" {
		*jsonOut = filepath.Join(filepath.Dir(*output), "grade.json")
	}
	t, err := grade.LoadTask(filepath.Join(*bundle, "task.json"))
	if err != nil {
		return err
	}
	opt := grade.Options{PowerPoint: *pp, RepoDir: *repo}
	if *answer != "" {
		b, err := os.ReadFile(*answer)
		if err != nil {
			return err
		}
		s := string(b)
		opt.Answer = &s
	}
	res, err := grade.Grade(t, filepath.Join(*bundle, "input.pptx"), *output, opt)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(*jsonOut, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: pass=%v unintended=%d hazards=%d wrong_object=%v -> %s\n", t.ID, res.Pass, res.Unintended, len(res.HazardsLeft), res.WrongObject, *jsonOut)
	if !res.Pass {
		return exitErr(1)
	}
	return nil
}

// cmdReference writes one reference-variant output deck for a bundle.
func cmdReference(args []string) error {
	fs := flag.NewFlagSet("reference", flag.ExitOnError)
	bundle := fs.String("bundle", "", "bundle directory")
	out := fs.String("o", "", "output deck")
	variant := fs.String("variant", oracle.Reference, "reference, noop, sloppy or reserialised")
	ans := fs.String("answer-out", "", "Identify: write the reference answer here")
	fs.Parse(args)
	if *bundle == "" || *out == "" {
		fs.Usage()
		return exitErr(2)
	}
	t, err := grade.LoadTask(filepath.Join(*bundle, "task.json"))
	if err != nil {
		return err
	}
	if err := oracle.Make(*variant, t.ID, filepath.Join(*bundle, "input.pptx"), t.Slide, t.Targets, *out); err != nil {
		return err
	}
	if *ans != "" {
		if a, ok := oracle.Answers[t.ID]; ok {
			return os.WriteFile(*ans, []byte(a[0]+"\n"), 0o644)
		}
	}
	return nil
}

// cmdReferences writes every editing task's reference output into one
// directory, named for `pptoracle smoke --all`: <deck>--source.pptx is
// the unedited deck and <deck>--<task>.pptx each output made from it.
func cmdReferences(args []string) error {
	fs := flag.NewFlagSet("references", flag.ExitOnError)
	corpus := fs.String("corpus", "", "corpus root (default $PPTX_CORPUS or "+defaultCorpusDoc+")")
	tasks := fs.String("tasks", "eval/tasks", "task specs")
	out := fs.String("out", "", "output directory")
	fs.Parse(args)
	if *out == "" {
		fs.Usage()
		return exitErr(2)
	}
	ts, err := grade.LoadTasks(*tasks)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	n := 0
	for _, t := range ts {
		if t.Type == grade.Identify {
			continue
		}
		slug := deckSlug(t.Deck.Path)
		src := filepath.Join(*out, slug+"--source.pptx")
		if !grade.FileExists(src) {
			b, err := os.ReadFile(filepath.Join(corpusDir(*corpus), filepath.FromSlash(t.Deck.Path)))
			if err != nil {
				return err
			}
			if err := os.WriteFile(src, b, 0o644); err != nil {
				return err
			}
		}
		if err := oracle.Make(oracle.Reference, t.ID, src, t.Slide, t.Targets, filepath.Join(*out, slug+"--"+t.ID+".pptx")); err != nil {
			return err
		}
		n++
	}
	fmt.Printf("%d reference outputs in %s\n", n, *out)
	return nil
}

// deckSlug is a deck's base name with runs of other characters folded to
// single hyphens, so it never contains the "--" separator.
func deckSlug(p string) string {
	base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// selftest outcome expectations.
type outcome struct {
	variant string
	answer  *string
	want    bool // expected pass
}

// cmdSelftest grades each task's reference and variant outputs, reporting unexpected results.
func cmdSelftest(args []string) error {
	fs := flag.NewFlagSet("selftest", flag.ExitOnError)
	corpus := fs.String("corpus", "", "corpus root (default $PPTX_CORPUS or "+defaultCorpusDoc+")")
	tasks := fs.String("tasks", "eval/tasks", "task specs")
	keep := fs.String("keep", "", "keep bundles and outputs here instead of a temporary directory")
	only := fs.String("only", "", "comma-separated task ids")
	verbose := fs.Bool("v", false, "print failing check details")
	fs.Parse(args)
	ts, err := loadTasks(*tasks, *only)
	if err != nil {
		return err
	}
	work := *keep
	if work == "" {
		if work, err = os.MkdirTemp("", "pptxbench-selftest-"); err != nil {
			return err
		}
		defer os.RemoveAll(work)
	}
	counts := map[string][2]int{} // variant -> {as expected, total}
	byType := map[string]int{}
	bad := 0
	for _, t := range ts {
		byType[t.Type]++
		dir := filepath.Join(work, t.ID)
		if err := materialise(t, *tasks, corpusDir(*corpus), dir); err != nil {
			return err
		}
		in := filepath.Join(dir, "input.pptx")
		var cases []outcome
		if t.Type == grade.Identify {
			a := oracle.Answers[t.ID]
			right, wrong, none := a[0], a[1], ""
			cases = []outcome{
				{oracle.Reference, &right, true},
				{"wrong-answer", &wrong, false},
				{"no-answer", &none, false},
				{oracle.Sloppy, &right, false},
			}
		} else {
			cases = []outcome{
				{oracle.Reference, nil, true},
				{oracle.NoOp, nil, false},
				{oracle.Sloppy, nil, false},
				{oracle.Reserialised, nil, true},
			}
		}
		var line []string
		for _, c := range cases {
			variant := c.variant
			if variant == "wrong-answer" || variant == "no-answer" {
				variant = oracle.Reference
			}
			out := filepath.Join(dir, c.variant+".pptx")
			if err := oracle.Make(variant, t.ID, in, t.Slide, t.Targets, out); err != nil {
				return err
			}
			res, err := grade.Grade(t, in, out, grade.Options{Answer: c.answer})
			if err != nil {
				return fmt.Errorf("%s %s: %w", t.ID, c.variant, err)
			}
			b, _ := json.MarshalIndent(res, "", "  ")
			if err := os.WriteFile(filepath.Join(dir, c.variant+".grade.json"), append(b, '\n'), 0o644); err != nil {
				return err
			}
			ok := res.Pass == c.want
			// Sloppy must fail on unintended changes; wrong answers
			// must be flagged as wrong objects.
			if c.variant == oracle.Sloppy && res.Unintended == 0 {
				ok = false
			}
			if c.variant == "wrong-answer" && !res.WrongObject {
				ok = false
			}
			k := counts[c.variant]
			k[1]++
			mark := "ok"
			if ok {
				k[0]++
			} else {
				bad++
				mark = "UNEXPECTED"
			}
			counts[c.variant] = k
			line = append(line, fmt.Sprintf("%s=%s(%s,u%d)", c.variant, passWord(res.Pass), mark, res.Unintended))
			if *verbose && (!ok || !res.Pass) {
				for _, ch := range res.Checks {
					if !ch.Pass {
						fmt.Printf("    %s %s: %s\n", c.variant, ch.Type, ch.Detail)
					}
				}
				for _, ch := range res.Changes {
					fmt.Printf("    %s change: %s\n", c.variant, ch)
				}
				for _, h := range res.HazardsLeft {
					fmt.Printf("    %s hazard: %s\n", c.variant, h)
				}
				if res.Error != "" {
					fmt.Printf("    %s error: %s\n", c.variant, res.Error)
				}
			}
		}
		fmt.Printf("%-26s %-9s %s\n", t.ID, t.Type, strings.Join(line, " "))
	}
	var vs []string
	for v := range counts {
		vs = append(vs, v)
	}
	sort.Strings(vs)
	fmt.Printf("\n%d tasks:", len(ts))
	for _, ty := range grade.Types {
		fmt.Printf(" %s %d", ty, byType[ty])
	}
	fmt.Println()
	for _, v := range vs {
		fmt.Printf("  %-13s %d/%d as expected\n", v, counts[v][0], counts[v][1])
	}
	if bad > 0 {
		fmt.Printf("%d unexpected results\n", bad)
		return exitErr(1)
	}
	return nil
}

// passWord returns "pass" or "fail" for a grading result.
func passWord(p bool) string {
	if p {
		return "pass"
	}
	return "fail"
}

// cmdList prints the task table as Markdown to standard output.
func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	tasks := fs.String("tasks", "eval/tasks", "task specs")
	fs.Parse(args)
	ts, err := grade.LoadTasks(*tasks)
	if err != nil {
		return err
	}
	order := map[string]int{}
	for i, ty := range grade.Types {
		order[ty] = i
	}
	sort.SliceStable(ts, func(i, j int) bool { return order[ts[i].Type] < order[ts[j].Type] })
	w := io.Writer(os.Stdout)
	fmt.Fprintln(w, "| Task | Type | Deck | Slide | Instruction |")
	fmt.Fprintln(w, "| --- | --- | --- | --- | --- |")
	for _, t := range ts {
		deck := strings.TrimSuffix(filepath.Base(t.Deck.Path), ".pptx")
		fmt.Fprintf(w, "| `%s` | %s | %s | %s | %s |\n", t.ID, t.Type, deck, strings.TrimSuffix(filepath.Base(t.Slide), ".xml"), strings.ReplaceAll(t.Instruction, "|", "\\|"))
	}
	return nil
}
