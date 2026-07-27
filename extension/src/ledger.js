export const CLAIM_RESERVED = "reserved";
export const CLAIM_SENT = "sent";
export const CLAIM_FAILED_PRE_SEND = "failed_pre_send";
export const CLAIM_UNCERTAIN = "uncertain";

const LEDGER_KEY = "claim_ledger";
const TERMINAL = new Set([CLAIM_SENT, CLAIM_FAILED_PRE_SEND, CLAIM_UNCERTAIN]);

export class ClaimLedger {
  constructor(storage, now = () => Date.now(), maxEntries = 200) {
    this.storage = storage;
    this.now = now;
    this.maxEntries = maxEntries;
  }

  async all() { return (await this.storage.get(LEDGER_KEY))[LEDGER_KEY] ?? {}; }

  async get(claimId) { return (await this.all())[claimId] ?? null; }

  async reserve(claimId, commandId) {
    const entries = await this.all();
    if (entries[claimId]) return { created: false, entry: entries[claimId] };
    const now = this.now();
    const entry = { claim_id: claimId, command_id: commandId, state: CLAIM_RESERVED, created_at: now, updated_at: now, acknowledged: false };
    entries[claimId] = entry;
    await this.save(entries);
    return { created: true, entry };
  }

  async mark(claimId, state, diagnostic = "") {
    if (!TERMINAL.has(state)) throw new Error("invalid_claim_state");
    const entries = await this.all();
    const entry = entries[claimId];
    if (!entry) throw new Error("claim_ledger_missing");
    entry.state = state;
    entry.diagnostic = String(diagnostic).slice(0, 80);
    entry.updated_at = this.now();
    entries[claimId] = entry;
    await this.save(entries);
    return entry;
  }

  async acknowledge(claimId, acknowledged = true) {
    const entries = await this.all();
    const entry = entries[claimId];
    if (!entry) return null;
    entry.acknowledged = Boolean(acknowledged);
    entry.updated_at = this.now();
    entries[claimId] = entry;
    await this.save(entries);
    return entry;
  }

  async recoverReserved() {
    const entries = await this.all();
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
  }

  async prune() {
    const entries = await this.all();
    const removable = Object.values(entries)
      .filter((entry) => TERMINAL.has(entry.state) && entry.acknowledged)
      .sort((left, right) => left.updated_at - right.updated_at);
    const excess = Math.max(0, Object.keys(entries).length - this.maxEntries);
    for (const entry of removable.slice(0, excess)) delete entries[entry.claim_id];
    await this.save(entries);
  }

  async save(entries) { await this.storage.set({ [LEDGER_KEY]: entries }); }
}

export function memoryStorage(initial = {}) {
  const values = { ...initial };
  return {
    async get(key) { return { [key]: values[key] }; },
    async set(next) { Object.assign(values, next); },
    values
  };
}
