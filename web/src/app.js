// MLVM playground: a model run and a local dispute, backed by WebAssembly machines in
// Web Workers.
import { bisect, adjudicate, disputeRecord } from "./dispute.js";
import { renderTimeline, Ladder, el, fmt, pct, shortHash, tooltip } from "./viz.js";

const $ = (id) => document.getElementById(id);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const lowMemory = (navigator.deviceMemory && navigator.deviceMemory <= 4) || window.innerWidth < 600;
// Saved machine copies (about 8 MB each for this guest); fewer on small devices.
const CHECKPOINT_INTERVAL = lowMemory ? 1 << 23 : 1 << 22;

let wasmModule = null;
let wasmReady = null; // settles once wasmModule is set
let determinism = null;
const act = { honest: null, result: null, prompt: "", tokens: 16, running: false };

// ---------------------------------------------------------------- workers

class Machine {
  constructor(name) {
    this.name = name;
    this.next = 0;
    this.pending = new Map();
    this.worker = new Worker(new URL("./worker.js", import.meta.url), { type: "module", name });
    this.worker.onmessage = (e) => {
      const msg = e.data;
      const p = this.pending.get(msg.id);
      if (!p) return;
      if (msg.event) return p.onEvent && p.onEvent(msg.event);
      this.pending.delete(msg.id);
      if (msg.error) p.reject(new Error(msg.error));
      else p.resolve(msg.result);
    };
    this.worker.onerror = (e) => {
      for (const p of this.pending.values()) p.reject(new Error(e.message || "worker failed"));
      this.pending.clear();
    };
    this.ready = this.call("init", null, null, { module: wasmModule });
  }
  call(cmd, args, onEvent, extra = {}) {
    const id = ++this.next;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject, onEvent });
      this.worker.postMessage({ id, cmd, args, ...extra });
    });
  }
  async run(config, onEvent) { await this.ready; return this.call("partyRun", { config }, onEvent); }
  async hashAt(step) { return this.call("hashAt", { step }); }
  async observe(round) { return this.call("observe", { round }); }
  async transitionAt(step) { return this.call("transitionAt", { step }); }
  async referee(transition, liarPost) { await this.ready; return this.call("referee", { transition, liarPost }); }
  async recheck(record) { await this.ready; return this.call("recheck", { record }); }
  close() { this.worker.terminate(); }
}

async function loadWasm() {
  const status = $("status");
  const res = await fetch("mlvm.wasm");
  if (!res.ok) throw new Error(`mlvm.wasm: HTTP ${res.status}`);
  const total = Number(res.headers.get("content-length")) || 0;
  const reader = res.body.getReader();
  const chunks = [];
  let got = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    got += value.length;
    status.textContent = `Loading the RISC-V VM and story model: ${(got / 1e6).toFixed(1)} MB${total && got <= total ? ` of ${(total / 1e6).toFixed(1)}` : ""}…`;
  }
  const bytes = new Uint8Array(got);
  let off = 0;
  for (const c of chunks) { bytes.set(c, off); off += c.length; }
  status.textContent = "Preparing the RISC-V VM in your browser…";
  return WebAssembly.compile(bytes);
}

// ---------------------------------------------------------------- Act 1

function findPrecomputed(prompt, tokens) {
  if (!determinism || !Array.isArray(determinism.runs)) return null;
  return determinism.runs.find((r) => r && r.prompt === prompt && r.maxNewTokens === tokens && Array.isArray(r.platforms)) || null;
}

function setStory(prompt, generated) {
  const story = $("story");
  story.querySelector(".prompt-text").textContent = prompt;
  story.querySelector(".gen-text").textContent = generated;
}

