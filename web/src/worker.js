// One MLVM WebAssembly instance per worker. The page starts up to three of these: an
// honest machine, a lying machine and the referee. They share nothing but messages.
import { handle } from "./machine-core.js";

let ready = null;
const queue = [];

async function boot(module) {
  await import("./wasm_exec.js");
  const go = new globalThis.Go();
  const instance = await WebAssembly.instantiate(module, go.importObject);
  go.run(instance); // returns once main() parks; globalThis.mlvm is set by then
}

self.onmessage = async (e) => {
  const data = e.data;
  if (data.cmd === "init") {
    ready = boot(data.module).then(
      () => self.postMessage({ id: data.id, result: true }),
      (err) => self.postMessage({ id: data.id, error: String(err) }),
    );
    return;
  }
  queue.push(data);
  await ready;
  while (queue.length) {
    handle(globalThis.mlvm, queue.shift(), (msg) => self.postMessage(msg));
  }
};
