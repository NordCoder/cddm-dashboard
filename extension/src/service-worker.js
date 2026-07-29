import { BackendClient, BackendHTTPError, provisioningCompletionPayload } from "./api.js";
import { ChromeTargetAdapter } from "./adapter.js";
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

function validTarget(value) {
  return value?.kind === "chatgpt_conversation" && value.origin === "https://chatgpt.com"
    && typeof value.path === "string" && value.path.startsWith("/c/");
}

function validManagedRecord(value) {
  return value && typeof value === "object" && isOpaqueIdentifier(value.worker_id)
    && isOpaqueIdentifier(value.request_id) && Number.isInteger(value.tab_id) && value.tab_id > 0
    && (!value.target || validTarget(value.target)) && typeof value.claim_owner === "string"
    && typeof value.claim_token === "string" && typeof value.chatgpt_project_url === "string";
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
    this.clientFactory = dependencies.clientFactory || ((origin) => new BackendClient(origin));
    this.sessionId = dependencies.sessionId || SESSION_ID;
    this.workerId = null;
    this.backend = null;
    this.backendOrigin = null;
    this.currentTarget = null;
    this.presenceTarget = null;
    this.presenceCurrent = false;
    this.conflict = false;
    this.pollInFlight = new Set();
    this.provisionInFlight = false;
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

  managedByRequest(requestID) {
    for (const record of this.managedWorkers.values()) {
      if (record.request_id === requestID) return record;
    }
    return null;
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
      protocol_version: "m11-c2",
      capabilities: ["chatgpt_conversation", "exact_prompt_send", "session_provisioning"],
      observation: { target },
    };
  }

  managedPayload(record, target) {
    return {
      worker_id: record.worker_id,
      worker_session_id: managedSessionID(this.sessionId, record.worker_id),
      protocol_version: "m11-c2-managed",
      capabilities: ["chatgpt_conversation", "exact_prompt_send", "managed_exact_tab", "session_provision_surface"],
      observation: { target },
    };
  }

  async managedObservation(record) {
    return this.adapter.managedObservation(record.tab_id, record.chatgpt_project_url, record.target || null);
  }

  async heartbeatManaged(record) {
    const observation = await this.managedObservation(record);
    if (!observation.available) {
      this.registeredManaged.delete(record.worker_id);
      return { available: false, target: null };
    }
    if (observation.target && !record.target) {
      record.target = observation.target;
      await this.saveManagedWorkers();
    }
    const payload = this.managedPayload(record, observation.target);
    const result = this.registeredManaged.has(record.worker_id)
      ? await this.backend.heartbeat(record.worker_id, payload)
      : await this.backend.register(payload);
    if (result?.state === "conflict") {
      this.registeredManaged.delete(record.worker_id);
      throw new Error("managed_worker_session_conflict");
    }
    this.registeredManaged.add(record.worker_id);
    return observation;
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
          const observation = await this.heartbeatManaged(record);
          if (observation.available) managedReady += 1;
        } catch { /* each managed worker retries independently */ }
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
    if (!this.backend) return;
    const origin = this.backendOrigin;
    const entries = await this.ledger.all();
    const coordinator = new ExecutionCoordinator({ ledger: this.ledger, backend: this.backend, adapter: this.adapter, backendOrigin: origin });
    for (const entry of Object.values(entries)) {
      if (entry.acknowledged || entry.state === "reserved" || entry.backend_origin !== origin) continue;
      try { await coordinator.acknowledge(entry); } catch { /* a later heartbeat retries */ }
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
      await this.status(result.outcome === "delivered" ? "delivered" : result.outcome === "failed" ? "failed_pre_send" : "uncertain");
    } catch (error) {
      if (error instanceof BackendHTTPError && error.status === 409) await this.status("worker_session_conflict");
      else await this.status("backend_unavailable");
    } finally {
      this.pollInFlight.delete(identity.workerId);
    }
  }

  async completeSurfaceReady(request, record) {
    const observation = await this.managedObservation(record);
    if (!observation.available) throw new PreSendError("managed_creation_tab_unavailable");
    await this.heartbeatManaged(record);
    const completed = await this.backend.completeProvision(request.request_id, provisioningCompletionPayload(request, "surface_ready", {
      workerId: record.worker_id,
      tabId: record.tab_id,
      target: observation.target || undefined,
    }));
    record.claim_owner = completed.claim_owner;
    record.claim_token = completed.claim_token;
    record.provision_status = completed.status;
    if (completed.target) record.target = completed.target;
    record.pending_surface_completion = false;
    await this.saveManagedWorkers();
    return completed;
  }

  async createManagedSurface(request) {
    let created;
    try {
      created = await this.adapter.createConversationSurface(request.chatgpt_project_url || "");
    } catch (error) {
      await this.backend.completeProvision(request.request_id, provisioningCompletionPayload(request, "safe_failed", {
        reason: error?.message || "chat_creation_failed",
      }));
      throw error;
    }
    const workerID = randomId();
    const record = {
      worker_id: workerID,
      request_id: request.request_id,
      claim_owner: request.claim_owner,
      claim_token: request.claim_token,
      tab_id: created.tabId,
      target: created.target || null,
      project_id: request.project_id,
      intent_id: request.intent_id,
      issue_number: request.issue_number,
      role: request.role,
      lane_key: request.lane_key,
      expected_head: request.expected_head || "",
      chatgpt_project_url: request.chatgpt_project_url || "",
      provision_status: "claimed",
      pending_surface_completion: true,
      created_at: Date.now(),
    };
    this.managedWorkers.set(workerID, record);
    await this.saveManagedWorkers();
    try {
      await this.completeSurfaceReady(request, record);
    } catch (error) {
      await this.status("surface_completion_pending");
    }
    return record;
  }

  async reconcileManagedProvisioning() {
    for (const record of this.managedWorkers.values()) {
      if (!record.pending_surface_completion) continue;
      const request = {
        request_id: record.request_id,
        claim_owner: record.claim_owner,
        claim_token: record.claim_token,
      };
      try { await this.completeSurfaceReady(request, record); } catch { /* later provisioning tick retries exact local tab */ }
    }
  }

  async provisionCycle() {
    if (this.provisionInFlight || !this.backend || !this.workerId) return;
    this.provisionInFlight = true;
    try {
      await this.reconcileManagedProvisioning();
      const request = await this.backend.claimProvision({
        claim_request_id: randomId(),
        claim_owner: this.workerId,
        claim_ttl_seconds: 120,
      });
      if (!request) return;
      const existing = this.managedByRequest(request.request_id);
      if (existing) {
        existing.claim_owner = request.claim_owner;
        existing.claim_token = request.claim_token;
        if (request.status === "claimed") existing.pending_surface_completion = true;
        await this.saveManagedWorkers();
        if (existing.pending_surface_completion) await this.completeSurfaceReady(request, existing);
        return;
      }
      if (request.status === "claimed") await this.createManagedSurface(request);
    } catch (error) {
      await this.status(error instanceof BackendHTTPError && error.status === 409 ? "provisioning_conflict" : "provisioning_unavailable");
    } finally {
      this.provisionInFlight = false;
    }
  }

  async pollCycle() {
    try {
      const origin = await this.enabledOrigin();
      if (!origin || origin !== this.backendOrigin || !this.backend) {
        this.presenceCurrent = false;
        return;
      }
      await this.provisionCycle();
      const target = await this.adapter.currentTarget();
      this.currentTarget = target;
      if (!sameTarget(target, this.presenceTarget)) this.presenceCurrent = false;
      if (!this.conflict && target && this.presenceCurrent) {
        await this.pollIdentity({ workerId: this.workerId, sessionId: this.sessionId }, target, this.adapter);
      }
      for (const record of this.managedWorkers.values()) {
        if (!record.target) continue;
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

  async status(code) {
    await this.storage.set({ [STATUS_KEY]: { code: safeDiagnostic(code), worker_id: this.workerId, worker_session_id: this.sessionId, updated_at: Date.now() } });
  }

  async handleAlarm(name) { await this.scheduler.alarm(name); }

  async handleTabActivated(tabId) {
    if (!this.adapter.isManagedTab?.(tabId)) {
      try { await this.adapter.observeActivatedTab?.(tabId); } catch { /* periodic validation remains authoritative */ }
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
}
