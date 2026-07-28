import { PreSendError, AmbiguousSendError } from "./executor.js";
import { normalizeTargetUrl, sameTarget } from "./protocol.js";

const TRACKED_TAB_KEY = "tracked_chatgpt_tab_id";
const NEW_CHAT_URL = "https://chatgpt.com/";
const CHAT_CREATE_TIMEOUT_MS = 30_000;
const CHAT_CREATE_POLL_MS = 100;

function delay(milliseconds) { return new Promise((resolve) => setTimeout(resolve, milliseconds)); }

class ExactTabAdapter {
  constructor(parent, tabId, target) {
    this.parent = parent;
    this.tabId = tabId;
    this.target = target;
  }

  async currentTarget() {
    try {
      const tab = await this.parent.chrome.tabs.get(this.tabId);
      const current = tab?.url ? normalizeTargetUrl(tab.url) : null;
      return sameTarget(current, this.target) ? current : null;
    } catch { return null; }
  }

  async expectedTab(expectedTarget, phase) {
    if (!sameTarget(expectedTarget, this.target)) throw new PreSendError(`target_changed_before_${phase}`);
    let tab;
    try { tab = await this.parent.chrome.tabs.get(this.tabId); } catch { throw new PreSendError("managed_chat_tab_unavailable"); }
    const current = tab?.url ? normalizeTargetUrl(tab.url) : null;
    if (!sameTarget(current, this.target)) throw new PreSendError(`target_changed_before_${phase}`);
    return tab;
  }

  async insertPrompt(prompt, expectedTarget) {
    const tab = await this.expectedTab(expectedTarget, "insert");
    const result = await this.parent.sendMessage(tab.id, { type: "insert-prompt", prompt, expected_target: expectedTarget });
    if (!result?.ok) throw new PreSendError(result?.reason || "compose_unavailable");
  }

  async sendPrompt(expectedTarget, prompt) {
    const tab = await this.expectedTab(expectedTarget, "send");
    try {
      const result = await this.parent.chrome.tabs.sendMessage(tab.id, { type: "send-prompt", expected_target: expectedTarget, prompt });
      if (result?.ok) return { sent: true };
      if (result?.safe_no_send) return { sent: false, reason: result.reason || "send_control_unavailable" };
      throw new AmbiguousSendError(result?.reason || "send_outcome_unknown");
    } catch (error) {
      if (error instanceof AmbiguousSendError || error instanceof PreSendError) throw error;
      throw new AmbiguousSendError("send_outcome_unknown");
    }
  }
}

export class ChromeTargetAdapter {
  constructor(chromeApi) {
    this.chrome = chromeApi;
    this.sessionStorage = chromeApi.storage?.session || null;
    this.memoryTabId = null;
    this.managedTabIds = new Set();
  }

  async trackedTabId() {
    if (!this.sessionStorage) return this.memoryTabId;
    try {
      const value = (await this.sessionStorage.get(TRACKED_TAB_KEY))[TRACKED_TAB_KEY];
      return Number.isInteger(value) && value > 0 ? value : null;
    } catch {
      return this.memoryTabId;
    }
  }

  async rememberTab(tabId) {
    if (this.managedTabIds.has(tabId)) return;
    this.memoryTabId = tabId;
    if (!this.sessionStorage) return;
    try { await this.sessionStorage.set({ [TRACKED_TAB_KEY]: tabId }); } catch { /* in-memory tracking remains bounded to this service worker */ }
  }

  async forgetTab() {
    this.memoryTabId = null;
    if (!this.sessionStorage) return;
    try { await this.sessionStorage.remove(TRACKED_TAB_KEY); } catch { /* stale ID is always revalidated before use */ }
  }

  reserveManagedTab(tabId) {
    if (Number.isInteger(tabId) && tabId > 0) this.managedTabIds.add(tabId);
  }

  releaseManagedTab(tabId) { this.managedTabIds.delete(tabId); }

  isManagedTab(tabId) { return this.managedTabIds.has(tabId); }

  exactTab(tabId, target) {
    this.reserveManagedTab(tabId);
    return new ExactTabAdapter(this, tabId, target);
  }

  async observeActivatedTab(tabId) {
    if (!Number.isInteger(tabId) || tabId <= 0 || this.managedTabIds.has(tabId)) return null;
    let tab;
    try { tab = await this.chrome.tabs.get(tabId); } catch { return null; }
    const target = tab?.url ? normalizeTargetUrl(tab.url) : null;
    if (!tab?.id || !target) return null;
    await this.rememberTab(tab.id);
    return target;
  }

