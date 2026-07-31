import test from "node:test";
import assert from "node:assert/strict";
import { ClaimLedger, CLAIM_UNCERTAIN, memoryStorage } from "../src/ledger.js";
import { ExtensionRuntime } from "../src/service-worker.js";

const backendOrigin = "http://localhost:1338";
const projectURL = "https://chatgpt.com/g/g-project/repository/project";
const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/recovered" };
const observedURL = "https://chatgpt.com/g/g-project/repository/c/recovered";
const attachments = ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"];

test("assignment delivery restart marks the reserved claim uncertain and never retargets it", async () => {
  const storage = memoryStorage();
  const first = new ClaimLedger(storage, () => 1000);
  await first.reserve("claim-autopilot", "command-original", backendOrigin);

  const restarted = new ClaimLedger(storage, () => 2000);
  await restarted.recoverReserved();
  const duplicate = await restarted.reserve("claim-autopilot", "command-retargeted", backendOrigin);

  assert.equal(duplicate.created, false);
  assert.equal(duplicate.entry.state, CLAIM_UNCERTAIN);
  assert.equal(duplicate.entry.command_id, "command-original");
  assert.equal(duplicate.entry.diagnostic, "runtime_restart_after_claim");
});

test("sent bootstrap phase resumes observation and finalization without replaying the send", async () => {
  const managed = {
    worker_id: "managed-recovery-worker",
    request_id: "provision-recovery",
    claim_owner: "primary-worker",
    claim_token: "claim-token",
    tab_id: 77,
    target: null,
    project_id: 9,
    intent_id: "intent-recovery",
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
    bootstrap_phase: "sent",
    attachment_evidence: attachments,
    observed_chatgpt_url: "",
    created_at: 1,
  };
  const storage = memoryStorage({
    backend_origin: backendOrigin,
    worker_id: "primary-worker",
    managed_chat_workers: { [managed.worker_id]: managed },
  });
  const targets = new Map([[77, null]]);
  const adapter = {
    reserveManagedTab() {},
    async currentTarget() { return null; },
    async managedObservation(tabID) { return { available: true, target: targets.get(tabID) ?? null }; },
    exactTab(tabID) { return { async currentTarget() { return targets.get(tabID) ?? null; } }; },
    async closeManagedTab() { throw new Error("recovered tab must not be closed"); },
  };
  const bootstrapCalls = { send: 0, wait: 0 };
  const bootstrapper = {
    async send() { bootstrapCalls.send += 1; throw new Error("bootstrap send must not replay"); },
    async waitForConversation() {
      bootstrapCalls.wait += 1;
      targets.set(77, target);
      return { target, observedURL };
    },
  };
  let requestAvailable = true;
  const calls = { finalize: 0, claimNext: 0 };
  const backend = {
    async register() { return { state: "live" }; },
    async heartbeat() { return { state: "live" }; },
    async claimNext() { calls.claimNext += 1; return null; },
    async complete() {},
    async claimProvision() {
      if (!requestAvailable) return null;
      return {
        request_id: managed.request_id, project_id: managed.project_id, intent_id: managed.intent_id,
        lane_key: managed.lane_key, issue_number: managed.issue_number, role: managed.role,
        expected_head: managed.expected_head, attachments, bootstrap_text: managed.bootstrap_text,
        session_policy: managed.session_policy, chatgpt_project_url: projectURL,
        status: "surface_ready", claim_owner: managed.claim_owner, claim_token: managed.claim_token,
      };
    },
    async completeProvision() { throw new Error("surface completion is already durable"); },
    async finalizeProvision(requestID, payload) {
      calls.finalize += 1;
      requestAvailable = false;
      return {
        request_id: requestID, project_id: managed.project_id, intent_id: managed.intent_id,
        lane_key: managed.lane_key, issue_number: managed.issue_number, role: managed.role,
        expected_head: managed.expected_head, attachments, bootstrap_text: managed.bootstrap_text,
        session_policy: managed.session_policy, chatgpt_project_url: projectURL,
        status: "provisioned", claim_owner: payload.claim_owner, claim_token: payload.claim_token,
        target: payload.target, observed_chatgpt_url: payload.observed_chatgpt_url,
        attachment_evidence: payload.attachment_evidence, bound_binding_id: "binding-recovered",
        bound_binding_version: 7,
      };
    },
  };
  const chrome = {
    storage: { local: storage },
    permissions: { async contains() { return true; } },
    alarms: { async create() {} },
    tabs: { onActivated: { addListener() {} } },
  };
  const scheduler = { active: true, start() {}, async alarm() {} };
  const runtime = new ExtensionRuntime(chrome, {
    storage, adapter, bootstrapper, scheduler, sessionId: "runtime-restarted", clientFactory: () => backend,
  });

  await runtime.start();

  assert.equal(bootstrapCalls.send, 0);
  assert.equal(bootstrapCalls.wait, 1);
  assert.equal(calls.finalize, 1);
  const stored = storage.values.managed_chat_workers[managed.worker_id];
  assert.equal(stored.provision_status, "provisioned");
  assert.equal(stored.bootstrap_phase, "provisioned");
  assert.deepEqual(stored.target, target);
  assert.equal(stored.bound_binding_id, "binding-recovered");
});
