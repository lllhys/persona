import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { trayIconFile } from "../src/tray-icon.js";

// dist-test/test → the package root
const root = join(import.meta.dirname, "..", "..");

/** A PNG's width and height from its IHDR chunk, and whether it has an alpha channel (colour type 4 or 6). */
function png(path: string): { width: number; height: number; alpha: boolean } {
  const b = readFileSync(path);
  assert.equal(b.subarray(1, 4).toString("latin1"), "PNG", path);
  return { width: b.readUInt32BE(16), height: b.readUInt32BE(20), alpha: b[25] === 4 || b[25] === 6 };
}

/** The sizes an .ico holds (its directory: 0 stands for 256). */
function icoSizes(path: string): number[] {
  const b = readFileSync(path);
  assert.equal(b.readUInt16LE(2), 1, `${path} is an icon`);
  return Array.from({ length: b.readUInt16LE(4) }, (_, i) => b[6 + i * 16] || 256);
}

test("each platform's tray file is there, the menu bar's as a 1x/2x template pair", () => {
  assert.equal(trayIconFile("darwin"), "trayTemplate.png");
  assert.equal(trayIconFile("win32"), "tray.ico");
  assert.equal(trayIconFile("linux"), "tray.png");
  const assets = join(root, "src", "assets");
  assert.deepEqual(png(join(assets, "trayTemplate.png")), { width: 18, height: 18, alpha: true });
  assert.deepEqual(png(join(assets, "trayTemplate@2x.png")), { width: 36, height: 36, alpha: true });
  assert.deepEqual(icoSizes(join(assets, "tray.ico")).sort((a, b) => a - b), [16, 20, 24, 32, 40, 48]);
  assert.deepEqual(png(join(assets, "tray.png")), { width: 32, height: 32, alpha: true });
});

test("the app icons electron-builder packs: macOS's 1024 PNG and Windows' .ico up to 256", () => {
  assert.deepEqual(png(join(root, "build", "icon.png")), { width: 1024, height: 1024, alpha: true });
  assert.deepEqual(icoSizes(join(root, "build", "icon.ico")).sort((a, b) => a - b), [16, 24, 32, 48, 64, 128, 256]);
});

test("electron-builder icon configuration is equivalent in LF and CRLF checkouts", () => {
  const source = readFileSync(join(root, "electron-builder.yml"), "utf8");
  const lf = source.replaceAll("\r\n", "\n");
  for (const config of [lf, lf.replaceAll("\n", "\r\n")]) {
    const normalized = config.replaceAll("\r\n", "\n");
    assert.match(normalized, /mac:\n(?:  .*\n)*?  icon: build\/icon\.png\n/);
    assert.match(normalized, /win:\n  icon: build\/icon\.ico\n/);
  }
});
