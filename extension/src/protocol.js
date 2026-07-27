export const TARGET_KIND = "chatgpt_conversation";
export const CHATGPT_ORIGIN = "https://chatgpt.com";
export const WORKER_ID_KEY = "worker_id";
export const BACKEND_ORIGIN_KEY = "backend_origin";

export function randomId() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
  const bytes = new Uint8Array(16);
  globalThis.crypto?.getRandomValues?.(bytes);
  if (!bytes.some(Boolean)) {
    for (let index = 0; index < bytes.length; index += 1) bytes[index] = Math.floor(Math.random() * 256);
  }
  return [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
}

export function normalizeBackendOrigin(value) {
  const raw = String(value ?? "").trim();
  if (!raw) throw new Error("backend_origin_required");
  let parsed;
  try { parsed = new URL(raw); } catch { throw new Error("backend_origin_invalid"); }
  if (!/^https?:$/.test(parsed.protocol) || parsed.username || parsed.password || parsed.search || parsed.hash) {
    throw new Error("backend_origin_invalid");
  }
  if (parsed.pathname !== "" && parsed.pathname !== "/") throw new Error("backend_origin_must_be_origin");
  return parsed.origin;
}

export function backendPermissionOrigin(origin) {
  return `${normalizeBackendOrigin(origin)}/*`;
}

export function normalizeTargetUrl(value) {
  let parsed;
  try { parsed = new URL(String(value ?? "")); } catch { return null; }
  if (parsed.origin !== CHATGPT_ORIGIN || parsed.search || parsed.hash) return null;
  const match = parsed.pathname.match(/^\/c\/([^/]+)$/);
  if (!match || !match[1] || parsed.pathname !== `/c/${match[1]}`) return null;
  return { kind: TARGET_KIND, origin: CHATGPT_ORIGIN, path: parsed.pathname };
}

export function normalizeTargetRef(value) {
  if (!value || value.kind !== TARGET_KIND || value.origin !== CHATGPT_ORIGIN) return null;
  return normalizeTargetUrl(`${value.origin}${value.path}`);
}

export function targetRefFromCommand(command) {
  if (!command?.target_ref) return null;
  const target = normalizeTargetUrl(command.target_ref);
  if (!target || command.target_kind !== target.kind) return null;
  return target;
}

export function sameTarget(left, right) {
  return Boolean(left && right && left.kind === right.kind && left.origin === right.origin && left.path === right.path);
}

export function safeDiagnostic(code) {
  return String(code ?? "unknown").replace(/[^a-z0-9_.-]/gi, "_").slice(0, 80) || "unknown";
}
