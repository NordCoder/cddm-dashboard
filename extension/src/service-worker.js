import { BackendClient, BackendHTTPError } from "./api.js";
import { ChromeTargetAdapter } from "./adapter.js";
import { ExecutionCoordinator } from "./executor.js";
import { ClaimLedger } from "./ledger.js";
import {
  BACKEND_ORIGIN_KEY, WORKER_ID_KEY, backendPermissionOrigin, normalizeBackendOrigin,
  randomId, safeDiagnostic, sameTarget
} from "./protocol.js";

const SESSION_ID = randomId();
const HEARTBEAT_ALARM = "cddm-heartbeat";
const POLL_ALARM = "cddm-claim-poll";
const STATUS_KEY = "extension_status";
export const HEARTBEAT_INTERVAL_MS = 10_000;
export const POLL_INTERVAL_MS = 5_000;
export const ALARM_FALLBACK_DELAY_MS = 30_000;

export class RuntimeScheduler {
  constructor({ setTimeoutFn = globalThis.setTimeout, clearTimeoutFn = globalThis.clearTimeout, alarms = null, now = () => Date.now() } = {}) {
    this.setTimeout = setTimeoutFn;
    this.clearTimeout = clearTimeoutFn;
    this.alarms = alarms;
    this.now = now;
    this.timers = new Map();
    this.handlers = {};
    this.active = false;
  }

  start(handlers) {
    this.stop();
    this.handlers = handlers;
    this.active = true;
    this.scheduleTimer("heartbeat", HEARTBEAT_INTERVAL_MS);
    this.scheduleTimer("poll", POLL_INTERVAL_MS);
    this.scheduleAlarm(HEARTBEAT_ALARM);
    this.scheduleAlarm(POLL_ALARM);
  }

  stop() {
    for (const timer of this.timers.values()) this.clearTimeout(timer);
    this.timers.clear();
    this.active = false;
  }

  scheduleTimer(kind, delay) {
    if (!this.active) return;
    const handler = this.handlers[kind];
    this.timers.set(kind, this.setTimeout(async () => {
      this.scheduleTimer(kind, delay);
      await handler?.();
    }, delay));
  }

  scheduleAlarm(name) {
    if (!this.active || !this.alarms?.create) return;
    try {
      const result = this.alarms.create(name, { when: this.now() + ALARM_FALLBACK_DELAY_MS });
      result?.catch?.(() => {});
    } catch { /* browser alarm failure does not create durable presence */ }
  }

  async alarm(name) {
    if (!this.active) return;
    this.scheduleAlarm(name);
    await this.handlers[name === HEARTBEAT_ALARM ? "heartbeat" : "poll"]?.();
  }
}

export class ExtensionRuntime {
  constructor(chromeApi, dependencies = {}) {
    this.chrome = chromeApi;
    this.storage = dependencies.storage || chromeApi.storage.local;
    this.adapter = dependencies.adapter || new ChromeTargetAdapter(chromeApi);
    this.ledger = dependencies.ledger || new ClaimLedger(this.storage);
    this.clientFactory = dependencies.clientFactory || ((origin) => new BackendClient(origin));
    this.sessionId = dependencies.sessionId || SESSION_ID;
    this.workerId = null;
    this.backend = null;
    this.coordinator = null;
    this.currentTarget = null;
    this.presenceTarget = null;
    this.presenceCurrent = false;
    this.conflict = false;
    this.pollInFlight = false;
    this.heartbeatInFlight = false;
    this.heartbeatPending = false;
    this.registered = false;
    this.scheduler = dependencies.scheduler || new RuntimeScheduler({ alarms: chromeApi.alarms });
  }

