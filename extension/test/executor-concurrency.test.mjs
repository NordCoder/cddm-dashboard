import test from "node:test";
import assert from "node:assert/strict";
import { ClaimLedger, memoryStorage } from "../src/ledger.js";
import { ExecutionCoordinator } from "../src/executor.js";

const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/one" };
const execution = {
  claim_id: "claim-1",
  prompt: "exact prompt",
  command: {
    id: "command-1", claim_id: "claim-1", status: "claimed", worker_id: "worker-1", worker_session_id: "session-1",
    target_kind: "chatgpt_conversation", target_ref: "https://chatgpt.com/c/one",
    prompt_hash: "eed1d81b1a386e05e946a46581d3a07f3a1be21fb4ff482de024318f1fab19e9",
  },
};

test("concurrent duplicate sees claim_in_progress without premature completion or second DOM send", async () => {
  const ledger = new ClaimLedger(memoryStorage());
  let releaseInsert;
  let insertStarted;
  const started = new Promise((resolve) => { insertStarted = resolve; });
  const calls = { insert: 0, send: 0, complete: 0 };
  const coordinator = new ExecutionCoordinator({
    ledger,
    backendOrigin: "http://localhost:8080",
    backend: { async complete() { calls.complete += 1; } },
    adapter: {
      async insertPrompt() {
        calls.insert += 1;
        insertStarted();
        await new Promise((resolve) => { releaseInsert = resolve; });
      },
      async sendPrompt() { calls.send += 1; return { sent: true }; },
    },
  });
  const identity = { workerId: "worker-1", sessionId: "session-1" };
  const first = coordinator.execute(execution, identity, target);
  await started;
  const duplicate = await coordinator.execute(execution, identity, target);
  assert.deepEqual(duplicate, { outcome: "uncertain", reason: "claim_in_progress", completion_diagnostic: "" });
  assert.deepEqual(calls, { insert: 1, send: 0, complete: 0 });
  releaseInsert();
  assert.equal((await first).outcome, "delivered");
  assert.deepEqual(calls, { insert: 1, send: 1, complete: 1 });
});