  async activeSupportedTab() {
    const tabs = await this.chrome.tabs.query({ active: true, lastFocusedWindow: true });
    const tab = tabs[0];
    if (!tab?.id || !tab.url || this.managedTabIds.has(tab.id)) return null;
    const target = normalizeTargetUrl(tab.url);
    if (!target) return null;
    await this.rememberTab(tab.id);
    return { tab, target };
  }

  async rememberedSupportedTab() {
    const tabId = await this.trackedTabId();
    if (!tabId || this.managedTabIds.has(tabId)) return null;
    let tab;
    try { tab = await this.chrome.tabs.get(tabId); } catch {
      await this.forgetTab();
      return null;
    }
    const target = tab?.url ? normalizeTargetUrl(tab.url) : null;
    if (!tab?.id || !target) {
      await this.forgetTab();
      return null;
    }
    return { tab, target };
  }

  async currentTab() {
    // The primary manual target remains distinct from managed role chats. Managed
    // chats are addressed by their persisted exact tab IDs and never replace it.
    const active = await this.activeSupportedTab();
    return active || await this.rememberedSupportedTab();
  }

  async currentTarget() {
    try { return (await this.currentTab())?.target ?? null; } catch { return null; }
  }

  async expectedTab(expectedTarget, phase) {
    const current = await this.currentTab();
    if (!current) throw new PreSendError("active_target_unavailable");
    if (!sameTarget(current.target, expectedTarget)) throw new PreSendError(`target_changed_before_${phase}`);
    return current.tab;
  }

  async sendMessage(tabId, message) {
    try { return await this.chrome.tabs.sendMessage(tabId, message); }
    catch { throw new PreSendError("chatgpt_surface_unavailable"); }
  }

  async insertPrompt(prompt, expectedTarget) {
    const tab = await this.expectedTab(expectedTarget, "insert");
    const result = await this.sendMessage(tab.id, { type: "insert-prompt", prompt, expected_target: expectedTarget });
    if (!result?.ok) throw new PreSendError(result?.reason || "compose_unavailable");
  }

  async sendPrompt(expectedTarget, prompt) {
    const tab = await this.expectedTab(expectedTarget, "send");
    try {
      const result = await this.chrome.tabs.sendMessage(tab.id, { type: "send-prompt", expected_target: expectedTarget, prompt });
      if (result?.ok) return { sent: true };
      if (result?.safe_no_send) return { sent: false, reason: result.reason || "send_control_unavailable" };
      throw new AmbiguousSendError(result?.reason || "send_outcome_unknown");
    } catch (error) {
      if (error instanceof AmbiguousSendError || error instanceof PreSendError) throw error;
      throw new AmbiguousSendError("send_outcome_unknown");
    }
  }

  async createConversation(prompt) {
    if (typeof prompt !== "string" || !prompt.trim()) throw new PreSendError("bootstrap_prompt_invalid");
    if (!this.chrome.tabs?.create) throw new PreSendError("chat_creation_unavailable");
    const tab = await this.chrome.tabs.create({ url: NEW_CHAT_URL, active: true });
    if (!tab?.id) throw new PreSendError("chat_creation_tab_unavailable");
    this.reserveManagedTab(tab.id);

    const deadline = Date.now() + CHAT_CREATE_TIMEOUT_MS;
    let bootstrapResult = null;
    while (Date.now() < deadline) {
      try {
        bootstrapResult = await this.chrome.tabs.sendMessage(tab.id, { type: "bootstrap-new-chat", prompt });
        if (bootstrapResult?.ok) break;
        if (bootstrapResult?.reason && bootstrapResult.reason !== "compose_unavailable") {
          throw new PreSendError(bootstrapResult.reason);
        }
      } catch (error) {
        if (error instanceof PreSendError) throw error;
      }
      await delay(CHAT_CREATE_POLL_MS);
    }
    if (!bootstrapResult?.ok) throw new PreSendError(bootstrapResult?.reason || "chat_bootstrap_timeout");

    while (Date.now() < deadline) {
      let current;
      try { current = await this.chrome.tabs.get(tab.id); } catch { throw new PreSendError("chat_creation_tab_closed"); }
      const target = current?.url ? normalizeTargetUrl(current.url) : null;
      if (target) return { target, tabId: tab.id };
      await delay(CHAT_CREATE_POLL_MS);
    }
    throw new AmbiguousSendError("chat_conversation_url_unobserved");
  }
}
