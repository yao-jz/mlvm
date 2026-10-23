function setText(id, text, className) {
  const el = document.getElementById(id);
  el.textContent = text;
  if (className) el.className = className;
}

function fillClaim(rows) {
  const dl = document.getElementById("claim");
  dl.replaceChildren();
  for (const [k, v] of rows) {
    const dt = document.createElement("dt");
    dt.textContent = k;
    const dd = document.createElement("dd");
    dd.textContent = v ?? "";
    dl.append(dt, dd);
  }
}

async function loadReceipt() {
  const status = document.getElementById("status");
  try {
    const res = await fetch("/api/receipt");
    const r = await res.json();
    const ok = r.verifier && r.verifier.passed;
    status.className = "panel " + (ok ? "ok" : "bad");
    status.textContent = `${r.demo} · ${Number(r.steps).toLocaleString()} steps · profile ${r.profile}`;
    const prompt = r.prompt || "";
    const generated = r.generatedText || "";
    const story = document.getElementById("story");
    story.replaceChildren();
    if (prompt || generated) {
      const p = document.createElement("span");
      p.className = "prompt";
      p.textContent = prompt;
      const g = document.createElement("span");
      g.className = "gen";
      g.textContent = generated;
      story.append(p, g);
    } else {
      story.textContent = "MLP answer committed at MLVM_OUT (no natural-language decoder).";
    }
    fillClaim([
      ["prompt", prompt || "(embedded MLP)"],
      ["generated", generated || r.answerHex],
      ["tokens", (r.tokens || []).join(" ")],
      ["prestate", r.preState],
      ["poststate", r.postState],
      ["claim", r.commitment && r.commitment.claimId],
      ["verifier", `${r.verifier.mode} / passed=${r.verifier.passed}`],
    ]);
    document.getElementById("artifacts").textContent = JSON.stringify(
      { guest: r.guest, inputs: r.inputs, request: r.request, createdAt: r.createdAt },
      null,
      2
    );
  } catch (err) {
    setText("status", `Failed to load receipt: ${err}`, "panel bad");
  }
}

async function loadDispute() {
  const res = await fetch("/api/dispute");
  const d = await res.json();
  document.getElementById("dispute").textContent = JSON.stringify(d, null, 2);
}

function wireActs() {
  const buttons = document.querySelectorAll(".act");
  buttons.forEach((btn) => {
    btn.addEventListener("click", () => {
      buttons.forEach((b) => b.classList.remove("active"));
      btn.classList.add("active");
      const id = btn.dataset.act;
      document.querySelectorAll(".act-panel").forEach((p) => p.classList.add("hidden"));
      document.getElementById("act" + id).classList.remove("hidden");
    });
  });
  document.getElementById("bisect").addEventListener("click", async () => {
    await fetch("/api/dispute/bisect", { method: "POST" });
    await loadDispute();
  });
  document.getElementById("tamper").addEventListener("click", async () => {
    const res = await fetch("/api/dispute/tamper", { method: "POST" });
    const body = await res.json();
    document.getElementById("tamper-out").textContent = JSON.stringify(body, null, 2);
    await loadDispute();
  });
}

wireActs();
loadReceipt();
loadDispute();
