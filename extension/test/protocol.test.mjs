import test from "node:test";
import assert from "node:assert/strict";
import {
  chatGPTProjectScope,
  conversationURLBelongsToProject,
  isOpaqueIdentifier,
  normalizeBackendOrigin,
  normalizeChatGPTProjectUrl,
  normalizeTargetUrl,
  randomId,
  sameTarget,
  sha256Hex,
} from "../src/protocol.js";

test("opaque identifiers reject prototype keys and malformed values", () => {
  assert.equal(isOpaqueIdentifier("claim-1"), true);
  assert.equal(isOpaqueIdentifier("__proto__"), false);
  assert.equal(isOpaqueIdentifier("bad/value"), false);
  assert.equal(isOpaqueIdentifier(""), false);
});

test("random and digest primitives require secure browser crypto", async () => {
  assert.match(randomId(), /^[A-Za-z0-9._-]{1,200}$/);
  assert.equal(await sha256Hex("exact prompt"), "eed1d81b1a386e05e946a46581d3a07f3a1be21fb4ff482de024318f1fab19e9");
});

test("normalizes only credential-free backend origins", () => {
  assert.equal(normalizeBackendOrigin("http://localhost:8080/"), "http://localhost:8080");
  assert.throws(() => normalizeBackendOrigin("http://user:pass@localhost:8080"));
  assert.throws(() => normalizeBackendOrigin("http://localhost:8080/api"));
  assert.throws(() => normalizeBackendOrigin("http://localhost:8080/?token=x"));
});

test("normalizes global and project-scoped conversation URLs to one canonical target", () => {
  const target = normalizeTargetUrl("https://chatgpt.com/c/abc-123");
  const scoped = normalizeTargetUrl("https://chatgpt.com/g/g-p-repository/c/abc-123");
  assert.deepEqual(target, { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/abc-123" });
  assert.deepEqual(scoped, target);
  assert.equal(normalizeTargetUrl("https://chatgpt.com/"), null);
  assert.equal(normalizeTargetUrl("https://chatgpt.com/c/abc?x=1"), null);
  assert.equal(normalizeTargetUrl("https://chatgpt.com/share/abc"), null);
  assert.equal(sameTarget(target, normalizeTargetUrl("https://chatgpt.com/c/abc-123#ignored")), false);
});

test("normalizes ChatGPT project pages and verifies exact project ownership", () => {
  const project = "https://chatgpt.com/g/g-p-repository/project";
  assert.equal(normalizeChatGPTProjectUrl(`${project}/`), project);
  assert.equal(chatGPTProjectScope(project), "/g/g-p-repository");
  assert.equal(conversationURLBelongsToProject("https://chatgpt.com/g/g-p-repository/c/fresh", project), true);
  assert.equal(conversationURLBelongsToProject("https://chatgpt.com/g/g-p-other/c/fresh", project), false);
  assert.equal(conversationURLBelongsToProject("https://chatgpt.com/c/fresh", project), false);
  assert.throws(() => normalizeChatGPTProjectUrl("https://chatgpt.com/c/existing"));
  assert.throws(() => normalizeChatGPTProjectUrl("https://example.com/project"));
});
