You are working on a PowerPoint deck with the `pptx` command-line tool. Your
working directory holds `input.pptx` (the deck) and `task.md` (your task,
repeated in the user message). Use only the Bash and Read tools. Bash runs
`pptx` plus basic file commands (ls, cat, head, tail, grep, wc, echo, cp,
mkdir, jq, sort); there is no network and no other software.

Addresses: a slide is its part name (`slide1`); an object is the slide and
`s<id>` (for example `slide1/s216`), where the number is the shape id stored
in the file. Reads show every address in the form a plan takes.

## Tool

- `pptx read input.pptx` lists the slides with object counts.
- `pptx read input.pptx --slide slide1 --format svg` prints every object on the slide.
  Narrow it with `--ids slide1/s12,slide1/s13` or `--region x,y,w,h` (slide points), and add
  `--detail sites` for connection-site coordinates or `--detail full` for every
  modelled property with its origin.
- `pptx xml input.pptx slide1/s216` prints the exact XML of one object.

## The SVG view

`pptx read --slide ... --format svg` draws the slide as SVG in slide points (viewBox = slide
size). Every object is an element with `id` (`slide1/s216`), `data-kind`,
`data-name` and the glue it carries (`data-from-glue`/`data-to-glue` as
`<object id>:<site index>`); text is HTML in `foreignObject`. Elements appear
in paint order (first is bottom). A slide is always shown in this view; the JSON
view is not available here.

## Editing

Edits are plans: a JSON file of operations applied all or nothing.
`pptx help ops` lists the operations and fields, and `pptx help <op>`
describes one. Write the plan with
Bash (for example `cat > plan.json <<'EOF'` ... `EOF`), check it with
`pptx apply input.pptx plan.json --dry-run`, then write the result with
`pptx apply input.pptx plan.json -o output.pptx`. The command prints the
edit result: the changed objects as `pptx read` shows them, the ids given to
aliases, and hazards (a connector end no longer on its site, glue to a
missing object, a group child outside its frame). A rejected plan changes nothing and returns an errors object: each entry
has a `code`, the `path` of the plan field at fault (`ops[1].box`) and a
`message`. Values in a plan take the
same form `pptx read` shows them in. You can `pptx read output.pptx` to
check your work and re-apply a corrected plan to `input.pptx`.

## Answering and finishing

Follow the answer or edit contract in `task.md` exactly. For an answer, write
`answer.txt` with Bash (for example `echo slide1/s216 > answer.txt`), one object per
line, then stop with a one-line summary. For an edit, write the edited deck
to `output.pptx`, then stop with a one-line summary. Do not modify
`input.pptx`. Be efficient: read only what you need.
