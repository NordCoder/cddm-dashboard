import test from "node:test";
import assert from "node:assert/strict";
import { memoryStorage } from "../src/ledger.js";
import { ALARM_FALLBACK_DELAY_MS, HEARTBEAT_INTERVAL_MS, POLL_INTERVAL_MS, ExtensionRuntime, RuntimeScheduler } from "../src/service-worker.js";

const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/one" };

function chromeFor(storage, permission = { granted: true }) {
  return {
    storage: { local: storage },
    permissions: { async contains() { return permission.granted; } },
    alarms: { async create() {} },
  };
}

function runtimeFixture({ sessionId = "session-1", targetValue = target, origin = "http://localhost:8080" } = {}) {
  const storage = memoryStorage({ backend_origin: origin });
  const permission = { granted: true };
  const adapter = {
    target: targetValue,
    insertCalls: 0,
    sendCalls: 0,
    async currentTarget() { return this.target; },
    async insertPrompt() { this.insertCalls += 1; },
    async sendPrompt() { this.sendCalls += 1; return { sent: true }; },
  };
  const calls = { register: [], heartbeat: [], claim: 0, complete: [] };
  const backends = new Map();
  function backendFor(value) {
    if (!backends.has(value)) backends.set(value, {
      async register(payload) { calls.register.push({ origin: value, payload }); return { state: "live" }; },
      async heartbeat(_workerId, payload) { calls.heartbeat.push({ origin: value, payload }); return { state: "live" }; },
      async claimNext() { calls.claim += 1; return null; },
      async complete(_commandId, payload) { calls.complete.push({ origin: value, payload }); },
    });
    return backends.get(value);
  }
  const scheduler = { active: true, handlers: null, start(handlers) { this.handlers = handlers; }, async alarm() {} };
  const runtime = new ExtensionRuntime(chromeFor(storage, permission), { storage, adapter, sessionId, scheduler, clientFactory: backendFor });
  return { runtime, storage, permission, adapter, calls, scheduler, backendFor };
}

test("active-worker scheduler catches rejected tick handlers and keeps bounded scheduling", async () => {
  const timers = [];
  const alarms = [];
  const scheduler = new RuntimeScheduler({
    setTimeoutFn(callback, delay) { const timer = { callback, delay }; timers.push(timer); return timer; },
    clearTimeoutFn() {}, alarms: { create(name, options) { alarms.push({ name, options }); } }, now: () => 1000,
  });
  scheduler.start({ heartbeat() { throw new Error("tick failed"); }, poll() {} });
  assert.deepEqual(timers.map((timer) => timer.delay).sort((a, b) => a - b), [POLL_INTERVAL_MS, HEARTBEAT_INTERVAL_MS]);
  assert.deepEqual(alarms.map(({ name, options }) => [name, options]), [
    ["cddm-heartbeat", { when: 1000 + ALARM_FALLBACK_DELAY_MS }],
    ["cddm-claim-poll", { when: 1000 + ALARM_FALLBACK_DELAY_MS }],
  ]);
  await timers.find((timer) => timer.delay === HEARTBEAT_INTERVAL_MS).callback();
  assert.equal(timers.filter((timer) => timer.delay === HEARTBEAT_INTERVAL_MS).length, 2);
});

test("permission removal disables polling immediately instead of using a stale backend", async () => {
  const fixture = runtimeFixture();
  await fixture.runtime.start();
  fixture.calls.claim = 0;
  fixture.permission.granted = false;
  await fixture.runtime.pollCycle();
  assert.equal(fixture.calls.claim, 0);
  assert.equal(fixture.runtime.presenceCurrent, false);
});

test("backend origin replacement creates a new client and re-registers rather than heartbeating stale authority", async () => {
  const fixture = runtimeFixture();
  await fixture.runtime.start();
  fixture.storage.values.backend_origin = "http://localhost:9000";
  await fixture.runtime.heartbeatCycle();
  assert.deepEqual(fixture.calls.register.map((call) => call.origin), ["http://localhost:8080", "http://localhost:9000"]);
  assert.equal(fixture.calls.heartbeat.length, 0);
  assert.equal(fixture.runtime.backendOrigin, "http://localhost:9000");
});

