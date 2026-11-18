import { fileURLToPath } from "node:url";
import { build, normalizePath } from "vite";
import { describe, expect, it } from "vitest";

const configFile = fileURLToPath(new URL("../vite.public.config.ts", import.meta.url));
const buildPublic = (plugins = []) => build({ configFile, logLevel: "silent", plugins, build: { write: false } });

describe("public viewer build isolation", () => {
  it("builds the allowed graph and records portable paths on the native host", async () => {
    const { output } = await buildPublic();
    const graph = output.find((entry) => entry.fileName === "share-assets/graph.json");
    expect(graph?.type).toBe("asset");
    const { modules } = JSON.parse(graph.source);
    expect(modules).toContain("public-share/main.ts");
    expect(modules).toContain("src/ui/share-tokens.css");
    expect(modules).toContain("src/i18n/locales/en/public-share.json");
    expect(modules).toContain("src/i18n/locales/ja/public-share.json");
    expect(modules).toContain("src/i18n/locales/zh-Hans/public-share.json");
    expect(modules.every((id) => !id.includes("\\") && !/^(?:[A-Za-z]:|\/)/.test(id))).toBe(true);
    expect(output.every((entry) => entry.fileName === "index.html" || entry.fileName.startsWith("share-assets/"))).toBe(true);
  });

  it("still rejects a module outside the public viewer's allowed graph", async () => {
    const forbidden = "\0private-client-test";
    const injection = {
      name: "inject-private-client-test",
      transform(code, id) {
        if (normalizePath(id).endsWith("/public-share/main.ts")) return `import "virtual:private-client-test";\n${code}`;
      },
      resolveId(id) { if (id === "virtual:private-client-test") return forbidden; },
      load(id) { if (id === forbidden) return "globalThis.__privateClientTest = true;"; },
    };
    await expect(buildPublic([injection])).rejects.toThrow(`Public viewer imported a module outside its isolated graph: ${forbidden}`);
  });
});