async function runHonest(prompt, tokens) {
  if (act.running) return;
  act.running = true;
  act.prompt = prompt;
  act.tokens = tokens;
  act.result = null;
  if (act.honest) act.honest.close();
  act.honest = new Machine("honest");
  $("run-button").disabled = true;
  $("liar-button").disabled = true;
  $("receipt").classList.add("hidden");
  for (const id of ["machines", "liar-headline", "ladder-wrap", "verdict"]) $(id).classList.add("hidden");
  $("liar-button").textContent = "Wait for the story to finish";
  for (const id of ["steps", "elapsed"]) $(id).textContent = id === "steps" ? "0" : "–";
  $("status").classList.remove("error");
  setStory(prompt, "");
  $("story").classList.add("running");
  const meter = $("meter");
  meter.style.width = "0";
  meter.classList.add("indeterminate");
  $("status").textContent = "Running the model's RISC-V program with your prompt…";

  const start = performance.now();
  let generated = "";
  let count = 0;
  let lastStep = 0;
  const onEvent = (ev) => {
    const secs = (performance.now() - start) / 1000;
    if (ev.type === "tick" || ev.type === "token") {
      lastStep = Math.max(lastStep, ev.step);
      $("steps").textContent = fmt(lastStep);
      $("elapsed").textContent = secs.toFixed(1);
      const known = findPrecomputed(prompt, tokens);
      if (known) {
        meter.classList.remove("indeterminate");
        meter.style.width = `${Math.min(100, (100 * lastStep) / known.steps)}%`;
      }
    }
    if (ev.type === "token") {
      generated += ev.text;
      count++;
      setStory(prompt, generated);
      if (!findPrecomputed(prompt, tokens)) {
        meter.classList.remove("indeterminate");
        meter.style.width = `${(100 * count) / tokens}%`;
      }
      $("status").textContent = `Generated ${count} of ${tokens} pieces of text…`;
    }
  };
  try {
    const r = await act.honest.run({
      role: "honest", prompt, maxNewTokens: tokens, profile: true,
      interval: CHECKPOINT_INTERVAL, tickEvery: 1 << 18,
    }, onEvent);
    act.result = r;
    const secs = r.millis / 1000;
    setStory(prompt, r.text);
    $("steps").textContent = fmt(r.steps);
    $("elapsed").textContent = secs.toFixed(1);
    meter.classList.remove("indeterminate");
    meter.style.width = "100%";
    $("status").textContent = `The RISC-V program finished in ${secs.toFixed(1)} seconds. Compare its final VM hash with another computer, or run it again with a deliberate mistake.`;
    showReceipt(r);
    prepareLiar(r);
  } catch (err) {
    $("status").textContent = `Error: ${err.message}`;
    $("status").classList.add("error");
    $("liar-button").textContent = "Generate a story first";
  } finally {
    $("story").classList.remove("running");
    $("run-button").disabled = false;
    act.running = false;
  }
}

function showReceipt(r) {
  $("receipt").classList.remove("hidden");
  $("r-pre").textContent = r.prestate;
  $("r-post").textContent = r.poststate;
  $("r-steps").textContent = fmt(r.steps);
  $("r-tokens").textContent = r.tokens.join(" ");

  const box = $("compare");
  box.replaceChildren();
  const row = (ok, name, hash) => {
    const div = document.createElement("div");
    div.className = "compare-row";
    const mark = document.createElement("span");
    mark.className = ok === null ? "" : ok ? "ok" : "bad";
    mark.textContent = ok === null ? "·" : ok ? "✓" : "✗";
    mark.setAttribute("aria-label", ok === null ? "" : ok ? "matches" : "differs");
    const label = document.createElement("span");
    label.textContent = name;
    const code = document.createElement("code");
    code.textContent = shortHash(hash);
    code.title = hash;
    div.append(mark, label, code);
    box.append(div);
  };
  row(true, "this browser", r.poststate);

  const params = new URLSearchParams(location.search);
  const shared = params.get("h");
  if (shared && params.get("p") === r.prompt && Number(params.get("n") || 16) === act.tokens) {
    row(shared === r.poststate, "the link you opened", shared);
  }
  const known = findPrecomputed(r.prompt, act.tokens);
  if (known) {
    const platformName = {
      "linux/amd64": "Linux on Intel or AMD",
      "linux/arm64": "Linux on ARM",
      "darwin/amd64": "Mac on Intel",
      "darwin/arm64": "Mac on Apple Silicon",
      "windows/amd64": "Windows on Intel or AMD",
      wasm: "another browser-compatible run",
    };
    for (const p of known.platforms) {
      row(p.poststate === r.poststate, platformName[p.platform] || p.platform, p.poststate);
    }
    const note = document.createElement("p");
    note.className = "note";
    note.textContent = "These saved runs used the same RISC-V program, model files, prompt and MLVM version as this run.";
    box.append(note);
  } else {
    const note = document.createElement("p");
    note.className = "note";
    note.textContent = "No other computer has a saved hash for this prompt and answer length. Try the default prompt, or share a link so someone else can run the same program and input.";
    box.append(note);
  }
}

async function shareLink() {
  if (!act.result) return;
  const url = new URL(location.href);
  url.search = new URLSearchParams({ p: act.result.prompt, n: String(act.tokens), h: act.result.poststate }).toString();
  url.hash = "run";
  try {
    if (!navigator.clipboard) throw new Error("Clipboard unavailable");
    await navigator.clipboard.writeText(url.toString());
    $("share").textContent = "Link copied — run the same input on another device";
  } catch {
    $("share").textContent = "Could not copy the link";
  }
  setTimeout(() => { $("share").textContent = "Copy a link to this result"; }, 3000);
}

// ---------------------------------------------------------------- faulty run

let liarRun = null;
let liarTokenIndex = 9;
let lastRecord = null;

// What each lie does, shown under the choices.
const LIE_ABOUT = {
  ulp: "One RISC-V floating-point instruction returns a slightly wrong number. The words may stay the same; the checker re-executes that instruction.",
  load: "One RISC-V load instruction reads a number incorrectly. The checker compares it with the value saved in VM memory.",
  int: "One RISC-V integer instruction returns a wrong value. Some mistakes stop the run, so MLVM tries one that lasts to the end.",
  output: "The VM prints a different word but does not save it in its answer. If no VM value changes, the final hashes still match.",
  token: "The model saves a different word as its answer. MLVM can find and check the RISC-V instruction that wrote it.",
};
const STEP_LIES = new Set(["ulp", "load", "int"]);
const lieKind = () => document.querySelector('input[name="lie"]:checked').value;
const utf8 = (s) => new TextEncoder().encode(s).length;

