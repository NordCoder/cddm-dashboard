import test from "node:test";
import assert from "node:assert/strict";
import { memoryStorage } from "../src/ledger.js";
import { ExtensionRuntime } from "../src/service-worker.js";

const backendOrigin = "http://localhost:1338";
const projectURL = "https://chatgpt.com/g/g-project/repository/project";
const attachments = ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"];
const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/final-chat" };
const observedURL = "https://chatgpt.com/g/g-project/repository/c/final-chat";

function request(overrides = {}) {
  return {
    request_id: "request-new",
    project_id: 9,
    intent_id: "intent-new",
    lane_lease_id: "lease-new",
    lane_key: "project:9:issue:140:qa:head",
    issue_number: 140,
    role: "qa",
    expected_head: "241401d9f5c1fb2004eeb19ec612323f74b57199",
    attachments,
    bootstrap_text: "Wait for a Workflow Command.",
    session_policy: "fresh_per_intent",
    chatgpt_project_url: projectURL,
    status: "surface_ready",
    claim_owner: "primary-worker",
    claim_token: "claim-token",
    ...overrides,
  };
}

function record(overrides = {}) {
  return {
    worker_id: "managed-worker",
    request_id: "request-new",
    claim_owner: "primary-worker",
    claim_token: "claim-token",
    tab_id: 77,
    target: null,
    project_id: 9,
    intent_id: "intent-new",
    issue_number: 140,
    role: "qa",
    lane_key: "project:9:issue:140:qa:head",
    expected_head: "241401d9f5c1fb2004eeb19ec612323f74b57199",
    chatgpt_project_url: projectURL,
    attachments,
    bootstrap_text: "Wait for a Workflow Command.",
    session_policy: "fresh_per_intent",
    provision_status: "surface_ready",
    pending_surface_completion: false,
    bootstrap_phase: "not_started",
    attachment_evidence: [],
    observed_chatgpt_url: "",
    created_at: 1,
    ...overrides,
  };
}

function chromeFixture(storage) {
  return {
    storage: { local: storage },
    permissions: { async contains() { return true; } },
    alarms: { async create() {} },
    tabs: {
      async get() { return { id: 77, url: projectURL }; },
      async sendMessage() { return { ok: true }; },
      onActivated: { addListener() {} },
    },
  };
}

function schedulerFixture() {
  return { active: true, start() {}, async alarm() {} };
}

function adapterFixture(initialTargets = new Map([[77, null]])) {
  const targets = new Map(initialTargets);
  const calls = { create: 0, close: [], reserve: [] };
  return {
    calls,
    targets,
    async currentTarget() { return null; },
    reserveManagedTab(tabID) { calls.reserve.push(tabID); },
    async managedObservation(tabID) {
      return targets.has(tabID) ? { available: true, target: targets.get(tabID) } : { available: false, target: null };
    },
    async createConversationSurface() {
      calls.create += 1;
      targets.set(88, null);
      return { tabId: 88, target: null, chatGPTProjectUrl: projectURL };
    },
    async closeManagedTab(tabID) { calls.close.push(tabID); targets.delete(tabID); },
    exactTab(tabID, expected) {
      return { async currentTarget() { return targets.get(tabID) ?? null; }, expected };
    },
  };
}

