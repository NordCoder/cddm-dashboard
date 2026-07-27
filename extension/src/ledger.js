import { isOpaqueIdentifier, normalizeBackendOrigin } from "./protocol.js";

export const CLAIM_RESERVED = "reserved";
export const CLAIM_SENT = "sent";
export const CLAIM_FAILED_PRE_SEND = "failed_pre_send";
export const CLAIM_UNCERTAIN = "uncertain";

const LEDGER_KEY = "claim_ledger";
const STATES = new Set([CLAIM_RESERVED, CLAIM_SENT, CLAIM_FAILED_PRE_SEND, CLAIM_UNCERTAIN]);
const TERMINAL = new Set([CLAIM_SENT, CLAIM_FAILED_PRE_SEND, CLAIM_UNCERTAIN]);

function cloneEntry(entry) { return entry ? { ...entry } : entry; }
function normalizedOrigin(value) {
  try { return normalizeBackendOrigin(value); } catch { return ""; }
}

function normalizeEntry(claimId, value, now) {
  const valid = value && typeof value === "object" && !Array.isArray(value)
    && value.claim_id === claimId
    && isOpaqueIdentifier(value.command_id)
    && STATES.has(value.state)
    && normalizedOrigin(value.backend_origin);
  if (!valid) {
    return {
      claim_id: claimId,
      command_id: value && typeof value === "object" && isOpaqueIdentifier(value.command_id) ? value.command_id : "",
      backend_origin: value && typeof value === "object" ? normalizedOrigin(value.backend_origin) : "",
      state: CLAIM_UNCERTAIN,
      diagnostic: "claim_ledger_corrupt",
      created_at: Number.isFinite(value?.created_at) ? value.created_at : now,
      updated_at: now,
      acknowledged: false,
    };
  }
  return {
    claim_id: claimId,
    command_id: value.command_id,
    backend_origin: normalizedOrigin(value.backend_origin),
    state: value.state,
    diagnostic: typeof value.diagnostic === "string" ? value.diagnostic.slice(0, 80) : "",
    ack_diagnostic: typeof value.ack_diagnostic === "string" ? value.ack_diagnostic.slice(0, 80) : "",
    created_at: Number.isFinite(value.created_at) ? value.created_at : now,
    updated_at: Number.isFinite(value.updated_at) ? value.updated_at : now,
    acknowledged: Boolean(value.acknowledged),
  };
}

export class ClaimLedger {
  constructor(storage, now = () => Date.now(), maxEntries = 200) {
    this.storage = storage;
    this.now = now;
    this.maxEntries = Math.max(0, maxEntries);
    this.tail = Promise.resolve();
  }

  exclusive(task) {
    const run = this.tail.then(task, task);
    this.tail = run.then(() => undefined, () => undefined);
    return run;
  }

  async read() {
    const raw = (await this.storage.get(LEDGER_KEY))[LEDGER_KEY] ?? {};
    if (typeof raw !== "object" || raw === null || Array.isArray(raw)) throw new Error("claim_ledger_corrupt");
    const entries = Object.create(null);
    const now = this.now();
    for (const [claimId, value] of Object.entries(raw)) {
      if (!isOpaqueIdentifier(claimId)) throw new Error("claim_ledger_corrupt");
      entries[claimId] = normalizeEntry(claimId, value, now);
    }
    return entries;
  }

  async save(entries) {
    const serialized = {};
    for (const [claimId, entry] of Object.entries(entries)) serialized[claimId] = cloneEntry(entry);
    await this.storage.set({ [LEDGER_KEY]: serialized });
  }

  all() { return this.exclusive(async () => this.read()); }

  get(claimId) {
    return this.exclusive(async () => cloneEntry((await this.read())[claimId] ?? null));
  }

  reserve(claimId, commandId, backendOrigin) {
    return this.exclusive(async () => {
      if (!isOpaqueIdentifier(claimId) || !isOpaqueIdentifier(commandId)) throw new Error("claim_identity_invalid");
      const origin = normalizedOrigin(backendOrigin);
      if (!origin) throw new Error("claim_origin_invalid");
      const entries = await this.read();
      if (Object.prototype.hasOwnProperty.call(entries, claimId)) return { created: false, entry: cloneEntry(entries[claimId]) };
      const now = this.now();
      const entry = { claim_id: claimId, command_id: commandId, backend_origin: origin, state: CLAIM_RESERVED, created_at: now, updated_at: now, acknowledged: false };
      entries[claimId] = entry;
      await this.save(entries);
      return { created: true, entry: cloneEntry(entry) };
    });
  }

  mark(claimId, state, diagnostic = "") {
    return this.exclusive(async () => {
      if (!TERMINAL.has(state)) throw new Error("invalid_claim_state");
      const entries = await this.read();
      const entry = entries[claimId];
      if (!entry) throw new Error("claim_ledger_missing");
      entry.state = state;
      entry.diagnostic = String(diagnostic).slice(0, 80);
      entry.updated_at = this.now();
      entries[claimId] = entry;
      await this.save(entries);
      return cloneEntry(entry);
    });
  }

  acknowledge(claimId, acknowledged = true, ackDiagnostic = "") {
    return this.exclusive(async () => {
      const entries = await this.read();
      const entry = entries[claimId];
      if (!entry) return null;
      entry.acknowledged = Boolean(acknowledged);
      if (ackDiagnostic) entry.ack_diagnostic = String(ackDiagnostic).slice(0, 80);
      entry.updated_at = this.now();
      entries[claimId] = entry;
      await this.save(entries);
      return cloneEntry(entry);
    });
  }

  recoverReserved() {
    return this.exclusive(async () => {
      const entries = await this.read();
      let changed = false;
      for (const entry of Object.values(entries)) {
        if (entry.state === CLAIM_RESERVED) {
          entry.state = CLAIM_UNCERTAIN;
          entry.diagnostic = "runtime_restart_after_claim";
          entry.updated_at = this.now();
          changed = true;
        }
      }
      if (changed) await this.save(entries);
      return entries;
    });
  }

  prune() {
    return this.exclusive(async () => {
      const entries = await this.read();
      const removable = Object.values(entries)
        .filter((entry) => TERMINAL.has(entry.state) && entry.acknowledged)
        .sort((left, right) => left.updated_at - right.updated_at);
      const excess = Math.max(0, Object.keys(entries).length - this.maxEntries);
      const selected = removable.slice(0, excess);
      for (const entry of selected) delete entries[entry.claim_id];
      if (selected.length > 0) await this.save(entries);
    });
  }
}

function cloneValue(value) {
  if (value === undefined) return undefined;
  if (globalThis.structuredClone) return globalThis.structuredClone(value);
  return JSON.parse(JSON.stringify(value));
}

export function memoryStorage(initial = {}) {
  const values = cloneValue(initial) ?? {};
  return {
    async get(key) { return { [key]: cloneValue(values[key]) }; },
    async set(next) { for (const [key, value] of Object.entries(next)) values[key] = cloneValue(value); },
    async remove(key) { delete values[key]; },
    values
  };
}
