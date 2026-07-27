import { BackendClient, BackendHTTPError } from "./api.js";
import { ChromeTargetAdapter } from "./adapter.js";
import { ExecutionCoordinator } from "./executor.js";
import { ClaimLedger } from "./ledger.js";
import {
  BACKEND_ORIGIN_KEY, WORKER_ID_KEY, backendPermissionOrigin, normalizeBackendOrigin,
  randomId, safeDiagnostic
} from "./protocol.js";

const SESSION_ID = randomId();
const HEARTBEAT_ALARM = "cddm-heartbeat";
const POLL_ALARM = "cddm-claim-poll";
const STATUS_KEY = "extension_status";

export class ExtensionRuntime {
  constructor(chromeApi, dependencies = {}) {
    this.chrome = chromeApi;
    this.storage = dependencies.storage || chromeApi.storage.local;
    this.adapter = dependencies.adapter || new ChromeTargetAdapter(chromeApi);
    this.ledger = dependencies.ledger || new ClaimLedger(this.storage);
    this.sessionId = dependencies.sessionId || SESSION_ID;
    this.workerId = null;
    this.backend = null;
    this.coordinator = null;
    this.currentTarget = null;
    this.conflict = false;
    this.pollInFlight = false;
    this.registered = false;
  }

  async start() {
    this.workerId = await this.loadWorkerId();
    await this.ledger.recoverReserved();
    await this.ledger.prune();
    await this.tick();
    if (this.chrome.alarms) {
      await this.chrome.alarms.create(HEARTBEAT_ALARM, { periodInMinutes: 0.15 });
      await this.chrome.alarms.create(POLL_ALARM, { periodInMinutes: 0.1 });
    }
  }

  async loadWorkerId() {
    const found = (await this.storage.get(WORKER_ID_KEY))[WORKER_ID_KEY];
    if (found) return found;
    const generated = randomId();
    await this.storage.set({ [WORKER_ID_KEY]: generated });
    return generated;
  }

  async config() {
    const origin = (await this.storage.get(BACKEND_ORIGIN_KEY))[BACKEND_ORIGIN_KEY];
    if (!origin) return null;
    try { return normalizeBackendOrigin(origin); } catch { return null; }
  }

  async enabledOrigin() {
    const origin = await this.config();
    if (!origin) return null;
    const granted = await this.chrome.permissions.contains({ origins: [backendPermissionOrigin(origin)] });
    return granted ? origin : null;
  }

  async tick() {
    const origin = await this.enabledOrigin();
    if (!origin) { this.currentTarget = null; await this.status("disabled_backend"); return; }
    this.backend = new BackendClient(origin);
    this.coordinator = new ExecutionCoordinator({ ledger: this.ledger, backend: this.backend, adapter: this.adapter });
    const target = await this.adapter.currentTarget();
    this.currentTarget = target;
    try {
      const payload = { worker_id: this.workerId, worker_session_id: this.sessionId, protocol_version: "m6-c3", capabilities: ["chatgpt_conversation", "exact_prompt_send"], observation: { target } };
      const result = this.registered ? await this.backend.heartbeat(this.workerId, payload) : await this.backend.register(payload);
      this.registered = true;
      this.conflict = result?.state === "conflict";
      await this.status(this.conflict ? "worker_session_conflict" : target ? "ready" : "no_supported_target");
      await this.reconcileLedger();
      if (target && !this.conflict && !this.pollInFlight) await this.poll();
    } catch (error) {
      this.conflict = error?.status === 409;
      await this.status(this.conflict ? "worker_session_conflict" : "backend_unavailable");
    }
  }

  async reconcileLedger() {
    if (!this.coordinator) return;
    const entries = await this.ledger.all();
    for (const entry of Object.values(entries)) {
      if (!entry.acknowledged && entry.state !== "reserved") await this.coordinator.acknowledge(entry);
    }
  }

  async poll() {
    if (this.pollInFlight || this.conflict || !this.backend || !this.currentTarget) return;
    this.pollInFlight = true;
    const requestId = randomId();
    try {
      const execution = await this.backend.claimNext({ worker_id: this.workerId, worker_session_id: this.sessionId, claim_request_id: requestId });
      if (execution) {
        const result = await this.coordinator.execute(execution, { workerId: this.workerId, sessionId: this.sessionId }, this.currentTarget);
        await this.status(result.outcome === "delivered" ? "delivered" : result.outcome === "failed" ? "failed_pre_send" : "uncertain");
      }
    } catch (error) {
      this.conflict = error instanceof BackendHTTPError && error.status === 409;
      await this.status(this.conflict ? "worker_session_conflict" : "backend_unavailable");
    } finally { this.pollInFlight = false; }
  }

  async status(code) {
    await this.storage.set({ [STATUS_KEY]: { code: safeDiagnostic(code), worker_id: this.workerId, worker_session_id: this.sessionId, updated_at: Date.now() } });
  }
}

const runtime = new ExtensionRuntime(chrome);
runtime.start().catch(() => runtime.status("startup_failed"));
chrome.alarms?.onAlarm.addListener((alarm) => {
  if (alarm.name === HEARTBEAT_ALARM || alarm.name === POLL_ALARM) runtime.tick().catch(() => runtime.status("tick_failed"));
});
