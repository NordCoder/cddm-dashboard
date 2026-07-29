import { safeDiagnostic } from "./protocol.js";

export const DEFAULT_REQUEST_TIMEOUT_MS = 8_000;

export class BackendHTTPError extends Error {
  constructor(status, body = "") {
    super(`backend_http_${status}`);
    this.name = "BackendHTTPError";
    this.status = status;
    this.body = body;
  }
}

export class BackendClient {
  constructor(origin, fetchFn = globalThis.fetch, retryDelay = 25, requestTimeout = DEFAULT_REQUEST_TIMEOUT_MS, timerApi = globalThis) {
    this.origin = origin.replace(/\/$/, "");
    this.fetchFn = fetchFn;
    this.retryDelay = retryDelay;
    this.requestTimeout = requestTimeout;
    this.timerApi = timerApi;
  }

  async request(path, body, retryTransport = true) {
    const baseInit = { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) };
    let lastError;
    for (let attempt = 0; attempt < (retryTransport ? 2 : 1); attempt += 1) {
      const controller = new AbortController();
      const timeout = this.requestTimeout > 0
        ? this.timerApi.setTimeout(() => controller.abort(), this.requestTimeout)
        : null;
      try {
        const response = await this.fetchFn(`${this.origin}${path}`, { ...baseInit, signal: controller.signal });
        const text = await response.text();
        if (!response.ok) throw new BackendHTTPError(response.status, text);
        return response.status === 204 || !text ? null : JSON.parse(text);
      } catch (error) {
        lastError = controller.signal.aborted ? new Error("backend_request_timeout") : error;
        if (lastError instanceof BackendHTTPError || attempt === 1) throw lastError;
        await new Promise((resolve) => this.timerApi.setTimeout(resolve, this.retryDelay));
      } finally {
        if (timeout !== null) this.timerApi.clearTimeout(timeout);
      }
    }
    throw lastError;
  }

  register(payload) { return this.request("/api/browser/workers", payload); }
  heartbeat(workerId, payload) { return this.request(`/api/browser/workers/${encodeURIComponent(workerId)}/heartbeat`, payload); }
  claimNext(payload) { return this.request("/api/browser/deliveries/claim-next", payload, true); }
  complete(commandId, payload) { return this.request(`/api/browser/deliveries/${encodeURIComponent(commandId)}/complete`, payload, true); }
  claimProvision(payload) { return this.request("/api/browser/provisioning/claim-next", payload, true); }
  completeProvision(requestId, payload) { return this.request(`/api/browser/provisioning/${encodeURIComponent(requestId)}/complete`, payload, true); }
  finalizeProvision(requestId, payload) { return this.request(`/api/browser/provisioning/${encodeURIComponent(requestId)}/finalize`, payload, true); }
}

export function completionPayload(commandId, claimId, outcome, reason) {
  return { command_id: commandId, claim_id: claimId, outcome, reason: safeDiagnostic(reason), evidence: "extension_dom_executor" };
}

export function provisioningCompletionPayload(request, outcome, input = {}) {
  const payload = {
    claim_owner: request.claim_owner,
    claim_token: request.claim_token,
    outcome,
    reason: safeDiagnostic(input.reason || ""),
  };
  if (input.workerId) payload.worker_id = input.workerId;
  if (Number.isInteger(input.tabId) && input.tabId > 0) payload.tab_id = input.tabId;
  if (input.target) payload.target = input.target;
  if (Array.isArray(input.attachmentEvidence)) payload.attachment_evidence = input.attachmentEvidence;
  return payload;
}

export function provisioningFinalizePayload(request, record) {
  return {
    claim_owner: request.claim_owner,
    claim_token: request.claim_token,
    worker_id: record.worker_id,
    tab_id: record.tab_id,
    target: record.target,
    observed_chatgpt_url: record.observed_chatgpt_url,
    attachment_evidence: [...(record.attachment_evidence || [])],
  };
}
