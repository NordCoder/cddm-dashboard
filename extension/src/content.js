const TARGET_KIND = "chatgpt_conversation";
const CHATGPT_ORIGIN = "https://chatgpt.com";

function normalizeTargetUrl(value) {
  let parsed;
  try { parsed = new URL(String(value ?? "")); } catch { return null; }
  if (parsed.origin !== CHATGPT_ORIGIN || parsed.search || parsed.hash) return null;
  const match = parsed.pathname.match(/^\/c\/([^/]+)$/);
  return match && parsed.pathname === `/c/${match[1]}` ? { kind: TARGET_KIND, origin: CHATGPT_ORIGIN, path: parsed.pathname } : null;
}
function normalizeTargetRef(value) {
  return value?.kind === TARGET_KIND && value.origin === CHATGPT_ORIGIN ? normalizeTargetUrl(`${value.origin}${value.path}`) : null;
}
function sameTarget(left, right) {
  return Boolean(left && right && left.kind === right.kind && left.origin === right.origin && left.path === right.path);
}

const COMPOSER_SELECTORS = [
  "textarea[data-testid='textbox']",
  "textarea[placeholder*='Message']",
  "[contenteditable='true'][role='textbox']",
  "div[contenteditable='true']"
];
const SEND_SELECTORS = [
  "button[data-testid='send-button']",
  "button[aria-label='Send prompt']",
  "button[aria-label='Send message']",
  "form button[type='submit']"
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

function insert(prompt, expected) {
  if (!currentTarget(expected)) return error("target_changed_before_insert");
  if (typeof prompt !== "string") return error("prompt_invalid");
  const composer = uniqueUsable(COMPOSER_SELECTORS);
  if (!composer.element || composer.ambiguous || composer.element.disabled || composer.element.readOnly) return error("compose_unavailable");
  if (composer.element instanceof HTMLTextAreaElement || composer.element instanceof HTMLInputElement) {
    const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
    if (setter) setter.call(composer.element, prompt); else composer.element.value = prompt;
  } else {
    composer.element.replaceChildren(document.createTextNode(prompt));
  }
  composer.element.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText", data: null }));
  return { ok: true };
}

function composerValue(element) {
  return element instanceof HTMLTextAreaElement || element instanceof HTMLInputElement ? element.value : element.textContent;
}

function send(expected, prompt) {
  if (!currentTarget(expected)) return error("target_changed_before_send");
  const composer = uniqueUsable(COMPOSER_SELECTORS);
  if (!composer.element || composer.ambiguous || composer.element.disabled || composer.element.readOnly) return error("compose_unavailable");
  if (typeof prompt !== "string" || composerValue(composer.element) !== prompt) return error("prompt_changed_before_send");
  const control = uniqueUsable(SEND_SELECTORS);
  if (!control.element || control.ambiguous || control.element.disabled || control.element.getAttribute("aria-disabled") === "true") return error("send_control_unavailable");
  try {
    control.element.click();
    return { ok: true };
  } catch {
    return error("send_outcome_unknown", false);
  }
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
    respond(send(message.expected_target, message.prompt));
    return false;
  }
  return false;
});