function showLieControls() {
  const kind = lieKind();
  $("lie-about").textContent = LIE_ABOUT[kind];
  $("step-controls").classList.toggle("hidden", !STEP_LIES.has(kind));
  $("lie-op-wrap").classList.toggle("hidden", kind !== "ulp");
  $("word-controls").classList.toggle("hidden", STEP_LIES.has(kind));
  updateWordNote();
}

// The honest answer as printed, one string per generated token.
function honestTokens() {
  return ((act.result && act.result.xray.marks) || []).filter((m) => m.label !== "first token starts").map((m) => m.label);
}

// What the output lie prints in place of the chosen token: the typed word, keeping the
// token's leading space.
function forgedPrint() {
  const old = honestTokens()[liarTokenIndex] || "";
  const word = $("forge-word").value.trim() || "Tim";
  return { old, forged: (old.startsWith(" ") ? " " : "") + word };
}

function updateWordNote() {
  const note = $("word-note");
  note.classList.remove("error");
  if (lieKind() !== "output" || !act.result) {
    note.textContent = lieKind() === "token" ? "Choose a word from the generated story to replace." : "";
    return;
  }
  const { old, forged } = forgedPrint();
  const ok = utf8(old) === utf8(forged);
  note.classList.toggle("error", !ok);
  note.textContent = ok
    ? `The VM will print “${forged.trim()}” where the model generated “${old.trim()}”.`
    : `Choose replacement text with ${utf8(old.trim())} UTF-8 bytes, the same length as “${old.trim()}”.`;
}

function prepareLiar(r) {
  $("liar-button").disabled = false;
  $("liar-button").textContent = "Run again with this mistake";
  renderTimeline($("lie-timeline"), r.steps, r.xray.marks || [], { lieStep: lieStepFromSlider() });

  // Functions to aim at: the hottest ones of this run.
  const fn = $("lie-fn");
  const keep = fn.value;
  fn.replaceChildren(el("option", { value: "" }, "Any function"));
  for (const f of r.xray.functions.slice(0, 8)) fn.append(el("option", { value: f.name }, `${f.name} (${pct(f.share, 0)})`));
  fn.value = [...fn.options].some((o) => o.value === keep) ? keep : "";

  // Token picker: the honest answer, one clickable span per generated token.
  const picker = $("token-picker");
  picker.replaceChildren();
  const p = document.createElement("span");
  p.className = "tok prompt";
  p.textContent = r.prompt;
  picker.append(p);
  const tokens = honestTokens();
  liarTokenIndex = Math.min(liarTokenIndex, Math.max(0, tokens.length - 1));
  tokens.forEach((t, i) => {
    const span = document.createElement("span");
    span.className = `tok${i === liarTokenIndex ? " selected" : ""}`;
    span.textContent = t;
    span.tabIndex = 0;
    span.title = `token ${i}`;
    const pick = () => {
      liarTokenIndex = i;
      picker.querySelectorAll(".tok").forEach((s) => s.classList.remove("selected"));
      span.classList.add("selected");
      updateWordNote();
    };
    span.addEventListener("click", pick);
    span.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); pick(); } });
    picker.append(span);
  });
  updateWordNote();
}

function lieStepFromSlider() {
  if (!act.result) return undefined;
  return Math.floor((Number($("lie-at").value) / 100) * act.result.steps);
}

function diffText(honest, liar) {
  // Highlight the span where the liar's text differs from the honest one.
  let a = 0;
  while (a < honest.length && a < liar.length && honest[a] === liar[a]) a++;
  let b = 0;
  while (b < honest.length - a && b < liar.length - a && honest[honest.length - 1 - b] === liar[liar.length - 1 - b]) b++;
  return [liar.slice(0, a), liar.slice(a, liar.length - b), liar.slice(liar.length - b)];
}

// setDiff fills node with text, marking where it differs from base.
function setDiff(node, prefix, base, text) {
  const [same, changed, rest] = diffText(base, text);
  node.replaceChildren(document.createTextNode(prefix + same));
  if (changed) node.append(el("mark", { class: "diff" }, changed));
  node.append(document.createTextNode(rest));
}

// What the liar's deviation did, in one line.
function deviationLine(d, fn) {
  const where = `${d.instruction}${fn ? ` in ${fn}` : ""} (RISC-V instruction #${fmt(d.step)})`;
  switch (d.kind) {
    case "token":
    case "commit":
      return `${where}: the second run stores ${d.forged} instead of ${d.honest} at ${d.location}`;
    case "output":
      return `${where}: printed output has ${d.forged} instead of ${d.honest} at ${d.location}`;
    default:
      return `${where}: ${d.location} should be ${d.honest}; the second run kept ${d.forged}`;
  }
}

