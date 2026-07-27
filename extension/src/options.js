import { BACKEND_ORIGIN_KEY, backendPermissionOrigin, normalizeBackendOrigin } from "./protocol.js";

const input = document.querySelector("#backend-origin");
const button = document.querySelector("#save");
const status = document.querySelector("#status");

function show(message) { status.textContent = message; }

const existing = (await chrome.storage.local.get(BACKEND_ORIGIN_KEY))[BACKEND_ORIGIN_KEY];
if (existing) input.value = existing;

button.addEventListener("click", async () => {
  try {
    const origin = normalizeBackendOrigin(input.value);
    const granted = await chrome.permissions.request({ origins: [backendPermissionOrigin(origin)] });
    if (!granted) { show("Backend access was not granted; delivery remains disabled."); return; }
    await chrome.storage.local.set({ [BACKEND_ORIGIN_KEY]: origin });
    show("Saved. Delivery is enabled only for this backend origin.");
  } catch (error) { show(error.message === "backend_origin_must_be_origin" ? "Enter an origin without a path, query, or credentials." : "Enter a valid HTTP(S) backend origin."); }
});
