import test from "node:test";
import assert from "node:assert/strict";
import { isOpaqueIdentifier, randomId, sha256Hex } from "../src/protocol.js";

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