async function runLiar() {
  const honestResult = act.result;
  if (!honestResult || liarRun) return;
  const kind = lieKind();
  let fault;
  let print = null;
  if (STEP_LIES.has(kind)) {
    fault = { kind, atStep: lieStepFromSlider(), mnemonic: kind === "ulp" ? $("lie-op").value : "", function: $("lie-fn").value };
  } else if (kind === "token") {
    fault = { kind, tokenIndex: liarTokenIndex, word: $("forge-word").value.trim() || "Tim" };
  } else {
    print = forgedPrint();
    const start = honestResult.stdout.indexOf("generated: ");
    if (utf8(print.old) !== utf8(print.forged) || start < 0) {
      updateWordNote();
      $("forge-word").focus();
      return;
    }
    // The chosen token's byte offset in the honest machine's stdout.
    const before = honestResult.stdout.slice(0, start + "generated: ".length) + honestTokens().slice(0, liarTokenIndex).join("");
    fault = { kind, outputOffset: utf8(before), output: print.forged };
  }
  lastRecord = null;
  $("liar-button").disabled = true;
  $("run-button").disabled = true;
  $("machines").classList.remove("hidden");
  $("ladder-wrap").classList.add("hidden");
  $("verdict").classList.add("hidden");
  $("liar-headline").classList.add("hidden");
  $("m-honest-text").textContent = honestResult.prompt + honestResult.text;
  $("m-honest-post").textContent = honestResult.poststate;
  $("m-liar-text").textContent = "running the same program…";
  $("m-liar-post").textContent = "running…";
  $("attempts").replaceChildren();
  const meter = $("liar-meter");
  meter.style.width = "0";
  $("machines").scrollIntoView({ behavior: "smooth", block: "start" });

  const liar = new Machine("liar");
  liarRun = liar;
  let printed = "";
  try {
    const l = await liar.run({
      role: "liar", prompt: honestResult.prompt, maxNewTokens: act.tokens, fault,
      honestPoststate: honestResult.poststate, honestSteps: honestResult.steps,
      interval: CHECKPOINT_INTERVAL, tickEvery: 1 << 18,
    }, (ev) => {
      if (ev.type === "token") printed += ev.text;
      if (ev.type === "tick") meter.style.width = `${Math.min(100, (100 * ev.step) / honestResult.steps)}%`;
      if (ev.type === "restart") {
        printed = "";
        meter.style.width = "0";
        $("m-liar-text").textContent = `The changed value disappeared or stopped the program. Trying another instruction (attempt ${ev.run})…`;
      }
    });
    meter.style.width = "100%";
    // The misprinting liar is shown by what it printed; the others by what they committed.
    setDiff($("m-liar-text"), honestResult.prompt, honestResult.text, kind === "output" ? printed : l.text);
    $("m-liar-post").textContent = l.poststate;

    const list = $("attempts");
    for (const a of l.attempts || []) {
      const at = `${a.instruction} in ${a.function} (RISC-V instruction #${fmt(a.step)})`;
      let text = `${at}: the changed value was ${kind === "int" ? "overwritten" : "erased by rounding"} ${fmt(a.erasedAfter)} instructions later`;
      if (a.kept) text = `${at}: the changed value remained in the final VM state`;
      else if (a.vanished) text = `${at}: the changed value disappeared before the program finished`;
      else if (a.faulted) text = `${at}: the change stopped the program or made it run too long`;
      list.append(el("li", a.kept ? { class: "kept" } : {}, text));
    }
    if (l.deviation && !STEP_LIES.has(l.deviation.kind)) {
      list.append(el("li", { class: "kept" }, deviationLine(l.deviation, l.deviationFunction)));
    }

    const headline = $("liar-headline");
    headline.classList.remove("hidden");
    if (l.poststate === honestResult.poststate) {
      headline.textContent = kind === "output"
        ? `The screen shows “${print.forged.trim()}”, but the saved answer is still “${print.old.trim()}”. Both VMs finished with the same hash because printed output is not included in it. Try changing “The answer saved by the model” instead.`
        : "Later instructions erased the mistake. Both VMs finished with the same hash, so there is no remaining difference to check.";
      return;
    }
    const erased = (l.attempts || []).filter((a) => !a.kept).length;
    headline.textContent = l.text === honestResult.text
      ? `The stories match, but the final VM hashes differ.${erased ? ` MLVM tried ${erased} earlier ${erased === 1 ? "mistake" : "mistakes"} before finding one that remained.` : ""} Now MLVM locates a disputed RISC-V instruction.`
      : "The stories and final VM hashes differ. Now MLVM locates a disputed RISC-V instruction.";

    // Bisection: questions to both machines run in parallel workers.
    const total = Math.max(honestResult.steps, l.steps);
    $("ladder-wrap").classList.remove("hidden");
    const ladder = new Ladder($("ladder"), total);
    $("ladder-title").textContent = `Comparing ${fmt(total)} RISC-V instructions`;
    renderTimeline($("lie-timeline"), honestResult.steps, honestResult.xray.marks || [], { lieStep: l.deviation?.step });
    const t0 = performance.now();
    const { lo, rounds, noDispute } = await bisect(act.honest, liar, total, async (round) => {
      ladder.add(round);
      await sleep(90);
    });
    if (noDispute) {
      headline.textContent = "The changed value left no trace in the final VM state, so there is no remaining difference to check.";
      return;
    }
    $("ladder-title").textContent = `${rounds.length} comparisons found one disputed instruction among ${fmt(total)} (${((performance.now() - t0) / 1000).toFixed(1)} s)`;

    const referee = new Machine("referee");
    const { verdict } = await adjudicate(act.honest, liar, referee, lo);
    referee.close();
    lastRecord = disputeRecord({
      guest: `TinyStories-260K decoder, ${JSON.stringify(honestResult.prompt)}, ${act.tokens} tokens`,
      prompt: honestResult.prompt, honest: honestResult, liar: l, rounds, verdict,
    });
    $("verdict").classList.remove("hidden");
    renderVerdict($("verdict-body"), verdict, kind === "token");
    $("verdict").scrollIntoView({ behavior: "smooth", block: "nearest" });
    renderTimeline($("lie-timeline"), honestResult.steps, honestResult.xray.marks || [], { lieStep: l.deviation?.step, foundStep: lo });
  } catch (err) {
    $("liar-headline").classList.remove("hidden");
    $("liar-headline").textContent = `Error: ${err.message}`;
  } finally {
    liar.close();
    liarRun = null;
    $("liar-button").disabled = false;
    $("run-button").disabled = false;
  }
}

