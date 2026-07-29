import { AmbiguousSendError, PreSendError } from "./executor.js";
import {
  conversationURLBelongsToProject,
  creationSurfaceMatches,
  normalizeTargetUrl,
} from "./protocol.js";

const TARGET_TIMEOUT_MS = 30_000;
const TARGET_POLL_MS = 100;

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

function exactStrings(left, right) {
  return Array.isArray(left) && Array.isArray(right) && left.length === right.length
    && left.every((value, index) => value === right[index]);
}

export class ManagedSessionBootstrapper {
  constructor(chromeApi, dependencies = {}) {
    this.chrome = chromeApi;
    this.now = dependencies.now || (() => Date.now());
    this.delay = dependencies.delay || delay;
    this.targetTimeout = dependencies.targetTimeout || TARGET_TIMEOUT_MS;
    this.targetPoll = dependencies.targetPoll || TARGET_POLL_MS;
  }

  async exactTab(tabId, projectURL) {
    let tab;
    try { tab = await this.chrome.tabs.get(tabId); } catch { throw new PreSendError("managed_creation_tab_unavailable"); }
    if (!tab?.url || !creationSurfaceMatches(tab.url, projectURL || "")) {
      throw new PreSendError("managed_creation_scope_mismatch");
    }
    return tab;
  }

  async send(tabId, request) {
    await this.exactTab(tabId, request.chatgpt_project_url || "");
    let result;
    try {
      result = await this.chrome.tabs.sendMessage(tabId, {
        type: "bootstrap-session",
        attachments: request.attachments,
        bootstrap_text: request.bootstrap_text,
        chatgpt_project_url: request.chatgpt_project_url || "",
      });
    } catch {
      throw new PreSendError("bootstrap_surface_unavailable");
    }
    if (!result?.ok) {
      if (result?.safe_no_send) throw new PreSendError(result.reason || "bootstrap_safe_failure");
      throw new AmbiguousSendError(result?.reason || "bootstrap_send_outcome_unknown");
    }
    if (!exactStrings(result.attachment_evidence, request.attachments)) {
      throw new AmbiguousSendError("bootstrap_attachment_evidence_mismatch");
    }
    return { attachmentEvidence: [...result.attachment_evidence] };
  }

  async waitForConversation(tabId, projectURL = "") {
    const deadline = this.now() + this.targetTimeout;
    while (this.now() < deadline) {
      let tab;
      try { tab = await this.chrome.tabs.get(tabId); } catch { throw new AmbiguousSendError("conversation_tab_closed_after_bootstrap"); }
      const target = tab?.url ? normalizeTargetUrl(tab.url) : null;
      if (target) {
        if (projectURL && !conversationURLBelongsToProject(tab.url, projectURL)) {
          throw new AmbiguousSendError("conversation_outside_project_after_bootstrap");
        }
        if (!projectURL && !/^https:\/\/chatgpt\.com\/c\/[^/?#]+$/.test(tab.url)) {
          throw new AmbiguousSendError("conversation_outside_global_scope_after_bootstrap");
        }
        return { target, observedURL: tab.url };
      }
      if (tab?.url && !creationSurfaceMatches(tab.url, projectURL)) {
        throw new AmbiguousSendError("conversation_scope_drift_after_bootstrap");
      }
      await this.delay(this.targetPoll);
    }
    throw new AmbiguousSendError("conversation_url_unobserved_after_bootstrap");
  }
}
