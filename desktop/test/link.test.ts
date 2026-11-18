import assert from "node:assert/strict";
import test from "node:test";
import { linkArguments, publicLinkStatus } from "../src/link-contract.cjs";
import { hubPartition } from "../src/sessions.js";
import { parseHubAddress } from "../src/urls.js";

test("Link commands have fixed argv and pairing credentials only on stdin", () => {
  assert.deepEqual(linkArguments({ action: "status" }), { args: ["status", "--json"] });
  assert.deepEqual(linkArguments({ action: "pair-active", value: "persona://link?token=example" }), { args: ["pair", "-"], stdin: "persona://link?token=example" });
  assert.equal(linkArguments({ action: "pair-active", value: "https://evil.test" }), undefined);
  assert.equal(linkArguments({ action: "rename", value: "x".repeat(41) }), undefined);
  assert.equal(linkArguments({ action: "pair-passive", value: "--help" }), undefined);
  assert.deepEqual(linkArguments({ action: "plugin-install" }), { args: ["plugin", "install"] });
  assert.deepEqual(linkArguments({ action: "plugin-uninstall" }), { args: ["plugin", "uninstall"] });
});

test("Link status forwards the Claude Code plugin's state words, never its directories or errors", () => {
  const result = publicLinkStatus({ running: true, claude_plugin: { state: "installed", auto: true, version: "0.1.0", bundled: "0.1.0",
    error: "C:\Users\someone\.claude: failed", config_dirs: [{ path: "/home/user/.claude", state: "installed" }] } });
  assert.deepEqual(result, { running: true, claude_plugin: { state: "installed", auto: true, version: "0.1.0" } });
  assert.deepEqual(publicLinkStatus({ claude_plugin: { state: "x".repeat(40), auto: "yes" } }), { claude_plugin: {} });
});

test("Link status never forwards config paths, credentials or logs", () => {
  const result = publicLinkStatus({ running: true, machine: "computer", config: "/home/user/.persona/link.json", token: "secret", link: { state: "connected", address: "https://internal.example", pid: 123, node_error: "internal detail", pending: true } });
  assert.deepEqual(result, { running: true, machine: "computer", link: { state: "connected", pending: true } });
});

test("reverse-proxy prefixes on one origin have separate web sessions", () => {
  const first = hubPartition(parseHubAddress("https://hub.example/one"));
  const second = hubPartition(parseHubAddress("https://hub.example/two"));
  assert.notEqual(first, second);
  assert.equal(first, hubPartition(parseHubAddress("https://hub.example/one/")));
  assert.match(first, /^persist:hub-[a-f0-9]{32}$/);
});
