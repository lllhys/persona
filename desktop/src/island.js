// The island page (#7669): draws what the shell sends (island-model.ts islandView) and reports the pointer and a
// clicked row. Words come from the web already in the user's language; they are set as text, never as markup.
const SVG = "http://www.w3.org/2000/svg";
const root = document.querySelector("#island");
const sides = [document.querySelector("#left"), document.querySelector("#right")];
const list = document.querySelector("#list");

function mark(state) {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("class", "mark");
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS(SVG, "use");
  use.setAttribute("href", `#${state}`);
  svg.append(use);
  return svg;
}

function drawSide(element, side) {
  element.replaceChildren();
  element.hidden = !side;
  if (!side) return;
  element.dataset.state = side.category;
  const count = document.createElement("span");
  count.className = "count";
  count.textContent = side.count > 9999 ? "9999+" : String(side.count);
  element.append(mark(side.category), count);
}

function row(item) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "row";
  button.dataset.state = item.state;
  button.setAttribute("aria-label", [item.title, item.label, item.line].filter(Boolean).join(", "));
  const words = document.createElement("span");
  words.className = "words";
  const title = document.createElement("span");
  title.className = "title";
  title.textContent = item.title;
  const meta = document.createElement("span");
  meta.className = "meta";
  const label = document.createElement("span");
  label.className = "label";
  label.textContent = item.label;
  meta.append(label);
  if (item.line) meta.append(` · ${item.line}`);
  words.append(title, meta);
  button.append(mark(item.state), words);
  button.addEventListener("click", () => window.islandShell.open(item.route));
  return button;
}

window.islandShell.onView(view => {
  root.dataset.notch = view.notch ? "yes" : "no";
  root.dataset.open = view.open ? "yes" : "no";
  root.style.setProperty("--bar", `${view.bar}px`);
  root.setAttribute("aria-label", view.summary || "Persona");
  drawSide(sides[0], view.sides[0]);
  drawSide(sides[1], view.sides[1]);
  list.hidden = !view.open;
  list.replaceChildren(...(view.open ? view.rows.map(row) : []));
});

root.addEventListener("mouseenter", () => window.islandShell.hover(true));
root.addEventListener("mouseleave", () => window.islandShell.hover(false));
