import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import vm from "node:vm";

class FakeInput {
  constructor(value = "") { this._value = value; this.disabled = false; this.readOnly = false; }
  get value() { return this._value; }
  set value(value) { this._value = value; }
  focus() {}
  dispatchEvent() {}
  getBoundingClientRect() { return { width: 300, height: 40 }; }
}
class FakeTextArea extends FakeInput {}

async function fixture(onClick) {
  let listener;
  const composer = new FakeTextArea("exact prompt");
  const button = {
    disabled: false,
    getAttribute() { return null; },
    getBoundingClientRect() { return { width: 30, height: 30 }; },
    click() { onClick(composer); },
  };
  const document = {
    querySelectorAll(selector) {
      if (selector === "#prompt-textarea") return [composer];
      if (selector === "button[data-testid='send-button']") return [button];
      return [];
    },
    createRange() { throw new Error("not used"); },
  };
  let source = await fs.readFile(new URL("../src/content.js", import.meta.url), "utf8");
  source = source.replace("const SEND_ACK_TIMEOUT_MS = 1500;", "const SEND_ACK_TIMEOUT_MS = 25;")
    .replace("const SEND_ACK_INTERVAL_MS = 50;", "const SEND_ACK_INTERVAL_MS = 1;");
  const context = {
    URL, location: { href: "https://chatgpt.com/c/one" }, document,
    chrome: { runtime: { onMessage: { addListener(value) { listener = value; } } } },
    HTMLTextAreaElement: FakeTextArea, HTMLInputElement: FakeInput,
    InputEvent: class {}, getComputedStyle() { return { display: "block", visibility: "visible" }; },
    setTimeout, clearTimeout, Date, Promise,
  };
  vm.runInNewContext(source, context, { filename: "content.js" });
  async function message(payload) {
    return await new Promise((resolve) => {
      const asyncResponse = listener(payload, {}, resolve);
      if (!asyncResponse) throw new Error("expected async response");
    });
  }
  return { composer, message };
}

test("send is delivered only after the composer acknowledges submission by clearing", async () => {
  const f = await fixture((composer) => setTimeout(() => { composer.value = ""; }, 2));
  const result = await f.message({ type: "send-prompt", prompt: "exact prompt", expected_target: { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/one" } });
  assert.equal(result.ok, true);
});

test("a click with an unchanged composer is uncertain, not delivered", async () => {
  const f = await fixture(() => {});
  const result = await f.message({ type: "send-prompt", prompt: "exact prompt", expected_target: { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/one" } });
  assert.equal(result.ok, false);
  assert.equal(result.safe_no_send, false);
  assert.equal(result.reason, "send_outcome_unknown");
});

test("an unrelated composer edit after click is not accepted as submit evidence", async () => {
  const f = await fixture((composer) => setTimeout(() => { composer.value = "user edit"; }, 2));
  const result = await f.message({ type: "send-prompt", prompt: "exact prompt", expected_target: { kind: "chatgpt_conversation", origin: "https://chatgpt.com", path: "/c/one" } });
  assert.equal(result.ok, false);
  assert.equal(result.safe_no_send, false);
});

test("DOM adapter contains no broad generic contenteditable or submit fallback", async () => {
  const source = await fs.readFile(new URL("../src/content.js", import.meta.url), "utf8");
  assert.equal(source.includes('div[contenteditable=\'true\']'), false);
  assert.equal(source.includes('form button[type=\'submit\']'), false);
  assert.equal(source.includes('button[data-testid*=\'send-button\']'), false);
});
