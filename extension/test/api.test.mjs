import test from "node:test";
import assert from "node:assert/strict";
import { BackendClient } from "../src/api.js";

test("retries a transport failure with the same claim request body", async () => {
  const requests = [];
  let attempt = 0;
  const fetchFn = async (_url, init) => {
    requests.push(JSON.parse(init.body));
    attempt += 1;
    if (attempt === 1) throw new Error("offline");
    return new Response(null, { status: 204 });
  };
  const client = new BackendClient("http://localhost:8080", fetchFn, 0, 1000);
  assert.equal(await client.claimNext({ worker_id: "w", worker_session_id: "s", claim_request_id: "r" }), null);
  assert.deepEqual(requests[0], requests[1]);
});

test("bounds a hung backend request and retries the same logical operation", async () => {
  let attempts = 0;
  const fetchFn = async (_url, init) => {
    attempts += 1;
    return await new Promise((_resolve, reject) => {
      init.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true });
    });
  };
  const client = new BackendClient("http://localhost:8080", fetchFn, 0, 5);
  await assert.rejects(() => client.claimNext({ worker_id: "w", worker_session_id: "s", claim_request_id: "r" }), /backend_request_timeout/);
  assert.equal(attempts, 2);
});