function backendFixture(initialRequest, options = {}) {
  let current = initialRequest ? { ...initialRequest } : null;
  let finalizeFailures = options.finalizeFailures || 0;
  const calls = { register: [], heartbeat: [], claimProvision: 0, complete: [], finalize: [], claimNext: [] };
  return {
    calls,
    async register(payload) { calls.register.push(payload); return { state: "live" }; },
    async heartbeat(workerID, payload) { calls.heartbeat.push({ workerID, payload }); return { state: "live" }; },
    async claimNext(payload) { calls.claimNext.push(payload); return null; },
    async complete() {},
    async claimProvision() { calls.claimProvision += 1; return current ? { ...current } : null; },
    async completeProvision(requestID, payload) {
      calls.complete.push({ requestID, payload });
      current = payload.outcome === "surface_ready"
        ? { ...current, request_id: requestID, status: "surface_ready", worker_id: payload.worker_id, tab_id: payload.tab_id, target: payload.target || null }
        : null;
      return current || { request_id: requestID, status: payload.outcome, claim_owner: payload.claim_owner, claim_token: payload.claim_token };
    },
    async finalizeProvision(requestID, payload) {
      calls.finalize.push({ requestID, payload });
      if (finalizeFailures > 0) {
        finalizeFailures -= 1;
        throw new Error("backend_offline_after_bootstrap");
      }
      current = null;
      return {
        ...initialRequest,
        request_id: requestID,
        status: "provisioned",
        target: payload.target,
        observed_chatgpt_url: payload.observed_chatgpt_url,
        attachment_evidence: payload.attachment_evidence,
        bound_binding_id: "binding-one",
        bound_binding_version: 1,
        claim_owner: payload.claim_owner,
        claim_token: payload.claim_token,
      };
    },
  };
}

function bootstrapperFixture(adapter, tabID = 77, nextTarget = target, nextURL = observedURL) {
  const calls = { send: 0, wait: 0 };
  return {
    calls,
    async send(_tabID, input) {
      calls.send += 1;
      assert.deepEqual(input.attachments, attachments);
      return { attachmentEvidence: attachments };
    },
    async waitForConversation() {
      calls.wait += 1;
      adapter.targets.set(tabID, nextTarget);
      return { target: nextTarget, observedURL: nextURL };
    },
  };
}

test("surface-ready worker sends bootstrap once, registers exact target, and atomically finalizes", async () => {
  const managed = record();
  const storage = memoryStorage({ backend_origin: backendOrigin, worker_id: "primary-worker", managed_chat_workers: { [managed.worker_id]: managed } });
  const adapter = adapterFixture();
  const backend = backendFixture(request());
  const bootstrapper = bootstrapperFixture(adapter);
  const runtime = new ExtensionRuntime(chromeFixture(storage), {
    storage, adapter, bootstrapper, scheduler: schedulerFixture(), sessionId: "runtime-one", clientFactory: () => backend,
  });
  await runtime.start();

  assert.equal(bootstrapper.calls.send, 1);
  assert.equal(bootstrapper.calls.wait, 1);
  assert.equal(backend.calls.finalize.length, 1);
  assert.deepEqual(backend.calls.finalize[0].payload.attachment_evidence, attachments);
  const stored = storage.values.managed_chat_workers[managed.worker_id];
  assert.equal(stored.provision_status, "provisioned");
  assert.equal(stored.bootstrap_phase, "provisioned");
  assert.equal(stored.bound_binding_id, "binding-one");
  const sessions = [...backend.calls.register, ...backend.calls.heartbeat.map((call) => call.payload)]
    .filter((payload) => payload.worker_id === managed.worker_id)
    .map((payload) => payload.worker_session_id);
  assert.ok(sessions.length > 0);
  assert.deepEqual(new Set(sessions), new Set([`managed.${managed.worker_id}`]));
});

