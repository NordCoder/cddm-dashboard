import { PreSendError, AmbiguousSendError } from "./executor.js";
import { normalizeTargetUrl, sameTarget } from "./protocol.js";

export class ChromeTargetAdapter {
  constructor(chromeApi) { this.chrome = chromeApi; }

  async activeTab() {
    const tabs = await this.chrome.tabs.query({ active: true, lastFocusedWindow: true });
    const tab = tabs[0];
    if (!tab?.id || !tab.url) throw new PreSendError("active_target_unavailable");
    const target = normalizeTargetUrl(tab.url);
    if (!target) throw new PreSendError("unsupported_current_target");
    return { tab, target };
  }

  async currentTarget() {
    try { return (await this.activeTab()).target; } catch { return null; }
  }

  async sendMessage(tabId, message) {
    try { return await this.chrome.tabs.sendMessage(tabId, message); }
    catch { throw new PreSendError("chatgpt_surface_unavailable"); }
  }

  async insertPrompt(prompt, expectedTarget) {
    const { tab, target } = await this.activeTab();
    if (!sameTarget(target, expectedTarget)) throw new PreSendError("target_changed_before_insert");
    const result = await this.sendMessage(tab.id, { type: "insert-prompt", prompt, expected_target: expectedTarget });
    if (!result?.ok) throw new PreSendError(result?.reason || "compose_unavailable");
  }

  async sendPrompt(expectedTarget, prompt) {
    const { tab, target } = await this.activeTab();
    if (!sameTarget(target, expectedTarget)) throw new PreSendError("target_changed_before_send");
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
