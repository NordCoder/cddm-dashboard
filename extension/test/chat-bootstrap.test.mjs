import test from "node:test";
import assert from "node:assert/strict";
import { ChatBootstrapCoordinator } from "../src/chat-bootstrap.js";
import { memoryStorage } from "../src/ledger.js";

const target = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/fresh" };
const message = {
  type: "create-worker-chat",
  request_id: "chat-request-1",
  project_id: 7,
  issue_number: 140,
  role: "implementor",
  expected_lane_key: "nordcoder/misak-website#140:implementor",
  chatgpt_project_url: "https://chatgpt.com/g/g-p-repository/project",
  bootstrap_prompt: "@02-implementor-trigger.md\n@gpt-gh-connector-guidelines.md\n\nWait for the command.",
};
const sender = { url: "http://localhost:1338/projects/7/work-units/140" };

test("bootstrap provisions one exact project-scoped chat worker and binds it to the asserted lane", async () => {
  const storage = memoryStorage();
  const coordinator = new ChatBootstrapCoordinator(storage, () => 1000);
  const calls = { provision: 0, bind: [] };
  const runtime = {
    async provisionConversation(request) {
      calls.provision += 1;
      assert.equal(request.role, "implementor");
      assert.equal(request.chatGPTProjectUrl, "https://chatgpt.com/g/g-p-repository/project");
      return { workerId: "managed-worker-1", target };
    },
    backend: {
      async bindCurrent(projectID, issueNumber, input) {
        calls.bind.push({ projectID, issueNumber, input });
        return { binding_id: "binding-1", worker_id: input.worker_id, target: input.target };
      },
    },
  };

  const result = await coordinator.execute(message, sender, runtime);
  assert.equal(result.ok, true);
  assert.equal(result.worker_id, "managed-worker-1");
  assert.equal(calls.provision, 1);
  assert.deepEqual(calls.bind, [{
    projectID: 7,
    issueNumber: 140,
    input: {
      expected_lane_key: "nordcoder/misak-website#140:implementor",
      worker_id: "managed-worker-1",
      target,
    },
  }]);
  assert.equal(storage.values.chat_bootstrap_jobs[message.request_id].chatgpt_project_url, message.chatgpt_project_url);
});

test("completed bootstrap request is idempotent and never creates a second chat", async () => {
  const storage = memoryStorage();
  const coordinator = new ChatBootstrapCoordinator(storage);
  let provisionCalls = 0;
  const runtime = {
    async provisionConversation() { provisionCalls += 1; return { workerId: "managed-worker-1", target }; },
    backend: { async bindCurrent() { return { binding_id: "binding-1" }; } },
  };
  const first = await coordinator.execute(message, sender, runtime);
  const second = await coordinator.execute(message, sender, runtime);
  assert.equal(first.ok, true);
  assert.equal(second.ok, true);
  assert.equal(second.reused, true);
  assert.equal(provisionCalls, 1);
});

test("bootstrap rejects non-Dashboard external origins before creating a chat", async () => {
  const coordinator = new ChatBootstrapCoordinator(memoryStorage());
  await assert.rejects(
    () => coordinator.execute(message, { url: "https://example.com/" }, { provisionConversation() {}, backend: {} }),
    /bootstrap_sender_not_allowed/,
  );
});

test("bootstrap rejects invalid or conversation-shaped ChatGPT project URLs", async () => {
  const coordinator = new ChatBootstrapCoordinator(memoryStorage());
  await assert.rejects(
    () => coordinator.execute({ ...message, request_id: "bad-project", chatgpt_project_url: "https://chatgpt.com/c/existing" }, sender, { provisionConversation() {}, backend: {} }),
    /chatgpt_project_url_is_conversation/,
  );
  await assert.rejects(
    () => coordinator.execute({ ...message, request_id: "evil-project", chatgpt_project_url: "https://example.com/project" }, sender, { provisionConversation() {}, backend: {} }),
    /chatgpt_project_url_invalid/,
  );
});

test("failed request is consumed so an ambiguous retry cannot create a duplicate chat", async () => {
  const coordinator = new ChatBootstrapCoordinator(memoryStorage());
  let calls = 0;
  const runtime = {
    async provisionConversation() { calls += 1; throw new Error("chat_conversation_url_unobserved"); },
    backend: { async bindCurrent() {} },
  };
  const first = await coordinator.execute(message, sender, runtime);
  const second = await coordinator.execute(message, sender, runtime);
  assert.equal(first.ok, false);
  assert.equal(first.reason, "chat_conversation_url_unobserved");
  assert.equal(second.ok, false);
  assert.equal(second.reason, "bootstrap_request_already_consumed");
  assert.equal(calls, 1);
});
