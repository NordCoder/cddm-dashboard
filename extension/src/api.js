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

  async request(path, body, retryTransport = true, method = "POST") {
    const baseInit = { method, headers: { "content-type": "application/json" }, body: JSON.stringify(body) };
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
  bindCurrent(projectId, issueNumber, payload) {
    return this.request(`/api/projects/${encodeURIComponent(projectId)}/work-units/${encodeURIComponent(issueNumber)}/browser-binding`, payload, true, "PUT");
  }
}

export function completionPayload(commandId, claimId, outcome, reason) {
  return { command_id: commandId, claim_id: claimId, outcome, reason: safeDiagnostic(reason), evidence: "extension_dom_executor" };
}
