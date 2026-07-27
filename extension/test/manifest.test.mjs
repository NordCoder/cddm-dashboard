import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

test("manifest keeps page access and permissions bounded", async () => {
  const manifest = JSON.parse(await readFile(new URL("../manifest.json", import.meta.url), "utf8"));
  assert.deepEqual(manifest.host_permissions, ["https://chatgpt.com/*"]);
  assert.ok(manifest.optional_host_permissions?.length);
  assert.ok(!manifest.host_permissions.some((permission) => permission.includes("all_urls") || permission === "*://*/*"));
  assert.ok(!manifest.permissions.includes("cookies"));
  assert.ok(!manifest.permissions.includes("webRequest"));
  assert.ok(!manifest.permissions.includes("clipboardRead"));
  assert.equal(manifest.content_scripts[0].matches[0], "https://chatgpt.com/*");
});
