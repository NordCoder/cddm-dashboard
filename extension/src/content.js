const TARGET_KIND = "chatgpt_conversation";
const CHATGPT_ORIGIN = "https://chatgpt.com";
const SEND_ACK_TIMEOUT_MS = 1500;
const SEND_ACK_INTERVAL_MS = 50;

function cleanChatGPTURL(value) {
  let parsed;
  try { parsed = new URL(String(value ?? "")); } catch { return null; }
  if (parsed.origin !== CHATGPT_ORIGIN || parsed.username || parsed.password || parsed.search || parsed.hash) return null;
  return parsed;
}

function normalizeTargetUrl(value) {
  const parsed = cleanChatGPTURL(value);
  if (!parsed) return null;
  const match = parsed.pathname.match(/(?:^|\/)c\/([^/]+)$/);
  if (!match || !match[1]) return null;
  return { kind: TARGET_KIND, origin: CHATGPT_ORIGIN, path: `/c/${match[1]}` };
}

function normalizeTargetRef(value) {
  return value?.kind === TARGET_KIND && value.origin === CHATGPT_ORIGIN ? normalizeTargetUrl(`${value.origin}${value.path}`) : null;
}

function sameTarget(left, right) {
  return Boolean(left && right && left.kind === right.kind && left.origin === right.origin && left.path === right.path);
}

function normalizeProjectURL(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return "";
  const parsed = cleanChatGPTURL(raw);
  if (!parsed) return "";
  const pathname = parsed.pathname.replace(/\/+$/, "");
  if (!pathname || pathname === "/") return "";
  return `${CHATGPT_ORIGIN}${pathname}`;
}

function projectScope(value) {
  const normalized = normalizeProjectURL(value);
  if (!normalized) return "";
  const segments = new URL(normalized).pathname.split("/").filter(Boolean);
  if (segments.at(-1) === "project") segments.pop();
  return `/${segments.join("/")}`;
}

