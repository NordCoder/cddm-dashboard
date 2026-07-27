import test from "node:test";
import assert from "node:assert/strict";
import { ClaimLedger, memoryStorage } from "../src/ledger.js";
import { ExecutionCoordinator } from "../src/executor.js";

function execution() {
  return {
    claim_id: "claim-1",
    prompt: "exact prompt",
    command: {
      id: "command-1", claim_id: "claim-1", status: "claimed", worker_id: "worker-1", worker_session_id: "session-1",
      target_kind: "chatgpt_conversation", target_ref: "https://chatgpt.com/c/conversation-1"
    }
  };
}

test("duplicate claim delivery acknowledges ledger state without invoking DOM twice", async () => {
  const ledger = new ClaimLedger(memoryStorage());
  const calls = { insert: 0, send: 0, complete: 0 };
  const backend = { async complete() { calls.complete += 1; } };
  const adapter = { async insertPrompt() { calls.insert += 1; }, async sendPrompt() { calls.send += 1; return { sent: true }; } };
  const coordinator = new ExecutionCoordinator({ ledger, backend, adapter });
  const identity = { workerId: "worker-1", sessionId: "session-1" };
  const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/conversation-1" };
  assert.equal((await coordinator.execute(execution(), identity, target)).outcome, "delivered");
  assert.equal((await coordinator.execute(execution(), identity, target)).outcome, "delivered");
  assert.deepEqual(calls, { insert: 1, send: 1, complete: 1 });
});

test("identity mismatch is rejected before any DOM adapter call", async () => {
  let invoked = false;
  const coordinator = new ExecutionCoordinator({ ledger: new ClaimLedger(memoryStorage()), backend: { async complete() {} }, adapter: { async insertPrompt() { invoked = true; } } });
  const result = await coordinator.execute(execution(), { workerId: "other", sessionId: "session-1" }, { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/conversation-1" });
  assert.equal(result.outcome, "uncertain");
  assert.equal(invoked, false);
});
