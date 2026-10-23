// Small hand-built visualizations: an icicle/flame graph of the X-ray call tree, bar rows,
// a token timeline and the bisection ladder. Plain SVG and DOM; every label is inserted
// with textContent.

const SVG = "http://www.w3.org/2000/svg";
const RAMP = [
  [0xb7, 0xd3, 0xf6], [0x86, 0xb6, 0xef], [0x55, 0x98, 0xe7],
  [0x2a, 0x78, 0xd6], [0x1c, 0x5c, 0xab], [0x10, 0x42, 0x81],
];

export const fmt = (n) => Math.round(n).toLocaleString("en-US");
export const pct = (x, digits = 1) => `${(100 * x).toFixed(digits)}%`;

// floatColor maps the share of a frame's own instructions that are F/D onto one blue ramp.
export function floatColor(share) {
  const x = Math.max(0, Math.min(1, share)) * (RAMP.length - 1);
  const i = Math.min(Math.floor(x), RAMP.length - 2);
  const t = x - i;
  const c = (k) => Math.round(RAMP[i][k] + (RAMP[i + 1][k] - RAMP[i][k]) * t);
  return `rgb(${c(0)},${c(1)},${c(2)})`;
}
const floatInk = (share) => (share > 0.55 ? "#ffffff" : "#0b0b0b");

export function el(tag, attrs = {}, text) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") node.className = v;
    else node.setAttribute(k, v);
  }
  if (text !== undefined) node.textContent = text;
  return node;
}

function svgEl(tag, attrs = {}) {
  const node = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  return node;
}

// A single floating tooltip shared by every chart on the page.
let tip;
export function tooltip() {
  if (!tip) {
    tip = el("div", { class: "tooltip", role: "status" });
    document.body.append(tip);
  }
  return {
    show(lines, x, y) {
      tip.replaceChildren();
      for (const [i, line] of lines.entries()) tip.append(el("div", { class: i === 0 ? "tip-value" : "tip-label" }, line));
      tip.style.display = "block";
      const pad = 14;
      const w = tip.offsetWidth;
      const h = tip.offsetHeight;
      const left = Math.min(x + pad, window.innerWidth - w - 8);
      const top = y + pad + h > window.innerHeight ? y - h - pad : y + pad;
      tip.style.transform = `translate(${Math.max(8, left)}px, ${Math.max(8, top)}px)`;
    },
    hide() { tip.style.display = "none"; },
  };
}

function subtreeFloat(n) {
  let f = n.float || 0;
  for (const c of n.children || []) f += subtreeFloat(c);
  return f;
}

