import test from "node:test";
import assert from "node:assert/strict";
import { ChromeTargetAdapter } from "../src/adapter.js";

const chatOne = { id: 11, url: "https://chatgpt.com/c/one" };
const dashboard = { id: 22, url: "http://localhost:8080/projects/1/work-units/20/plans" };
const targetOne = { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/one" };

function fixture() {
  const tabs = new Map([[chatOne.id, { ...chatOne }], [dashboard.id, { ...dashboard }]]);
  let activeId = chatOne.id;
  const session = {};
  const messages = [];
  const chrome = {
    storage: { session: {
      async get(key) { return { [key]: session[key] }; },
      async set(values) { Object.assign(session, values); },
      async remove(key) { delete session[key]; }
    } },
    tabs: {
      async query() { const tab = tabs.get(activeId); return tab ? [{ ...tab }] : []; },
      async get(id) { const tab = tabs.get(id); if (!tab) throw new Error("missing tab"); return { ...tab }; },
      async sendMessage(id, message) { messages.push({ id, message }); return { ok: true }; }
    }
  };
  return {
    adapter: new ChromeTargetAdapter(chrome), tabs, messages,
    activate(id) { activeId = id; },
    close(id) { tabs.delete(id); },
    navigate(id, url) { tabs.get(id).url = url; }
  };
}

test("tracked ChatGPT target remains current while the dashboard tab is active", async () => {
  const f = fixture();
  assert.deepEqual(await f.adapter.currentTarget(), targetOne);
  f.activate(dashboard.id);
  assert.deepEqual(await f.adapter.currentTarget(), targetOne);
});

test("activation event captures the exact ChatGPT tab even after focus already returned to dashboard", async () => {
  const f = fixture();
  f.activate(dashboard.id);
  assert.deepEqual(await f.adapter.observeActivatedTab(chatOne.id), targetOne);
  assert.deepEqual(await f.adapter.currentTarget(), targetOne);
  assert.equal(await f.adapter.observeActivatedTab(dashboard.id), null);
  assert.deepEqual(await f.adapter.currentTarget(), targetOne);
});

test("tracked target fails closed when its exact tab closes or navigates away", async () => {
  const closed = fixture();
  await closed.adapter.currentTarget();
  closed.activate(dashboard.id);
  closed.close(chatOne.id);
  assert.equal(await closed.adapter.currentTarget(), null);

  const navigated = fixture();
  await navigated.adapter.currentTarget();
  navigated.activate(dashboard.id);
  navigated.navigate(chatOne.id, "https://chatgpt.com/");
  assert.equal(await navigated.adapter.currentTarget(), null);
});

test("DOM execution addresses only the tracked exact ChatGPT tab and rechecks it before send", async () => {
  const f = fixture();
  await f.adapter.currentTarget();
  f.activate(dashboard.id);
  await f.adapter.insertPrompt("exact prompt", targetOne);
  assert.deepEqual(await f.adapter.sendPrompt(targetOne, "exact prompt"), { sent: true });
  assert.deepEqual(f.messages.map(({ id, message }) => [id, message.type]), [[chatOne.id, "insert-prompt"], [chatOne.id, "send-prompt"]]);

  f.navigate(chatOne.id, "https://chatgpt.com/c/other");
  await assert.rejects(() => f.adapter.sendPrompt(targetOne, "exact prompt"), /target_changed_before_send|active_target_unavailable/);
  assert.equal(f.messages.length, 2);
});

test("activating a different supported ChatGPT conversation replaces the tracked target", async () => {
  const f = fixture();
  const chatTwo = { id: 33, url: "https://chatgpt.com/c/two" };
  f.tabs.set(chatTwo.id, chatTwo);
  await f.adapter.currentTarget();
  f.activate(chatTwo.id);
  assert.deepEqual(await f.adapter.currentTarget(), { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/two" });
  f.activate(dashboard.id);
  assert.deepEqual(await f.adapter.currentTarget(), { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/two" });
});
