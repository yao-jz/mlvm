// Keep the article's contents compact on phones, and make links into folded details work.
const contents = document.querySelector(".toc-menu");
const wideScreen = window.matchMedia("(min-width: 1001px)");
if (contents) {
  contents.open = wideScreen.matches;
  wideScreen.addEventListener("change", (event) => { contents.open = event.matches; });
}

function reveal(hash) {
  if (!hash) return null;
  let id;
  try { id = decodeURIComponent(hash.slice(1)); } catch { return null; }
  const target = document.getElementById(id);
  if (!target) return null;
  for (let node = target; node; node = node.parentElement) {
    if (node.tagName === "DETAILS") node.open = true;
  }
  return target;
}

document.addEventListener("click", (event) => {
  const link = event.target.closest("a[href]");
  if (!link || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  const url = new URL(link.href);
  if (url.origin !== location.origin || url.pathname !== location.pathname || url.search !== location.search || !url.hash) return;
  reveal(url.hash);
  if (contents?.contains(link) && !wideScreen.matches) contents.open = false;
});

function followHash() {
  const target = reveal(location.hash);
  if (target) requestAnimationFrame(() => target.scrollIntoView({ block: "start" }));
}
window.addEventListener("hashchange", followHash);
followHash();
