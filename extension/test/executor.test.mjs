import test from "node:test";
import assert from "node:assert/strict";
import { ClaimLedger, memoryStorage } from "../src/ledger.js";
import { ExecutionCoordinator, PreSendError } from "../src/executor.js";

const origin = "http://localhost:8080";
const promptHash = "eed1d81b1a386e05e946a46581d3a07f3a1be21fb4ff482de024318f1fab19e9";
function execution(overrides = {}) {
  return {
    claim_id: "claim-1",
    prompt: "exact prompt",
    command: {
      id: "command-1", claim_id: "claim-1", status: "claimed", worker_id: "worker-1", worker_session_id: "session-1",
      target_kind: "chatgpt_conversation", target_ref: "https://chatgpt.com/c/conversation-1", prompt_hash: promptHash,
      ...overrides.command,
    },
    ...overrides,
  };
}
const identity = { workerId: "worker-1", sessionId: "session-1" };
const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/conversation-1" };
function coordinator({ ledger = new ClaimLedger(memoryStorage()), backend, adapter }) {
  return new ExecutionCoordinator({ ledger, backend, adapter, backendOrigin: origin });
}

test("duplicate claim delivery acknowledges ledger state without invoking DOM twice", async () => {
  const ledger = new ClaimLedger(memoryStorage());
  const calls = { insert: 0, send: 0, complete: 0 };
  const backend = { async complete() { calls.complete += 1; } };
  const adapter = { async insertPrompt() { calls.insert += 1; }, async sendPrompt() { calls.send += 1; return { sent: true }; } };
  const subject = coordinator({ ledger, backend, adapter });
  assert.equal((await subject.execute(execution(), identity, target)).outcome, "delivered");
  assert.equal((await subject.execute(execution(), identity, target)).outcome, "delivered");
  assert.deepEqual(calls, { insert: 1, send: 1, complete: 1 });
});

test("prompt hash mismatch is reported uncertain without touching the DOM", async () => {
  let domCalls = 0;
  const completions = [];
  const subject = coordinator({
    backend: { async complete(_id, payload) { completions.push(payload); } },
    adapter: { async insertPrompt() { domCalls += 1; } },
  });
  const result = await subject.execute(execution({ prompt: "tampered prompt" }), identity, target);
  assert.equal(result.reason, "prompt_hash_mismatch");
  assert.equal(domCalls, 0);
  assert.equal(completions[0].outcome, "uncertain");
});

test("malformed execution identities fail closed before ledger or DOM access", async () => {
  let invoked = false;
  const subject = coordinator({ backend: { async complete() {} }, adapter: { async insertPrompt() { invoked = true; } } });
  const result = await subject.execute(execution({ command: { id: "__proto__" } }), identity, target);
  assert.equal(result.reason, "execution_identity_invalid");
  assert.equal(invoked, false);
});

test("claim entries never cross backend authority domains", async () => {
  const ledger = new ClaimLedger(memoryStorage());
  await ledger.reserve("claim-1", "command-1", "http://localhost:9000");
  await ledger.mark("claim-1", "uncertain", "old_backend");
  let invoked = false;
  const subject = coordinator({ ledger, backend: { async complete() {} }, adapter: { async insertPrompt() { invoked = true; } } });
  const result = await subject.execute(execution(), identity, target);
  assert.equal(result.reason, "claim_authority_mismatch");
  assert.equal(invoked, false);
});

test("backend configuration guard is persisted as proved pre-send failure", async () => {
  const ledger = new ClaimLedger(memoryStorage());
  let invoked = false;
  const completions = [];
  const subject = coordinator({
    ledger,
    backend: { async complete(_id, payload) { completions.push(payload); } },
    adapter: { async insertPrompt() { invoked = true; } },
  });
  const result = await subject.execute(execution(), identity, target, async () => { throw new PreSendError("backend_configuration_changed_before_send"); });
  assert.equal(result.outcome, "failed");
  assert.equal(invoked, false);
  assert.equal(completions[0].outcome, "failed");
  assert.equal((await ledger.get("claim-1")).state, "failed_pre_send");
});

test("permanent completion rejection is terminal locally without DOM replay", async () => {
  const ledger = new ClaimLedger(memoryStorage());
  const calls = { insert: 0, send: 0, complete: 0 };
  const backend = { async complete() { calls.complete += 1; const error = new Error("gone"); error.status = 404; throw error; } };
  const adapter = { async insertPrompt() { calls.insert += 1; }, async sendPrompt() { calls.send += 1; return { sent: true }; } };
  const subject = coordinator({ ledger, backend, adapter });
  const first = await subject.execute(execution(), identity, target);
  const second = await subject.execute(execution(), identity, target);
  assert.equal(first.completion_diagnostic, "completion_rejected_404");
  assert.equal(second.completion_diagnostic, "completion_rejected_404");
  assert.deepEqual(calls, { insert: 1, send: 1, complete: 1 });
});

test("completion conflict is terminal locally and never replays the DOM send or completion", async () => {
  const ledger = new ClaimLedger(memoryStorage());
  const calls = { insert: 0, send: 0, complete: 0 };
  const backend = { async complete() { calls.complete += 1; const error = new Error("conflict"); error.status = 409; throw error; } };
  const adapter = { async insertPrompt() { calls.insert += 1; }, async sendPrompt() { calls.send += 1; return { sent: true }; } };
  const subject = coordinator({ ledger, backend, adapter });
  const first = await subject.execute(execution(), identity, target);
  const second = await subject.execute(execution(), identity, target);
  assert.equal(first.completion_diagnostic, "completion_conflict");
  assert.equal(second.completion_diagnostic, "completion_conflict");
  assert.deepEqual(calls, { insert: 1, send: 1, complete: 1 });
});

test("transport failure while completing remains retryable without replaying DOM send", async () => {
  const ledger = new ClaimLedger(memoryStorage());
  const calls = { insert: 0, send: 0, complete: 0 };
  let fail = true;
  const backend = { async complete() { calls.complete += 1; if (fail) throw new Error("offline"); } };
  const adapter = { async insertPrompt() { calls.insert += 1; }, async sendPrompt() { calls.send += 1; return { sent: true }; } };
  const subject = coordinator({ ledger, backend, adapter });
  await subject.execute(execution(), identity, target);
  assert.equal((await ledger.get("claim-1")).acknowledged, false);
  fail = false;
  await subject.execute(execution(), identity, target);
  assert.equal((await ledger.get("claim-1")).acknowledged, true);
  assert.deepEqual(calls, { insert: 1, send: 1, complete: 2 });
});