// renderVerdict fills container with the referee's ruling on the disputed instruction.
function renderVerdict(container, v, forged) {
  const inst = el("p", { class: "instruction" });
  inst.append(el("code", {}, v.instruction), el("span", { class: "muted" }, ` in ${v.function || "?"} · instruction ${fmt(v.step)} · program counter 0x${v.pc.toString(16)}`));
  const dl = el("dl", { class: "kv hashes" });
  for (const [key, label, hash] of [
    ["", "the runs agree before this instruction", v.agreedPrestate],
    ["honest", "original run after the instruction", v.honestPost],
    ["liar", "second run after the instruction", v.liarPost],
    ["", "checker after re-executing the instruction", v.refereePost],
  ]) {
    const dt = el("dt");
    if (key) dt.append(el("span", { class: `key key-${key}` }));
    dt.append(document.createTextNode(label));
    const dd = el("dd");
    dd.append(el("code", {}, hash));
    dl.append(dt, dd);
  }
  const caught = v.winner === "honest";
  const winner = el("p", { class: "winner" });
  winner.append(el("span", { class: caught ? "ok" : "bad" }, caught ? "✓ " : "✗ "), document.createTextNode(caught
    ? `Re-executing this RISC-V instruction gives the original run's result. The second run ${forged ? "saved a different answer" : "used the wrong value"}.`
    : `Unexpected ruling: ${v.winner}.`));
  const bytes = (v.stateWitness.length - 2) / 2;
  const evm = v.evm ? ` RISCV.sol's result matches, for ${fmt(v.evm.gasUsed)} gas.` : "";
  const details = el("details", { class: "technical-details verdict-details" });
  details.append(
    el("summary", {}, "See the exact instruction and how it was checked"), inst, dl,
    el("p", { class: "note" }, `Checked by ${v.method} using a ${fmt(bytes)}-byte snapshot of the VM state before this instruction and ${fmt(v.proofBytes)} bytes proving the memory values it used.${evm}`),
    el("p", { class: "note boundary" }, v.boundary),
  );
  container.replaceChildren(el("p", { class: "eyebrow" }, "Check complete"), winner, details);
}

