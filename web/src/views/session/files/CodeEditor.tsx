// The web's text editor (#5829, iOS CodeEditor): CodeMirror 6, loaded only when Edit opens (files/FileEdit.tsx imports it
// with import(); nothing here is on the first screen). Colored by the viewer's lexer (lib/files.ts lexLine) with the
// viewer's classes, line numbers, the active line, find and replace (⌘F / ⌥⌘F, in the app's words), undo / redo, Tab
// indents, ⌘S saves.
import { EditorState, RangeSetBuilder } from "@codemirror/state";
import { Decoration, drawSelection, EditorView, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers, ViewPlugin, type DecorationSet, type ViewUpdate } from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { highlightSelectionMatches, search, searchKeymap } from "@codemirror/search";
import { useEffect, useRef } from "react";
import { t } from "../../../i18n";
import { lexLine, type Lang, type LexState, type TokenKind } from "../../../lib/files";
import { TOKEN_CLASS } from "./file-data";

const marks = Object.fromEntries(Object.entries(TOKEN_CLASS).map(([kind, cls]) => [kind, Decoration.mark({ class: cls })])) as Record<TokenKind, Decoration>;

/** The viewer's colors on the lines in view; the lexer's state at each line start kept as far as it has gone. */
function lexed(lang: Lang) {
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet;
      /** starts[n - 1]: what is open at the start of line n. */
      starts: LexState[] = [null];
      constructor(view: EditorView) {
        this.decorations = this.build(view);
      }
      update(u: ViewUpdate) {
        if (u.docChanged) {
          let first = Infinity;
          u.changes.iterChangedRanges((_a, _b, fromB) => (first = Math.min(first, fromB)));
          if (first !== Infinity) this.starts.length = Math.min(this.starts.length, u.state.doc.lineAt(first).number);
        }
        if (u.docChanged || u.viewportChanged) this.decorations = this.build(u.view);
      }
      build(view: EditorView): DecorationSet {
        const b = new RangeSetBuilder<Decoration>();
        const doc = view.state.doc;
        for (const { from, to } of view.visibleRanges) {
          const first = doc.lineAt(from).number,
            last = doc.lineAt(to).number;
          while (this.starts.length < first) {
            const n = this.starts.length;
            this.starts.push(lexLine(doc.line(n).text, lang, this.starts[n - 1]).state);
          }
          for (let n = first; n <= last; n++) {
            const line = doc.line(n);
            const r = lexLine(line.text, lang, this.starts[n - 1]);
            if (this.starts.length === n) this.starts.push(r.state);
            for (const tok of r.tokens) if (tok.to > tok.from) b.add(line.from + tok.from, line.from + tok.to, marks[tok.kind]);
          }
        }
        return b.finish();
      }
    },
    { decorations: (v) => v.decorations },
  );
}

/** In the app's look: its fonts, colors and fields (index.css variables), in light and dark alike. */
const look = EditorView.theme({
  "&": { height: "100%", fontSize: "var(--text-xs)", backgroundColor: "var(--page)", color: "var(--ink)" },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace)", lineHeight: "19px" },
  ".cm-gutters": { backgroundColor: "var(--page)", color: "var(--ink-3)", border: "none" },
  ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "var(--hover)" },
  ".cm-cursor": { borderLeftColor: "var(--accent)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": { backgroundColor: "color-mix(in srgb, var(--accent) 25%, transparent)" },
  ".cm-searchMatch": { backgroundColor: "color-mix(in srgb, #ffcc00 40%, transparent)" },
  ".cm-searchMatch-selected": { backgroundColor: "color-mix(in srgb, #ff9500 60%, transparent)" },
  ".cm-panels": { backgroundColor: "var(--inset)", color: "var(--ink)", borderColor: "var(--line)" },
  ".cm-panel.cm-search": { padding: "6px 8px", fontSize: "var(--text-xs)" },
  ".cm-panel.cm-search input, .cm-panel.cm-search button": { fontSize: "var(--text-xs)", borderRadius: "6px" },
  ".cm-textfield": { backgroundColor: "var(--field)", border: "1px solid var(--line)", color: "var(--ink)" },
  ".cm-button": { backgroundImage: "none", backgroundColor: "var(--page)", border: "1px solid var(--line)", color: "var(--ink)" },
});

/** The search panel's words, in the app's language. */
function phrases() {
  return EditorState.phrases.of({
    Find: t("files-editor.find"),
    Replace: t("files-editor.replace"),
    next: t("files-editor.next"),
    previous: t("files-editor.previous"),
    all: t("files-editor.all"),
    "match case": t("files-editor.match_case"),
    regexp: t("files-editor.regexp"),
    "by word": t("files-editor.by_word"),
    replace: t("files-editor.replace_single"),
    "replace all": t("files-editor.replace_all"),
    close: t("files-editor.close"),
  });
}

export default function CodeEditor({ text, lang, onChange, onSave, bind }: {
  text: string;
  lang: Lang | null;
  /** The text was edited (each change). */
  onChange: () => void;
  onSave: () => void;
  /** Hands the page a reader of the text being edited. */
  bind: (read: () => string) => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  const handlers = useRef({ onChange, onSave });
  handlers.current = { onChange, onSave };
  useEffect(() => {
    if (!box.current) return;
    const view = new EditorView({
      parent: box.current,
      state: EditorState.create({
        doc: text,
        extensions: [
          lineNumbers(),
          highlightActiveLineGutter(),
          highlightActiveLine(),
          drawSelection(),
          history(),
          search({ top: true }),
          highlightSelectionMatches(),
          EditorView.lineWrapping,
          keymap.of([{ key: "Mod-s", preventDefault: true, run: () => (handlers.current.onSave(), true) }, ...searchKeymap, ...historyKeymap, indentWithTab, ...defaultKeymap]),
          EditorView.updateListener.of((u) => u.docChanged && handlers.current.onChange()),
          EditorView.contentAttributes.of({ "aria-label": t("files-editor.label"), spellcheck: "false", autocapitalize: "off", autocorrect: "off" }),
          phrases(),
          look,
          ...(lang ? [lexed(lang)] : []),
        ],
      }),
    });
    bind(() => view.state.doc.toString());
    view.focus();
    return () => view.destroy();
    // The editor is made once per file opened: a new text means a new editor (FileEdit keys it).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return <div ref={box} className="min-h-0 flex-1 overflow-hidden" data-testid="code-editor" />;
}