// renderFlame draws the call tree top-down (callers above callees). Width is instructions
// executed; color is each frame's own float share. Click a frame to zoom into it.
export function renderFlame(container, report, onZoom) {
  const total = report.steps;
  const tt = tooltip();
  let focus = report.tree;
  const path = [report.tree];

  function draw() {
    container.replaceChildren();
    const width = Math.max(320, container.clientWidth);
    const row = 22;
    const depthOf = (n) => 1 + Math.max(0, ...(n.children || []).map(depthOf));
    const ancestors = path.length - 1;
    const rows = ancestors + depthOf(focus);
    const height = rows * row;
    const svg = svgEl("svg", { width, height, viewBox: `0 0 ${width} ${height}`, role: "img", "aria-label": "Call tree of every executed instruction" });
    const scale = width / focus.value;

    const frame = (n, x, w, level, isAncestor) => {
      if (w < 0.5) return;
      const y = level * row;
      const ownShare = n.self > 0 ? (n.float || 0) / n.self : 0;
      const caller = level === 0 || isAncestor || n.self * 50 < n.value;
      const fill = caller ? "var(--caller)" : floatColor(ownShare);
      const ink = caller ? "var(--ink)" : floatInk(ownShare);
      const g = svgEl("g", { class: "frame", tabindex: "0" });
      const rect = svgEl("rect", { x: x + 1, y: y + 1, width: Math.max(0, w - 2), height: row - 2, rx: 3, fill });
      g.append(rect);
      if (w > 34) {
        const label = svgEl("text", { x: x + 6, y: y + 15, fill: ink, class: "frame-label" });
        const maxChars = Math.floor((w - 10) / 7);
        label.textContent = n.name.length > maxChars ? n.name.slice(0, Math.max(0, maxChars - 1)) + "…" : n.name;
        g.append(label);
      }
      const describe = (ev) => {
        const r = ev.target.closest("g").getBoundingClientRect();
        const px = ev.clientX ?? r.left + r.width / 2;
        const py = ev.clientY ?? r.top;
        tt.show([
          `${fmt(n.value)} instructions · ${pct(n.value / total, 2)}`,
          n.name,
          caller ? "caller frame" : `${pct(ownShare, 0)} of its own instructions are floating point`,
        ], px, py);
      };
      g.addEventListener("pointermove", describe);
      g.addEventListener("focus", describe);
      g.addEventListener("pointerleave", () => tt.hide());
      g.addEventListener("blur", () => tt.hide());
      g.addEventListener("click", () => {
        tt.hide();
        if (isAncestor) {
          const i = path.indexOf(n);
          path.length = i + 1;
          focus = n;
        } else if (n !== focus && n.children && n.children.length) {
          let cur = n;
          const chain = [];
          while (cur && cur !== focus) { chain.unshift(cur); cur = parentOf.get(cur); }
          path.push(...chain);
          focus = n;
        }
        draw();
        if (onZoom) onZoom(focus, path.length > 1);
      });
      svg.append(g);
    };

    // Ancestors of the focused frame, full width.
    path.slice(0, -1).forEach((n, i) => frame(n, 0, width, i, true));
    const walk = (n, x, level) => {
      const w = n.value * scale;
      frame(n, x, w, level, false);
      let cx = x;
      for (const c of n.children || []) {
        walk(c, cx, level + 1);
        cx += c.value * scale;
      }
    };
    walk(focus, 0, ancestors);
    container.append(svg);
  }

  const parentOf = new Map();
  (function index(n) {
    for (const c of n.children || []) { parentOf.set(c, n); index(c); }
  })(report.tree);

  draw();
  const ro = new ResizeObserver(() => draw());
  ro.observe(container);
  return {
    reset() { focus = report.tree; path.length = 1; draw(); if (onZoom) onZoom(focus, false); },
  };
}

// renderBars draws one horizontal bar per row: label, bar, share. A single series, so
// every bar wears the same color and the title names what is measured.
export function renderBars(container, rows, { max = 1 } = {}) {
  container.replaceChildren();
  const tt = tooltip();
  for (const r of rows) {
    const line = el("div", { class: "bar-row", tabindex: "0" });
    line.append(el("span", { class: "bar-label" }, r.name));
    const track = el("span", { class: "bar-track" });
    const bar = el("span", { class: "bar" });
    bar.style.width = `${Math.max(0.4, (100 * r.share) / max)}%`;
    track.append(bar);
    line.append(track, el("span", { class: "bar-value" }, pct(r.share)));
    const describe = (ev) => {
      const b = line.getBoundingClientRect();
      tt.show([`${fmt(r.count)} instructions · ${pct(r.share, 2)}`, r.name], ev.clientX ?? b.right, ev.clientY ?? b.top);
    };
    line.addEventListener("pointermove", describe);
    line.addEventListener("focus", describe);
    line.addEventListener("pointerleave", () => tt.hide());
    line.addEventListener("blur", () => tt.hide());
    container.append(line);
  }
}

