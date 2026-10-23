// Keep the article's contents compact on phones, and make links into folded details work.
const contents = document.querySelector(".toc-menu");
const wideScreen = window.matchMedia("(min-width: 1001px)");
if (contents) {
  contents.open = wideScreen.matches;
  wideScreen.addEventListener("change", (event) => { contents.open = event.matches; });
}

const homeContents = document.querySelector(".home-toc-menu");
if (homeContents) {
  const railScreen = window.matchMedia("(min-width: 1500px)");
  const links = [...homeContents.querySelectorAll('nav a[href^="#"]')];
  const sections = links.map((link) => document.querySelector(link.getAttribute("href")));
  homeContents.open = railScreen.matches;
  railScreen.addEventListener("change", (event) => { homeContents.open = event.matches; });
  homeContents.addEventListener("click", (event) => {
    if (event.target.closest("nav a") && !railScreen.matches) homeContents.open = false;
  });

  let queued = false;
  const markCurrent = () => {
    queued = false;
    let current = 0;
    for (let i = 0; i < sections.length; i++) {
      if (sections[i] && sections[i].getBoundingClientRect().top <= 160) current = i;
    }
    links.forEach((link, i) => {
      link.classList.toggle("active", i === current);
      if (i === current) link.setAttribute("aria-current", "location");
      else link.removeAttribute("aria-current");
    });
  };
  const queueMark = () => {
    if (queued) return;
    queued = true;
    requestAnimationFrame(markCurrent);
  };
  window.addEventListener("scroll", queueMark, { passive: true });
  window.addEventListener("resize", queueMark);
  queueMark();
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
