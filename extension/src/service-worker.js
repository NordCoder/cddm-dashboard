import {
  BackendClient,
  BackendHTTPError,
  provisioningCompletionPayload,
  provisioningFinalizePayload,
} from "./api.js";
import { ChromeTargetAdapter, SurfaceCreationError } from "./adapter.js";
import { AmbiguousSendError, ExecutionCoordinator, PreSendError } from "./executor.js";
import { ClaimLedger } from "./ledger.js";
import { ManagedSessionBootstrapper } from "./session-bootstrap.js";
import {
  BACKEND_ORIGIN_KEY,
  WORKER_ID_KEY,
  backendPermissionOrigin,
  isOpaqueIdentifier,
  normalizeBackendOrigin,
  randomId,
  safeDiagnostic,
  sameTarget,
} from "./protocol.js";

const SESSION_ID = randomId();
const HEARTBEAT_ALARM = "cddm-heartbeat";
const POLL_ALARM = "cddm-claim-poll";
const STATUS_KEY = "extension_status";
const MANAGED_WORKERS_KEY = "managed_chat_workers";
const BOOTSTRAP_PHASES = new Set(["not_started", "send_reserved", "sent", "target_observed", "provisioned"]);
export const HEARTBEAT_INTERVAL_MS = 10_000;
export const POLL_INTERVAL_MS = 5_000;
export const ALARM_FALLBACK_DELAY_MS = 30_000;

function managedSessionID(workerID) {
  return `managed.${workerID}`;
}

function exactStrings(left, right) {
  return Array.isArray(left) && Array.isArray(right) && left.length === right.length
    && left.every((value, index) => value === right[index]);
}

function validTarget(value) {
  return value?.kind === "chatgpt_conversation" && value.origin === "https://chatgpt.com"
    && typeof value.path === "string" && /^\/c\/[^/]+$/.test(value.path);
}

function validManagedRecord(value) {
  return value && typeof value === "object" && isOpaqueIdentifier(value.worker_id)
    && isOpaqueIdentifier(value.request_id) && Number.isInteger(value.tab_id) && value.tab_id > 0
    && (!value.target || validTarget(value.target)) && typeof value.claim_owner === "string"
    && typeof value.claim_token === "string" && typeof value.chatgpt_project_url === "string";
}

function normalizeManagedRecord(value) {
  const phase = BOOTSTRAP_PHASES.has(value.bootstrap_phase)
    ? value.bootstrap_phase
    : value.provision_status === "provisioned" ? "provisioned" : "not_started";
  return {
    ...value,
    bootstrap_phase: phase,
    attachment_evidence: Array.isArray(value.attachment_evidence) ? [...value.attachment_evidence] : [],
    attachments: Array.isArray(value.attachments) ? [...value.attachments] : [],
    bootstrap_text: typeof value.bootstrap_text === "string" ? value.bootstrap_text : "",
    session_policy: typeof value.session_policy === "string" ? value.session_policy : value.role === "lead" ? "persistent_project_lead" : "fresh_per_intent",
    observed_chatgpt_url: typeof value.observed_chatgpt_url === "string" ? value.observed_chatgpt_url : "",
    pending_surface_completion: Boolean(value.pending_surface_completion),
  };
}

function requestFromRecord(record) {
  return {
    request_id: record.request_id,
    project_id: record.project_id,
    intent_id: record.intent_id,
    lane_key: record.lane_key,
    issue_number: record.issue_number,
    role: record.role,
    expected_head: record.expected_head || "",
    attachments: [...(record.attachments || [])],
    bootstrap_text: record.bootstrap_text || "",
    session_policy: record.session_policy || "",
    chatgpt_project_url: record.chatgpt_project_url || "",
    status: record.provision_status,
    claim_owner: record.claim_owner,
    claim_token: record.claim_token,
  };
}

