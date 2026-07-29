import test from "node:test";
import assert from "node:assert/strict";
import { SurfaceCreationError } from "../src/adapter.js";
import { PreSendError } from "../src/executor.js";
import { memoryStorage } from "../src/ledger.js";
import { ExtensionRuntime } from "../src/service-worker.js";

const backendOrigin = "http://localhost:1338";
const projectURL = "https://chatgpt.com/g/g-project/repository/project";

function provisionRequest(overrides = {}) {
  return {
    request_id: "provision-request",
    project_id: 9,
    intent_id: "intent-one",
    lane_lease_id: "lease-one",
    lane_key: "project:9:issue:140:qa:head",
    issue_number: 140,
    role: "qa",
    expected_head: "241401d9f5c1fb2004eeb19ec612323f74b57199",
    attachment_profile: "cddm-dashboard-attachments/v2:qa:bootstrap",
    attachments: ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"],
    bootstrap_text: "Wait for a Workflow Command.",
    session_policy: "fresh_per_intent",
    chatgpt_project_url: projectURL,
    expected_binding_version: 0,
    status: "claimed",
    claim_id: "claim-one",
    claim_owner: "primary-worker",
    claim_token: "claim-token",
    ...overrides,
  };
}

function chromeFixture(storage) {
  return {
    storage: { local: storage },
    permissions: { async contains() { return true; } },
    alarms: { async create() {} },
    tabs: { onActivated: { addListener() {} } },
  };
}

function schedulerFixture() {
  return {
    active: true,
    handlers: null,
    start(handlers) { this.handlers = handlers; },
    async alarm(name) { await this.handlers?.[name === "cddm-heartbeat" ? "heartbeat" : "poll"]?.(); },
  };
}

function backendFixture({ request = provisionRequest(), completeFailures = 0 } = {}) {
  const calls = { register: [], heartbeat: [], claimProvision: [], completeProvision: [], claimNext: [] };
  let requestAvailable = true;
  let failures = completeFailures;
  return {
    calls,
    setRequestAvailable(value) { requestAvailable = value; },
    async register(payload) { calls.register.push(payload); return { state: "live" }; },
    async heartbeat(workerID, payload) { calls.heartbeat.push({ workerID, payload }); return { state: "live" }; },
    async claimNext(payload) { calls.claimNext.push(payload); return null; },
    async complete() {},
    async claimProvision(payload) {
      calls.claimProvision.push(payload);
      return requestAvailable ? { ...request } : null;
    },
    async completeProvision(requestID, payload) {
      calls.completeProvision.push({ requestID, payload });
      if (failures > 0) {
        failures -= 1;
        throw new Error("backend_offline_after_tab_creation");
      }
      requestAvailable = false;
      return {
        ...request,
        request_id: requestID,
        status: payload.outcome,
        claim_owner: payload.claim_owner,
        claim_token: payload.claim_token,
        worker_id: payload.worker_id || "",
        tab_id: payload.tab_id || 0,
        target: payload.target || null,
      };
    },
  };
}

function surfaceAdapter({ available = true, createError = null } = {}) {
  const calls = { create: 0, reserve: [], close: [], observations: 0 };
  return {
    calls,
    async currentTarget() { return null; },
    reserveManagedTab(tabID) { calls.reserve.push(tabID); },
    async createConversationSurface(url) {
      calls.create += 1;
      assert.equal(url, projectURL);
      if (createError) throw createError;
      return { tabId: 77, chatGPTProjectUrl: url, target: null };
    },
    async managedObservation(tabID, url, expectedTarget) {
      calls.observations += 1;
      assert.equal(tabID, 77);
      assert.equal(url, projectURL);
      assert.equal(expectedTarget ?? null, null);
      return { available, target: null };
    },
    async closeManagedTab(tabID) { calls.close.push(tabID); },
    exactTab() { throw new Error("surface_ready worker must not poll normal delivery"); },
  };
}

