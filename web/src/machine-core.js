// Message handler shared by the browser worker (worker.js) and the Node test harness.
// Each worker owns one WebAssembly instance of MLVM and plays one role: an honest
// machine, a lying machine, or the referee.

function unwrap(json) {
  const value = JSON.parse(json);
  if (value && typeof value === "object" && "error" in value && Object.keys(value).length === 1) {
    throw new Error(value.error);
  }
  return value;
}

export function handle(mlvm, data, post) {
  const { id, cmd, args = {} } = data;
  try {
    let result;
    switch (cmd) {
      case "partyRun":
        result = unwrap(mlvm.partyRun(JSON.stringify(args.config), (ev) => post({ id, event: JSON.parse(ev) })));
        break;
      case "hashAt":
        result = unwrap(mlvm.hashAt(args.step)).hash;
        break;
      case "observe":
        result = unwrap(mlvm.observe(JSON.stringify(args.round)));
        break;
      case "transitionAt":
        result = unwrap(mlvm.transitionAt(args.step));
        break;
      case "referee":
        result = unwrap(mlvm.referee(JSON.stringify(args.transition), args.liarPost, true));
        break;
      case "recheck":
        result = unwrap(mlvm.recheck(args.record));
        break;
      case "disasm":
        result = unwrap(mlvm.disasm(args.word, args.pc));
        break;
      default:
        throw new Error(`unknown command ${cmd}`);
    }
    post({ id, result });
  } catch (err) {
    post({ id, error: String((err && err.message) || err) });
  }
}
