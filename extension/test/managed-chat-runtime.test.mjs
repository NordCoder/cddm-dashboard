import test from "node:test";
import assert from "node:assert/strict";
import { memoryStorage } from "../src/ledger.js";
import { ExtensionRuntime } from "../src/service-worker.js";

const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/fresh-qa" };

test("created chat receives its own persistent worker identity and exact target binding", async () => {
  const storage = memoryStorage({ backend_origin: "http://localhost:1338" });
  const calls = { register: [], heartbeat: [], bind: [] };
  const exactAdapter = {
    async currentTarget() { return target; },
    async insertPrompt() {},
    async sendPrompt() { return { sent: true }; },
  };
  const adapter = {
    managed: new Set(),
    async currentTarget() { return null; },
    reserveManagedTab(tabID) { this.managed.add(tabID); },
    isManagedTab(tabID) { return this.managed.has(tabID); },
    exactTab(tabID, expected) {
      assert.equal(tabID, 77);
      assert.deepEqual(expected, target);
      return exactAdapter;
    },
    async createConversation(prompt) {
      assert.match(prompt, /@03-qa-trigger\.md/);
      this.managed.add(77);
      return { target, tabId: 77 };
    },
  };
  const backend = {
    async register(payload) { calls.register.push(payload); return { state: "live" }; },
    async heartbeat(_workerID, payload) { calls.heartbeat.push(payload); return { state: "live" }; },
    async claimNext() { return null; },
    async complete() {},
    async bindCurrent(projectID, issueNumber, input) {
      calls.bind.push({ projectID, issueNumber, input });
      return { binding_id: "binding-qa", binding_version: 1, worker_id: input.worker_id, target: input.target };
    },
  };
  const chrome = {
    storage: { local: storage },
    permissions: { async contains() { return true; } },
    alarms: { async create() {} },
  };
  const scheduler = { active: true, start() {}, async alarm() {} };
  const runtime = new ExtensionRuntime(chrome, { storage, adapter, sessionId: "runtime-session", scheduler, clientFactory: () => backend });
  await runtime.start();

  const result = await runtime.handleExternalMessage({
    type: "create-worker-chat",
    request_id: "qa-chat-request",
    project_id: 9,
    issue_number: 140,
    role: "qa",
    expected_lane_key: "nordcoder/misak-website#140:qa",
    bootstrap_prompt: "@03-qa-trigger.md\n@gpt-gh-connector-guidelines.md\n\nWait for the command.",
  }, { url: "http://localhost:1338/projects/9/work-units/140" });

  assert.equal(result.ok, true);
  assert.notEqual(result.worker_id, runtime.workerId);
  assert.equal(calls.bind.length, 1);
  assert.equal(calls.bind[0].input.worker_id, result.worker_id);
  assert.deepEqual(calls.bind[0].input.target, target);
  assert.ok(calls.register.some((payload) => payload.worker_id === result.worker_id && payload.observation.target.path === "/c/fresh-qa"));
  assert.equal(adapter.managed.has(77), true);

  const stored = storage.values.managed_chat_workers;
  assert.equal(stored[result.worker_id].tab_id, 77);
  assert.deepEqual(stored[result.worker_id].target, target);

  await runtime.heartbeatCycle();
  assert.ok(calls.heartbeat.some((payload) => payload.worker_id === result.worker_id));
});
