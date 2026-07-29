import test from "node:test";
import assert from "node:assert/strict";
import {
  chatGPTProjectScope,
  conversationURLBelongsToProject,
  creationSurfaceMatches,
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

test("normalizes global and Project-scoped conversation URLs to canonical targets", () => {
  const target = normalizeTargetUrl("https://chatgpt.com/c/abc-123");
  assert.deepEqual(target, { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/abc-123" });
  assert.deepEqual(normalizeTargetUrl("https://chatgpt.com/g/g-project/repository/c/abc-123"), target);
  assert.equal(normalizeTargetUrl("https://chatgpt.com/"), null);
  assert.equal(normalizeTargetUrl("https://chatgpt.com/c/abc?x=1"), null);
  assert.equal(normalizeTargetUrl("https://chatgpt.com/share/abc"), null);
  assert.equal(sameTarget(target, normalizeTargetUrl("https://chatgpt.com/c/abc-123#ignored")), false);
});

test("validates exact ChatGPT Project creation and conversation scope", () => {
  const project = "https://chatgpt.com/g/g-project/repository/project";
  assert.equal(normalizeChatGPTProjectUrl(`${project}/`), project);
  assert.equal(chatGPTProjectScope(project), "/g/g-project/repository");
  assert.equal(creationSurfaceMatches(project, project), true);
  assert.equal(creationSurfaceMatches("https://chatgpt.com/g/g-project/repository/c/chat-one", project), true);
  assert.equal(conversationURLBelongsToProject("https://chatgpt.com/g/g-project/repository/c/chat-one", project), true);
  assert.equal(conversationURLBelongsToProject("https://chatgpt.com/c/chat-one", project), false);
  assert.equal(creationSurfaceMatches("https://chatgpt.com/g/other/project", project), false);
  assert.throws(() => normalizeChatGPTProjectUrl("https://chatgpt.com/c/already-a-chat"));
  assert.throws(() => normalizeChatGPTProjectUrl("https://evil.example/project"));
});