function conversationBelongsToProject(value, projectURL) {
  if (!projectURL) return /^https:\/\/chatgpt\.com\/c\/[^/?#]+$/.test(String(value ?? ""));
  const parsed = cleanChatGPTURL(value);
  if (!parsed) return false;
  const match = parsed.pathname.match(/^(.*)\/c\/([^/]+)$/);
  return Boolean(match && match[1] === projectScope(projectURL) && match[2]);
}

function creationSurfaceMatches(value, projectURL = "") {
  const parsed = cleanChatGPTURL(value);
  if (!parsed) return false;
  if (!projectURL) return parsed.pathname === "/" || conversationBelongsToProject(value, "");
  const normalized = normalizeProjectURL(projectURL);
  return Boolean(normalized) && (`${parsed.origin}${parsed.pathname.replace(/\/+$/, "")}` === normalized || conversationBelongsToProject(value, normalized));
}

const COMPOSER_SELECTORS = [
  "#prompt-textarea",
  "form textarea[data-testid='textbox']",
  "form textarea[placeholder*='Message']",
  "form [contenteditable='true'][role='textbox']",
];
const SEND_SELECTORS = [
  "button[data-testid='send-button']",
  "form button[aria-label='Send prompt']",
  "form button[aria-label='Send message']",
];
const LIBRARY_OPTION_SELECTORS = [
  "[data-testid='mention-option']",
  "[data-testid='file-mention-item']",
  "[role='listbox'] [role='option']",
];
const ATTACHMENT_CHIP_SELECTORS = [
  "form [data-testid='attachment-chip']",
  "form [data-testid='file-chip']",
  "form [data-testid='composer-file-chip']",
];

function visible(element) {
  const style = getComputedStyle(element);
  const rectangle = element.getBoundingClientRect();
  return style.display !== "none" && style.visibility !== "hidden" && rectangle.width > 0 && rectangle.height > 0;
}

function uniqueUsable(selectors) {
  for (const selector of selectors) {
    const found = [...document.querySelectorAll(selector)].filter(visible);
    if (found.length > 1) return { ambiguous: true };
    if (found.length === 1) return { element: found[0] };
  }
  return {};
}

function allVisible(selectors) {
  for (const selector of selectors) {
    const found = [...document.querySelectorAll(selector)].filter(visible);
    if (found.length > 0) return found;
  }
  return [];
}

function currentTarget(expected) {
  const target = normalizeTargetUrl(location.href);
  return target && (!expected || sameTarget(target, normalizeTargetRef(expected))) ? target : null;
}

function error(reason, safeNoSend = true) { return { ok: false, reason, safe_no_send: safeNoSend }; }

function composerValue(element) {
  if (element instanceof HTMLTextAreaElement || element instanceof HTMLInputElement) return element.value;
  return element.innerText ?? element.textContent ?? "";
}

function writePlainInput(element, prompt) {
  const prototype = element instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(prototype, "value")?.set;
  element.focus();
  if (setter) setter.call(element, prompt); else element.value = prompt;
  element.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText", data: null }));
  return composerValue(element) === prompt;
}

function writeContentEditable(element, prompt) {
  element.focus();
  const selection = globalThis.getSelection?.();
  if (!selection) return false;
  const range = document.createRange();
  range.selectNodeContents(element);
  selection.removeAllRanges();
  selection.addRange(range);
  let inserted = false;
  try { inserted = document.execCommand("insertText", false, prompt); } catch { inserted = false; }
  selection.removeAllRanges();
  return inserted && composerValue(element) === prompt;
}

function writeComposer(prompt) {
  const composer = uniqueUsable(COMPOSER_SELECTORS);
  if (!composer.element || composer.ambiguous || composer.element.disabled || composer.element.readOnly) return false;
  return composer.element instanceof HTMLTextAreaElement || composer.element instanceof HTMLInputElement
    ? writePlainInput(composer.element, prompt)
    : composer.element.isContentEditable && writeContentEditable(composer.element, prompt);
}

function insert(prompt, expected) {
  if (!currentTarget(expected)) return error("target_changed_before_insert");
  if (typeof prompt !== "string") return error("prompt_invalid");
  if (!writeComposer(prompt)) return error("prompt_insert_verification_failed");
  return { ok: true };
}

function delay(milliseconds) { return new Promise((resolve) => setTimeout(resolve, milliseconds)); }

async function submitAcknowledged(expected) {
  const deadline = Date.now() + SEND_ACK_TIMEOUT_MS;
  do {
    if (!currentTarget(expected)) return false;
    const composer = uniqueUsable(COMPOSER_SELECTORS);
    if (composer.ambiguous) return false;
    if (composer.element && composerValue(composer.element) === "") return true;
    await delay(SEND_ACK_INTERVAL_MS);
  } while (Date.now() < deadline);
  return false;
}

async function send(expected, prompt) {
  if (!currentTarget(expected)) return error("target_changed_before_send");
  const composer = uniqueUsable(COMPOSER_SELECTORS);
  if (!composer.element || composer.ambiguous || composer.element.disabled || composer.element.readOnly) return error("compose_unavailable");
  if (typeof prompt !== "string" || composerValue(composer.element) !== prompt) return error("prompt_changed_before_send");
  const control = uniqueUsable(SEND_SELECTORS);
  if (!control.element || control.ambiguous || control.element.disabled || control.element.getAttribute("aria-disabled") === "true") return error("send_control_unavailable");
  try { control.element.click(); } catch { return error("send_outcome_unknown", false); }
  return await submitAcknowledged(expected) ? { ok: true } : error("send_outcome_unknown", false);
}

function exactNodeName(node) {
  const explicit = node?.getAttribute?.("data-filename") || node?.querySelector?.("[data-filename]")?.getAttribute?.("data-filename");
  if (explicit) return explicit;
  const named = node?.querySelector?.("[data-testid='file-name']")?.textContent;
  return named || node?.textContent || "";
}

async function bootstrapAcknowledged(projectURL) {
  const deadline = Date.now() + SEND_ACK_TIMEOUT_MS;
  do {
    if (!creationSurfaceMatches(location.href, projectURL)) return false;
    const target = normalizeTargetUrl(location.href);
    if (target && conversationBelongsToProject(location.href, projectURL)) return true;
    const composer = uniqueUsable(COMPOSER_SELECTORS);
    if (composer.ambiguous) return false;
    if (composer.element && composerValue(composer.element) === "") return true;
    await delay(SEND_ACK_INTERVAL_MS);
  } while (Date.now() < deadline);
  return false;
}

async function bootstrapSession(message) {
  const projectURL = normalizeProjectURL(message.chatgpt_project_url || "");
  if ((message.chatgpt_project_url || "") && !projectURL) return error("chatgpt_project_url_invalid");
  if (!creationSurfaceMatches(location.href, projectURL)) return error("bootstrap_creation_scope_mismatch");
  const prompt = typeof message.bootstrap_text === "string" ? message.bootstrap_text.trim() : "";
  if (!prompt || prompt.length > 4000) return error("bootstrap_prompt_invalid");
  const resolver = globalThis.CDDMLibraryResolver;
  if (!resolver?.resolveExactAttachments) return error("library_resolver_unavailable");
  let evidence;
  try {
    evidence = await resolver.resolveExactAttachments(message.attachments, {
      setQuery: async (query) => {
        if (!creationSurfaceMatches(location.href, projectURL)) throw new resolver.LibraryResolutionError("bootstrap_creation_scope_mismatch");
        if (!writeComposer(query)) throw new resolver.LibraryResolutionError("compose_unavailable");
      },
      optionNodes: () => allVisible(LIBRARY_OPTION_SELECTORS),
      optionName: exactNodeName,
      clickOption: async (node) => { node.click(); },
      chipNodes: () => allVisible(ATTACHMENT_CHIP_SELECTORS),
      chipName: exactNodeName,
    });
  } catch (resolutionError) {
    return error(resolutionError?.message || "library_resolution_failed", resolutionError?.safeNoSend !== false);
  }
  if (!writeComposer(prompt)) return error("bootstrap_prompt_insert_verification_failed");
  const chips = allVisible(ATTACHMENT_CHIP_SELECTORS).map((node) => resolver.normalizeFilename(exactNodeName(node))).filter(Boolean);
  if (chips.length !== evidence.length || chips.some((value, index) => value !== evidence[index])) return error("attachment_chip_verification_failed");
  const control = uniqueUsable(SEND_SELECTORS);
  if (!control.element || control.ambiguous || control.element.disabled || control.element.getAttribute("aria-disabled") === "true") return error("send_control_unavailable");
  try { control.element.click(); } catch { return error("bootstrap_send_outcome_unknown", false); }
  if (!await bootstrapAcknowledged(projectURL)) return error("bootstrap_send_outcome_unknown", false);
  return { ok: true, attachment_evidence: evidence };
}

chrome.runtime.onMessage.addListener((message, _sender, respond) => {
  if (message?.type === "get-target") {
    respond({ target: normalizeTargetUrl(location.href) });
    return false;
  }
  if (message?.type === "insert-prompt") {
    respond(insert(message.prompt, message.expected_target));
    return false;
  }
  if (message?.type === "send-prompt") {
    send(message.expected_target, message.prompt).then(respond, () => respond(error("send_outcome_unknown", false)));
    return true;
  }
  if (message?.type === "bootstrap-session") {
    bootstrapSession(message).then(respond, () => respond(error("bootstrap_send_outcome_unknown", false)));
    return true;
  }
  return false;
});