// renderTimeline marks where each generated token was printed along the instruction axis.
export function renderTimeline(container, total, marks, { lieStep, foundStep } = {}) {
  container.replaceChildren();
  const width = Math.max(320, container.clientWidth);
  const height = 46;
  const svg = svgEl("svg", { width, height, viewBox: `0 0 ${width} ${height}`, role: "img", "aria-label": "Token boundaries along the run" });
  const x = (s) => 6 + (s / total) * (width - 12);
  svg.append(svgEl("line", { x1: 6, x2: width - 6, y1: 22, y2: 22, class: "axis" }));
  const tt = tooltip();
  const first = marks.find((m) => m.label === "first token starts");
  if (first) {
    const load = svgEl("rect", { x: 6, y: 17, width: x(first.step) - 6, height: 10, rx: 3, class: "phase-load" });
    svg.append(load);
  }
  marks.filter((m) => m.label !== "first token starts").forEach((m, i) => {
    const hit = svgEl("g", { class: "token-mark", tabindex: "0" });
    hit.append(svgEl("rect", { x: x(m.step) - 8, y: 4, width: 16, height: 38, fill: "transparent" }));
    hit.append(svgEl("circle", { cx: x(m.step), cy: 22, r: 4 }));
    const show = (ev) => tt.show([`token ${i + 1}: ${JSON.stringify(m.label)}`, `printed at instruction ${fmt(m.step)}`], ev.clientX ?? 0, ev.clientY ?? 0);
    hit.addEventListener("pointermove", show);
    hit.addEventListener("focus", show);
    hit.addEventListener("pointerleave", () => tt.hide());
    svg.append(hit);
  });
  for (const [step, cls] of [[lieStep, "lie-mark"], [foundStep, "found-mark"]]) {
    if (step === undefined || step === null) continue;
    svg.append(svgEl("line", { x1: x(step), x2: x(step), y1: 2, y2: 44, class: cls }));
  }
  const left = svgEl("text", { x: 6, y: 42, class: "axis-label" });
  left.textContent = "0";
  const right = svgEl("text", { x: width - 6, y: 42, class: "axis-label", "text-anchor": "end" });
  right.textContent = fmt(total);
  svg.append(left, right);
  container.append(svg);
}

// Ladder renders one row per bisection round: the interval still in dispute, split at the
// question, with the half that survives filled. Each row zooms to its own interval, so
// the halving stays visible long after the interval is a single pixel of the whole run.
export class Ladder {
  constructor(container, total) {
    this.container = container;
    this.total = total;
    container.replaceChildren();
  }

  add(round) {
    const row = el("div", { class: "rung" });
    row.append(el("span", { class: "rung-n" }, String(round.n)));
    const bar = el("span", { class: "rung-bar" });
    const left = el("span", { class: `half ${round.agree ? "dropped" : "kept"}` });
    const right = el("span", { class: `half ${round.agree ? "kept" : "dropped"}` });
    const split = (round.mid - round.lo) / (round.hi - round.lo);
    left.style.width = `${100 * split}%`;
    right.style.width = `${100 * (1 - split)}%`;
    bar.append(left, right);
    const where = el("span", { class: "rung-where" });
    where.style.left = `${(100 * round.lo) / this.total}%`;
    where.style.width = `${Math.max(0.15, (100 * (round.hi - round.lo)) / this.total)}%`;
    const mini = el("span", { class: "rung-mini" });
    mini.append(where);
    const verdict = el("span", { class: `rung-answer ${round.agree ? "agree" : "differ"}` },
      round.agree ? "✓ same hash → lie is later" : "✗ different → lie is earlier");
    const ask = el("span", { class: "rung-ask" }, `step ${fmt(round.mid)}`);
    const left2 = el("span", { class: "rung-left" }, `${fmt(round.hi - round.lo)} in dispute`);
    row.append(bar, mini, ask, verdict, left2);
    this.container.append(row);
    this.container.scrollTop = this.container.scrollHeight;
  }
}

export function shortHash(h) {
  return h ? `${h.slice(0, 10)}…${h.slice(-6)}` : "";
}