test("provisions and persists an exact managed surface without any Dashboard page or bootstrap send", async () => {
  const storage = memoryStorage({ backend_origin: backendOrigin, worker_id: "primary-worker" });
  const backend = backendFixture();
  const adapter = surfaceAdapter();
  const scheduler = schedulerFixture();
  const runtime = new ExtensionRuntime(chromeFixture(storage), {
    storage,
    adapter,
    scheduler,
    sessionId: "runtime-session",
    clientFactory: () => backend,
  });

  await runtime.start();

  assert.equal(adapter.calls.create, 1);
  assert.equal(backend.calls.claimProvision.length, 1);
  assert.equal(backend.calls.completeProvision.length, 1);
  assert.equal(backend.calls.completeProvision[0].payload.outcome, "surface_ready");
  assert.equal(backend.calls.completeProvision[0].payload.tab_id, 77);
  assert.equal(backend.calls.completeProvision[0].payload.target, undefined);
  assert.equal(backend.calls.claimNext.length, 0);

  const stored = storage.values.managed_chat_workers;
  const records = Object.values(stored);
  assert.equal(records.length, 1);
  assert.equal(records[0].request_id, "provision-request");
  assert.equal(records[0].tab_id, 77);
  assert.equal(records[0].target, null);
  assert.equal(records[0].provision_status, "surface_ready");
  assert.equal(records[0].pending_surface_completion, false);
  assert.ok(backend.calls.register.some((payload) => payload.worker_id === records[0].worker_id && payload.observation.target === null));
});

test("service-worker restart reuses the persisted creation tab and does not create a duplicate", async () => {
  const storage = memoryStorage({ backend_origin: backendOrigin, worker_id: "primary-worker" });
  const firstBackend = backendFixture();
  const firstAdapter = surfaceAdapter();
  const first = new ExtensionRuntime(chromeFixture(storage), {
    storage,
    adapter: firstAdapter,
    scheduler: schedulerFixture(),
    sessionId: "first-session",
    clientFactory: () => firstBackend,
  });
  await first.start();
  assert.equal(firstAdapter.calls.create, 1);

  const recoveredRequest = provisionRequest({ status: "surface_ready" });
  const secondBackend = backendFixture({ request: recoveredRequest });
  const secondAdapter = surfaceAdapter();
  secondAdapter.createConversationSurface = async () => {
    secondAdapter.calls.create += 1;
    throw new Error("duplicate tab creation");
  };
  const second = new ExtensionRuntime(chromeFixture(storage), {
    storage,
    adapter: secondAdapter,
    scheduler: schedulerFixture(),
    sessionId: "second-session",
    clientFactory: () => secondBackend,
  });
  await second.start();

  assert.equal(secondAdapter.calls.create, 0);
  assert.ok(secondAdapter.calls.reserve.includes(77));
  const record = Object.values(storage.values.managed_chat_workers)[0];
  assert.equal(record.tab_id, 77);
  assert.equal(record.provision_status, "surface_ready");
  assert.ok(secondBackend.calls.register.some((payload) => payload.worker_id === record.worker_id));
  assert.equal(secondBackend.calls.claimNext.length, 0);
});

test("a backend failure after tab creation preserves the same local tab for retry", async () => {
  const storage = memoryStorage({ backend_origin: backendOrigin, worker_id: "primary-worker" });
  const backend = backendFixture({ completeFailures: 1 });
  const adapter = surfaceAdapter();
  const runtime = new ExtensionRuntime(chromeFixture(storage), {
    storage,
    adapter,
    scheduler: schedulerFixture(),
    sessionId: "runtime-session",
    clientFactory: () => backend,
  });
  await runtime.start();

  assert.equal(adapter.calls.create, 1);
  let record = Object.values(storage.values.managed_chat_workers)[0];
  assert.equal(record.tab_id, 77);
  assert.equal(record.pending_surface_completion, true);

  backend.setRequestAvailable(false);
  await runtime.provisionCycle();
  record = Object.values(storage.values.managed_chat_workers)[0];
  assert.equal(adapter.calls.create, 1);
  assert.equal(record.pending_surface_completion, false);
  assert.equal(record.provision_status, "surface_ready");
  assert.equal(backend.calls.completeProvision.length, 2);
});

