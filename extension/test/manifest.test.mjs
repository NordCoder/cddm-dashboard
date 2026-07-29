import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";

function extensionID(key) {
  const digest = createHash("sha256").update(Buffer.from(key, "base64")).digest().subarray(0, 16).toString("hex");
  return [...digest].map((value) => String.fromCharCode("a".charCodeAt(0) + Number.parseInt(value, 16))).join("");
}

test("manifest keeps page access, identity, and provisioning permissions bounded", async () => {
  const manifest = JSON.parse(await readFile(new URL("../manifest.json", import.meta.url), "utf8"));
  assert.equal(extensionID(manifest.key), "biakfbpkfdpniphmoafgldedkbnjfibp");
  assert.deepEqual(manifest.host_permissions, ["https://chatgpt.com/*"]);
  assert.ok(manifest.optional_host_permissions?.length);
  assert.ok(!manifest.host_permissions.some((permission) => permission.includes("all_urls") || permission === "*://*/*"));
  assert.deepEqual(manifest.permissions, ["storage", "alarms", "tabs"]);
  assert.ok(!manifest.permissions.includes("cookies"));
  assert.ok(!manifest.permissions.includes("webRequest"));
  assert.ok(!manifest.permissions.includes("clipboardRead"));
  assert.equal(manifest.content_scripts[0].matches[0], "https://chatgpt.com/*");
  assert.equal(manifest.externally_connectable, undefined);
});