test("configuration change after claim is a durable pre-send failure with no DOM action", async () => {
  const fixture = runtimeFixture();
  await fixture.runtime.start();
  const backend = fixture.backendFor("http://localhost:8080");
  backend.claimNext = async () => {
    fixture.calls.claim += 1;
    fixture.storage.values.backend_origin = "http://localhost:9000";
    return {
      claim_id: "claim-1",
      prompt: "exact prompt",
      command: {
        id: "command-1", claim_id: "claim-1", status: "claimed", worker_id: fixture.runtime.workerId,
        worker_session_id: fixture.runtime.sessionId, target_kind: "chatgpt_conversation",
        target_ref: "https://chatgpt.com/c/one",
        prompt_hash: "eed1d81b1a386e05e946a46581d3a07f3a1be21fb4ff482de024318f1fab19e9",
      },
    };
  };
  await fixture.runtime.pollCycle();
  assert.equal(fixture.adapter.insertCalls, 0);
  assert.equal(fixture.adapter.sendCalls, 0);
  assert.equal(fixture.calls.complete.at(-1).origin, "http://localhost:8080");
  assert.equal(fixture.calls.complete.at(-1).payload.outcome, "failed");
});

test("reconciliation never sends an old-origin ledger entry to a replacement backend", async () => {
  const fixture = runtimeFixture();
  await fixture.runtime.start();
  await fixture.runtime.ledger.reserve("claim-old", "command-old", "http://localhost:8080");
  await fixture.runtime.ledger.mark("claim-old", "uncertain", "offline");
  fixture.storage.values.backend_origin = "http://localhost:9000";
  await fixture.runtime.heartbeatCycle();
  assert.equal(fixture.calls.complete.some((call) => call.origin === "http://localhost:9000" && call.payload.claim_id === "claim-old"), false);
});

test("heartbeat proves current target, then explicitly reports no target", async () => {
  const fixture = runtimeFixture();
  await fixture.runtime.start();
  fixture.adapter.target = null;
  await fixture.scheduler.handlers.heartbeat();
  assert.equal(fixture.calls.register.length, 1);
  assert.equal(fixture.calls.heartbeat.length, 1);
  assert.deepEqual(fixture.calls.register[0].payload.observation.target, target);
  assert.equal(fixture.calls.heartbeat[0].payload.observation.target, null);
  assert.equal(fixture.runtime.presenceCurrent, false);
});

test("runtime restart keeps worker identity but creates a fresh session and registers again", async () => {
  const durable = memoryStorage({ backend_origin: "http://localhost:8080" });
  function make(sessionId) {
    const calls = [];
    const backend = { async register(payload) { calls.push(payload); return { state: "live" }; }, async claimNext() { return null; }, async complete() {} };
    const runtime = new ExtensionRuntime(chromeFor(durable), {
      storage: durable,
      adapter: { async currentTarget() { return target; } },
      sessionId,
      scheduler: { active: true, start() {}, async alarm() {} },
      clientFactory: () => backend,
    });
    return { runtime, calls };
  }
  const first = make("session-1");
  await first.runtime.start();
  const second = make("session-2");
  await second.runtime.start();
  assert.equal(first.runtime.workerId, second.runtime.workerId);
  assert.notEqual(first.runtime.sessionId, second.runtime.sessionId);
  assert.equal(first.calls[0].worker_session_id, "session-1");
  assert.equal(second.calls[0].worker_session_id, "session-2");
});

test("polling remains serial when scheduler events overlap", async () => {
  const fixture = runtimeFixture();
  await fixture.runtime.start();
  fixture.calls.claim = 0;
  let release;
  fixture.runtime.backend.claimNext = async () => { fixture.calls.claim += 1; await new Promise((resolve) => { release = resolve; }); return null; };
  const first = fixture.runtime.pollCycle();
  const second = fixture.runtime.pollCycle();
  await new Promise((resolve) => setTimeout(resolve, 0));
  release();
  await Promise.all([first, second]);
  assert.equal(fixture.calls.claim, 1);
});
