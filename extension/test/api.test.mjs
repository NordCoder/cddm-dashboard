import test from "node:test";
import assert from "node:assert/strict";
import {
  BackendClient,
  provisioningCompletionPayload,
  provisioningFinalizePayload,
} from "../src/api.js";

test("retries a transport failure with the same delivery claim request body", async () => {
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

test("claims, completes, and atomically finalizes durable provisioning requests", async () => {
  const calls = [];
  const request = {
    request_id: "provision-one",
    claim_owner: "extension-worker",
    claim_token: "claim-token",
  };
  const fetchFn = async (url, init) => {
    calls.push({ url, body: JSON.parse(init.body) });
    return new Response(JSON.stringify(request), { status: 200, headers: { "content-type": "application/json" } });
  };
  const client = new BackendClient("http://localhost:8080/", fetchFn, 0, 1000);
  const claimed = await client.claimProvision({ claim_request_id: "claim-one", claim_owner: "extension-worker", claim_ttl_seconds: 120 });
  assert.deepEqual(claimed, request);
  const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/one" };
  const completion = provisioningCompletionPayload(request, "surface_ready", { workerId: "managed-worker", tabId: 77, target });
  await client.completeProvision(request.request_id, completion);
  const finalize = provisioningFinalizePayload(request, {
    worker_id: "managed-worker",
    tab_id: 77,
    target,
    observed_chatgpt_url: "https://chatgpt.com/c/one",
    attachment_evidence: ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"],
  });
  await client.finalizeProvision(request.request_id, finalize);

  assert.equal(calls[0].url, "http://localhost:8080/api/browser/provisioning/claim-next");
  assert.deepEqual(calls[0].body, { claim_request_id: "claim-one", claim_owner: "extension-worker", claim_ttl_seconds: 120 });
  assert.equal(calls[1].url, "http://localhost:8080/api/browser/provisioning/provision-one/complete");
  assert.deepEqual(calls[1].body, {
    claim_owner: "extension-worker",
    claim_token: "claim-token",
    outcome: "surface_ready",
    reason: "unknown",
    worker_id: "managed-worker",
    tab_id: 77,
    target,
  });
  assert.equal(calls[2].url, "http://localhost:8080/api/browser/provisioning/provision-one/finalize");
  assert.deepEqual(calls[2].body, {
    claim_owner: "extension-worker",
    claim_token: "claim-token",
    worker_id: "managed-worker",
    tab_id: 77,
    target,
    observed_chatgpt_url: "https://chatgpt.com/c/one",
    attachment_evidence: ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"],
  });
});

test("terminal completion diagnostics remain bounded and attachment evidence is explicit", () => {
  const request = { claim_owner: "extension", claim_token: "token" };
  assert.deepEqual(provisioningCompletionPayload(request, "uncertain", {
    reason: "exact attachments could not be confirmed!",
    attachmentEvidence: ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"],
  }), {
    claim_owner: "extension",
    claim_token: "token",
    outcome: "uncertain",
    reason: "exact_attachments_could_not_be_confirmed_",
    attachment_evidence: ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"],
  });
});
