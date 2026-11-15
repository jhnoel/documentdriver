// Pi extension for the pptxbench Pi runner: wraps the pptx CLI as tools so
// the model needs no bash, read, edit or write tool. The model never gives
// a file path: every tool works on input.pptx / output.pptx / answer.txt in
// the trial's work directory.
//
// The trial's configuration comes from the environment of the pi process,
// set by the runner before the model starts. The model has no tool that can
// read or change that environment or any file, so it cannot alter it:
//
//   PPTXBENCH_PPTX    absolute path of the pptx binary (a copy inside the trial dir)
//   PPTXBENCH_WORK    the work directory (input.pptx lives here)
//   PPTXBENCH_VIEW    json or svg: the read view; not a tool parameter
//   PPTXBENCH_RENDER  1 registers pptx_render
//   PPTXBENCH_TASK    identify (registers submit_answer) or edit (pptx_apply, pptx_read_output)
//
// Tool descriptions that document the CLI (plan format, flags, operations)
// are taken from the binary's own help text when the extension loads. The
// CLI follows docs/contract.md: a result is one JSON document on stdout; a
// failure is one errors object on stderr, which the tool call throws.
import { execFile } from "node:child_process";
import { copyFile, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const env = (k) => process.env[k] ?? "";
const PPTX = env("PPTXBENCH_PPTX");
const WORK = env("PPTXBENCH_WORK");
const VIEW = env("PPTXBENCH_VIEW") === "svg" ? "svg" : "json";
const RENDER = env("PPTXBENCH_RENDER") === "1";
const TASK = env("PPTXBENCH_TASK") === "edit" ? "edit" : "identify";

const SLIDE = /^slide\d+$/;
const OBJ = /^[A-Za-z0-9$][A-Za-z0-9_$.~:\/]*$/;

function run(args, { signal, timeout = 60000 } = {}) {
  return new Promise((resolve) => {
    execFile(PPTX, args, { cwd: WORK, timeout, signal, maxBuffer: 64 * 1024 * 1024, env: { PATH: "/usr/bin:/bin:/opt/homebrew/bin", HOME: tmpdir(), TMPDIR: tmpdir(), LANG: "en_US.UTF-8" } },
      (err, stdout, stderr) => resolve({ code: err ? (typeof err.code === "number" ? err.code : 1) : 0, stdout, stderr, err }));
  });
}

// helpBody is a `pptx help <topic>` text without its usage line and flags
// list, which name flags the model does not control.
async function helpBody(...topic) {
  const r = await run(["help", ...topic], { timeout: 10000 });
  const text = (r.stdout + r.stderr).trim();
  const i = text.indexOf("\n\n");
  const body = i < 0 ? text : text.slice(i + 2).trim();
  // The flags list names flags too; the tool's parameters stand for them.
  const j = body.indexOf("\nflags:");
  return j < 0 ? body : body.slice(0, j).trim();
}

const text = (t) => ({ content: [{ type: "text", text: t }], details: {} });

async function ok(args, opts) {
  const r = await run(args, opts);
  if (r.code !== 0) throw new Error((r.stderr || r.stdout || String(r.err)).trim() || `pptx exited ${r.code}`);
  return r.stdout;
}

function check(re, v, what) {
  if (typeof v !== "string" || !re.test(v)) throw new Error(`${what}: invalid value ${JSON.stringify(v)}`);
  return v;
}

function readArgs(deck, p) {
  const a = ["read", deck];
  if (p.slide !== undefined) a.push("--slide", check(SLIDE, p.slide, "slide"));
  if (p.slide !== undefined) a.push("--format", VIEW);
  if (p.detail !== undefined) {
    if (p.detail !== "sites" && p.detail !== "full") throw new Error("detail must be sites or full");
    a.push("--detail", p.detail);
  }
  if (p.ids !== undefined) {
    const ids = Array.isArray(p.ids) ? p.ids : String(p.ids).split(",");
    a.push("--ids", ids.map((x) => check(OBJ, String(x).trim(), "ids")).join(","));
  }
  if (p.region !== undefined) {
    if (!Array.isArray(p.region) || p.region.length !== 4 || !p.region.every((n) => typeof n === "number" && Number.isFinite(n))) throw new Error("region must be [x, y, w, h] numbers");
    a.push("--region", p.region.join(","));
  }
  if (p.units !== undefined) {
    if (p.units !== "pt" && p.units !== "emu") throw new Error("units must be pt or emu");
    a.push("--units", p.units);
  }
  return a;
}

const readParams = {
  type: "object",
  properties: {
    slide: { type: "string", description: "Slide part name such as slide3. Omit to list the slides." },
    ids: { type: "array", items: { type: "string" }, description: "Only these objects, as shown, e.g. [\"slide3/s12\",\"slide3/s13\"]" },
    region: { type: "array", items: { type: "number" }, minItems: 4, maxItems: 4, description: "Only objects in this region [x, y, w, h] in slide points" },
    detail: { type: "string", enum: ["sites", "full"], description: "sites: add connection-site coordinates; full: every modelled property with its origin" },
    units: { type: "string", enum: ["pt", "emu"] },
  },
  additionalProperties: false,
};

export default async function pptxExtension(pi) {
  if (!PPTX || !WORK) throw new Error("pptx extension: PPTXBENCH_PPTX and PPTXBENCH_WORK must be set");
  const [readHelp, xmlHelp, applyHelp, opsHelp, renderHelp] = await Promise.all([
    helpBody("read"), helpBody("xml"), helpBody("apply"), run(["help", "ops"], { timeout: 10000 }).then((r) => r.stdout.trim()),
    RENDER ? helpBody("render") : Promise.resolve(""),
  ]);
  const mine = [];
  const register = (t) => { mine.push(t.name); pi.registerTool(t); };
  // The runner already passes an allowlist; this makes the extension
  // enforce it too and reports the final tool set on stderr, which the
  // runner checks and records.
  pi.on?.("before_agent_start", async () => {
    let active = pi.getActiveTools?.() ?? [];
    if (active.length !== mine.length || active.some((n) => !mine.includes(n))) {
      pi.setActiveTools?.(mine);
      active = pi.getActiveTools?.() ?? active;
    }
    process.stderr.write(`pptxbench-tools: ${JSON.stringify(active)}\n`);
  });
  const viewNote = `A slide is always shown in the ${VIEW} view; the view is fixed for this task and is not a parameter.`;

  register({
    name: "pptx_list",
    label: "pptx list",
    description: "List the slides of the deck (input.pptx) in presentation order with part names, titles and object counts.",
    parameters: { type: "object", properties: {}, additionalProperties: false },
    async execute(_id, _in, signal) {
      return text(await ok(["read", "input.pptx"], { signal }));
    },
  });

  register({
    name: "pptx_read",
    label: "pptx read",
    description: `Read objects of the deck (input.pptx). ${viewNote}\n\n${readHelp}`,
    parameters: readParams,
    async execute(_id, p, signal) {
      return text(await ok(readArgs("input.pptx", p ?? {}), { signal }));
    },
  });

  register({
    name: "pptx_xml",
    label: "pptx xml",
    description: `Print the exact XML of one object of the deck (input.pptx). address is the object's address as pptx_read shows it, e.g. slide3/s17.\n\n${xmlHelp}`,
    parameters: {
      type: "object", required: ["address"],
      properties: { address: { type: "string", description: "slide3/s17, or slide3/s17~2 for the second object with id 17" } },
      additionalProperties: false,
    },
    async execute(_id, p, signal) {
      check(/^slide\d+\/s\d+(~\d+)?$/, p?.address, "address");
      return text(await ok(["xml", "input.pptx", p.address], { signal }));
    },
  });

  if (TASK === "edit") {
    register({
      name: "pptx_apply",
      label: "pptx apply",
      description: "Apply an edit plan to the original deck (input.pptx) all or nothing and write the result to output.pptx, replacing any earlier output.pptx. " +
        "Every call starts again from input.pptx, so a corrected plan replaces the previous one; it is not added to it. With dry_run true nothing is written. " +
        "Returns the edit result, or the errors object (code, path, message for each fault) when the plan is rejected.\n\n" +
        "The plan is a JSON object, passed as the plan parameter.\n\n" + applyHelp + "\n\n" + opsHelp,
      parameters: {
        type: "object", required: ["plan"],
        properties: {
          plan: { type: "object", description: "The plan: {\"ops\": [{\"op\": \"object.move\", \"at\": \"slide3/s17\", \"dx\": 10}]}" },
          dry_run: { type: "boolean", description: "Check the plan and print the result without writing output.pptx" },
        },
        additionalProperties: false,
      },
      async execute(_id, p, signal) {
        if (!p || typeof p.plan !== "object" || p.plan === null || Array.isArray(p.plan)) throw new Error("plan must be a JSON object");
        const dir = await mkdtemp(join(tmpdir(), "plan-"));
        try {
          await writeFile(join(dir, "plan.json"), JSON.stringify(p.plan));
          const args = ["apply", "input.pptx", join(dir, "plan.json")];
          const tmpOut = join(dir, "output.pptx");
          if (p.dry_run === true) args.push("--dry-run");
          else args.push("-o", tmpOut);
          const result = text(await ok(args, { signal }));
          if (p.dry_run !== true) {
            // A rejected plan leaves an earlier output.pptx as it was.
            await rm(join(WORK, "output.pptx"), { force: true });
            await copyFile(tmpOut, join(WORK, "output.pptx"));
          }
          return result;
        } finally {
          await rm(dir, { recursive: true, force: true });
        }
      },
    });

    register({
      name: "pptx_read_output",
      label: "pptx read output",
      description: `Read objects of the edited deck (output.pptx, written by pptx_apply), to check your work. Same parameters as pptx_read. ${viewNote}`,
      parameters: readParams,
      async execute(_id, p, signal) {
        return text(await ok(readArgs("output.pptx", p ?? {}), { signal }));
      },
    });
  }

  if (RENDER) {
    register({
      name: "pptx_render",
      label: "pptx render",
      description: `Render one slide of the deck as a PNG image. deck selects input.pptx (default) or the edited output.pptx.\n\n${renderHelp}`,
      parameters: {
        type: "object", required: ["slide"],
        properties: {
          slide: { type: "string", description: "Slide part name such as slide3" },
          deck: { type: "string", enum: TASK === "edit" ? ["input", "output"] : ["input"], description: "Which deck to render; default input" },
          overlay: { type: "array", items: { type: "string", enum: ["ids", "sites", "glue"] }, description: "Marks drawn on the slide" },
          ids: { type: "array", items: { type: "string" }, description: "Limit the marks to these objects" },
        },
        additionalProperties: false,
      },
      async execute(_id, p, signal) {
        const deck = p?.deck === "output" && TASK === "edit" ? "output.pptx" : "input.pptx";
        const dir = await mkdtemp(join(tmpdir(), "render-"));
        try {
          const png = join(dir, "slide.png");
          const args = ["render", deck, "--slide", check(SLIDE, p?.slide, "slide")];
          if (p?.overlay?.length) {
            for (const o of p.overlay) if (!["ids", "sites", "glue"].includes(o)) throw new Error("overlay must be ids, sites or glue");
            args.push("--overlay", p.overlay.join(","));
          }
          if (p?.ids?.length) args.push("--ids", p.ids.map((x) => check(OBJ, String(x).trim(), "ids")).join(","));
          args.push("-o", png);
          const out = await ok(args, { signal, timeout: 180000 });
          const data = (await readFile(png)).toString("base64");
          return { content: [{ type: "text", text: out.trim() || `rendered ${deck} ${p.slide}` }, { type: "image", data, mimeType: "image/png" }], details: {} };
        } finally {
          await rm(dir, { recursive: true, force: true });
        }
      },
    });
  }

  if (TASK === "identify") {
    register({
      name: "submit_answer",
      label: "submit answer",
      description: "Submit the answer: the objects the task asks for, each by its address as pptx_read shows it (e.g. slide3/s17; the shape id alone, 17 or s17, is accepted too). Writes answer.txt, one object per line; a later call replaces an earlier one. Call it once, then stop.",
      parameters: {
        type: "object", required: ["ids"],
        properties: { ids: { type: "array", items: { type: "string" }, minItems: 1, description: "Object addresses, e.g. [\"slide3/s20\"]" } },
        additionalProperties: false,
      },
      async execute(_id, p) {
        const ids = (p?.ids ?? []).map((x) => String(x).trim());
        if (!ids.length || ids.some((x) => !x || x.length > 200 || /[\r\n]/.test(x))) throw new Error("ids must be a non-empty list of single-line object ids");
        await writeFile(join(WORK, "answer.txt"), ids.join("\n") + "\n");
        return text(`answer recorded: ${ids.join(", ")}`);
      },
    });
  }
}
