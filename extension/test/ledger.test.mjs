import test from "node:test";
import assert from "node:assert/strict";
import { ClaimLedger, CLAIM_SENT, CLAIM_UNCERTAIN, memoryStorage } from "../src/ledger.js";

const origin = "http://localhost:8080";

test("claim ledger reserves before side effect and rejects duplicate claims", async () => {
  const ledger = new ClaimLedger(memoryStorage(), () => 1000);
  const first = await ledger.reserve("claim-1", "command-1", origin);
  const duplicate = await ledger.reserve("claim-1", "command-1", origin);
  assert.equal(first.created, true);
  assert.equal(duplicate.created, false);
  await ledger.mark("claim-1", CLAIM_SENT, "prompt_send_confirmed");
  const entry = await ledger.get("claim-1");
  assert.equal(entry.state, CLAIM_SENT);
  assert.equal(entry.backend_origin, origin);
});

test("serialized read-modify-write preserves concurrent claims and acknowledgements", async () => {
  const storage = memoryStorage();
  const ledger = new ClaimLedger(storage, (() => { let now = 0; return () => ++now; })());
  await Promise.all([
    ledger.reserve("claim-1", "command-1", origin),
    ledger.reserve("claim-2", "command-2", origin),
  ]);
  await Promise.all([
    ledger.mark("claim-1", CLAIM_SENT, "sent"),
    ledger.mark("claim-2", CLAIM_UNCERTAIN, "unknown"),
    ledger.acknowledge("claim-1", true),
  ]);
  const entries = await ledger.all();
  assert.deepEqual(Object.keys(entries).sort(), ["claim-1", "claim-2"]);
  assert.equal(entries["claim-1"].acknowledged, true);
  assert.equal(entries["claim-2"].state, CLAIM_UNCERTAIN);
});

test("reserved claims become uncertain after runtime restart and stay bound to their backend", async () => {
  const ledger = new ClaimLedger(memoryStorage(), () => 2000, 0);
  await ledger.reserve("claim-1", "command-1", origin);
  const recovered = await ledger.recoverReserved();
  assert.equal(recovered["claim-1"].state, CLAIM_UNCERTAIN);
  assert.equal(recovered["claim-1"].backend_origin, origin);
  await ledger.prune();
  assert.ok(await ledger.get("claim-1"));
});

test("corrupt root storage fails closed instead of being overwritten", async () => {
  const ledger = new ClaimLedger(memoryStorage({ claim_ledger: [] }));
  await assert.rejects(() => ledger.reserve("claim-1", "command-1", origin), /claim_ledger_corrupt/);
});

test("legacy entries without backend authority become uncertain and cannot be acknowledged cross-origin", async () => {
  const storage = memoryStorage({ claim_ledger: {
    "claim-1": { claim_id: "claim-1", command_id: "command-1", state: CLAIM_SENT, created_at: 1, updated_at: 1, acknowledged: false },
  } });
  const entry = await new ClaimLedger(storage, () => 2).get("claim-1");
  assert.equal(entry.state, CLAIM_UNCERTAIN);
  assert.equal(entry.backend_origin, "");
  assert.equal(entry.diagnostic, "claim_ledger_corrupt");
});
