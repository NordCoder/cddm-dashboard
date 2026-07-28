import { safeDiagnostic } from "./protocol.js";

const JOBS_KEY = "chat_bootstrap_jobs";
const ALLOWED_ROLES = new Set(["lead", "implementor", "qa"]);
const ALLOWED_DASHBOARD_ORIGINS = new Set([
  "http://localhost:1337",
  "http://localhost:1338",
  "http://localhost:5173",
  "http://127.0.0.1:1337",
  "http://127.0.0.1:1338",
  "http://127.0.0.1:5173",
]);

function parsePositiveInteger(value, name) {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new Error(`${name}_invalid`);
  return parsed;
}

function parseExpectedVersion(value) {
  if (value === undefined || value === null) return undefined;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 0) throw new Error("expected_binding_version_invalid");
  return parsed;
}

function parseRequest(message) {
  if (!message || message.type !== "create-worker-chat") throw new Error("bootstrap_request_invalid");
  const requestId = String(message.request_id ?? "").trim();
  const role = String(message.role ?? "").trim().toLowerCase();
  const laneKey = String(message.expected_lane_key ?? "").trim();
  const prompt = String(message.bootstrap_prompt ?? "");
  if (!/^[A-Za-z0-9._-]{1,200}$/.test(requestId)) throw new Error("bootstrap_request_id_invalid");
  if (!ALLOWED_ROLES.has(role)) throw new Error("bootstrap_role_invalid");
  if (!laneKey || laneKey.length > 400) throw new Error("bootstrap_lane_invalid");
  if (!prompt.trim() || prompt.length > 20_000) throw new Error("bootstrap_prompt_invalid");
  return {
    requestId,
    projectId: parsePositiveInteger(message.project_id, "project_id"),
    issueNumber: parsePositiveInteger(message.issue_number, "issue_number"),
    role,
    laneKey,
    prompt,
    expectedVersion: parseExpectedVersion(message.expected_binding_version),
  };
}

function validateSender(sender) {
  let origin;
  try { origin = new URL(String(sender?.url ?? "")).origin; } catch { throw new Error("bootstrap_sender_invalid"); }
  if (!ALLOWED_DASHBOARD_ORIGINS.has(origin)) throw new Error("bootstrap_sender_not_allowed");
}

export class ChatBootstrapCoordinator {
  constructor(storage, now = () => Date.now()) {
    this.storage = storage;
    this.now = now;
    this.inFlight = new Map();
  }

  async jobs() {
    const value = (await this.storage.get(JOBS_KEY))[JOBS_KEY];
    return value && typeof value === "object" && !Array.isArray(value) ? value : {};
  }

  async save(jobs) { await this.storage.set({ [JOBS_KEY]: jobs }); }

  async execute(message, sender, runtime) {
    validateSender(sender);
    const request = parseRequest(message);
    const existingInFlight = this.inFlight.get(request.requestId);
    if (existingInFlight) return existingInFlight;
    const task = this.executeOnce(request, runtime).finally(() => this.inFlight.delete(request.requestId));
    this.inFlight.set(request.requestId, task);
    return task;
  }

  async executeOnce(request, runtime) {
    if (!runtime?.provisionConversation || !runtime?.backend?.bindCurrent) {
      return { ok: false, reason: "chat_creation_unavailable" };
    }
    const jobs = await this.jobs();
    const previous = jobs[request.requestId];
    if (previous?.state === "completed") {
      return { ok: true, target: previous.target, binding: previous.binding, worker_id: previous.worker_id, reused: true };
    }
    if (previous) return { ok: false, reason: "bootstrap_request_already_consumed" };

    jobs[request.requestId] = {
      state: "in_progress",
      project_id: request.projectId,
      issue_number: request.issueNumber,
      role: request.role,
      lane_key: request.laneKey,
      started_at: this.now(),
    };
    await this.save(jobs);

    try {
      const provisioned = await runtime.provisionConversation(request);
      const input = {
        expected_lane_key: request.laneKey,
        worker_id: provisioned.workerId,
        target: provisioned.target,
      };
      if (request.expectedVersion !== undefined) input.expected_binding_version = request.expectedVersion;
      const binding = await runtime.backend.bindCurrent(request.projectId, request.issueNumber, input);
      jobs[request.requestId] = {
        ...jobs[request.requestId],
        state: "completed",
        completed_at: this.now(),
        worker_id: provisioned.workerId,
        target: provisioned.target,
        binding,
      };
      await this.save(jobs);
      return { ok: true, target: provisioned.target, worker_id: provisioned.workerId, binding };
    } catch (error) {
      const reason = safeDiagnostic(error instanceof Error ? error.message : "chat_creation_failed");
      jobs[request.requestId] = {
        ...jobs[request.requestId],
        state: "failed",
        completed_at: this.now(),
        reason,
      };
      await this.save(jobs);
      return { ok: false, reason };
    }
  }
}
