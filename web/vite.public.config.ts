import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin } from "vite";

const root = fileURLToPath(new URL("./public-share", import.meta.url));
const source = fileURLToPath(new URL("./src", import.meta.url));
const paletteID = "\0virtual:public-share-palette";
const palette: Plugin = {
  name: "public-share-palette",
  resolveId(id) { if (id === "virtual:public-share-palette") return `${paletteID}.css`; },
  load(id) {
    if (id !== `${paletteID}.css`) return;
    const css = readFileSync(`${source}/styles.css`, "utf8");
    const roles = ["page", "surface", "inset", "hover", "ink", "ink-2", "ink-3", "line", "line-strong", "accent-ink", "ai", "orange", "shadow-hairline"];
    return [":root", ".dark"].map((selector) => {
      const block = css.split(`${selector} {`)[1]?.split("}")[0];
      if (!block) throw new Error("Missing design-system palette");
      return `${selector}{${roles.map((role) => {
        const match = new RegExp(`--${role}:([^;]+);`).exec(block);
        if (!match) throw new Error(`Missing design-system role ${role}`);
        return `--${role}:${match[1]};`;
      }).join("")}}`;
    }).join("\n");
  },
};
// Positive transitive allowlist, enforced on actual emitted chunks (including any dynamic import).
const isolation: Plugin = {
  name: "public-share-isolation",
  generateBundle(_, bundle) {
    const modules = new Set<string>();
    for (const entry of Object.values(bundle)) {
      if (entry.type !== "chunk") continue;
      for (const id of entry.moduleIds) {
        const allowed = id.startsWith(`${root}/`) || id === `${source}/ui/share-tokens.css`
          || /^.*\/src\/i18n\/locales\/(en|zh-Hans|ja)\/public-share\.json$/.test(id)
          || id.startsWith(paletteID) || id === "\0vite/modulepreload-polyfill.js";
        if (!allowed) this.error(`Public viewer imported a module outside its isolated graph: ${id}`);
        modules.add(id.replace(root, "public-share").replace(source, "src"));
      }
      if ([...entry.imports, ...entry.dynamicImports].some((name) => !name.startsWith("share-assets/"))) this.error("Public viewer import left its asset namespace");
    }
    if (Object.keys(bundle).some((name) => name !== "index.html" && !name.startsWith("share-assets/"))) this.error("Public viewer emitted a non-public artifact");
    this.emitFile({ type: "asset", fileName: "share-assets/graph.json", source: JSON.stringify({ modules: [...modules].sort() }) });
  },
};
export default defineConfig({
  root, publicDir: false, base: "/",
  plugins: [palette, isolation],
  server: {
    host: "127.0.0.1",
    fs: { allow: [fileURLToPath(new URL(".", import.meta.url))] },
    proxy: { "/api/public/agent-shares/": { target: process.env.PERSONA_SHARE_FIXTURE_ORIGIN || "http://127.0.0.1:18661", changeOrigin: false } },
  },
  build: {
    outDir: "../dist/public-share", emptyOutDir: true, target: "es2022", sourcemap: false,
    rollupOptions: { output: { entryFileNames: "share-assets/[name]-[hash].js", chunkFileNames: "share-assets/[name]-[hash].js", assetFileNames: "share-assets/[name]-[hash][extname]" } },
  },
});
