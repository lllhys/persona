import "virtual:public-share-palette";
import "../src/ui/share-tokens.css";
import "./style.css";
import en from "../src/i18n/locales/en/public-share.json";
import zh from "../src/i18n/locales/zh-Hans/public-share.json";
import ja from "../src/i18n/locales/ja/public-share.json";
import { Viewer, type ViewerState } from "./viewer";
import { element, messageView } from "./render";

const primary = navigator.language.split("-")[0];
const lang = primary === "zh" ? "zh-Hans" : primary === "ja" ? "ja" : "en";
const strings = lang === "zh-Hans" ? zh : lang === "ja" ? ja : en;
const t = (key: `public-share.${keyof typeof en}`) => strings[key.slice("public-share.".length) as keyof typeof en];
document.documentElement.lang = lang;
const scheme = matchMedia("(prefers-color-scheme: dark)");
const applyScheme = () => document.documentElement.classList.toggle("dark", scheme.matches);
applyScheme(); scheme.addEventListener("change", applyScheme);
document.title = t("public-share.title");
const root = document.querySelector<HTMLElement>("#viewer")!;
const openedWork = new Set<string>();
let lastPage: ViewerState["page"] | undefined;
let lastPhase = "";
function render(state: ViewerState) {
  // Busy updates don't rebuild disclosures or erase the reader's manual expansion.
  if (state.page === lastPage && state.phase === lastPhase) {
    root.querySelectorAll<HTMLButtonElement>("button").forEach((b) => { b.disabled = state.busy; }); return;
  }
  openedWork.clear();
  if (state.phase === "ready" && lastPhase === "ready" && state.page && lastPage && state.page.revision === lastPage.revision && state.page.content_version === lastPage.content_version) {
    root.querySelectorAll<HTMLElement>("article[data-message-id]").forEach((article) => { if (article.querySelector("details")?.open && article.dataset.messageId) openedWork.add(article.dataset.messageId); });
  }
  lastPage = state.page; lastPhase = state.phase;
  const masthead = element("header", undefined, "public-masthead");
  masthead.append(element("div", t("public-share.readonly"), "public-eyebrow"), element("h1", t("public-share.title")), element("p", t("public-share.intro")));
  if (state.phase === "ready") {
    const sensitive = element("p", t("public-share.sensitive"), "public-sensitive"); sensitive.setAttribute("role", "note"); masthead.append(sensitive);
  }
  const content = element("div", undefined, "share-messages");
  if (state.page && state.phase === "ready") {
    const date = new Intl.DateTimeFormat(lang, { dateStyle: "medium", timeStyle: "short" }).format(new Date(state.page.expires_at));
    const meta = element("div", undefined, "public-meta");
    meta.append(element("span", t(`public-share.${state.page.publication_status}`)), element("span", t("public-share.expires").replace("{date}", date)));
    masthead.append(meta);
    if (!state.page.messages.length) content.append(element("p", t("public-share.empty")));
    for (const m of state.page.messages) content.append(messageView(m, t, openedWork.has(m.message_id)));
    if (state.page.next_cursor) {
      const more = element("button", t("public-share.more"), "public-button"); more.type = "button"; more.disabled = state.busy;
      more.addEventListener("click", () => viewer.more()); content.append(more);
    }
  } else {
    const box = element("section", undefined, "public-state share-box"); box.setAttribute("role", "status");
    const key = state.phase === "unavailable" ? "unavailable" : state.phase === "error" ? "error" : "loading";
    box.append(element("h2", t(`public-share.${key}`)));
    if (key !== "loading") box.append(element("p", t(key === "error" ? "public-share.error_help" : "public-share.unavailable_help")));
    if (state.phase === "error") { const retry = element("button", t("public-share.retry"), "public-button"); retry.type = "button"; retry.disabled = state.busy; retry.addEventListener("click", () => viewer.retry()); box.append(retry); }
    content.append(box);
  }
  root.replaceChildren(masthead, content, element("footer", t("public-share.note"), "public-footer"));
}
const match = /^\/share\/([A-Za-z0-9_-]{64})$/.exec(location.pathname);
const viewer = new Viewer(match?.[1] ?? "", render);
if (match) { render(viewer.state); viewer.visibility(document.hidden); viewer.start(); }
else render({ phase: "unavailable", page: null, busy: false });
document.addEventListener("visibilitychange", () => viewer.visibility(document.hidden));
window.addEventListener("pagehide", () => viewer.visibility(true));
window.addEventListener("pageshow", () => viewer.visibility(document.hidden));
