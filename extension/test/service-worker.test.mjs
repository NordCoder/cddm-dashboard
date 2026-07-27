import test from "node:test";
import assert from "node:assert/strict";
import { memoryStorage } from "../src/ledger.js";
import {
  ALARM_FALLBACK_DELAY_MS, HEARTBEAT_INTERVAL_MS, POLL_INTERVAL_MS,
  ExtensionRuntime, RuntimeScheduler
} from "../src/service-worker.js";

const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/one" };

function chromeFor(storage) {
  return {
    storage: { local: storage },
    permissions: { async contains() { return true; } },
    alarms: { async create() {} }
  };
}

function runtimeFixture({ sessionId = "session-1", targetValue = target } = {}) {
  const storage = memoryStorage({ backend_origin: "http://localhost:8080" });
  const adapter = { target: targetValue, async currentTarget() { return this.target; } };
  const calls = { register: [], heartbeat: [], claim: 0 };
  const backend = {
    async register(payload) { calls.register.push(payload); return { state: "live" }; },
    async heartbeat(_workerId, payload) { calls.heartbeat.push(payload); return { state: "live" }; },
    async claimNext() { calls.claim += 1; return null; }
  };
  const scheduler = { handlers: null, start(handlers) { this.handlers = handlers; }, async alarm() {} };
  const runtime = new ExtensionRuntime(chromeFor(storage), { storage, adapter, sessionId, scheduler, clientFactory: () => backend });
  return { runtime, storage, adapter, calls, scheduler };
}

test("active-worker scheduler uses bounded timers and one-shot 30s alarm wakeups", () => {
  const timers = [];
  const alarms = [];
  const scheduler = new RuntimeScheduler({
    setTimeoutFn(callback, delay) { const timer = { callback, delay }; timers.push(timer); return timer; },
    clearTimeoutFn() {}, alarms: { create(name, options) { alarms.push({ name, options }); } }, now: () => 1000
  });
  let heartbeatCalls = 0;
  scheduler.start({ heartbeat() { heartbeatCalls += 1; }, poll() {} });
  assert.deepEqual(timers.map((timer) => timer.delay).sort((left, right) => left - right), [POLL_INTERVAL_MS, HEARTBEAT_INTERVAL_MS]);
  assert.deepEqual(alarms.map(({ name, options }) => [name, options]), [
    ["cddm-heartbeat", { when: 1000 + ALARM_FALLBACK_DELAY_MS }],
    ["cddm-claim-poll", { when: 1000 + ALARM_FALLBACK_DELAY_MS }]
  ]);
  assert.ok(alarms.every(({ options }) => !Object.hasOwn(options, "periodInMinutes")));
  return timers.find((timer) => timer.delay === HEARTBEAT_INTERVAL_MS).callback().then(() => {
    assert.equal(heartbeatCalls, 1);
    assert.equal(timers.filter((timer) => timer.delay === HEARTBEAT_INTERVAL_MS).length, 2);
  });
});

test("heartbeat proves current target, then explicitly reports no target", async () => {
  const fixture = runtimeFixture();
  await fixture.runtime.start();
  fixture.adapter.target = null;
  await fixture.scheduler.handlers.heartbeat();
  assert.equal(fixture.calls.register.length, 1);
  assert.equal(fixture.calls.heartbeat.length, 1);
  assert.deepEqual(fixture.calls.register[0].observation.target, target);
  assert.equal(fixture.calls.heartbeat[0].observation.target, null);
  assert.equal(fixture.runtime.presenceCurrent, false);
  assert.equal(fixture.calls.claim, 1, "no-target heartbeat must not create another claim poll");
});

test("runtime restart keeps worker identity but creates a fresh session and registers again", async () => {
  const first = runtimeFixture({ sessionId: "session-1" });
  await first.runtime.start();
  const second = runtimeFixture({ sessionId: "session-2" });
  second.storage.values.backend_origin = "http://localhost:8080";
  // Share the durable installation identity while using a new runtime object.
  second.storage.values.worker_id = (await first.storage.get("worker_id")).worker_id;
  await second.runtime.start();
  assert.equal(first.runtime.workerId, second.runtime.workerId);
  assert.notEqual(first.runtime.sessionId, second.runtime.sessionId);
  assert.equal(first.calls.register[0].worker_session_id, "session-1");
  assert.equal(second.calls.register[0].worker_session_id, "session-2");
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
