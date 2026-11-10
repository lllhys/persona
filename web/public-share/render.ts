// DOM for one shared message. Every value goes in as textContent: no HTML, links, images or attachments are ever made.
import type { PublicMessage } from "../src/sharing/types";

export type PublicKey = "public-share.user" | "public-share.assistant" | "public-share.work" | "public-share.arguments" | "public-share.results" | "public-share.report";

export function element<K extends keyof HTMLElementTagNameMap>(tag: K, value?: string, className?: string) {
  const el = document.createElement(tag);
  if (value) el.textContent = value;
  if (className) el.className = className;
  return el;
}

/** One message; its work details stay collapsed unless the reader had opened them (super rule 2). */
export function messageView(m: PublicMessage, t: (key: PublicKey) => string, open = false) {
  const article = element("article", undefined, `public-message public-${m.role}`);
  article.dataset.messageId = m.message_id;
  article.append(element("h2", t(m.role === "user" ? "public-share.user" : "public-share.assistant"), "public-role"), element("div", m.text, "share-text"));
  if (m.execution?.length) {
    const details = element("details", undefined, "share-work");
    details.open = open;
    details.append(element("summary", t("public-share.work")));
    for (const item of m.execution) {
      const block = element("section", undefined, "share-box");
      if (item.kind === "tool") {
        block.append(element("h3", item.name), element("h4", t("public-share.arguments")), element("pre", item.arguments), element("h4", t("public-share.results")));
        for (const result of item.results) block.append(element("pre", result.text));
      } else {
        if (item.kind === "agent") block.append(element("h3", t("public-share.report")));
        block.append(element("div", item.text, "share-text"));
      }
      details.append(block);
    }
    article.append(details);
  }
  return article;
}
