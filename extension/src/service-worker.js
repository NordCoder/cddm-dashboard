import { BackendClient, BackendHTTPError } from "./api.js";
import { ChromeTargetAdapter } from "./adapter.js";
import { ChatBootstrapCoordinator } from "./chat-bootstrap.js";
import { ExecutionCoordinator, PreSendError } from "./executor.js";
import { ClaimLedger } from "./ledger.js";
import {
  BACKEND_ORIGIN_KEY, WORKER_ID_KEY, backendPermissionOrigin, isOpaqueIdentifier, normalizeBackendOrigin,
  randomId, safeDiagnostic, sameTarget
} from "./protocol.js";

const SESSION_ID = randomId();
const HEARTBEAT_ALARM = "cddm-heartbeat";
const POLL_ALARM = "cddm-claim-poll";
const STATUS_KEY = "extension_status";
const MANAGED_WORKERS_KEY = "managed_chat_workers";
export const HEARTBEAT_INTERVAL_MS = 10_000;
export const POLL_INTERVAL_MS = 5_000;
export const ALARM_FALLBACK_DELAY_MS = 30_000;

function managedSessionID(runtimeSessionID, workerID) {
  return `${runtimeSessionID}.${workerID.slice(0, 24)}`;
}

function validManagedRecord(value) {
  return value && typeof value === "object" && isOpaqueIdentifier(value.worker_id)
    && Number.isInteger(value.tab_id) && value.tab_id > 0
    && value.target?.kind === "chatgpt_conversation" && value.target.origin === "https://chatgpt.com"
    && typeof value.target.path === "string" && value.target.path.startsWith("/c/");
}

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
      try { await handler?.(); } catch { /* the next bounded tick retries */ }
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
    this.chatBootstrap = dependencies.chatBootstrap || new ChatBootstrapCoordinator(this.storage);
    this.clientFactory = dependencies.clientFactory || ((origin) => new BackendClient(origin));
    this.sessionId = dependencies.sessionId || SESSION_ID;
    this.workerId = null;
    this.backend = null;
    this.backendOrigin = null;
    this.coordinator = null;
    this.currentTarget = null;
    this.presenceTarget = null;
    this.presenceCurrent = false;
    this.conflict = false;
    this.pollInFlight = new Set();
    this.heartbeatInFlight = false;
    this.heartbeatPending = false;
    this.registered = false;
    this.registeredManaged = new Set();
    this.managedWorkers = new Map();
    this.scheduler = dependencies.scheduler || new RuntimeScheduler({ alarms: chromeApi.alarms });
  }

  async start() {
    this.workerId = await this.loadWorkerId();
    await this.loadManagedWorkers();
    await this.ledger.recoverReserved();
    await this.ledger.prune();
    await this.heartbeatCycle();
    await this.pollCycle();
    this.scheduler.start({ heartbeat: () => this.heartbeatCycle(), poll: () => this.pollCycle() });
  }

  async loadWorkerId() {
    const found = (await this.storage.get(WORKER_ID_KEY))[WORKER_ID_KEY];
    if (isOpaqueIdentifier(found)) return found;
    const generated = randomId();
    await this.storage.set({ [WORKER_ID_KEY]: generated });
    return generated;
  }

  async loadManagedWorkers() {
    const stored = (await this.storage.get(MANAGED_WORKERS_KEY))[MANAGED_WORKERS_KEY];
    const values = stored && typeof stored === "object" && !Array.isArray(stored) ? stored : {};
    this.managedWorkers.clear();
    for (const [workerID, record] of Object.entries(values)) {
      if (!validManagedRecord(record) || record.worker_id !== workerID) continue;
      this.managedWorkers.set(workerID, record);
      this.adapter.reserveManagedTab?.(record.tab_id);
    }
  }

  async saveManagedWorkers() {
    await this.storage.set({ [MANAGED_WORKERS_KEY]: Object.fromEntries(this.managedWorkers) });
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

  disableRuntimeBackend() {
    this.backend = null;
    this.backendOrigin = null;
    this.coordinator = null;
    this.registered = false;
    this.registeredManaged.clear();
    this.conflict = false;
    this.currentTarget = null;
    this.presenceTarget = null;
    this.presenceCurrent = false;
  }

  primaryPayload(target) {
    return {
      worker_id: this.workerId,
      worker_session_id: this.sessionId,
      protocol_version: "m8-c1",
      capabilities: ["chatgpt_conversation", "exact_prompt_send", "chatgpt_conversation_create"],
      observation: { target },
    };
  }

  managedPayload(record, target) {
    return {
      worker_id: record.worker_id,
      worker_session_id: managedSessionID(this.sessionId, record.worker_id),
      protocol_version: "m8-c1-managed",
      capabilities: ["chatgpt_conversation", "exact_prompt_send", "managed_exact_tab"],
      observation: { target },
    };
  }

  async heartbeatManaged(record) {
    const exactAdapter = this.adapter.exactTab(record.tab_id, record.target);
    const target = await exactAdapter.currentTarget();
    const payload = this.managedPayload(record, target);
    const result = this.registeredManaged.has(record.worker_id)
      ? await this.backend.heartbeat(record.worker_id, payload)
      : await this.backend.register(payload);
    this.registeredManaged.add(record.worker_id);
    if (result?.state === "conflict") throw new Error("managed_worker_session_conflict");
    return target;
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
        this.disableRuntimeBackend();
        await this.status("disabled_backend");
        return;
      }
      if (origin !== this.backendOrigin) {
        this.backend = this.clientFactory(origin);
        this.backendOrigin = origin;
        this.coordinator = new ExecutionCoordinator({ ledger: this.ledger, backend: this.backend, adapter: this.adapter, backendOrigin: origin });
        this.registered = false;
        this.registeredManaged.clear();
        this.presenceCurrent = false;
      }
      const target = await this.adapter.currentTarget();
      this.currentTarget = target;
      const result = this.registered
        ? await this.backend.heartbeat(this.workerId, this.primaryPayload(target))
        : await this.backend.register(this.primaryPayload(target));
      this.registered = true;
      this.conflict = result?.state === "conflict";
      this.presenceTarget = target;
      this.presenceCurrent = Boolean(target) && !this.conflict;

      let managedReady = 0;
      for (const record of this.managedWorkers.values()) {
        try {
          if (await this.heartbeatManaged(record)) managedReady += 1;
        } catch { /* each managed binding independently becomes stale/unavailable */ }
      }
      await this.status(this.conflict ? "worker_session_conflict" : target || managedReady > 0 ? "ready" : "no_supported_target");
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
    const coordinator = this.coordinator;
    const origin = this.backendOrigin;
    const entries = await this.ledger.all();
    for (const entry of Object.values(entries)) {
      if (entry.acknowledged || entry.state === "reserved" || entry.backend_origin !== origin) continue;
      try { await coordinator.acknowledge(entry); } catch { /* a later heartbeat retries remaining durable acknowledgements */ }
    }
  }

  async pollIdentity(identity, target, adapter) {
    if (!target || !this.backend || !this.backendOrigin || this.pollInFlight.has(identity.workerId)) return;
    this.pollInFlight.add(identity.workerId);
    const requestId = randomId();
    const backend = this.backend;
    const claimedOrigin = this.backendOrigin;
    const coordinator = new ExecutionCoordinator({ ledger: this.ledger, backend, adapter, backendOrigin: claimedOrigin });
    try {
      const execution = await backend.claimNext({ worker_id: identity.workerId, worker_session_id: identity.sessionId, claim_request_id: requestId });
      if (!execution) return;
      const result = await coordinator.execute(
        execution,
        identity,
        target,
        async () => {
          const currentOrigin = await this.enabledOrigin();
          const currentTarget = await adapter.currentTarget();
          if (!currentOrigin || currentOrigin !== claimedOrigin || claimedOrigin !== this.backendOrigin) {
            throw new PreSendError("backend_configuration_changed_before_send");
          }
          if (!sameTarget(currentTarget, target)) throw new PreSendError("target_changed_before_send");
        },
      );
      if (result.completion_diagnostic === "completion_conflict") {
        await this.status("delivery_completion_conflict");
      } else if (result.completion_diagnostic?.startsWith("completion_rejected_")) {
        await this.status(result.completion_diagnostic);
      } else {
        await this.status(result.outcome === "delivered" ? "delivered" : result.outcome === "failed" ? "failed_pre_send" : "uncertain");
      }
    } catch (error) {
      if (error instanceof BackendHTTPError && error.status === 409) await this.status("worker_session_conflict");
      else await this.status("backend_unavailable");
    } finally {
      this.pollInFlight.delete(identity.workerId);
    }
  }

  async pollCycle() {
    try {
      const origin = await this.enabledOrigin();
      if (!origin || origin !== this.backendOrigin || !this.backend) {
        this.presenceCurrent = false;
        return;
      }
      const target = await this.adapter.currentTarget();
      this.currentTarget = target;
      if (!sameTarget(target, this.presenceTarget)) this.presenceCurrent = false;
      if (!this.conflict && target && this.presenceCurrent) {
        await this.pollIdentity({ workerId: this.workerId, sessionId: this.sessionId }, target, this.adapter);
      }
      for (const record of this.managedWorkers.values()) {
        const adapter = this.adapter.exactTab(record.tab_id, record.target);
        const managedTarget = await adapter.currentTarget();
        if (!managedTarget) continue;
        await this.pollIdentity({ workerId: record.worker_id, sessionId: managedSessionID(this.sessionId, record.worker_id) }, managedTarget, adapter);
      }
    } catch {
      this.presenceCurrent = false;
      await this.status("backend_unavailable");
    }
  }

  async provisionConversation(request) {
    if (!this.backend) throw new Error("backend_unavailable");
    const created = await this.adapter.createConversation(request.prompt);
    const workerID = randomId();
    const record = {
      worker_id: workerID,
      tab_id: created.tabId,
      target: created.target,
      project_id: request.projectId,
      issue_number: request.issueNumber,
      role: request.role,
      lane_key: request.laneKey,
      created_at: Date.now(),
    };
    this.managedWorkers.set(workerID, record);
    await this.saveManagedWorkers();
    const target = await this.heartbeatManaged(record);
    if (!target || !sameTarget(target, created.target)) throw new Error("created_target_presence_unavailable");
    return { workerId: workerID, target: created.target };
  }

  async handleExternalMessage(message, sender) {
    if (message?.type !== "create-worker-chat") return { ok: false, reason: "unsupported_external_message" };
    if (!this.workerId) return { ok: false, reason: "extension_startup_incomplete" };
    const origin = await this.enabledOrigin();
    if (!origin) return { ok: false, reason: "disabled_backend" };
    if (origin !== this.backendOrigin || !this.backend) await this.heartbeatCycle();
    if (!this.backend || origin !== this.backendOrigin) return { ok: false, reason: "backend_unavailable" };
    const result = await this.chatBootstrap.execute(message, sender, {
      backend: this.backend,
      provisionConversation: (request) => this.provisionConversation(request),
    });
    await this.status(result.ok ? "chat_created_and_bound" : result.reason || "chat_creation_failed");
    return result;
  }

  async status(code) {
    await this.storage.set({ [STATUS_KEY]: { code: safeDiagnostic(code), worker_id: this.workerId, worker_session_id: this.sessionId, updated_at: Date.now() } });
  }

  async handleAlarm(name) { await this.scheduler.alarm(name); }

  async handleTabActivated(tabId) {
    if (!this.adapter.isManagedTab?.(tabId)) {
      try { await this.adapter.observeActivatedTab?.(tabId); } catch { /* the periodic target validation remains authoritative */ }
    }
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
  globalThis.chrome.runtime?.onMessageExternal?.addListener((message, sender, respond) => {
    runtime.handleExternalMessage(message, sender).then(respond, (error) => respond({ ok: false, reason: safeDiagnostic(error?.message || "chat_creation_failed") }));
    return true;
  });
}
