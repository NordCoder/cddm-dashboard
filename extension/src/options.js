import { BACKEND_ORIGIN_KEY, backendPermissionOrigin, normalizeBackendOrigin } from "./protocol.js";

export class BackendOriginSaveError extends Error {
  constructor(code) {
    super(code);
    this.name = "BackendOriginSaveError";
    this.code = code;
  }
}

async function bestEffortRemove(permissions, origin) {
  try { await permissions.remove({ origins: [backendPermissionOrigin(origin)] }); } catch { /* fail-closed caller handles storage state */ }
}

export async function replaceBackendOrigin({ storage, permissions }, value) {
  const origin = normalizeBackendOrigin(value);
  const stored = (await storage.get(BACKEND_ORIGIN_KEY))[BACKEND_ORIGIN_KEY];
  let previous = null;
  if (stored) {
    try { previous = normalizeBackendOrigin(stored); } catch { throw new BackendOriginSaveError("stored_backend_origin_invalid"); }
  }

  const nextPermission = backendPermissionOrigin(origin);
  const alreadyGranted = await permissions.contains({ origins: [nextPermission] });
  if (!alreadyGranted) {
    const granted = await permissions.request({ origins: [nextPermission] });
    if (!granted) throw new BackendOriginSaveError("backend_permission_denied");
  }

  if (previous === origin) {
    await storage.set({ [BACKEND_ORIGIN_KEY]: origin });
    return origin;
  }

  try {
    await storage.set({ [BACKEND_ORIGIN_KEY]: origin });
  } catch (error) {
    if (!alreadyGranted) await bestEffortRemove(permissions, origin);
    throw new BackendOriginSaveError("backend_origin_storage_failed");
  }

  if (previous) {
    let removed = false;
    try { removed = await permissions.remove({ origins: [backendPermissionOrigin(previous)] }); } catch { removed = false; }
    if (!removed) {
      let rolledBack = false;
      try {
        await storage.set({ [BACKEND_ORIGIN_KEY]: previous });
        rolledBack = true;
      } catch { /* removing the new permission below still disables unsafe execution */ }
      if (!alreadyGranted) await bestEffortRemove(permissions, origin);
      throw new BackendOriginSaveError(rolledBack ? "previous_backend_permission_not_revoked" : "backend_origin_rollback_failed");
    }
  }

  return origin;
}

function userMessage(error) {
  if (error?.message === "backend_origin_must_be_origin") return "Enter an origin without a path, query, or credentials.";
  if (error?.message === "backend_origin_invalid" || error?.message === "backend_origin_required") return "Enter a valid HTTP(S) backend origin.";
  if (error?.code === "backend_permission_denied") return "Backend access was not granted; delivery remains disabled.";
  if (error?.code === "previous_backend_permission_not_revoked") return "Could not revoke the previous backend permission. The previous backend remains configured.";
  if (error?.code === "backend_origin_rollback_failed") return "Backend permission update failed safely. Reopen this page and configure the backend again.";
  if (error?.code === "stored_backend_origin_invalid") return "Stored backend configuration is invalid. Clear extension storage before enabling delivery.";
  return "Could not save the backend origin.";
}

export async function installOptionsPage(documentRef = globalThis.document, chromeApi = globalThis.chrome) {
  if (!documentRef || !chromeApi?.storage?.local || !chromeApi?.permissions) return;
  const input = documentRef.querySelector("#backend-origin");
  const button = documentRef.querySelector("#save");
  const status = documentRef.querySelector("#status");
  if (!input || !button || !status) return;

  const existing = (await chromeApi.storage.local.get(BACKEND_ORIGIN_KEY))[BACKEND_ORIGIN_KEY];
  if (existing) input.value = existing;

  button.addEventListener("click", async () => {
    try {
      const origin = await replaceBackendOrigin({ storage: chromeApi.storage.local, permissions: chromeApi.permissions }, input.value);
      input.value = origin;
      status.textContent = "Saved. Delivery is enabled only for this backend origin.";
    } catch (error) {
      status.textContent = userMessage(error);
    }
  });
}

if (globalThis.document && globalThis.chrome) void installOptionsPage();
