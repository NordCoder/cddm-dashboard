import { PreSendError, AmbiguousSendError } from "./executor.js";
import { normalizeTargetUrl, sameTarget } from "./protocol.js";

const TRACKED_TAB_KEY = "tracked_chatgpt_tab_id";

export class ChromeTargetAdapter {
  constructor(chromeApi) {
    this.chrome = chromeApi;
    this.sessionStorage = chromeApi.storage?.session || null;
    this.memoryTabId = null;
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
    this.memoryTabId = tabId;
    if (!this.sessionStorage) return;
    try { await this.sessionStorage.set({ [TRACKED_TAB_KEY]: tabId }); } catch { /* in-memory tracking remains bounded to this service worker */ }
  }

  async forgetTab() {
    this.memoryTabId = null;
    if (!this.sessionStorage) return;
    try { await this.sessionStorage.remove(TRACKED_TAB_KEY); } catch { /* stale ID is always revalidated before use */ }
  }

  async observeActivatedTab(tabId) {
    if (!Number.isInteger(tabId) || tabId <= 0) return null;
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
    if (!tab?.id || !tab.url) return null;
    const target = normalizeTargetUrl(tab.url);
    if (!target) return null;
    await this.rememberTab(tab.id);
    return { tab, target };
  }

  async rememberedSupportedTab() {
    const tabId = await this.trackedTabId();
    if (!tabId) return null;
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
    // A supported ChatGPT tab becomes the tracked target only when the user has
    // explicitly made it active. Once tracked, switching to the dashboard does
    // not erase it; we revalidate that exact tab ID and URL instead of scanning
    // other ChatGPT conversations.
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
}