test("safe pre-surface failure and uncertain post-tab outcome are terminal and never persisted as workers", async () => {
  for (const [error, expected] of [
    [new PreSendError("chat_creation_unavailable"), "safe_failed"],
    [new SurfaceCreationError("chat_creation_surface_timeout", "uncertain", 77), "uncertain"],
  ]) {
    const storage = memoryStorage({ backend_origin: backendOrigin, worker_id: "primary-worker" });
    const backend = backendFixture();
    const adapter = surfaceAdapter({ createError: error });
    const runtime = new ExtensionRuntime(chromeFixture(storage), {
      storage,
      adapter,
      scheduler: schedulerFixture(),
      sessionId: `runtime-${expected}`,
      clientFactory: () => backend,
    });
    await runtime.start();

    assert.equal(backend.calls.completeProvision.length, 1);
    assert.equal(backend.calls.completeProvision[0].payload.outcome, expected);
    assert.equal(Object.keys(storage.values.managed_chat_workers || {}).length, 0);
    if (expected === "uncertain") assert.deepEqual(adapter.calls.close, [77]);
  }
});

test("a closed pre-bootstrap managed tab is safely retired and never retargeted", async () => {
  const record = {
    worker_id: "managed-worker",
    request_id: "provision-request",
    claim_owner: "primary-worker",
    claim_token: "claim-token",
    tab_id: 77,
    target: null,
    project_id: 9,
    intent_id: "intent-one",
    issue_number: 140,
    role: "qa",
    lane_key: "project:9:issue:140:qa:head",
    expected_head: "241401d9f5c1fb2004eeb19ec612323f74b57199",
    chatgpt_project_url: projectURL,
    provision_status: "surface_ready",
    pending_surface_completion: false,
    created_at: 1,
  };
  const storage = memoryStorage({
    backend_origin: backendOrigin,
    worker_id: "primary-worker",
    managed_chat_workers: { "managed-worker": record },
  });
  const backend = backendFixture({ request: null });
  backend.setRequestAvailable(false);
  const adapter = surfaceAdapter({ available: false });
  const runtime = new ExtensionRuntime(chromeFixture(storage), {
    storage,
    adapter,
    scheduler: schedulerFixture(),
    sessionId: "runtime-session",
    clientFactory: () => backend,
  });
  await runtime.start();

  assert.equal(backend.calls.completeProvision.length, 1);
  assert.equal(backend.calls.completeProvision[0].payload.outcome, "safe_failed");
  assert.deepEqual(adapter.calls.close, [77]);
  assert.deepEqual(storage.values.managed_chat_workers, {});
  assert.equal(backend.calls.claimNext.length, 0);
});

test("alarm polling provisions work without an external Dashboard message", async () => {
  const storage = memoryStorage({ backend_origin: backendOrigin, worker_id: "primary-worker" });
  const backend = backendFixture();
  backend.setRequestAvailable(false);
  const adapter = surfaceAdapter();
  const scheduler = schedulerFixture();
  const runtime = new ExtensionRuntime(chromeFixture(storage), {
    storage,
    adapter,
    scheduler,
    sessionId: "runtime-session",
    clientFactory: () => backend,
  });
  await runtime.start();
  assert.equal(adapter.calls.create, 0);

  backend.setRequestAvailable(true);
  await runtime.handleAlarm("cddm-claim-poll");
  assert.equal(adapter.calls.create, 1);
  assert.equal(backend.calls.completeProvision[0].payload.outcome, "surface_ready");
});