function download(name, text) {
  const url = URL.createObjectURL(new Blob([text], { type: "application/json" }));
  const a = el("a", { href: url, download: name });
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// ---------------------------------------------------------------- referee and replays

let checker = null;

// referee rules on a dispute record (its JSON text) in a worker of its own.
async function referee(text) {
  await wasmReady;
  if (!checker) checker = new Machine("checker");
  return checker.recheck(text);
}

// renderCheck shows what the referee checked, or why the record does not hold up.
function renderCheck(container, rec, res, err) {
  if (err || !res) {
    const ul = el("ul", { class: "crosses" });
    ul.append(el("li", {}, `This file could not be verified: ${err ? err.message : "the checker returned no result"}`));
    container.replaceChildren(ul);
    return;
  }
  const v = res.verdict;
  const total = Math.max(rec.honestSteps, rec.liarSteps);
  const ul = el("ul", { class: "checks" });
  const li = (...parts) => {
    const item = el("li");
    for (const p of parts) item.append(typeof p === "string" ? document.createTextNode(p) : p);
    ul.append(item);
  };
  li(`${rec.rounds.length} recorded hash comparisons narrow ${fmt(total)} RISC-V instructions to instruction ${fmt(v.step)}.`);
  li(`The saved VM state before that instruction matches the hash both runs agreed on: `, el("code", {}, shortHash(v.agreedPrestate)), ".");
  li("The memory proof shows that the RISC-V instruction was ", el("code", {}, v.instruction), ".");
  const who = { honest: "matching the original run's result for this instruction.", liar: "matching the second run's result for this instruction." }[v.winner]
    || "matching neither run.";
  li(`${v.method.startsWith("SoftFloat") ? "Software floating-point arithmetic re-executes it" : "The Go checker re-executes it"}: `, el("code", {}, shortHash(v.refereePost)), `, ${who}`);
  const parts = [ul];
  if (res.matchesRecord) li("The result matches the verdict saved in this file.");
  else {
    const bad = el("ul", { class: "crosses" });
    bad.append(el("li", {}, `The file's saved verdict differs from this check. It says ${{ honest: "the original run", liar: "the second run" }[rec.verdict.winner] || rec.verdict.winner} is right, with hash ${shortHash(rec.verdict.refereePost)}.`));
    parts.push(bad);
  }
  const evm = rec.verdict.evm && !v.evm
    ? ` The native mlvm referee also runs RISCV.sol itself in an EVM; the record says ${fmt(rec.verdict.evm.gasUsed)} gas.`
    : "";
  parts.push(el("p", { class: "note" }, `Re-executed this RISC-V instruction in ${(res.micros / 1000).toFixed(1)} ms without rerunning the model. The saved file checks this instruction; it does not prove that either computer executed every earlier instruction.${evm}`));
  container.replaceChildren(...parts);
}

let replaying = null; // { rec, text } of the open replay
let replayToken = 0;

// openReplay replays a recorded dispute from the gallery: the two machines, the questions
// and the verdict, from its record. Nothing is re-run until the reader asks the referee.
async function openReplay(entry, lie, row) {
  const token = ++replayToken;
  replaying = null;
  document.querySelectorAll("#gallery-table tr.playing").forEach((r) => r.classList.remove("playing"));
  row.classList.add("playing");
  $("replay").classList.remove("hidden");
  $("replay-title").textContent = `${entry.title}: ${lie.lie}`;
  $("replay-lede").textContent = "Loading the saved check…";
  $("rp-ladder-title").textContent = "Finding the difference";
  for (const id of ["rp-honest-text", "rp-liar-text", "rp-honest-post", "rp-liar-post"]) $(id).textContent = "";
  for (const id of ["rp-attempts", "rp-ladder", "rp-verdict", "rp-check-out"]) $(id).replaceChildren();
  $("rp-check").disabled = true;
  $("rp-download").href = lie.record;
  $("rp-download").download = `mlvm-dispute-${entry.id}-${lie.kind}.json`;
  $("replay").scrollIntoView({ behavior: "smooth", block: "start" });

  let text;
  try {
    const res = await fetch(lie.record);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    text = await res.text();
  } catch (err) {
    $("replay-lede").textContent = `Could not load the saved check: ${err.message}`;
    return;
  }
  if (token !== replayToken) return;
  const rec = JSON.parse(text);
  replaying = { rec, text };
  $("rp-check").disabled = false;
  const total = Math.max(rec.honestSteps, rec.liarSteps);
  const steps = rec.honestSteps === rec.liarSteps
    ? `${fmt(total)} RISC-V instructions in each run`
    : `${fmt(rec.honestSteps)} RISC-V instructions in the original run and ${fmt(rec.liarSteps)} in the second`;
  $("replay-lede").textContent = `${steps}. The hash comparisons below were saved during the original test. Replaying them here does not rerun the model.`;

  const answer = (node, text, committed, base, baseCommitted) => {
    setDiff(node, "", base, text);
    if (committed) {
      const line = el("span", { class: "commit-line" }, "saved answer: ");
      const code = el("code");
      setDiff(code, "", baseCommitted, committed);
      line.append(code);
      node.append(line);
    }
  };
  // A decoder record names its prompt; other guests' answers are a line of what they printed.
  const prompt = rec.prompt || "";
  answer($("rp-honest-text"), prompt + rec.honestText, rec.honestCommitted, prompt + rec.honestText, rec.honestCommitted || "");
  answer($("rp-liar-text"), prompt + rec.liarText, rec.liarCommitted, prompt + rec.honestText, rec.honestCommitted || "");
  $("rp-honest-post").textContent = rec.honestPost;
  $("rp-liar-post").textContent = rec.liarPost;

  const atts = rec.attempts || [];
  const erased = atts.filter((a) => !a.kept && !a.vanished && !a.faulted).length;
  const vanished = atts.filter((a) => a.vanished).length;
  const faulted = atts.filter((a) => a.faulted).length;
  const list = $("rp-attempts");
  if (erased) list.append(el("li", {}, `${erased} earlier ${erased === 1 ? "changed value was" : "changed values were"} erased by later instructions`));
  if (vanished) list.append(el("li", {}, `${vanished} ${vanished === 1 ? "changed value disappeared" : "changed values disappeared"} before the program finished`));
  if (faulted) list.append(el("li", {}, `${faulted} ${faulted === 1 ? "change stopped" : "changes stopped"} the program or made it run too long`));
  // The gallery's short function names: Rust paths inside <...> keep their last segment.
  const verdict = { ...rec.verdict, function: lie.function || rec.verdict.function };
  if (rec.deviation) {
    const fn = verdict.step === rec.deviation.step ? verdict.function : "";
    list.append(el("li", { class: "kept" }, deviationLine(rec.deviation, fn)));
  }

  const ladder = new Ladder($("rp-ladder"), total);
  $("rp-ladder-title").textContent = `Comparing ${fmt(total)} RISC-V instructions`;
  for (const round of rec.rounds) {
    if (token !== replayToken) return;
    ladder.add(round);
    await sleep(60);
  }
  if (token !== replayToken) return;
  $("rp-ladder-title").textContent = `${rec.rounds.length} comparisons found one disputed instruction among ${fmt(total)}`;
  renderVerdict($("rp-verdict"), verdict, lie.kind === "token" || lie.kind === "commit");
}

// ---------------------------------------------------------------- wiring

function wire() {
  $("run-form").addEventListener("submit", (e) => {
    e.preventDefault();
    const prompt = $("prompt").value.trim() || "Once upon a time";
    runHonest(prompt, Number($("tokens").value));
  });
  $("prompt").addEventListener("input", () => {
    if (!act.running) setStory($("prompt").value, "");
  });
  $("share").addEventListener("click", shareLink);
  $("liar-form").addEventListener("submit", (e) => { e.preventDefault(); runLiar(); });
  $("lie-at").addEventListener("input", () => {
    $("lie-at-out").textContent = `${$("lie-at").value}%`;
    if (act.result) renderTimeline($("lie-timeline"), act.result.steps, act.result.xray.marks || [], { lieStep: lieStepFromSlider() });
  });
  $("step-controls").addEventListener("toggle", () => {
    if ($("step-controls").open && act.result) {
      renderTimeline($("lie-timeline"), act.result.steps, act.result.xray.marks || [], { lieStep: lieStepFromSlider() });
    }
  });
  for (const radio of document.querySelectorAll('input[name="lie"]')) radio.addEventListener("change", showLieControls);
  $("forge-word").addEventListener("input", updateWordNote);
  showLieControls();
  $("v-download").addEventListener("click", () => {
    if (lastRecord) download("mlvm-dispute.json", JSON.stringify(lastRecord, null, 2) + "\n");
  });

  $("replay-close").addEventListener("click", () => {
    replayToken++;
    $("replay").classList.add("hidden");
    document.querySelectorAll("#gallery-table tr.playing").forEach((r) => r.classList.remove("playing"));
  });
  $("rp-check").addEventListener("click", async () => {
    if (!replaying) return;
    const { rec, text } = replaying;
    const out = $("rp-check-out");
    $("rp-check").disabled = true;
    out.textContent = "Checking the saved run…";
    try {
      renderCheck(out, rec, await referee(text));
    } catch (err) {
      renderCheck(out, rec, null, err);
    } finally {
      $("rp-check").disabled = false;
    }
  });
  $("record-file").addEventListener("change", async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    const out = $("record-out");
    out.textContent = "Checking the saved run…";
    let text;
    let rec;
    try {
      text = await file.text();
      rec = JSON.parse(text);
    } catch (err) {
      renderCheck(out, null, null, new Error(`${file.name} is not JSON: ${err.message}`));
      return;
    }
    try {
      renderCheck(out, rec, await referee(text));
    } catch (err) {
      renderCheck(out, rec, null, err);
    }
  });

  const params = new URLSearchParams(location.search);
  if (params.get("p")) {
    $("prompt").value = params.get("p");
    setStory(params.get("p"), "");
    if (["4", "8", "16"].includes(params.get("n"))) $("tokens").value = params.get("n");
    if (params.get("h")) {
      const banner = $("shared");
      banner.classList.remove("hidden");
      banner.textContent = `The shared run ended with VM hash ${shortHash(params.get("h"))}. Run the same story prompt on your device to compare hashes.`;
    }
  }
}

