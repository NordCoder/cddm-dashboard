const TARGET_KIND = "chatgpt_conversation";
const CHATGPT_ORIGIN = "https://chatgpt.com";
const SEND_ACK_TIMEOUT_MS = 1500;
const SEND_ACK_INTERVAL_MS = 50;
const COMPOSER_WAIT_TIMEOUT_MS = 20_000;

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
  return match?.[1] ? { kind: TARGET_KIND, origin: CHATGPT_ORIGIN, path: `/c/${match[1]}` } : null;
}
function normalizeTargetRef(value) {
  return value?.kind === TARGET_KIND && value.origin === CHATGPT_ORIGIN ? normalizeTargetUrl(`${value.origin}${value.path}`) : null;
}
function sameTarget(left, right) {
  return Boolean(left && right && left.kind === right.kind && left.origin === right.origin && left.path === right.path);
}
function normalizeProjectUrl(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return "";
  const parsed = cleanChatGPTURL(raw);
  if (!parsed) return null;
  const pathname = parsed.pathname.replace(/\/+$/, "");
  if (!pathname || pathname === "/" || pathname.length > 500 || /(?:^|\/)c\/[^/]+$/.test(pathname)) return null;
  return `${CHATGPT_ORIGIN}${pathname}`;
}
function projectScope(value) {
  const normalized = normalizeProjectUrl(value);
  if (!normalized) return "";
  const segments = new URL(normalized).pathname.split("/").filter(Boolean);
  if (segments.at(-1) === "project") segments.pop();
  return segments.length > 0 ? `/${segments.join("/")}` : "";
}
function bootstrapSurface(expectedProjectUrl) {
  try {
    const parsed = new URL(location.href);
    if (parsed.origin !== CHATGPT_ORIGIN || parsed.search || parsed.hash) return false;
    if (!expectedProjectUrl) return parsed.pathname === "/" || parsed.pathname === "";
    const expectedScope = projectScope(expectedProjectUrl);
    return Boolean(expectedScope && projectScope(location.href) === expectedScope && !normalizeTargetUrl(location.href));
  } catch { return false; }
}

const COMPOSER_SELECTORS = [
  "#prompt-textarea",
  "form textarea[data-testid='textbox']",
  "form textarea[placeholder*='Message']",
  "form [contenteditable='true'][role='textbox']"
];
const SEND_SELECTORS = [
  "button[data-testid='send-button']",
  "form button[aria-label='Send prompt']",
  "form button[aria-label='Send message']"
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
function writeComposer(element, prompt) {
  return element instanceof HTMLTextAreaElement || element instanceof HTMLInputElement
    ? writePlainInput(element, prompt)
    : element.isContentEditable && writeContentEditable(element, prompt);
}
function insert(prompt, expected) {
  if (!currentTarget(expected)) return error("target_changed_before_insert");
  if (typeof prompt !== "string") return error("prompt_invalid");
  const composer = uniqueUsable(COMPOSER_SELECTORS);
  if (!composer.element || composer.ambiguous || composer.element.disabled || composer.element.readOnly) return error("compose_unavailable");
  if (!writeComposer(composer.element, prompt)) return error("prompt_insert_verification_failed");
  return { ok: true };
}
function delay(milliseconds) { return new Promise((resolve) => setTimeout(resolve, milliseconds)); }
async function waitForComposer() {
  const deadline = Date.now() + COMPOSER_WAIT_TIMEOUT_MS;
  while (Date.now() < deadline) {
    const composer = uniqueUsable(COMPOSER_SELECTORS);
    if (composer.ambiguous) return { ambiguous: true };
    if (composer.element && !composer.element.disabled && !composer.element.readOnly) return composer;
    await delay(SEND_ACK_INTERVAL_MS);
  }
  return {};
}
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
async function submitBootstrapAcknowledged() {
  const deadline = Date.now() + SEND_ACK_TIMEOUT_MS;
  do {
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
async function bootstrap(prompt, projectUrl) {
  if (!bootstrapSurface(projectUrl)) return error(projectUrl ? "bootstrap_project_surface_invalid" : "bootstrap_surface_invalid");
  if (typeof prompt !== "string" || !prompt.trim()) return error("bootstrap_prompt_invalid");
  const composer = await waitForComposer();
  if (!composer.element || composer.ambiguous || composer.element.disabled || composer.element.readOnly) return error("compose_unavailable");
  if (composerValue(composer.element).trim() !== "") return error("bootstrap_composer_not_empty");
  if (!writeComposer(composer.element, prompt)) return error("prompt_insert_verification_failed");
  const control = uniqueUsable(SEND_SELECTORS);
  if (!control.element || control.ambiguous || control.element.disabled || control.element.getAttribute("aria-disabled") === "true") return error("send_control_unavailable");
  try { control.element.click(); } catch { return error("send_outcome_unknown", false); }
  return await submitBootstrapAcknowledged() ? { ok: true } : error("send_outcome_unknown", false);
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
  if (message?.type === "bootstrap-new-chat") {
    bootstrap(message.prompt, message.project_url).then(respond, () => respond(error("send_outcome_unknown", false)));
    return true;
  }
  return false;
});