  async start() {
    this.workerId = await this.loadWorkerId();
    await this.ledger.recoverReserved();
    await this.ledger.prune();
    await this.heartbeatCycle();
    await this.pollCycle();
    this.scheduler.start({ heartbeat: () => this.heartbeatCycle(), poll: () => this.pollCycle() });
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

  async heartbeatCycle() {
    if (this.heartbeatInFlight) {
      this.heartbeatPending = true;
      return;
    }
    this.heartbeatInFlight = true;
    try {
      const origin = await this.enabledOrigin();
      if (!origin) {
        this.currentTarget = null;
        this.presenceTarget = null;
        this.presenceCurrent = false;
        await this.status("disabled_backend");
        return;
      }
      this.backend = this.clientFactory(origin);
      this.coordinator = new ExecutionCoordinator({ ledger: this.ledger, backend: this.backend, adapter: this.adapter });
      const target = await this.adapter.currentTarget();
      this.currentTarget = target;
      const payload = { worker_id: this.workerId, worker_session_id: this.sessionId, protocol_version: "m6-c3", capabilities: ["chatgpt_conversation", "exact_prompt_send"], observation: { target } };
      const result = this.registered ? await this.backend.heartbeat(this.workerId, payload) : await this.backend.register(payload);
      this.registered = true;
      this.conflict = result?.state === "conflict";
      this.presenceTarget = target;
      this.presenceCurrent = Boolean(target) && !this.conflict;
      await this.status(this.conflict ? "worker_session_conflict" : target ? "ready" : "no_supported_target");
      await this.reconcileLedger();
    } catch (error) {
      this.presenceCurrent = false;
      this.conflict = error?.status === 409;
      await this.status(this.conflict ? "worker_session_conflict" : "backend_unavailable");
    } finally {
      this.heartbeatInFlight = false;
      if (this.heartbeatPending && this.scheduler.active) {
        this.heartbeatPending = false;
        void this.heartbeatCycle();
      }
    }
  }

  async reconcileLedger() {
    if (!this.coordinator) return;
    const entries = await this.ledger.all();
    for (const entry of Object.values(entries)) {
      if (!entry.acknowledged && entry.state !== "reserved") await this.coordinator.acknowledge(entry);
    }
  }

  async pollCycle() {
    const target = await this.adapter.currentTarget();
    this.currentTarget = target;
    if (!sameTarget(target, this.presenceTarget)) this.presenceCurrent = false;
    await this.poll();
  }

  async poll() {
    if (this.pollInFlight || this.conflict || !this.backend || !this.currentTarget || !this.presenceCurrent) return;
    this.pollInFlight = true;
    const requestId = randomId();
    try {
      const execution = await this.backend.claimNext({ worker_id: this.workerId, worker_session_id: this.sessionId, claim_request_id: requestId });
      if (execution) {
        const result = await this.coordinator.execute(execution, { workerId: this.workerId, sessionId: this.sessionId }, this.currentTarget);
        if (result.completion_diagnostic === "completion_conflict") {
          await this.status("delivery_completion_conflict");
        } else {
          await this.status(result.outcome === "delivered" ? "delivered" : result.outcome === "failed" ? "failed_pre_send" : "uncertain");
        }
      }
    } catch (error) {
      this.conflict = error instanceof BackendHTTPError && error.status === 409;
      await this.status(this.conflict ? "worker_session_conflict" : "backend_unavailable");
    } finally { this.pollInFlight = false; }
  }

  async status(code) {
    await this.storage.set({ [STATUS_KEY]: { code: safeDiagnostic(code), worker_id: this.workerId, worker_session_id: this.sessionId, updated_at: Date.now() } });
  }

  async handleAlarm(name) { await this.scheduler.alarm(name); }

  async handleTabActivated(tabId) {
    try { await this.adapter.observeActivatedTab?.(tabId); } catch { /* the periodic target validation remains authoritative */ }
    if (this.workerId) await this.heartbeatCycle();
  }
}

if (globalThis.chrome?.storage?.local) {
  const runtime = new ExtensionRuntime(globalThis.chrome);
  runtime.start().catch(() => runtime.status("startup_failed"));
  globalThis.chrome.alarms?.onAlarm.addListener((alarm) => {
    if (alarm.name === HEARTBEAT_ALARM || alarm.name === POLL_ALARM) runtime.handleAlarm(alarm.name).catch(() => runtime.status("tick_failed"));
  });
  globalThis.chrome.tabs?.onActivated?.addListener(({ tabId }) => {
    runtime.handleTabActivated(tabId).catch(() => runtime.status("target_activation_failed"));
  });
}