// "Beyond the toy model": disputes and profiles recorded natively with scripts/gallery.sh.
async function showGallery() {
  let data;
  try {
    const res = await fetch("gallery.json");
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    data = await res.json();
  } catch {
    $("gallery-status").textContent = "The recorded examples could not be loaded. You can still open a saved check file below.";
    $("gallery-details").classList.add("hidden");
    return;
  }
  const entries = data.entries.filter((e) => e.lies.length > 0);
  if (entries.length === 0) {
    $("gallery-status").textContent = "This build has no recorded tests with differing VM hashes. You can still open a saved check file below.";
    $("gallery-details").classList.add("hidden");
    return;
  }
  const td = (text, cls) => {
    const c = document.createElement("td");
    if (cls) c.className = cls;
    c.textContent = text;
    return c;
  };
  const body = $("gallery-table").tBodies[0];
  body.replaceChildren();
  let disputes = 0;
  let found = 0;
  const replayRows = new Map();
  for (const e of entries) {
    e.lies.forEach((l, i) => {
      if (!l.noDispute) disputes++;
      if (l.instruction) found++;
      const tr = document.createElement("tr");
      replayRows.set(`${e.id}:${l.kind}`, tr);
      if (i === 0) tr.className = "group-start";
      if (i === 0) {
        const name = document.createElement("td");
        name.className = "gallery-program";
        name.rowSpan = e.lies.length;
        const title = document.createElement("strong");
        title.textContent = e.title;
        const about = document.createElement("span");
        about.className = "muted";
        about.textContent = e.about;
        name.append(title, about);
        const n = td(fmt(e.instructions), "num");
        n.rowSpan = e.lies.length;
        tr.append(name, n);
      }
      tr.append(td(l.lie));
      if (l.noDispute) {
        const why = td(`Both runs ended with the same VM hash. The second run printed "${l.liar.slice(0, 32)}…", but printed text is not part of the VM hash.`, "gallery-at");
        why.colSpan = 3;
        tr.append(td("–", "num"), why);
        body.append(tr);
        return;
      }
      tr.append(td(String(l.rounds), "num"));
      const at = document.createElement("td");
      at.className = "gallery-at";
      const code = document.createElement("code");
      code.textContent = l.instruction;
      at.append(code, document.createTextNode(` in ${l.function}`));
      tr.append(at, td(l.judge === "RISCV.sol" ? `RISCV.sol in an EVM, ${fmt(l.gas)} gas` : "SoftFloat re-execution"));
      const replay = td("", "replay-cell");
      if (l.record) {
        const button = el("button", { class: "ghost", type: "button", "aria-label": `Replay the search on ${e.title}: ${l.lie}` }, "Replay");
        button.addEventListener("click", () => openReplay(e, l, tr));
        replay.append(button);
      }
      tr.append(replay);
      body.append(tr);
    });
  }
  const cpu = entries.flatMap((e) => e.lies.map((l) => l.cpuSeconds)).filter((x) => x != null);
  const span = cpu.length
    ? ` These tests took ${Math.min(...cpu) < 1 ? "under a second" : `${Math.round(Math.min(...cpu))} s`} to ${Math.round(Math.max(...cpu) / 60)} min of CPU time on an Apple M3 Max, including both VM runs.`
    : "";
  $("gallery-note").textContent =
    `${found} of ${disputes} tests located the changed RISC-V instruction; re-executing it matched the original run.${span} ` +
    "The test script and full results are in the GitHub repository.";

  $("gallery-status").textContent = "The saved SmolLM2 example is ready. More recorded tests are below.";
  const featured = $("featured-runs");
  featured.replaceChildren();
  const descriptions = {
    smollm2: ["Saved example", "We changed the result of one RISC-V floating-point multiplication. The model still printed the same text."],
  };
  const compact = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });
  for (const [id, [label, about]] of Object.entries(descriptions)) {
    const entry = entries.find((e) => e.id === id);
    if (!entry) continue;
    const lie = entry.lies.find((l) => l.kind === "ulp" && l.record)
      || entry.lies.find((l) => l.kind === "token" && l.record)
      || entry.lies.find((l) => l.record);
    if (!lie) continue;
    const card = el("article", { class: "model-card" });
    const button = el("button", { type: "button", "aria-label": `Open the saved check for ${entry.title}` }, "Open the saved check →");
    button.addEventListener("click", () => openReplay(entry, lie, replayRows.get(`${id}:${lie.kind}`)));
    card.append(
      el("p", { class: "eyebrow" }, label), el("h3", {}, entry.title), el("p", {}, about),
      el("span", { class: "model-count" }, compact.format(lie.instructions)),
      el("p", {}, "RISC-V instructions in the run"),
      el("p", { class: "model-result" }, `The changed instruction was located and checked after ${lie.rounds} hash comparisons.`), button,
    );
    featured.append(card);
  }


}

async function main() {
  wire();
  tooltip();
  showGallery();
  fetch("determinism.json").then((r) => (r.ok ? r.json() : null)).then((d) => {
    determinism = d;
    if (d) $("build-info").textContent = `build ${d.commit.slice(0, 7)}`;
  }).catch(() => {});
  wasmReady = loadWasm().then((module) => (wasmModule = module));
  try {
    await wasmReady;
    $("run-button").disabled = false;
    $("run-button").textContent = "Generate a story";
    $("status").textContent = "Ready. Run the RISC-V model program in the VM, compare its final hash, then try a second run with a deliberate mistake.";
  } catch (err) {
    $("status").textContent = `Could not load MLVM: ${err.message}`;
    $("status").classList.add("error");
  }
}

main();
