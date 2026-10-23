// A worker_threads stand-in for web/src/worker.js, so `node web/test/protocol.mjs`
// exercises the same WebAssembly API the browser uses.
import { parentPort, workerData } from "node:worker_threads";
import fs from "node:fs";
import path from "node:path";
import { webcrypto } from "node:crypto";
import { handle } from "../src/machine-core.js";

globalThis.fs = fs;
globalThis.path = path;
globalThis.crypto ??= webcrypto;
await import(workerData.wasmExec);

const go = new Go();
const instance = await WebAssembly.instantiate(workerData.module, go.importObject);
go.run(instance);
parentPort.postMessage({ ready: true });
parentPort.on("message", (data) => handle(globalThis.mlvm, data, (msg) => parentPort.postMessage(msg)));
