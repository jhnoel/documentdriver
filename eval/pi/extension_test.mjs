// Exercises the extension without pi or a model: loads it against a fake
// pi, checks the registered tool set per condition and calls the tools.
// Run by TestPiExtension (node eval/pi/extension_test.mjs <pptx> <deck>).
import assert from "node:assert/strict";
import { copyFileSync, mkdtempSync, readFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const [pptx, deck] = process.argv.slice(2);
const ext = pathToFileURL(new URL("./pptx-extension.mjs", import.meta.url).pathname).href;

async function load(task, render, view = "json") {
  const work = mkdtempSync(join(tmpdir(), "extwork-"));
  copyFileSync(deck, join(work, "input.pptx"));
  Object.assign(process.env, { PPTXBENCH_PPTX: pptx, PPTXBENCH_WORK: work, PPTXBENCH_VIEW: view, PPTXBENCH_RENDER: render ? "1" : "0", PPTXBENCH_TASK: task });
  const tools = {};
  const m = await import(ext + `?${Math.random()}`);
  await m.default({ registerTool: (t) => (tools[t.name] = t) });
  return { tools, work };
}

let { tools, work } = await load("identify", false);
assert.deepEqual(Object.keys(tools).sort(), ["pptx_list", "pptx_read", "pptx_xml", "submit_answer"]);
assert.match(tools.pptx_read.description, /always shown in the json view/);
const list = JSON.parse((await tools.pptx_list.execute("1", {})).content[0].text);
assert.match(list.base, /^sha256:[0-9a-f]{64}$/);
assert.ok(list.slides.length > 0);
const slide = list.slides[0].slide;
const out = (await tools.pptx_read.execute("2", { slide })).content[0].text;
assert.ok(JSON.parse(out).objects.every((o) => o.id.startsWith(slide + "/s")) && out.length > 100);
await assert.rejects(tools.pptx_read.execute("3", { slide: "../../etc/passwd" }), /invalid/);
await assert.rejects(tools.pptx_read.execute("3", { slide, ids: ["-o"] }), /invalid/);
await assert.rejects(tools.pptx_xml.execute("4", { address: "slide1/s1 --x" }), /invalid/);
await tools.submit_answer.execute("5", { ids: ["s7", "s9"] });
assert.equal(readFileSync(join(work, "answer.txt"), "utf8"), "s7\ns9\n");

({ tools, work } = await load("edit", true, "svg"));
assert.deepEqual(Object.keys(tools).sort(), ["pptx_apply", "pptx_list", "pptx_read", "pptx_read_output", "pptx_render", "pptx_xml"]);
assert.match(tools.pptx_apply.description, /object\.add +at, kind/); // from pptx help ops
assert.match(tools.pptx_read.description, /always shown in the svg view/);
const plan = { units: "pt", ops: [{ op: "object.add", at: slide, kind: "shape", name: "probe", geom: "rect", box: [10, 10, 50, 50] }] };
await tools.pptx_apply.execute("6", { plan, dry_run: true });
assert.ok(!existsSync(join(work, "output.pptx")));
await tools.pptx_apply.execute("7", { plan });
await tools.pptx_apply.execute("8", { plan }); // re-apply replaces the output
assert.ok(existsSync(join(work, "output.pptx")));
assert.match((await tools.pptx_read_output.execute("9", { slide })).content[0].text, /probe/);
await assert.rejects(tools.pptx_apply.execute("10", { plan: { ops: [{ op: "nope" }] } }), /"code":"invalid_plan","path":"ops\[0\]\.op"/);
if (process.env.PPTXBENCH_TEST_RENDER === "1") {
  const r = await tools.pptx_render.execute("11", { slide, deck: "output", overlay: ["ids"] });
  assert.equal(r.content[1].type, "image");
  assert.ok(r.content[1].data.length > 1000);
}
console.log("ok");