test("target-observed recovery retries finalize without resending bootstrap across service-worker restart", async () => {
  const managed = record({ target, bootstrap_phase: "target_observed", attachment_evidence: attachments, observed_chatgpt_url: observedURL });
  const storage = memoryStorage({ backend_origin: backendOrigin, worker_id: "primary-worker", managed_chat_workers: { [managed.worker_id]: managed } });
  const adapter = adapterFixture(new Map([[77, target]]));
  const firstBackend = backendFixture(request(), { finalizeFailures: 10 });
  const firstBootstrapper = bootstrapperFixture(adapter);
  const first = new ExtensionRuntime(chromeFixture(storage), {
    storage, adapter, bootstrapper: firstBootstrapper, scheduler: schedulerFixture(), sessionId: "runtime-one", clientFactory: () => firstBackend,
  });
  await first.start();
  assert.equal(firstBootstrapper.calls.send, 0);
  assert.equal(storage.values.managed_chat_workers[managed.worker_id].bootstrap_phase, "target_observed");

  const secondBackend = backendFixture(request());
  const secondBootstrapper = bootstrapperFixture(adapter);
  const second = new ExtensionRuntime(chromeFixture(storage), {
    storage, adapter, bootstrapper: secondBootstrapper, scheduler: schedulerFixture(), sessionId: "runtime-two", clientFactory: () => secondBackend,
  });
  await second.start();
  assert.equal(secondBootstrapper.calls.send, 0);
  assert.equal(secondBackend.calls.finalize.length, 1);
  assert.equal(storage.values.managed_chat_workers[managed.worker_id].provision_status, "provisioned");
  assert.ok(secondBackend.calls.register.some((payload) => payload.worker_session_id === `managed.${managed.worker_id}`));
});

test("restart from send-reserved is terminal uncertain and never replays bootstrap", async () => {
  const managed = record({ bootstrap_phase: "send_reserved" });
  const storage = memoryStorage({ backend_origin: backendOrigin, worker_id: "primary-worker", managed_chat_workers: { [managed.worker_id]: managed } });
  const adapter = adapterFixture();
  const backend = backendFixture(request());
  const bootstrapper = bootstrapperFixture(adapter);
  const runtime = new ExtensionRuntime(chromeFixture(storage), {
    storage, adapter, bootstrapper, scheduler: schedulerFixture(), clientFactory: () => backend,
  });
  await runtime.start();
  assert.equal(bootstrapper.calls.send, 0);
  assert.equal(backend.calls.finalize.length, 0);
  assert.equal(backend.calls.complete[0].payload.outcome, "uncertain");
  assert.deepEqual(storage.values.managed_chat_workers, {});
});

test("healthy persistent Lead session is reused without a new tab or bootstrap", async () => {
  const leadTarget = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/lead" };
  const leadURL = "https://chatgpt.com/g/g-project/repository/c/lead";
  const managed = record({
    worker_id: "lead-worker", request_id: "old-lead", tab_id: 70, target: leadTarget,
    role: "lead", issue_number: 90, lane_key: "project:9:lead", session_policy: "persistent_project_lead",
    provision_status: "provisioned", bootstrap_phase: "provisioned", attachment_evidence: attachments,
    observed_chatgpt_url: leadURL,
  });
  const next = request({
    request_id: "new-lead", intent_id: "lead-intent", role: "lead", issue_number: 90,
    lane_key: "project:9:lead", session_policy: "persistent_project_lead", status: "claimed",
  });
  const storage = memoryStorage({ backend_origin: backendOrigin, worker_id: "primary-worker", managed_chat_workers: { [managed.worker_id]: managed } });
  const adapter = adapterFixture(new Map([[70, leadTarget]]));
  const backend = backendFixture(next);
  const bootstrapper = bootstrapperFixture(adapter, 70, leadTarget, leadURL);
  const runtime = new ExtensionRuntime(chromeFixture(storage), {
    storage, adapter, bootstrapper, scheduler: schedulerFixture(), clientFactory: () => backend,
  });
  await runtime.start();
  assert.equal(adapter.calls.create, 0);
  assert.equal(bootstrapper.calls.send, 0);
  assert.equal(storage.values.managed_chat_workers[managed.worker_id].request_id, "new-lead");
  assert.equal(storage.values.managed_chat_workers[managed.worker_id].provision_status, "surface_ready");

  await runtime.provisionCycle();
  assert.equal(backend.calls.finalize.length, 1);
  assert.equal(backend.calls.finalize[0].payload.worker_id, managed.worker_id);
  assert.equal(bootstrapper.calls.send, 0);
  assert.equal(storage.values.managed_chat_workers[managed.worker_id].provision_status, "provisioned");
});
