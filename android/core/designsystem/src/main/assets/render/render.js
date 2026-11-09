// Called by the app (`MarkdownRenderer`) with callAsyncJavaScript: draws into #out and answers its size in points.
let count = 0;

// The box of everything drawn: KaTeX moves its pieces with relative offsets (a fraction's numerator, a sum's limits)
// outside its own box, so its box alone cut the formula off. [x, y, width, height] in the page.
function size(el) {
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const e of [el, ...el.querySelectorAll("*")]) {
    const r = e.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) continue;
    x0 = Math.min(x0, r.left); y0 = Math.min(y0, r.top);
    x1 = Math.max(x1, r.right); y1 = Math.max(y1, r.bottom);
  }
  if (!isFinite(x0)) return [0, 0, 0, 0];
  const sx = window.scrollX, sy = window.scrollY;
  return [Math.floor(x0 + sx), Math.floor(y0 + sy), Math.ceil(x1 - x0) + 1, Math.ceil(y1 - y0) + 1];
}

// Two frames, so what was just drawn is laid out and painted; or a short wait where no frames come (the Mac's
// offscreen window is never on screen, and waiting on requestAnimationFrame there hung every drawing).
function frame() {
  return new Promise((done) => {
    const late = setTimeout(done, 80);
    requestAnimationFrame(() => requestAnimationFrame(() => { clearTimeout(late); done(); }));
  });
}

// After the app's snapshot: nothing left on the page.
window.clearOut = () => { document.getElementById("out").innerHTML = ""; };

// KaTeX's fonts, loaded once before the first formula: fonts.ready alone resolved before they had started loading
// (nothing laid out with them yet), and the snapshot had blank glyphs.
const katexFonts = ["Main", "Math", "AMS", "Size1", "Size2", "Size3", "Size4", "Caligraphic", "Fraktur", "SansSerif", "Script", "Typewriter"];
let fontsLoaded = null;
function loadFonts() {
  if (!fontsLoaded) {
    fontsLoaded = Promise.all(katexFonts.flatMap((f) => ["", "italic ", "bold "].map((s) => document.fonts.load(s + "16px KaTeX_" + f).catch(() => null))));
  }
  return fontsLoaded;
}

window.renderMath = async (tex, display, points) => {
  await loadFonts();
  const out = document.getElementById("out");
  out.className = display ? "block" : "inline";
  out.style.fontSize = points + "px";
  out.style.color = "#000";
  katex.render(tex, out, { displayMode: display, throwOnError: true, output: "html", strict: "ignore", trust: false });
  out.getBoundingClientRect(); // lay it out, so any font it still needs starts loading
  await document.fonts.ready;
  await frame();
  return size(out);
};

// Mermaid (5.5 MB) is loaded on the first diagram, so a page that draws only formulas never parses it. A failed load
// leaves `mermaid` undefined and the call below fails as it did when the page loaded it up front. Android's copy only:
// apple/Persona/Resources/Render still loads it with the page.
let mermaidLoaded = null;
function loadMermaid() {
  if (!mermaidLoaded) {
    mermaidLoaded = new Promise((done) => {
      const script = document.createElement("script");
      script.src = "mermaid.min.js";
      script.onload = script.onerror = () => done();
      document.head.appendChild(script);
    });
  }
  return mermaidLoaded;
}

window.renderMermaid = async (source, dark, points) => {
  await loadMermaid();
  const out = document.getElementById("out");
  out.className = "block";
  out.style.fontSize = points + "px";
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    theme: dark ? "dark" : "default",
    fontSize: points,
    htmlLabels: false,
    flowchart: { htmlLabels: false },
  });
  count += 1;
  const { svg } = await mermaid.render("diagram" + count, source);
  out.innerHTML = svg;
  const el = out.querySelector("svg");
  // Mermaid sizes the SVG to its container (max-width: 100%); draw it at its own size.
  const box = el.viewBox && el.viewBox.baseVal;
  if (box && box.width > 0) {
    el.style.maxWidth = "none";
    el.setAttribute("width", Math.ceil(box.width));
    el.setAttribute("height", Math.ceil(box.height));
  }
  await frame();
  return size(out);
};
