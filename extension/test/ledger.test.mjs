import test from "node:test";
import assert from "node:assert/strict";
import { ClaimLedger, CLAIM_SENT, CLAIM_UNCERTAIN, memoryStorage } from "../src/ledger.js";

test("claim ledger reserves before side effect and rejects duplicate claims", async () => {
  const storage = memoryStorage();
  const ledger = new ClaimLedger(storage, () => 1000);
  const first = await ledger.reserve("claim-1", "command-1");
  const duplicate = await ledger.reserve("claim-1", "command-1");
  assert.equal(first.created, true);
  assert.equal(duplicate.created, false);
  await ledger.mark("claim-1", CLAIM_SENT, "prompt_send_confirmed");
  assert.equal((await ledger.get("claim-1")).state, CLAIM_SENT);
});

test("reserved claims become uncertain after runtime restart and are never pruned while unacknowledged", async () => {
  const storage = memoryStorage();
  const ledger = new ClaimLedger(storage, () => 2000, 0);
  await ledger.reserve("claim-1", "command-1");
  const recovered = await ledger.recoverReserved();
  assert.equal(recovered["claim-1"].state, CLAIM_UNCERTAIN);
  await ledger.prune();
  assert.ok((await ledger.get("claim-1")));
});
