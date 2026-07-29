import test from "node:test";
import assert from "node:assert/strict";
import { ManagedSessionBootstrapper } from "../src/session-bootstrap.js";

const projectURL = "https://chatgpt.com/g/g-project/repository/project";
const projectConversation = "https://chatgpt.com/g/g-project/repository/c/session-one";
const attachments = ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"];

function request(overrides = {}) {
  return {
    attachments,
    bootstrap_text: "Wait for a Workflow Command.",
    chatgpt_project_url: projectURL,
    ...overrides,
  };
}

test("sends exact attachment profile once and observes the exact Project conversation", async () => {
  let url = projectURL;
  let sends = 0;
  const chrome = { tabs: {
    async get() { return { id: 77, url }; },
    async sendMessage(_tabID, message) {
      sends += 1;
      assert.equal(message.type, "bootstrap-session");
      assert.deepEqual(message.attachments, attachments);
      url = projectConversation;
      return { ok: true, attachment_evidence: attachments };
    },
  } };
  const bootstrapper = new ManagedSessionBootstrapper(chrome, { targetTimeout: 10, targetPoll: 1, delay: async () => {} });
  const sent = await bootstrapper.send(77, request());
  const observed = await bootstrapper.waitForConversation(77, projectURL);
  assert.equal(sends, 1);
  assert.deepEqual(sent.attachmentEvidence, attachments);
  assert.deepEqual(observed.target, { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/session-one" });
  assert.equal(observed.observedURL, projectConversation);
});

test("rejects attachment evidence drift as an ambiguous post-send outcome", async () => {
  const chrome = { tabs: {
    async get() { return { id: 77, url: projectURL }; },
    async sendMessage() { return { ok: true, attachment_evidence: [attachments[0]] }; },
  } };
  const bootstrapper = new ManagedSessionBootstrapper(chrome);
  await assert.rejects(() => bootstrapper.send(77, request()), /bootstrap_attachment_evidence_mismatch/);
});

test("fails closed when the created conversation leaves the configured Project scope", async () => {
  const chrome = { tabs: {
    async get() { return { id: 77, url: "https://chatgpt.com/c/outside" }; },
    async sendMessage() { throw new Error("not used"); },
  } };
  const bootstrapper = new ManagedSessionBootstrapper(chrome, { targetTimeout: 10, targetPoll: 1, delay: async () => {} });
  await assert.rejects(() => bootstrapper.waitForConversation(77, projectURL), /conversation_outside_project_after_bootstrap/);
});