function sameSessionScope(left, right) {
  if (left.project_id !== right.project_id || left.role !== right.role) return false;
  return left.role === "lead" || left.issue_number === right.issue_number;
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
    this.bootstrapper = dependencies.bootstrapper || new ManagedSessionBootstrapper(chromeApi);
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
    for (const [workerID, value] of Object.entries(values)) {
      if (!validManagedRecord(value) || value.worker_id !== workerID) continue;
      const record = normalizeManagedRecord(value);
      this.managedWorkers.set(workerID, record);
      this.adapter.reserveManagedTab?.(record.tab_id);
    }
    await this.saveManagedWorkers();
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

  applyRequest(record, request) {
    record.request_id = request.request_id;
    record.claim_owner = request.claim_owner;
    record.claim_token = request.claim_token;
    record.project_id = request.project_id;
    record.intent_id = request.intent_id;
    record.issue_number = request.issue_number;
    record.role = request.role;
    record.lane_key = request.lane_key;
    record.expected_head = request.expected_head || "";
    record.chatgpt_project_url = request.chatgpt_project_url || "";
    record.attachments = [...(request.attachments || [])];
    record.bootstrap_text = request.bootstrap_text || "";
    record.session_policy = request.session_policy || "";
    record.provision_status = request.status;
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
      protocol_version: "m11-c3",
      capabilities: ["chatgpt_conversation", "exact_prompt_send", "session_provisioning", "library_bootstrap"],
      observation: { target },
    };
  }

  managedPayload(record, target) {
    return {
      worker_id: record.worker_id,
      worker_session_id: managedSessionID(record.worker_id),
      protocol_version: "m11-c3-managed",
      capabilities: ["chatgpt_conversation", "exact_prompt_send", "managed_exact_tab", "library_bootstrap"],
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
    this.applyRequest(record, completed);
    if (completed.target) record.target = completed.target;
    record.pending_surface_completion = false;
    await this.saveManagedWorkers();
    return completed;
  }

  async terminalSurfaceFailure(request, outcome, reason, tabId = 0) {
    if (tabId > 0) await this.adapter.closeManagedTab?.(tabId);
    return this.backend.completeProvision(request.request_id, provisioningCompletionPayload(request, outcome, { reason }));
  }

  async retireRecord(record, outcome, reason) {
    const request = requestFromRecord(record);
    try { await this.terminalSurfaceFailure(request, outcome, reason, record.tab_id); } catch { return false; }
    this.registeredManaged.delete(record.worker_id);
    this.managedWorkers.delete(record.worker_id);
    await this.saveManagedWorkers();
    return true;
  }

  async createManagedSurface(request) {
    let created;
    try {
      created = await this.adapter.createConversationSurface(request.chatgpt_project_url || "");
    } catch (error) {
      const outcome = error instanceof SurfaceCreationError ? error.outcome : "safe_failed";
      await this.terminalSurfaceFailure(request, outcome, error?.message || "chat_creation_failed", error?.tabId || 0);
      throw error;
    }
    const workerID = randomId();
    const record = normalizeManagedRecord({
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
      attachments: [...(request.attachments || [])],
      bootstrap_text: request.bootstrap_text || "",
      session_policy: request.session_policy || "",
      provision_status: "claimed",
      pending_surface_completion: true,
      bootstrap_phase: "not_started",
      attachment_evidence: [],
      observed_chatgpt_url: "",
      created_at: Date.now(),
    });
    this.managedWorkers.set(workerID, record);
    await this.saveManagedWorkers();
    try {
      await this.completeSurfaceReady(request, record);
    } catch {
      await this.status("surface_completion_pending");
    }
    return record;
  }

  async findReusableLead(request) {
    if (request.session_policy !== "persistent_project_lead" || request.role !== "lead") return null;
    for (const record of this.managedWorkers.values()) {
      if (record.provision_status !== "provisioned" || record.role !== "lead" || record.project_id !== request.project_id
        || record.chatgpt_project_url !== (request.chatgpt_project_url || "") || !record.target
        || !exactStrings(record.attachment_evidence, request.attachments)) continue;
      try {
        const observation = await this.heartbeatManaged(record);
        if (observation.available && sameTarget(observation.target, record.target)) return record;
      } catch { /* an unhealthy Lead session is replaced with a fresh surface */ }
    }
    return null;
  }

  async reuseLeadSurface(request, record) {
    this.applyRequest(record, request);
    record.provision_status = "claimed";
    record.pending_surface_completion = true;
    record.bootstrap_phase = "target_observed";
    await this.saveManagedWorkers();
    await this.completeSurfaceReady(request, record);
    return record;
  }

  canBootstrap() {
    return Boolean(this.bootstrapper && this.chrome.tabs?.get && this.chrome.tabs?.sendMessage);
  }

  async finalizeRecord(request, record) {
    const observation = await this.heartbeatManaged(record);
    if (!observation.available || !observation.target || !sameTarget(observation.target, record.target)) {
      await this.retireRecord(record, "uncertain", "managed_target_unavailable_before_finalize");
      return;
    }
    let finalized;
    try {
      finalized = await this.backend.finalizeProvision(request.request_id, provisioningFinalizePayload(request, record));
    } catch (error) {
      if (error instanceof BackendHTTPError && error.status === 409) {
        await this.retireRecord(record, "superseded", "provisioning_finalize_conflict");
        return;
      }
      throw error;
    }
    this.applyRequest(record, finalized);
    record.provision_status = "provisioned";
    record.bootstrap_phase = "provisioned";
    record.target = finalized.target || record.target;
    record.observed_chatgpt_url = finalized.observed_chatgpt_url || record.observed_chatgpt_url;
    record.attachment_evidence = [...(finalized.attachment_evidence || record.attachment_evidence || [])];
    record.bound_binding_id = finalized.bound_binding_id || "";
    record.bound_binding_version = finalized.bound_binding_version || 0;
    await this.saveManagedWorkers();
    await this.retireSupersededLocal(record);
  }

  async advanceManagedProvisioning(request, record) {
    this.applyRequest(record, request);
    if (request.status === "claimed") {
      if (record.pending_surface_completion) await this.completeSurfaceReady(request, record);
      return;
    }
    if (request.status !== "surface_ready") return;
    record.provision_status = "surface_ready";
    record.pending_surface_completion = false;

    if (record.bootstrap_phase === "send_reserved") {
      await this.retireRecord(record, "uncertain", "runtime_restart_during_bootstrap_send");
      return;
    }
    if (record.bootstrap_phase === "not_started") {
      if (!this.canBootstrap()) return;
      record.bootstrap_phase = "send_reserved";
      await this.saveManagedWorkers();
      let result;
      try {
        result = await this.bootstrapper.send(record.tab_id, request);
      } catch (error) {
        if (error instanceof PreSendError) {
          await this.retireRecord(record, "safe_failed", error.message || "bootstrap_safe_failure");
          return;
        }
        await this.retireRecord(record, "uncertain", error?.message || "bootstrap_send_outcome_unknown");
        return;
      }
      record.bootstrap_phase = "sent";
      record.attachment_evidence = [...result.attachmentEvidence];
      await this.saveManagedWorkers();
    }
    if (record.bootstrap_phase === "sent") {
      let observed;
      try {
        observed = await this.bootstrapper.waitForConversation(record.tab_id, record.chatgpt_project_url);
      } catch (error) {
        await this.retireRecord(record, "uncertain", error?.message || "conversation_url_unobserved_after_bootstrap");
        return;
      }
      record.target = observed.target;
      record.observed_chatgpt_url = observed.observedURL;
      record.bootstrap_phase = "target_observed";
      await this.saveManagedWorkers();
      await this.heartbeatManaged(record);
    }
    if (record.bootstrap_phase === "target_observed") await this.finalizeRecord(request, record);
  }

  async retireSupersededLocal(current) {
    for (const other of [...this.managedWorkers.values()]) {
      if (other.worker_id === current.worker_id || other.provision_status !== "provisioned" || !sameSessionScope(other, current)) continue;
      this.registeredManaged.delete(other.worker_id);
      this.managedWorkers.delete(other.worker_id);
      await this.adapter.closeManagedTab?.(other.tab_id);
    }
    await this.saveManagedWorkers();
  }

  async reconcileManagedProvisioning() {
    for (const record of [...this.managedWorkers.values()]) {
      if (record.provision_status !== "claimed" && record.provision_status !== "surface_ready") continue;
      const observation = await this.managedObservation(record);
      if (!observation.available) {
        const uncertain = record.bootstrap_phase !== "not_started";
        await this.retireRecord(record, uncertain ? "uncertain" : "safe_failed", "managed_creation_tab_unavailable");
        continue;
      }
      const request = requestFromRecord(record);
      try {
        if (record.pending_surface_completion) await this.completeSurfaceReady(request, record);
        if (record.provision_status === "surface_ready") await this.advanceManagedProvisioning(requestFromRecord(record), record);
      } catch (error) {
        if (error instanceof BackendHTTPError && error.status === 409) {
          await this.retireRecord(record, "superseded", "provisioning_state_conflict");
        }
      }
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
        this.applyRequest(existing, request);
        await this.saveManagedWorkers();
        await this.advanceManagedProvisioning(request, existing);
        return;
      }
      if (request.status !== "claimed") return;
      const reusableLead = await this.findReusableLead(request);
      if (reusableLead) {
        await this.reuseLeadSurface(request, reusableLead);
        return;
      }
      await this.createManagedSurface(request);
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
        if (record.provision_status !== "provisioned" || !record.target) continue;
        const adapter = this.adapter.exactTab(record.tab_id, record.target);
        const managedTarget = await adapter.currentTarget();
        if (!managedTarget) continue;
        await this.pollIdentity({ workerId: record.worker_id, sessionId: managedSessionID(record.worker_id) }, managedTarget, adapter);
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
