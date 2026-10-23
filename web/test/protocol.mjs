// End-to-end check of the browser protocol in Node: three WebAssembly instances (honest,
// liar, referee) in worker threads play a full dispute, and the honest machine's
// poststate is printed so CI can compare it with native builds.
//
//   node web/test/protocol.mjs web/dist/mlvm.wasm "$(go env GOROOT)/lib/wasm/wasm_exec.js" [--quick]
import { Worker } from "node:worker_threads";
import fs from "node:fs";
import { bisect, adjudicate, disputeRecord } from "../src/dispute.js";

const [wasmPath, wasmExec, ...flags] = process.argv.slice(2);
const quick = flags.includes("--quick");
const module = await WebAssembly.compile(fs.readFileSync(wasmPath));

class Machine {
  constructor(name) {
    this.name = name;
    this.next = 0;
    this.pending = new Map();
    this.worker = new Worker(new URL("./node-worker.mjs", import.meta.url), {
      workerData: { module, wasmExec },
    });
    this.ready = new Promise((resolve) => {
      this.worker.on("message", (msg) => {
        if (msg.ready) return resolve();
        const p = this.pending.get(msg.id);
        if (!p) return;
        if (msg.event) return p.onEvent && p.onEvent(msg.event);
        this.pending.delete(msg.id);
        if (msg.error) p.reject(new Error(`${this.name}: ${msg.error}`));
        else p.resolve(msg.result);
      });
    });
  }
  async call(cmd, args, onEvent) {
    await this.ready;
    const id = ++this.next;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject, onEvent });
      this.worker.postMessage({ id, cmd, args });
    });
  }
  run(config, onEvent) { return this.call("partyRun", { config }, onEvent); }
  hashAt(step) { return this.call("hashAt", { step }); }
  observe(round) { return this.call("observe", { round }); }
  transitionAt(step) { return this.call("transitionAt", { step }); }
  referee(transition, liarPost) { return this.call("referee", { transition, liarPost }); }
  recheck(record) { return this.call("recheck", { record: JSON.stringify(record) }); }
  close() { return this.worker.terminate(); }
}

const prompt = "Once upon a time";
const maxNewTokens = quick ? 2 : 16;
const t0 = Date.now();
const honest = new Machine("honest");
const liar = new Machine("liar");
const referee = new Machine("referee");

let text = "";
const honestRun = honest.run({ role: "honest", prompt, maxNewTokens, profile: true }, (ev) => {
  if (ev.type === "token") text += ev.text;
});
const h = await honestRun;
console.log(`honest: ${h.steps} steps in ${h.millis} ms (${(h.steps / h.millis / 1000).toFixed(1)} MIPS)`);
console.log(`honest: "${prompt}${text}"`);
console.log(`honest: poststate ${h.poststate}`);
if (text !== h.text) throw new Error(`streamed text ${JSON.stringify(text)} != committed ${JSON.stringify(h.text)}`);
console.log(`xray: ${h.xray.functions[0].name} ${(100 * h.xray.functions[0].share).toFixed(1)}%, float ${(100 * h.xray.floatShare).toFixed(1)}%`);

const lie = await liar.run({
  role: "liar", prompt, maxNewTokens, honestPoststate: h.poststate,
  fault: { kind: "ulp", atStep: Math.floor(h.steps / 2) },
});
if (lie.poststate === h.poststate) throw new Error("the lie left no trace even after retries");
console.log(`liar: ${lie.steps} steps, poststate ${lie.poststate}, text ${lie.text === h.text ? "identical" : "changed"}`);
console.log(`liar: ${lie.attempts.length - 1} lie(s) erased before ${lie.deviation.instruction} in ${lie.deviationFunction} stuck`);

const { lo, rounds, noDispute } = await bisect(honest, liar, Math.max(h.steps, lie.steps));
if (noDispute) throw new Error("bisect reported no dispute for different poststates");
const { verdict } = await adjudicate(honest, liar, referee, lo);
console.log(`bisection: ${rounds.length} rounds -> step ${lo}: ${verdict.instruction} in ${verdict.function}`);
console.log(`verdict: ${verdict.winner} wins (${verdict.method.split(",")[0]})`);
if (lo !== lie.deviation.step) throw new Error(`bisection found ${lo}, lie was at ${lie.deviation.step}`);
if (verdict.winner !== "honest") throw new Error(`referee ruled for ${verdict.winner}`);

// The record of the dispute, checked again from the record alone; a tampered one is not.
const record = disputeRecord({ guest: "TinyStories-260K decoder", prompt, honest: h, liar: lie, rounds, verdict });
const again = await referee.recheck(record);
if (!again.matchesRecord || again.verdict.winner !== "honest") throw new Error(`recheck: ${JSON.stringify(again)}`);
console.log(`recheck: the record holds up (${again.micros} µs)`);
const tampered = structuredClone(record);
tampered.rounds[tampered.rounds.length - 1].mid += 1;
if (await referee.recheck(tampered).then(() => true, () => false)) throw new Error("recheck accepted a tampered record");

// The other lies. Load and integer lies are disputed like the ULP lie; a forged printed
// word leaves no trace in the state, so there is nothing to dispute.
const tokens = (h.xray.marks || []).filter((m) => m.label !== "first token starts").map((m) => m.label);
const at = h.stdout.indexOf("generated: ") + "generated: ".length + tokens.slice(0, 1).join("").length;
const word = tokens[1];
const lies = [
  { kind: "load", atStep: Math.floor(h.steps / 2) },
  { kind: "int", atStep: Math.floor(h.steps / 2), function: "matmul" },
  { kind: "output", outputOffset: at, output: "x".repeat(word.length) },
];
for (const fault of lies) {
  const m = new Machine(`liar-${fault.kind}`);
  const l = await m.run({ role: "liar", prompt, maxNewTokens, honestPoststate: h.poststate, honestSteps: h.steps, fault });
  const total = Math.max(h.steps, l.steps);
  const b = await bisect(honest, m, total);
  if (fault.kind === "output") {
    if (!b.noDispute) throw new Error("a forged printed word left a trace in the state");
    if (!l.stdout.includes("x".repeat(word.length))) throw new Error(`the liar did not print the forged word: ${JSON.stringify(l.stdout)}`);
    console.log(`${fault.kind}: printed ${JSON.stringify("x".repeat(word.length))} for ${JSON.stringify(word)}; same poststate, nothing to dispute`);
  } else {
    if (b.noDispute) throw new Error(`${fault.kind}: no dispute`);
    const { verdict: v } = await adjudicate(honest, m, referee, b.lo);
    if (b.lo !== l.deviation.step || v.winner !== "honest") throw new Error(`${fault.kind}: found ${b.lo}, lie at ${l.deviation.step}, ${v.winner} wins`);
    const r = await referee.recheck(disputeRecord({ guest: "decoder", prompt, honest: h, liar: l, rounds: b.rounds, verdict: v }));
    if (!r.matchesRecord) throw new Error(`${fault.kind}: recheck disagrees`);
    console.log(`${fault.kind}: ${b.rounds.length} rounds -> ${v.instruction} in ${v.function}, ${v.winner} wins; record rechecked`);
  }
  await m.close();
}
console.log(`total ${(Date.now() - t0) / 1000}s`);
fs.writeFileSync(process.env.MLVM_PROTOCOL_OUT || "/dev/null", JSON.stringify({ prompt, maxNewTokens, steps: h.steps, prestate: h.prestate, poststate: h.poststate }));
await Promise.all([honest.close(), liar.close(), referee.close()]);
