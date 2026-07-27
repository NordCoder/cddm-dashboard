# Confirmed browser delivery

CDDM Dashboard can deliver one current backend-approved Prompt Plan to one explicitly bound ChatGPT conversation through the bundled Chrome Manifest V3 extension. The browser path never reads ChatGPT responses.

## Network boundary

The dashboard and browser-delivery APIs do not provide a public authentication layer. Docker Compose therefore publishes both host ports on `127.0.0.1` by default, and the backend rejects non-loopback HTTP Host authorities for both reads and mutations. This blocks direct LAN exposure and DNS-rebinding access to local Project, Prompt Plan, binding, and delivery data.

`BIND_HOST` changes only socket publication; it does not relax the backend Host guard. This build is intended to be opened as `localhost`, `127.0.0.1`, or `::1`. Do not expose the API or dashboard directly to an untrusted LAN or the public internet.

State-changing browser requests must also be same-origin, and JSON bodies must use `application/json`, preventing simple cross-site `text/plain` mutation requests. `chrome-extension://` access is accepted only from the stable bundled extension ID `biakfbpkfdpniphmoafgldedkbnjfibp` and only on `/api/browser/`.

## Enable the backend

Browser delivery is deliberately opt-in. In `.env` set:

```env
BROWSER_DELIVERY_ENABLED=true
BROWSER_BINDING_TTL=30s
BROWSER_DELIVERY_PENDING_TTL=5m
BROWSER_DELIVERY_CLAIM_TTL=1m
```

Then start or restart the application:

```bash
docker compose up --build
```

The normal Compose web endpoint is `http://localhost:3000`; it proxies `/api` to the backend. Direct backend access remains available at `http://localhost:8080`.

## Load the extension

1. Open `chrome://extensions`.
2. Enable **Developer mode**.
3. Select **Load unpacked** and choose the repository `extension/` directory.
4. Confirm that the displayed extension ID is `biakfbpkfdpniphmoafgldedkbnjfibp`.
5. Open the extension **Options** page.
6. Set the backend origin to either the proxied app origin (`http://localhost:3000`) or direct API origin (`http://localhost:8080`). Grant only the requested origin permission.

The manifest public key fixes the unpacked extension identity; it is not a credential. Changing the configured backend revokes the previously granted backend origin. Each durable claim is also bound to the exact normalized backend origin that issued it; an unfinished claim from an old origin is never acknowledged to a replacement backend.

## Bind a conversation

1. Open the intended `https://chatgpt.com/c/...` conversation and make that tab active once.
2. Open the relevant CDDM work unit or latest Prompt Plan in the dashboard.
3. The **Browser Delivery** panel lists only live browser targets projected by the backend. It does not accept a free-text ChatGPT URL.
4. Select the target and choose **Bind selected target** (or **Rebind selected target**).

After a ChatGPT tab has been explicitly activated, the extension remembers that exact tab for the current browser session. Switching to the dashboard does not lose the target: the extension revalidates the same tab ID and URL. Closing the tab, navigating it away from the bound conversation, browser restart, conflicting extension sessions, or presence timeout makes the binding unavailable until freshness is proved again.

## Confirm one delivery

Browser delivery is enabled only when the backend still projects a dispatchable current plan and the binding is `ready`.

1. Choose **Review delivery**.
2. Verify the plan, exact Head, lane, binding version, ChatGPT target and exact immutable backend prompt.
3. Choose **Confirm and send**.

One confirmation intent receives one idempotency key. Network failure, unreadable or malformed success data, or HTTP `5xx` keeps that same frozen intent because the backend may already have committed it. A definitive stale/conflicting response cancels the intent and requires another review. `uncertain` delivery outcomes are never automatically retried.

Local edits in the Stage 5 prompt textarea are intentionally not sent by browser delivery; use **Manual Copy** for edited prompt text or reset to the backend-generated prompt first.

## Safety behavior

- the backend remains authoritative for current plan, Head, lane, binding and command lifecycle;
- the dashboard confirmation request carries only immutable/CAS identities, never replacement prompt or target authority;
- the extension validates opaque command/claim/session identities and verifies the claimed prompt against the backend SHA-256 hash before touching the DOM;
- the extension durably records a backend-origin-bound claim before DOM send and serializes ledger updates to prevent lost terminal/acknowledgement state;
- an in-progress reserved claim has one executor owner and cannot be completed or sent by a concurrent duplicate;
- target identity and backend configuration are checked before insertion and again before the consequential send;
- DOM matching is limited to identified ChatGPT composer/send controls; broad generic contenteditable/submit fallbacks are not used;
- a Send click is `delivered` only after the composer clears as bounded submit acknowledgement; an unchanged or unrelatedly edited composer is `uncertain`;
- completion `409 Conflict` and other definitive client-side rejection statuses become bounded terminal diagnostics locally and do not cause a DOM resend;
- ordinary completion transport failures may retry only the acknowledgement for the same origin, never the DOM send;
- backend requests are time-bounded so a suspended or unavailable endpoint cannot block the service worker indefinitely;
- mutable presence timestamps are copied under lock before operator projection;
- the extension does not read, scrape, classify or persist ChatGPT response content;
- Manual Copy remains available independently of browser availability.
