import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import vm from "node:vm";

async function resolver() {
  const source = await fs.readFile(new URL("../src/library-resolver.js", import.meta.url), "utf8");
  const context = { globalThis: {}, setTimeout, Promise };
  context.globalThis = context;
  vm.runInNewContext(source, context, { filename: "library-resolver.js" });
  return context.CDDMLibraryResolver;
}

function node(name) {
  return { name };
}

function clockHooks(overrides = {}) {
  let clock = 0;
  return {
    now: () => clock,
    delay: async (milliseconds) => { clock += milliseconds; },
    timeoutMs: 3,
    intervalMs: 1,
    ...overrides,
  };
}

test("resolves exact NFC-normalized filenames and preserves ordered chip evidence", async () => {
  const api = await resolver();
  const chips = [];
  let query = "";
  const options = [node("03-qa-trigger.md"), node("gpt-gh-connector-guidelines.md")];
  const evidence = await api.resolveExactAttachments(
    ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"],
    clockHooks({
      async setQuery(value) { query = value; },
      optionNodes() { return options; },
      optionName(value) { return value.name; },
      async clickOption(value) {
        assert.equal(query, `@${value.name}`);
        chips.push(node(value.name));
      },
      chipNodes() { return chips; },
      chipName(value) { return value.name; },
    }),
  );
  assert.deepEqual([...evidence], ["03-qa-trigger.md", "gpt-gh-connector-guidelines.md"]);
});

test("rejects partial, duplicate, and missing Library matches before send", async () => {
  const api = await resolver();
  for (const [options, pattern] of [
    [[node("03-qa-trigger.md backup")], /attachment_exact_match_missing/],
    [[node("03-qa-trigger.md"), node("03-qa-trigger.md")], /attachment_exact_match_ambiguous/],
    [[], /attachment_exact_match_missing/],
  ]) {
    await assert.rejects(() => api.resolveExactAttachments(["03-qa-trigger.md"], clockHooks({
      async setQuery() {},
      optionNodes() { return options; },
      optionName(value) { return value.name; },
      async clickOption() {},
      chipNodes() { return []; },
      chipName(value) { return value.name; },
    })), pattern);
  }
});

test("rejects chip order or cardinality drift after exact selection", async () => {
  const api = await resolver();
  const chips = [];
  const options = [node("a.md"), node("b.md")];
  await assert.rejects(() => api.resolveExactAttachments(["a.md", "b.md"], clockHooks({
    async setQuery() {},
    optionNodes() { return options; },
    optionName(value) { return value.name; },
    async clickOption(value) { chips.unshift(node(value.name)); },
    chipNodes() { return chips; },
    chipName(value) { return value.name; },
  })), /attachment_chip_order_mismatch/);
});
