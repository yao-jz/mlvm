// The WebAssembly entry of the determinism matrix: runs the same prompts as
// scripts/determinism-record.py through mlvm.wasm in Node and writes the same record.
//
//   node web/test/record.mjs web/dist/mlvm.wasm "$(go env GOROOT)/lib/wasm/wasm_exec.js" rec.json
import fs from "node:fs";
import path from "node:path";
import { webcrypto } from "node:crypto";

globalThis.fs = fs;
globalThis.path = path;
globalThis.crypto ??= webcrypto;
const [wasmPath, wasmExec, out] = process.argv.slice(2);
await import(wasmExec);
const go = new Go();
const instance = await WebAssembly.instantiate(await WebAssembly.compile(fs.readFileSync(wasmPath)), go.importObject);
go.run(instance);

const spec = JSON.parse(fs.readFileSync(new URL("../determinism-prompts.json", import.meta.url)));
const runs = [];
for (const prompt of spec.prompts) {
  const r = JSON.parse(globalThis.mlvm.partyRun(JSON.stringify({ role: "honest", prompt, maxNewTokens: spec.maxNewTokens }), () => {}));
  if (r.error) throw new Error(r.error);
  runs.push({ prompt, maxNewTokens: spec.maxNewTokens, steps: r.steps, prestate: r.prestate, poststate: r.poststate, text: r.text });
  console.error(`${"wasm".padEnd(16)} ${r.poststate}  ${JSON.stringify(prompt)}  ${(r.steps / r.millis / 1000).toFixed(1)} MIPS`);
}
fs.writeFileSync(out, JSON.stringify({ platform: "wasm", runner: `node ${process.version}`, mlvm: "mlvm.wasm", runs }, null, 2) + "\n");
process.exit(0);
