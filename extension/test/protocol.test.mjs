import test from "node:test";
import assert from "node:assert/strict";
import { normalizeBackendOrigin, normalizeTargetUrl, sameTarget } from "../src/protocol.js";

test("normalizes only credential-free backend origins", () => {
  assert.equal(normalizeBackendOrigin("http://localhost:8080/"), "http://localhost:8080");
  assert.throws(() => normalizeBackendOrigin("http://user:pass@localhost:8080"));
  assert.throws(() => normalizeBackendOrigin("http://localhost:8080/api"));
  assert.throws(() => normalizeBackendOrigin("http://localhost:8080/?token=x"));
});

test("normalizes supported conversation URLs without query or fragment authority", () => {
  const target = normalizeTargetUrl("https://chatgpt.com/c/abc-123");
  assert.deepEqual(target, { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/abc-123" });
  assert.equal(normalizeTargetUrl("https://chatgpt.com/"), null);
  assert.equal(normalizeTargetUrl("https://chatgpt.com/c/abc?x=1"), null);
  assert.equal(normalizeTargetUrl("https://chatgpt.com/share/abc"), null);
  assert.equal(sameTarget(target, normalizeTargetUrl("https://chatgpt.com/c/abc-123#ignored")), false);
});
