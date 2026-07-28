import test from "node:test";
import assert from "node:assert/strict";
import { memoryStorage } from "../src/ledger.js";
import { ExtensionRuntime } from "../src/service-worker.js";

const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/one" };
const promptHash = "eed1d81b1a386e05e946a46581d3a07f3a1be21fb4ff482de024318f1fab19e9";

test("heartbeat replacement cannot move an in-flight claim completion to the new backend", async () => {
  const storage = memoryStorage({ backend_origin: "http://localhost:8080" });
  const completions = [];
  let releaseClaim;
  let claimStarted;
  let armed = false;
  const started = new Promise((resolve) => { claimStarted = resolve; });
  const backends = new Map();
  function backendFor(origin) {
    if (!backends.has(origin)) backends.set(origin, {
      async register() { return { state: "live" }; },
      async heartbeat() { return { state: "live" }; },
      async claimNext() {
        if (!armed || origin !== "http://localhost:8080") return null;
        claimStarted();
        await new Promise((resolve) => { releaseClaim = resolve; });
        return {
          claim_id: "claim-1",
          prompt: "exact prompt",
          command: {
            id: "command-1", claim_id: "claim-1", status: "claimed", worker_id: runtime.workerId,
            worker_session_id: runtime.sessionId, target_kind: "chatgpt_conversation",
            target_ref: "https://chatgpt.com/c/one", prompt_hash: promptHash,
          },
        };
      },
      async complete(_id, payload) { completions.push({ origin, payload }); },
    });
    return backends.get(origin);
  }
  const adapter = {
    insertCalls: 0, sendCalls: 0,
    async currentTarget() { return target; },
    async insertPrompt() { this.insertCalls += 1; },
    async sendPrompt() { this.sendCalls += 1; return { sent: true }; },
  };
  const chrome = {
    storage: { local: storage },
    permissions: { async contains() { return true; } },
    alarms: { async create() {} },
  };
  const runtime = new ExtensionRuntime(chrome, {
    storage, adapter, sessionId: "session-1", clientFactory: backendFor,
    scheduler: { active: true, start() {}, async alarm() {} },
  });
  await runtime.start();

  armed = true;
  const polling = runtime.pollCycle();
  await started;
  storage.values.backend_origin = "http://localhost:9000";
  await runtime.heartbeatCycle();
  releaseClaim();
  await polling;

  assert.equal(adapter.insertCalls, 0);
  assert.equal(adapter.sendCalls, 0);
  assert.equal(completions.length, 1);
  assert.equal(completions[0].origin, "http://localhost:8080");
  assert.equal(completions[0].payload.outcome, "failed");
});
