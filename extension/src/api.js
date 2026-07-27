import { safeDiagnostic } from "./protocol.js";

export class BackendHTTPError extends Error {
  constructor(status, body = "") {
    super(`backend_http_${status}`);
    this.name = "BackendHTTPError";
    this.status = status;
    this.body = body;
  }
}

export class BackendClient {
  constructor(origin, fetchFn = globalThis.fetch, retryDelay = 25) {
    this.origin = origin.replace(/\/$/, "");
    this.fetchFn = fetchFn;
    this.retryDelay = retryDelay;
  }

  async request(path, body, retryTransport = true) {
    const init = { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) };
    let lastError;
    for (let attempt = 0; attempt < (retryTransport ? 2 : 1); attempt += 1) {
      try {
        const response = await this.fetchFn(`${this.origin}${path}`, init);
        const text = await response.text();
        if (!response.ok) throw new BackendHTTPError(response.status, text);
        return response.status === 204 || !text ? null : JSON.parse(text);
      } catch (error) {
        lastError = error;
        if (error instanceof BackendHTTPError || attempt === 1) throw error;
        await new Promise((resolve) => setTimeout(resolve, this.retryDelay));
      }
    }
    throw lastError;
  }

  register(payload) { return this.request("/api/browser/workers", payload); }
  heartbeat(workerId, payload) { return this.request(`/api/browser/workers/${encodeURIComponent(workerId)}/heartbeat`, payload); }
  claimNext(payload) { return this.request("/api/browser/deliveries/claim-next", payload, true); }
  complete(commandId, payload) { return this.request(`/api/browser/deliveries/${encodeURIComponent(commandId)}/complete`, payload, true); }
}

export function completionPayload(commandId, claimId, outcome, reason) {
  return { command_id: commandId, claim_id: claimId, outcome, reason: safeDiagnostic(reason), evidence: "extension_dom_executor" };
}
